import { test } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import WebSocket from 'ws';
import { createBrowserServer } from './server.js';

test('HTTP serves only viewer/SDK assets and WebSocket enforces same-origin and URL validation', async () => {
  const joined = [];
  const app = createBrowserServer({ streams: { join: async (url, socket) => { joined.push(url); socket.send('ready'); }, close: async () => {} } });
  app.server.listen(0, '127.0.0.1');
  await once(app.server, 'listening');
  const origin = `http://127.0.0.1:${app.server.address().port}`;
  try {
    assert.equal((await fetch(origin)).status, 200);
    assert.match(await (await fetch(origin + '/phantom/renderer/index.js')).text(), /createViewer/);
    for (const path of ['/server.js', '/package.json', '/phantom/%2e%2e%2fpackage.json']) {
      assert.equal((await fetch(origin + path)).status, 404);
    }
    const endpoint = origin.replace('http:', 'ws:') + '/ws?url=' + encodeURIComponent('https://example.org/?a=1&b=2');
    const socket = new WebSocket(endpoint, { origin });
    assert.equal((await once(socket, 'message'))[0].toString(), 'ready');
    socket.close();
    await once(socket, 'close');
    assert.deepEqual(joined, ['https://example.org/?a=1&b=2']);
    for (const [url, headers] of [[endpoint, { origin: 'https://attacker.example' }], [endpoint, {}], [endpoint.replace(/url=.*/, 'url=javascript:alert(1)'), { origin }]]) {
      const rejected = new WebSocket(url, headers);
      rejected.on('error', () => {});
      await once(rejected, 'unexpected-response').then(([, response]) => { assert.equal(response.statusCode, 400); response.resume(); rejected.terminate(); });
    }
  } finally { await app.close(); }
});
