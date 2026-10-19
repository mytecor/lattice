import { test } from 'node:test';
import assert from 'node:assert/strict';
import { attachControl } from './public/control.js';
import * as protocol from '@full-self-browsing/phantom-stream/protocol';

const { REMOTE_CONTROL: RC, STREAM } = protocol;
const snapshot = { documentId: 'doc', streamSessionId: 'capture', snapshotId: 1 };
const tick = () => new Promise(resolve => queueMicrotask(resolve));
function viewerControl(frame) {
  let state, message;
  const events = new Map(), sent = [], errors = [];
  const surface = { value: '', focus() {}, addEventListener: (name, callback) => events.set(name, callback) };
  attachControl({
    viewer: { on: (_, callback) => { state = callback; }, getViewportMapping: () => ({ scale: 1, viewport: { width: 1280, height: 720 } }) },
    transport: { onMessage: callback => { message = callback; }, send: (type, payload) => sent.push({ type, payload }) },
    surface, container: { querySelector: () => frame, getBoundingClientRect: () => ({ left: 0, top: 0 }) },
    mapHostPointToViewport: p => ({ ...p, inside: true }), protocol, reportError: error => errors.push(error),
  });
  return { sent, errors, surface,
    state: value => state({ state: value }),
    snapshot: value => message(STREAM.SNAPSHOT, value),
    scroll: value => message(STREAM.SCROLL, value),
    ack: (command, ok = true) => message('lattice:control-result', { commandId: command.payload.commandId, ok }),
    text(value, isComposing = false) { surface.value += value; events.get('input')({ isComposing }); },
    compositionend: () => events.get('compositionend')(),
    key: key => events.get('keydown')({ key, preventDefault() {} }),
    click: () => events.get('click')({ clientX: 10, clientY: 10, detail: 1, preventDefault() {} }),
  };
}

test('stale input survives resync and Enter waits for the text acknowledgement', async () => {
  const v = viewerControl();
  v.snapshot(snapshot); v.state('stale');
  v.text('привет мир'); v.key('Enter');
  await tick();
  assert.equal(v.sent.length, 0);
  // Renderer marks live before other snapshot listeners run.
  v.state('live'); v.snapshot({ ...snapshot, streamSessionId: 'new-capture', snapshotId: 2 });
  await tick();
  assert.equal(v.sent.length, 1);
  assert.equal(v.sent[0].payload.text, 'привет мир');
  assert.equal(v.sent[0].payload.snapshotId, 2);
  v.ack(v.sent[0]); await tick();
  assert.equal(v.sent[1].payload.key, 'Enter');
  assert.equal(v.sent[1].payload.event, 'down');
  v.ack(v.sent[1]); await tick();
  assert.equal(v.sent.length, 2);
  assert.deepEqual(v.errors, []);
});

test('a normal Enter navigation before acknowledgement does not report cancelled input', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('live');
  v.key('Enter'); await tick();
  const enter = v.sent[0];
  v.snapshot({ ...snapshot, documentId: 'search-results' });
  v.ack(enter); await tick();
  assert.deepEqual(v.errors, []);
});

test('a click completes before its following text can reach the remote browser', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('live');
  v.click(); v.text('fast123');
  await tick();
  assert.equal(v.sent.length, 1);
  assert.equal(v.sent[0].type, RC.CLICK);
  v.ack(v.sent[0]); await tick();
  assert.equal(v.sent[1].payload.text, 'fast123');
});

test('navigation cancels old text; a failed click cancels its dependent input', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('stale'); v.text('old page');
  v.snapshot({ ...snapshot, documentId: 'new-document' }); v.state('live');
  await tick();
  assert.equal(v.sent.length, 0);
  assert.equal(v.errors.length, 1);
  v.click(); v.text('dependent'); await tick();
  v.ack(v.sent[0], false); await tick();
  v.key('Enter'); await tick();
  assert.equal(v.sent.length, 1);
  assert.equal(v.errors.length, 2);
});

test('resync never replays a coordinate click against a different snapshot', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('stale');
  v.click(); v.text('dependent');
  v.snapshot({ ...snapshot, snapshotId: 2 }); v.state('live'); await tick();
  assert.equal(v.sent.length, 0);
  assert.equal(v.errors.length, 1);
});

test('IME is committed once and long text preserves surrogate pairs across frames', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('live');
  const text = 'a'.repeat(4095) + '😀привет';
  v.text(text, true); await tick();
  assert.equal(v.sent.length, 0);
  v.compositionend(); v.text(''); await tick();
  v.ack(v.sent[0]); await tick();
  assert.equal(v.sent.map(command => command.payload.text).join(''), text);
  assert.equal(v.sent[1].payload.text, '😀привет');
});

test('queue overflow reports cancellation rather than submitting a truncated query', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('stale');
  for (let i = 0; i < 70; i++) v.text('x');
  v.key('Enter'); v.state('live'); await tick();
  assert.equal(v.sent.length, 0);
  assert.equal(v.errors.length, 1);
});


test('native mirror clicks are forwarded and local navigation is cancelled after each snapshot reload', async () => {
  function target() {
    const events = new Map();
    return { events, addEventListener: (name, listener) => events.set(name, listener), querySelectorAll: () => [] };
  }
  const first = target();
  const frame = { ...target(), contentDocument: first };
  const v = viewerControl(frame); v.snapshot(snapshot); v.state('live');
  let prevented = false;
  first.events.get('click')({ clientX: 210, clientY: 90, detail: 1, preventDefault() { prevented = true; } });
  await tick();
  assert.equal(prevented, true);
  assert.equal(v.sent[0].type, RC.CLICK);
  assert.equal(v.sent[0].payload.x, 210);
  assert.equal(v.sent[0].payload.y, 90);
  v.ack(v.sent[0]); await tick();
  const next = target(); frame.contentDocument = next;
  frame.events.get('load')();
  let submitted = false;
  next.events.get('submit')({ preventDefault() { submitted = true; } });
  assert.ok(submitted);
  next.events.get('click')({ clientX: 20, clientY: 30, preventDefault() {} });
  await tick();
  assert.equal(v.sent.length, 2);
  assert.equal(v.sent[1].payload.x, 20);
});


test('final source position is applied instantly and survives a snapshot iframe load', () => {
  const positions = [], events = new Map();
  const frame = { contentWindow: { scrollTo: value => positions.push(value) }, contentDocument: null,
    addEventListener: (name, callback) => events.set(name, callback) };
  const v = viewerControl(frame); v.snapshot(snapshot); v.state('live');
  v.scroll({ ...snapshot, scrollX: 0, scrollY: 9000 });
  assert.deepEqual(positions.at(-1), { left: 0, top: 9000, behavior: 'instant' });
  // The SDK restores the initial snapshot offset on load; our listener must
  // restore the newer scroll event rather than losing the final position.
  events.get('load')();
  assert.deepEqual(positions.at(-1), { left: 0, top: 9000, behavior: 'instant' });
  v.scroll({ ...snapshot, scrollX: 0, scrollY: 0 });
  assert.equal(positions.at(-1).top, 0);
  const count = positions.length;
  v.scroll({ ...snapshot, streamSessionId: 'old', scrollY: 200 });
  assert.equal(positions.length, count);
});
