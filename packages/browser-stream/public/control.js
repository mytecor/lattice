export function attachControl({ viewer, transport, surface, container, reportError, mapHostPointToViewport, protocol }) {
  const { REMOTE_CONTROL, STREAM } = protocol;
  let identity;
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
      const coordinates = [REMOTE_CONTROL.CLICK, REMOTE_CONTROL.SCROLL].includes(command.type);
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
      flush();
    }
    if (type === 'lattice:control-result' && pending?.id === payload.commandId) {
      pending = undefined;
      if (!payload.ok) cancel('Команда не выполнена. Повторите клик и ввод.');
      else flush();
    }
  });
  function send(type, payload) {
    if (type === REMOTE_CONTROL.CLICK) blocked = false;
    if (blocked) return;
    if (!identity?.documentId) {
      reportError('Дождитесь подключения страницы и повторите ввод.');
      return;
    }
    if (queue.length >= 64 || queue.reduce((size, command) => size + (command.payload.text?.length || 0), 0) + (payload.text?.length || 0) > 65536) {
      cancel('Очередь ввода переполнена. Повторите клик и ввод.');
      return;
    }
    queue.push({ id: ++sequence, type, payload, identity });
    flush();
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
    for (let offset = 0; offset < value.length;) {
      let end = Math.min(offset + 4096, value.length);
      if (end < value.length && /[\uD800-\uDBFF]/.test(value[end - 1])) end--;
      send(REMOTE_CONTROL.TEXT, { text: value.slice(offset, end) });
      offset = end;
    }
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
  });
}
