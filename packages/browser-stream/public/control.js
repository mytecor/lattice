import { MAX_SCROLL_DELTA } from './input-limits.js';

export function attachControl({ viewer, transport, surface, container, reportError, mapHostPointToViewport, protocol }) {
  const { REMOTE_CONTROL, STREAM } = protocol;
  let identity;
  let sourceScroll;
  let live = false;
  let sequence = 0;
  let pending;
  let queue = [];
  let scheduled = false;
  let blocked = false;
  function cancel(message) {
    queue = [];
    pending = undefined;
    blocked = true;
    reportError(message);
  }
  function flush() {
    if (scheduled) return;
    scheduled = true;
    // The renderer announces live while handling a snapshot, before our
    // transport listener has received its new identity.
    queueMicrotask(() => {
      scheduled = false;
      if (!live || !identity || pending || !queue.length) return;
      const command = queue.shift();
      const coordinates = command.type === REMOTE_CONTROL.CLICK;
      if (command.identity.documentId !== identity.documentId
          || (coordinates && (command.identity.streamSessionId !== identity.streamSessionId
            || command.identity.snapshotId !== identity.snapshotId))) {
        cancel('Страница изменилась. Повторите клик и ввод.');
        return;
      }
      pending = command;
      try {
        transport.send(command.type, { ...command.payload, ...identity, commandId: command.id });
      } catch {
        cancel('Не удалось отправить ввод. Проверьте соединение и повторите ввод.');
      }
    });
  }
  viewer.on('state', ({ state }) => {
    live = state === 'live';
    if (state === 'disconnected' && (pending || queue.length)) cancel('Соединение прервано. Неотправленный ввод отменён.');
    flush();
  });
  transport.onStatus?.(({ state }) => {
    if (['closed', 'error', 'reconnecting'].includes(state) && (pending || queue.length)) {
      cancel('Связь потеряна. Проверьте результат последней команды перед повторным вводом.');
    }
  });
  transport.onMessage((type, payload) => {
    if (type === STREAM.SNAPSHOT) {
      if (identity && identity.documentId !== payload.documentId && (pending || queue.length)) {
        // A click or Enter may navigate before its acknowledgement arrives.
        // With no dependent commands this is an ordinary completed action.
        if (!queue.length && (pending?.type === REMOTE_CONTROL.CLICK
            || (pending?.type === REMOTE_CONTROL.KEY && pending.payload.key === 'Enter'))) {
          pending = undefined;
        } else cancel('Открыта другая страница. Неотправленный ввод отменён.');
      }
      identity = { streamSessionId: payload.streamSessionId, snapshotId: payload.snapshotId, documentId: payload.documentId };
      sourceScroll = { x: payload.scrollX || 0, y: payload.scrollY || 0 };
      flush();
    }
    if (type === STREAM.SCROLL && payload.streamSessionId === identity?.streamSessionId && payload.snapshotId === identity?.snapshotId) {
      sourceScroll = { x: payload.scrollX || 0, y: payload.scrollY || 0 };
      syncScroll();
    }
    if (type === 'lattice:control-result' && pending?.id === payload.commandId) {
      pending = undefined;
      if (!payload.ok) cancel('Команда не выполнена. Повторите клик и ввод.');
      else flush();
    }
  });
  function send(type, payload, scrollTarget) {
    if (type === REMOTE_CONTROL.CLICK) blocked = false;
    if (blocked) return;
    if (!identity?.documentId) {
      reportError('Дождитесь подключения страницы и повторите ввод.');
      return;
    }
    const scrolling = type === REMOTE_CONTROL.SCROLL;
    const tail = queue.at(-1);
    if (scrolling && tail?.type === type && tail.scrollTarget === scrollTarget
        && tail.identity.documentId === identity.documentId
        && Math.sign(tail.payload.deltaX) === Math.sign(payload.deltaX)
        && Math.sign(tail.payload.deltaY) === Math.sign(payload.deltaY)
        && Math.abs(tail.payload.deltaX + payload.deltaX) <= MAX_SCROLL_DELTA
        && Math.abs(tail.payload.deltaY + payload.deltaY) <= MAX_SCROLL_DELTA) {
      // One queued command represents the gesture while its predecessor waits
      // for acknowledgement. Preserve distance, direction and scroll target.
      tail.payload = { ...payload, deltaX: tail.payload.deltaX + payload.deltaX,
        deltaY: tail.payload.deltaY + payload.deltaY };
      flush();
      return;
    }
    if (queue.length >= 64) {
      // Wheel overload must never cancel typed text or lock all subsequent
      // input. Drop excess wheel samples; reserve room for deliberate actions.
      if (scrolling) return;
      queue = queue.filter(command => command.type !== REMOTE_CONTROL.SCROLL);
    }
    if (queue.length >= 64 || queue.reduce((size, command) => size + (command.payload.text?.length || 0), 0) + (payload.text?.length || 0) > 65536) {
      cancel('Очередь ввода переполнена. Повторите клик и ввод.');
      return;
    }
    queue.push({ id: ++sequence, type, payload, identity, scrollTarget });
    flush();
  }
  function point(event) {
    const rect = container.getBoundingClientRect();
    return mapHostPointToViewport({ x: event.clientX - rect.left, y: event.clientY - rect.top }, viewer.getViewportMapping().scale);
  }
  function click(event, p) {
    event.preventDefault();
    if (p.inside) send(REMOTE_CONTROL.CLICK, { x: p.x, y: p.y, button: 'left', clickCount: event.detail || 1 });
    surface.focus({ preventScroll: true });
  }
  function wheel(event, p) {
    event.preventDefault();
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewer.getViewportMapping().viewport.height : 1;
    let target = event.target?.ownerDocument;
    for (let element = event.target; element?.nodeType === 1; element = element.parentElement) {
      if (element.scrollHeight <= element.clientHeight && element.scrollWidth <= element.clientWidth) continue;
      const style = element.ownerDocument.defaultView.getComputedStyle(element);
      if (/auto|scroll|overlay/.test(style.overflowX + ' ' + style.overflowY)) { target = element; break; }
    }
    if (p.inside) send(REMOTE_CONTROL.SCROLL, { x: p.x, y: p.y,
      deltaX: Math.max(-MAX_SCROLL_DELTA, Math.min(MAX_SCROLL_DELTA, event.deltaX * unit)),
      deltaY: Math.max(-MAX_SCROLL_DELTA, Math.min(MAX_SCROLL_DELTA, event.deltaY * unit)) }, target);
  }
  surface.addEventListener('click', event => click(event, point(event)));
  surface.addEventListener('wheel', event => wheel(event, point(event)), { passive: false });

  // Listen on the rendered document itself. An invisible full-page textarea
  // hides normal hit targets from native browser interaction/accessibility.
  // Keep the textarea only as an off-screen keyboard/IME input sink.
  const rootFrame = container.querySelector?.('iframe');
  const boundDocuments = new WeakSet();
  const boundFrames = new WeakSet();
  function framePoint(event, frame) {
    let x = event.clientX, y = event.clientY;
    for (let current = frame; current && current !== rootFrame; current = current.ownerDocument.defaultView.frameElement) {
      const rect = current.getBoundingClientRect();
      x = rect.left + x * rect.width / current.clientWidth;
      y = rect.top + y * rect.height / current.clientHeight;
    }
    const { width, height } = viewer.getViewportMapping().viewport;
    return { x, y, inside: x >= 0 && y >= 0 && x < width && y < height };
  }
  function syncScroll() {
    if (rootFrame?.contentWindow && sourceScroll) {
      // The SDK defaults to smooth follow, which trails rapid wheel gestures.
      // The mirror must show the source's exact final position immediately.
      rootFrame.contentWindow.scrollTo({ left: sourceScroll.x, top: sourceScroll.y, behavior: 'instant' });
    }
  }
  function bindFrame(frame) {
    if (frame === rootFrame) syncScroll();
    if (!boundFrames.has(frame)) {
      boundFrames.add(frame);
      frame.addEventListener('load', () => bindFrame(frame));
    }
    let doc;
    try { doc = frame.contentDocument; } catch { return; }
    if (!doc || boundDocuments.has(doc)) return;
    boundDocuments.add(doc);
    doc.addEventListener('click', event => click(event, framePoint(event, frame)), true);
    doc.addEventListener('auxclick', event => event.preventDefault(), true);
    doc.addEventListener('contextmenu', event => event.preventDefault(), true);
    doc.addEventListener('submit', event => event.preventDefault(), true);
    doc.addEventListener('wheel', event => wheel(event, framePoint(event, frame)), { capture: true, passive: false });
    doc.addEventListener('keydown', keydown, true);
    doc.addEventListener('beforeinput', event => {
      // Tab focus can enter a mirrored input without a pointer click. Never
      // edit only the local copy; deliver its text to the source as well.
      event.preventDefault();
      if (event.data) send(REMOTE_CONTROL.TEXT, { text: event.data });
    }, true);
    doc.addEventListener('load', event => {
      if (event.target.tagName === 'IFRAME') bindFrame(event.target);
    }, true);
    for (const child of doc.querySelectorAll('iframe')) bindFrame(child);
  }
  if (rootFrame) bindFrame(rootFrame);
  function text() {
    const value = surface.value;
    surface.value = '';
    for (let offset = 0; offset < value.length;) {
      let end = Math.min(offset + 4096, value.length);
      if (end < value.length && /[\uD800-\uDBFF]/.test(value[end - 1])) end--;
      send(REMOTE_CONTROL.TEXT, { text: value.slice(offset, end) });
      offset = end;
    }
  }
  surface.addEventListener('input', event => { if (!event.isComposing) text(); });
  surface.addEventListener('compositionend', text);
  function keydown(event) {
    if (event.isComposing) return;
    // Printable text, paste, and IME pass through the local textarea solely
    // as an input method; their text is sent to the remote focused element.
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'v') return;
    const selectAll = (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a';
    if (event.key.length === 1 && !selectAll && !event.ctrlKey && !event.metaKey && !event.altKey) return;
    if (['Shift', 'Control', 'Meta', 'Alt'].includes(event.key)) return;
    event.preventDefault();
    const key = selectAll ? 'a' : event.key;
    send(REMOTE_CONTROL.KEY, { key, event: 'down', selectAll });
  }
  surface.addEventListener('keydown', keydown);
}
