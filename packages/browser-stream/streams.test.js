import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { setTimeout as delay } from 'node:timers/promises';
import { Streams, pageURL } from './streams.js';
import { REMOTE_CONTROL as RC, CONTROL } from '@full-self-browsing/phantom-stream/protocol';

class Viewer extends EventEmitter {
  readyState = 1;
  bufferedAmount = 0;
  messages = [];
  send(raw) { this.messages.push(JSON.parse(raw)); }
  close() { this.readyState = 3; this.emit('close'); }
}
function browser(targetInfos = []) {
  const calls = [];
  let navigated = false;
  let failed = false;
  const connect = async () => ({
    send: async (method, params) => {
      calls.push([method, params]);
      if (method === 'Target.getTargets') return { targetInfos };
      if (method === 'Target.createTarget') {
        targetInfos.push({ targetId: 'new-tab', type: 'page', url: params.url });
        return { targetId: 'new-tab' };
      }
      if (method === 'Target.attachToTarget') return { sessionId: 'session' };
      return {};
    },
    evaluate: async (_, expression) => {
      calls.push(['evaluate', expression]);
      if (failed) throw new Error('disconnected');
      if (expression.includes('var queue = window.__latticePhantomQueue')) {
        return { url: navigated ? 'https://example.org/next' : 'https://example.org/', frames: [JSON.stringify({ type: 'ext:dom-snapshot', payload: {
          html: '<p>Live</p>', documentId: 'doc', streamSessionId: 'capture', snapshotId: 1,
          viewportWidth: 1280, viewportHeight: 720,
        } })] };
      }
      return true;
    },
    close: () => calls.push(['disconnect']),
  });
  return { connect, calls, navigate: () => { navigated = true; }, fail: () => { failed = true; } };
}

test('query URL round-trips nested queries and fragments; rejects executable schemes and credentials', () => {
  const target = 'https://example.org/path?q=a%26b&return=%2Ffoo#heading';
  assert.equal(pageURL(new URLSearchParams({ url: target }).get('url')), target);
  for (const invalid of [null, 'relative', 'javascript:alert(1)', 'file:///etc/passwd', 'https://user:secret@example.org']) {
    assert.throws(() => pageURL(invalid));
  }
});

test('control results acknowledge execution and report rejected commands', async () => {
  const fake = browser();
  const streams = new Streams({ connect: fake.connect });
  const viewer = new Viewer();
  try {
    await streams.join('https://example.org/', viewer);
    await delay(30);
    viewer.emit('message', JSON.stringify({ type: RC.TEXT, payload: {
      documentId: 'doc', streamSessionId: 'capture', snapshotId: 1, commandId: 1, text: 'whole query',
    } }));
    viewer.emit('message', JSON.stringify({ type: RC.TEXT, payload: {
      documentId: 'old-doc', streamSessionId: 'capture', snapshotId: 1, commandId: 2, text: 'wrong page',
    } }));
    await streams.streams.get('new-tab').input;
    assert.ok(fake.calls.some(([method, params]) => method === 'Input.insertText' && params.text === 'whole query'));
    assert.ok(!fake.calls.some(([method, params]) => method === 'Input.insertText' && params.text === 'wrong page'));
    assert.deepEqual(viewer.messages.filter(message => message.type === 'lattice:control-result').map(message => message.payload), [
      { commandId: 1, ok: true }, { commandId: 2, ok: false },
    ]);
  } finally { await streams.close(); }
});

test('a resync request is serviced even immediately after the previous resync', async () => {
  const fake = browser();
  const streams = new Streams({ connect: fake.connect });
  const viewer = new Viewer();
  const starts = () => fake.calls.filter(([method, expression]) => method === 'evaluate' && expression === 'window.__phantomStreamStart()').length;
  try {
    await streams.join('https://example.org/', viewer);
    await delay(30);
    viewer.emit('message', JSON.stringify({ type: CONTROL.START }));
    await delay(180);
    const before = starts();
    viewer.emit('message', JSON.stringify({ type: CONTROL.START }));
    await delay(180);
    assert.ok(starts() > before);
  } finally { await streams.close(); }
});

test('concurrent viewers share existing target, receive a snapshot, and ignore unknown commands', async () => {
  const fake = browser([{ type: 'page', targetId: 'agent-tab', url: 'https://example.org/' }]);
  const streams = new Streams({ connect: fake.connect, idleMs: 5 });
  const first = new Viewer();
  const second = new Viewer();
  try {
    await Promise.all([streams.join('https://example.org/', first), streams.join('https://example.org/', second)]);
    await delay(220);
    assert.equal(fake.calls.filter(([method]) => method === 'Target.attachToTarget').length, 1);
    assert.equal(fake.calls.filter(([method]) => method === 'Target.createTarget').length, 0);
    assert.ok(second.messages.some(message => message.type === 'ext:dom-snapshot'));
    first.emit('message', JSON.stringify({ type: 'dash:remote-click', payload: { x: 1, y: 1 } }));
    assert.ok(!fake.calls.some(([method]) => method.startsWith('Input.')));
    fake.navigate();
    await delay(220);
    assert.ok(first.messages.some(message => message.payload?.url === 'https://example.org/next'));
    first.close(); second.close();
    await delay(40);
    assert.ok(fake.calls.some(([method]) => method === 'Target.detachFromTarget'));
    assert.ok(!fake.calls.some(([method]) => method === 'Target.closeTarget'));
  } finally { await streams.close(); }
});

test('viewer-created tabs are reclaimed when the final viewer leaves', async () => {
  const fake = browser();
  const streams = new Streams({ connect: fake.connect, idleMs: 5 });
  const viewer = new Viewer();
  await streams.join('https://example.org/', viewer);
  viewer.close();
  await delay(50);
  assert.ok(fake.calls.some(([method]) => method === 'Target.closeTarget'));
  assert.equal(streams.streams.size, 0);
  await streams.close();
});

test('failed attach closes a newly created tab', async () => {
  const fake = browser();
  const streams = new Streams({ connect: async () => {
    const cdp = await fake.connect();
    const send = cdp.send;
    cdp.send = (method, params) => method === 'Target.attachToTarget' ? Promise.reject(new Error('attach failed')) : send(method, params);
    return cdp;
  } });
  await assert.rejects(streams.join('https://example.org/', new Viewer()));
  assert.ok(fake.calls.some(([method]) => method === 'Target.closeTarget'));
  await streams.close();
});
