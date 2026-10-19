import { MAX_SCROLL_DELTA } from './public/input-limits.js';
import { validateRemoteControlMessage } from '@full-self-browsing/phantom-stream/protocol';

// Only the authenticated viewer's typed protocol is accepted, never arbitrary CDP.
// Snapshot identity stops queued clicks from landing on a newly navigated page.
export async function dispatchControl(stream, type, payload = {}) {
  if (!stream.identity || !payload.documentId || payload.documentId !== stream.identity.documentId) return false;
  const validated = validateRemoteControlMessage(type, payload);
  if (!validated.ok) return false;
  if (!['click', 'text', 'key', 'scroll'].includes(validated.action.kind)) return false;
  const coordinates = ['click', 'scroll'].includes(validated.action.kind);
  if (validated.action.kind === 'click' && (payload.streamSessionId !== stream.identity.streamSessionId
      || payload.snapshotId !== stream.identity.snapshotId)) return false;
  if (coordinates && (validated.action.x < 0 || validated.action.y < 0 || validated.action.x >= stream.width || validated.action.y >= stream.height)) return false;
  const current = await stream.cdp.evaluate(stream.sessionId, `(() => {
    const identity = window.__latticePhantomQueue?.identity;
    return identity?.documentId === ${JSON.stringify(payload.documentId)}
      ${validated.action.kind === 'click' ? `&& identity?.streamSessionId === ${JSON.stringify(payload.streamSessionId)} && identity?.snapshotId === ${JSON.stringify(payload.snapshotId)}` : ''};
  })()`);
  if (!current) return false;
  const action = validated.action;
  const send = (method, params) => stream.cdp.send(method, params, stream.sessionId);
  if (action.kind === 'click' || action.kind === 'scroll') {
    if (action.x < 0 || action.y < 0 || action.x >= stream.width || action.y >= stream.height) return false;
    if (action.kind === 'scroll') {
      const wheel = { x: action.x, y: action.y,
        deltaX: Math.max(-MAX_SCROLL_DELTA, Math.min(MAX_SCROLL_DELTA, action.deltaX)), deltaY: Math.max(-MAX_SCROLL_DELTA, Math.min(MAX_SCROLL_DELTA, action.deltaY)) };
      try {
        await send('Input.dispatchMouseEvent', { type: 'mouseWheel', ...wheel });
      } catch {
        // Foxbridge's Juggler shim rejects mouseWheel. Apply the wheel in the
        // remote document, preserving cancelable handlers and nested scrollers.
        await stream.cdp.evaluate(stream.sessionId, `(${remoteScroll.toString()})(${JSON.stringify(wheel)})`);
      }
      return true;
    }
    await stream.cdp.evaluate(stream.sessionId, 'window.__latticeRemoteClick = true');
    try {
      await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: action.x, y: action.y });
      await send('Input.dispatchMouseEvent', { type: 'mousePressed', x: action.x, y: action.y, button: action.button, clickCount: Math.min(action.clickCount, 3) });
      await send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: action.x, y: action.y, button: action.button, clickCount: Math.min(action.clickCount, 3) });
    } finally {
      // Navigation can destroy the context before the release command returns.
      await stream.cdp.evaluate(stream.sessionId, 'window.__latticeRemoteClick = false').catch(() => {});
    }
  } else if (action.kind === 'text') {
    await send('Input.insertText', { text: action.text });
  } else if (action.kind === 'key') {
    if (action.key.length > 64) return false;
    const keyCode = { Enter: 13, Tab: 9, Backspace: 8, Delete: 46, Escape: 27,
      ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40, Home: 36, End: 35,
      PageUp: 33, PageDown: 34 }[action.key] || 0;
    const key = {
      type: action.event === 'down' ? 'keyDown' : 'keyUp', key: action.key,
      code: action.key.length === 1 ? `Key${action.key.toUpperCase()}` : action.key,
      windowsVirtualKeyCode: keyCode,
      // Foxbridge has a native editor-command path for selectAll.
      ...(payload.selectAll === true && action.key === 'a' ? { commands: ['selectAll'] } : {}),
    };
    await send('Input.dispatchKeyEvent', key);
    // Keep press/release in the same acknowledged command, including Enter
    // that navigates before the viewer receives the next snapshot.
    if (action.event === 'down') await send('Input.dispatchKeyEvent', { ...key, type: 'keyUp', commands: undefined });
  }
  return true;
}

function remoteScroll({ x, y, deltaX, deltaY }) {
  const target = document.elementFromPoint(x, y) || document.body;
  if (!target.dispatchEvent(new WheelEvent('wheel', { clientX: x, clientY: y,
    deltaX, deltaY, bubbles: true, cancelable: true }))) return;
  let remainingX = deltaX;
  let remainingY = deltaY;
  for (let element = target; element; element = element.parentElement) {
    const style = getComputedStyle(element);
    const root = element === document.scrollingElement;
    const scrollX = root || /auto|scroll|overlay/.test(style.overflowX);
    const scrollY = root || /auto|scroll|overlay/.test(style.overflowY);
    if (!scrollX && !scrollY) continue;
    const beforeX = element.scrollLeft;
    const beforeY = element.scrollTop;
    element.scrollBy({ left: scrollX ? remainingX : 0, top: scrollY ? remainingY : 0, behavior: 'instant' });
    remainingX -= element.scrollLeft - beforeX;
    remainingY -= element.scrollTop - beforeY;
    if ((!remainingX && !remainingY) || style.overscrollBehavior === 'contain' || style.overscrollBehavior === 'none') break;
  }
}
