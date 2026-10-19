import { captureScript } from './capture.js';
import { dispatchControl } from './control.js';
import { CONTROL, STREAM, isRemoteControlType } from '@full-self-browsing/phantom-stream/protocol';
import { CDP } from './cdp.js';

export function pageURL(value) {
  if (!value || value.length > 8192) throw new Error('Укажите адрес страницы в ?url=https%3A%2F%2Fexample.org%2F');
  let url;
  try { url = new URL(value); } catch { throw new Error('Некорректный URL страницы'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
    throw new Error('Разрешены HTTP(S) адреса без логина и пароля');
  }
  return url.href;
}

const bootstrap = `
if (!window.__latticeControlInstalled) {
  window.__latticeControlInstalled = true;
  // Keep target=_blank links inside the controlled remote tab. This applies
  // only during viewer-dispatched clicks, leaving agent clicks untouched.
  document.addEventListener('click', function (event) {
    if (!window.__latticeRemoteClick) return;
    var link = event.target.closest && event.target.closest('a[href]');
    if (link && link.target && link.target !== '_self') {
      var target = link.getAttribute('target');
      link.setAttribute('target', '_self');
      setTimeout(function () { link.setAttribute('target', target); }, 0);
    }
  }, true);
}
if (!window.__latticePhantomQueue) {
  window.__latticePhantomQueue = { frames: [], bytes: 0, overflow: false, needsSnapshot: false };
  // Upstream assigns IDs to body descendants, not the body/html shells.
  // Shell changes need a fresh snapshot because they cannot carry diff IDs.
  new MutationObserver(function (records) {
    if (records.some(function (record) {
      return record.target === document.body || record.target === document.documentElement;
    })) window.__latticePhantomQueue.needsSnapshot = true;
  }).observe(document.documentElement, { childList: true, attributes: true, subtree: true });
}
if (window.__latticePhantomQueue.version !== 2) {
  // Existing agent tabs survive service upgrades. Stop the old capture and
  // clear its injection guard so they also get the form-state fix.
  window.__phantomStreamStop?.();
  window.__phantomStreamInjected = false;
  Object.assign(window.__latticePhantomQueue, { version: 2, frames: [], bytes: 0,
    overflow: false, needsSnapshot: false, identity: null,
    documentId: Array.from(crypto.getRandomValues(new Uint32Array(4)), function (n) { return n.toString(16); }).join('-') });
}
window.__phantomStreamBridge = function (message) {
    var queue = window.__latticePhantomQueue;
    if (message.type === 'ext:dom-snapshot') {
      message.payload.documentId = queue.documentId;
      // Update atomically with capture, before the host's next poll. A text
      // command can survive a resnapshot, but never a document navigation.
      queue.identity = { documentId: queue.documentId,
        streamSessionId: message.payload.streamSessionId, snapshotId: message.payload.snapshotId };
    }
    var raw = JSON.stringify({ type: message.type, payload: message.payload });
    if (queue.bytes + raw.length * 2 > 2 * 1024 * 1024) {
      queue.frames = []; queue.bytes = 0; queue.overflow = true;
    }
    if (!queue.overflow) { queue.frames.push(raw); queue.bytes += raw.length * 2; }
};
`;
export const captureSource = bootstrap + captureScript();
const drain = `(() => {
  // Target.createTarget may return before its first navigation commits.
  // Never replace the shared link with about:blank or inject its empty body.
  if (!['http:', 'https:'].includes(location.protocol) || !document.body || document.readyState === 'loading') {
    return { frames: [], url: null };
  }
  var queue = window.__latticePhantomQueue;
  if (!queue || !window.__phantomStreamInjected) return null;
  if (queue.overflow || queue.needsSnapshot) {
    queue.frames = []; queue.bytes = 0; queue.overflow = false; queue.needsSnapshot = false;
    window.__phantomStreamStart();
  }
  var frames = queue.frames; queue.frames = []; queue.bytes = 0;
  return { frames: frames, url: location.href };
})()`;

export class Streams {
  constructor({ endpoint, connect = CDP.connect, idleMs = 15000, maxStreams = 8 }) {
    Object.assign(this, { endpoint, connect, idleMs, maxStreams });
    this.streams = new Map();
    this.serial = Promise.resolve();
    this.closed = false;
  }

  join(url, socket) {
    // Serialize target discovery/creation so concurrent viewers share one capture.
    const work = this.serial.then(() => this.open(url, socket));
    this.serial = work.catch(() => {});
    return work;
  }

  async open(url, socket) {
    if (this.closed || socket.readyState !== 1) return;
    let cdp;
    let stream;
    try {
      cdp = await this.connect(this.endpoint);
      const { targetInfos } = await cdp.send('Target.getTargets');
      let target = targetInfos.find(target => target.type === 'page' && target.url === url);
      stream = target && this.streams.get(target.targetId);
      if (stream) {
        cdp.close();
      } else {
        if (this.streams.size >= this.maxStreams) throw new Error('Слишком много активных страниц');
        stream = { cdp, viewers: new Set(), owned: !target, stopped: false, failures: 0, bootstrap: true, startedAt: Date.now() };
        if (!target) target = await cdp.send('Target.createTarget', { url });
        stream.targetId = target.targetId;
        const { sessionId } = await cdp.send('Target.attachToTarget', { targetId: target.targetId, flatten: true });
        stream.sessionId = sessionId;
        await cdp.send('Page.enable', {}, sessionId);
        await cdp.send('Runtime.enable', {}, sessionId);
        this.streams.set(stream.targetId, stream);
      }
      if (this.closed || socket.readyState !== 1) {
        if (!stream.viewers.size) await this.stop(stream);
        return;
      }
      clearTimeout(stream.idle);
      stream.viewers.add(socket);
      socket.once('close', () => {
        stream.viewers.delete(socket);
        if (!stream.viewers.size) {
          stream.idle = setTimeout(() => this.stop(stream), this.idleMs);
        }
      });
      // Each new viewer needs a fresh snapshot, never a diff without its base.
      stream.resnapshot = true;
      socket.on('message', raw => {
        let message;
        try { message = JSON.parse(raw); } catch { return; }
        if (message.type === 'ping') {
          if (socket.readyState === 1) socket.send(JSON.stringify({ type: 'pong' }));
        } else if (message.type === CONTROL.START) {
          // Coalesce requests until the next poll; never drop a latched
          // renderer resync request, which would leave it stale forever.
          stream.resnapshot = true;
        }
        if (isRemoteControlType(message.type)) {
          // Serialize mouse/key sequences across all viewers. Bound the queue
          // so a stalled CDP cannot accumulate unlimited pending input.
          const result = ok => {
            if (socket.readyState === 1) socket.send(JSON.stringify({ type: 'lattice:control-result',
              payload: { commandId: message.payload?.commandId, ok } }));
          };
          if ((stream.pendingInput || 0) >= 64) { result(false); return; }
          stream.pendingInput = (stream.pendingInput || 0) + 1;
          stream.input = (stream.input || Promise.resolve()).then(() => {
            if (!stream.stopped && socket.readyState === 1) return dispatchControl(stream, message.type, message.payload);
            return false;
          }).then(ok => result(ok === true)).catch(() => {
            result(false);
          }).finally(() => { stream.pendingInput--; });
        }
      });
      if (!stream.polling) {
        stream.polling = true;
        this.poll(stream);
      }
    } catch (error) {
      if (stream && !stream.viewers.size) await this.stop(stream);
      else if (cdp) cdp.close();
      throw error;
    }
  }

  async poll(stream) {
    if (stream.stopped) return;
    try {
      if (stream.bootstrap) {
        await stream.cdp.evaluate(stream.sessionId, bootstrap);
        stream.bootstrap = false;
      }
      let batch = await stream.cdp.evaluate(stream.sessionId, drain);
      if (!batch) {
        await stream.cdp.evaluate(stream.sessionId, captureSource);
        stream.resnapshot = false;
        batch = await stream.cdp.evaluate(stream.sessionId, drain);
      } else if (stream.resnapshot) {
        stream.resnapshot = false;
        await stream.cdp.evaluate(stream.sessionId, 'window.__phantomStreamStart()');
        batch = await stream.cdp.evaluate(stream.sessionId, drain);
      }
      for (const raw of batch?.frames || []) {
        const frame = JSON.parse(raw);
        if (frame.type === STREAM.SNAPSHOT) {
          stream.identity = { documentId: frame.payload.documentId,
            streamSessionId: frame.payload.streamSessionId, snapshotId: frame.payload.snapshotId };
          stream.width = frame.payload.viewportWidth || frame.payload.pageWidth;
          stream.height = frame.payload.viewportHeight || frame.payload.pageHeight;
        }
      }
      if (batch?.url) stream.started = true;
      if (!stream.started && Date.now() - stream.startedAt > 30000) throw new Error('Page did not load');
      for (const socket of stream.viewers) {
        if (socket.readyState !== 1) continue;
        if (socket.bufferedAmount > 2 * 1024 * 1024) {
          socket.close(1013, 'Viewer too slow');
          continue;
        }
        if (batch?.url) socket.send(JSON.stringify({ type: 'lattice:page', payload: { url: batch.url } }));
        for (const frame of batch?.frames || []) socket.send(frame);
      }
      stream.failures = 0;
    } catch {
      // Navigation briefly destroys the execution context. The next poll
      // injects capture into the new document; repeated failures end the stream.
      if (++stream.failures >= 5) {
        for (const socket of stream.viewers) socket.close(1011, 'Foxbridge unavailable or tab closed');
        await this.stop(stream);
        return;
      }
    }
    if (!stream.stopped) stream.timer = setTimeout(() => this.poll(stream), 150);
  }

  async stop(stream) {
    if (stream.stopped) return;
    stream.stopped = true;
    clearTimeout(stream.timer);
    clearTimeout(stream.idle);
    if (this.streams.get(stream.targetId) === stream) this.streams.delete(stream.targetId);
    try { await stream.cdp.evaluate(stream.sessionId, 'window.__phantomStreamStop?.()'); } catch { /* tab gone */ }
    try {
      if (stream.owned) await stream.cdp.send('Target.closeTarget', { targetId: stream.targetId });
      else await stream.cdp.send('Target.detachFromTarget', { sessionId: stream.sessionId });
    } catch { /* disconnected */ }
    stream.cdp.close();
  }

  async close() {
    this.closed = true;
    await this.serial;
    await Promise.all([...this.streams.values()].map(stream => this.stop(stream)));
  }
}
