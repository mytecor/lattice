import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, sep } from 'node:path';
import { WebSocketServer } from 'ws';
import { Streams, pageURL } from './streams.js';

const publicDir = fileURLToPath(new URL('./public/', import.meta.url));
const phantomDir = fileURLToPath(new URL('./node_modules/@full-self-browsing/phantom-stream/src/', import.meta.url));

export function createBrowserServer({ endpoint = 'http://127.0.0.1:9222', streams = new Streams({ endpoint }) } = {}) {
  const server = createServer(async (request, response) => {
    response.setHeader('Cache-Control', 'no-store');
    response.setHeader('X-Content-Type-Options', 'nosniff');
    response.setHeader('Referrer-Policy', 'no-referrer');
    response.setHeader('X-Frame-Options', 'DENY');
    if (!['GET', 'HEAD'].includes(request.method)) {
      response.writeHead(405).end();
      return;
    }
    let path;
    try { path = decodeURIComponent(new URL(request.url, 'http://localhost').pathname); }
    catch { response.writeHead(400).end(); return; }
    if (path === '/healthz') { response.writeHead(200).end('ok'); return; }
    const root = path.startsWith('/phantom/') ? phantomDir : publicDir;
    const relative = path.startsWith('/phantom/') ? path.slice('/phantom/'.length) : path === '/' ? 'index.html' : path.slice(1);
    const file = resolve(root, relative);
    if (!file.startsWith(resolve(root) + sep) || !/\.(html|css|js)$/.test(file)) {
      response.writeHead(404).end(); return;
    }
    try {
      const body = await readFile(file);
      const type = file.endsWith('.html') ? 'text/html' : file.endsWith('.css') ? 'text/css' : 'text/javascript';
      response.setHeader('Content-Type', type + '; charset=utf-8');
      response.writeHead(200).end(request.method === 'HEAD' ? undefined : body);
    } catch { response.writeHead(404).end(); }
  });
  const wss = new WebSocketServer({ noServer: true, maxPayload: 16384, perMessageDeflate: false });
  server.on('upgrade', (request, socket, head) => {
    let url;
    try {
      const parsed = new URL(request.url, 'http://localhost');
      if (parsed.pathname !== '/ws') throw new Error('Unknown route');
      // Caddy authenticates the handshake. Also reject cross-site WebSockets:
      // ambient SSO cookies must not let other origins read browser sessions.
      const origin = new URL(request.headers.origin);
      if (origin.host !== request.headers.host || !['http:', 'https:'].includes(origin.protocol)) throw new Error('Origin rejected');
      url = pageURL(parsed.searchParams.get('url'));
    } catch {
      socket.end('HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n');
      return;
    }
    wss.handleUpgrade(request, socket, head, ws => {
      ws.on('error', () => {});
      streams.join(url, ws).catch(() => {
        if (ws.readyState === 1) {
          ws.send(JSON.stringify({ type: 'lattice:error', payload: { message: 'Не удалось подключиться к Foxbridge. Попробуйте ещё раз.' } }));
          ws.close(1011, 'Stream unavailable');
        }
      });
    });
  });
  return {
    server,
    async close() {
      for (const socket of wss.clients) socket.terminate();
      await streams.close();
      await new Promise(resolve => wss.close(resolve));
      await new Promise(resolve => server.close(resolve));
    },
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const app = createBrowserServer({ endpoint: process.env.BROWSER_CDP_URL });
  app.server.listen(Number(process.env.BROWSER_STREAM_PORT || 9223), '127.0.0.1');
  for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, () => app.close().then(() => process.exit(0)));
}
