import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dispatchControl } from './control.js';
import { REMOTE_CONTROL as RC } from '@full-self-browsing/phantom-stream/protocol';

function source() {
  const calls = [];
  return { calls, stream: {
    identity: { documentId: 'document', streamSessionId: 'live', snapshotId: 3 }, width: 1280, height: 720, sessionId: 'remote-tab',
    cdp: { send: async (...args) => { calls.push(args); }, evaluate: async (...args) => { calls.push(['evaluate', ...args]); return true; } },
  } };
}
const identity = { documentId: 'document', streamSessionId: 'live', snapshotId: 3 };
test('authenticated input dispatches a complete mouse sequence, text, keys and wheel to the source session', async () => {
  const { stream, calls } = source();
  await dispatchControl(stream, RC.CLICK, { ...identity, x: 100, y: 50 });
  assert.deepEqual(calls.filter(([method]) => method === 'Input.dispatchMouseEvent').map(([, params]) => params.type), ['mouseMoved', 'mousePressed', 'mouseReleased']);
  await dispatchControl(stream, RC.TEXT, { ...identity, text: 'Привет' });
  await dispatchControl(stream, RC.KEY, { ...identity, key: 'Enter', event: 'down' });
  await dispatchControl(stream, RC.KEY, { ...identity, key: 'a', event: 'down', selectAll: true });
  await dispatchControl(stream, RC.SCROLL, { ...identity, x: 40, y: 40, deltaX: 0, deltaY: 120 });
  assert.ok(calls.some(([method, params]) => method === 'Input.insertText' && params.text === 'Привет'));
  assert.ok(calls.some(([method, params]) => method === 'Input.dispatchKeyEvent' && params.windowsVirtualKeyCode === 13));
  assert.ok(calls.some(([method, params]) => method === 'Input.dispatchKeyEvent' && params.commands?.includes('selectAll')));
  assert.ok(calls.some(([method, params]) => method === 'Input.dispatchMouseEvent' && params.type === 'mouseWheel' && params.deltaY === 120));
  assert.ok(calls.filter(([method]) => method.startsWith('Input.')).every(([, , session]) => session === 'remote-tab'));
});

test('stale snapshots, invalid input, out-of-viewport coordinates and arbitrary CDP never reach the browser', async () => {
  const { stream, calls } = source();
  for (const [type, payload] of [
    [RC.CLICK, { ...identity, snapshotId: 2, x: 10, y: 10 }],
    [RC.TEXT, { ...identity, documentId: 'previous-document', text: 'old input' }],
    [RC.CLICK, { ...identity, streamSessionId: 'previous', x: 10, y: 10 }],
    [RC.CLICK, { ...identity, x: -1, y: 10 }],
    [RC.CLICK, { ...identity, x: 1281, y: 10 }],
    [RC.CLICK, { ...identity, x: NaN, y: 10 }],
    [RC.TEXT, { ...identity, text: 'x'.repeat(4097) }],
    ['Runtime.evaluate', { ...identity, expression: 'alert(1)' }],
  ]) await dispatchControl(stream, type, payload);
  assert.equal(calls.length, 0);
});

test('text and keyboard survive a new snapshot of the same document, while coordinates do not', async () => {
  const { stream, calls } = source();
  const previous = { ...identity, streamSessionId: 'previous-capture', snapshotId: 1 };
  assert.equal(await dispatchControl(stream, RC.TEXT, { ...previous, text: 'still focused' }), true);
  assert.equal(await dispatchControl(stream, RC.KEY, { ...previous, key: 'Enter', event: 'down' }), true);
  assert.equal(await dispatchControl(stream, RC.CLICK, { ...previous, x: 10, y: 10 }), false);
  assert.equal(calls.filter(([method]) => method.startsWith('Input.')).length, 3);
  assert.deepEqual(calls.filter(([method]) => method === 'Input.dispatchKeyEvent').map(([, params]) => params.type), ['keyDown', 'keyUp']);
});

test('a source navigation invalidates input even before the host has polled the next snapshot', async () => {
  const { stream, calls } = source();
  stream.cdp.evaluate = async () => false;
  await dispatchControl(stream, RC.CLICK, { ...identity, x: 10, y: 10 });
  assert.equal(calls.length, 0);
});


test('Foxbridge wheel rejection falls back to scrolling the remote document', async () => {
  const { stream, calls } = source();
  stream.cdp.send = async () => { throw new Error('Juggler rejects wheel'); };
  await dispatchControl(stream, RC.SCROLL, { ...identity, x: 20, y: 30, deltaX: 0, deltaY: 240 });
  assert.ok(calls.some(([method, , expression]) => method === 'evaluate' && expression.includes('document.elementFromPoint') && expression.includes('"deltaY":240')));
});
