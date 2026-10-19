import { mapHostPointToViewport } from '/phantom/renderer/index.js';
import { REMOTE_CONTROL, STREAM } from '/phantom/protocol/messages.js';

export function attachControl({ viewer, transport, surface, container, reportError }) {
  let identity;
  let live = false;
  viewer.on('state', ({ state }) => { live = state === 'live'; });
  transport.onMessage((type, payload) => {
    if (type === STREAM.SNAPSHOT) identity = { streamSessionId: payload.streamSessionId, snapshotId: payload.snapshotId };
    if (type === 'lattice:control-error') reportError('Команда не выполнена. Попробуйте ещё раз.');
  });
  function send(type, payload) {
    if (live && identity) transport.send(type, { ...payload, ...identity });
  }
  function point(event) {
    const rect = container.getBoundingClientRect();
    return mapHostPointToViewport({ x: event.clientX - rect.left, y: event.clientY - rect.top }, viewer.getViewportMapping().scale);
  }
  surface.addEventListener('click', event => {
    event.preventDefault();
    const p = point(event);
    if (p.inside) send(REMOTE_CONTROL.CLICK, { x: p.x, y: p.y, button: 'left', clickCount: event.detail || 1 });
    surface.focus({ preventScroll: true });
  });
  surface.addEventListener('auxclick', event => event.preventDefault());
  surface.addEventListener('contextmenu', event => event.preventDefault());
  surface.addEventListener('wheel', event => {
    event.preventDefault();
    const p = point(event);
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewer.getViewportMapping().viewport.height : 1;
    if (p.inside) send(REMOTE_CONTROL.SCROLL, { x: p.x, y: p.y, deltaX: event.deltaX * unit, deltaY: event.deltaY * unit });
  }, { passive: false });
  function text() {
    const value = surface.value;
    surface.value = '';
    for (let offset = 0; offset < value.length; offset += 4096) send(REMOTE_CONTROL.TEXT, { text: value.slice(offset, offset + 4096) });
  }
  surface.addEventListener('input', event => { if (!event.isComposing) text(); });
  surface.addEventListener('compositionend', text);
  surface.addEventListener('keydown', event => {
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
    send(REMOTE_CONTROL.KEY, { key, event: 'up', selectAll });
  });
}
