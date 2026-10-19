import { attachControl } from './control.js';
import { createViewer, mapHostPointToViewport } from '/phantom/renderer/index.js';
import * as protocol from '/phantom/protocol/messages.js';
import { createWebSocketTransport } from '/phantom/transport/websocket.js';

const input = document.querySelector('#url');
const status = document.querySelector('#status');
const empty = document.querySelector('#empty');
const rawURL = new URL(location.href).searchParams.get('url');
if (rawURL) {
  input.value = rawURL;
  try {
    const target = new URL(rawURL);
    if (!['http:', 'https:'].includes(target.protocol) || target.username || target.password) throw new Error();
    const ws = new URL('/ws', location.href);
    ws.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws.searchParams.set('url', target.href);
    status.textContent = 'Подключение…';
    empty.querySelector('h1').textContent = 'Подключаем страницу…';
    const transport = createWebSocketTransport({ url: ws.href, role: 'viewer' });
    const viewer = createViewer({ container: document.querySelector('#mirror'), transport });
    attachControl({ viewer, transport, surface: document.querySelector('#control'), container: document.querySelector('#mirror'), mapHostPointToViewport, protocol, reportError: message => { status.textContent = message; } });
    const states = { connecting: 'Подключение…', live: 'В эфире', stale: 'Ожидание обновлений', disconnected: 'Соединение прервано' };
    viewer.on('state', ({ state }) => {
      status.textContent = states[state] || state;
      status.dataset.state = state;
      if (state === 'live') empty.hidden = true;
    });
    transport.onMessage((type, payload) => {
      if (type === 'lattice:error') {
        status.textContent = payload.message;
        empty.hidden = false;
        empty.querySelector('h1').textContent = 'Стрим недоступен';
      }
      if (type === 'lattice:page' && payload.url) {
        document.querySelector('#current').textContent = payload.url;
        // Keep the shared link useful after navigation in the source browser.
        const address = new URL(location.href);
        address.searchParams.set('url', payload.url);
        history.replaceState(null, '', address);
        if (document.activeElement !== input) input.value = payload.url;
      }
    });
    window.addEventListener('pagehide', () => { viewer.destroy(); transport.close(); });
  } catch {
    status.textContent = 'Укажите HTTP(S) адрес без логина и пароля';
  }
}
