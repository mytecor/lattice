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
  const surface = { value: '', focusCalls: 0, focus() { this.focusCalls++; }, addEventListener: (name, callback) => events.set(name, callback) };
  attachControl({
    viewer: { resolveNode: nid => frame?.resolveNode?.(nid), on: (_, callback) => { state = callback; }, getViewportMapping: () => ({ scale: 1, viewport: { width: 1280, height: 720 } }) },
    transport: { onMessage: callback => { message = callback; }, send: (type, payload) => sent.push({ type, payload }) },
    surface, container: { ownerDocument: frame?.hostDocument, querySelector: () => frame, getBoundingClientRect: () => ({ left: frame?.offset?.left || 0, top: frame?.offset?.top || 0, width: 1280, height: 720 }) },
    mapHostPointToViewport: p => ({ ...p, inside: true }), protocol, reportError: error => errors.push(error),
  });
  return { sent, errors, surface,
    state: value => state({ state: value }),
    snapshot: value => message(STREAM.SNAPSHOT, value),
    scroll: value => message(STREAM.SCROLL, value),
    focus: value => message('lattice:focus', value),
    ack: (command, ok = true) => message('lattice:control-result', { commandId: command.payload.commandId, ok }),
    text(value, isComposing = false) { surface.value += value; events.get('input')({ isComposing }); },
    compositionend: () => events.get('compositionend')(),
    key: key => events.get('keydown')({ key, preventDefault() {} }),
    wheel: (deltaY, target) => events.get('wheel')({ clientX: 10, clientY: 10, deltaX: 0, deltaY, deltaMode: 0, target, preventDefault() {} }),
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


test('a fast trackpad gesture coalesces while awaiting acknowledgements without losing text or distance', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('live');
  v.wheel(10); await tick();
  for (let i = 0; i < 1000; i++) v.wheel(10);
  v.click(); v.text('after scrolling');
  v.ack(v.sent[0]); await tick();
  assert.equal(v.sent[1].payload.deltaY, 10000);
  v.ack(v.sent[1]); await tick();
  assert.equal(v.sent[2].type, RC.CLICK);
  v.ack(v.sent[2]); await tick();
  assert.equal(v.sent[3].payload.text, 'after scrolling');
  assert.deepEqual(v.errors, []);
});

test('wheel coalescing preserves reversals, nested targets, and position across capture resyncs', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('stale');
  const outer = { ownerDocument: {} }, inner = { ownerDocument: {} };
  v.wheel(10, outer); v.wheel(20, outer); v.wheel(-15, outer); v.wheel(-25, inner);
  v.snapshot({ ...snapshot, streamSessionId: 'new-capture', snapshotId: 2 }); v.state('live'); await tick();
  assert.equal(v.sent[0].payload.deltaY, 30);
  assert.equal(v.sent[0].payload.snapshotId, 2);
  v.ack(v.sent[0]); await tick(); assert.equal(v.sent[1].payload.deltaY, -15);
  v.ack(v.sent[1]); await tick(); assert.equal(v.sent[2].payload.deltaY, -25);
  assert.deepEqual(v.errors, []);
});

test('pathological wheel overload does not poison a following click and typed input', async () => {
  const v = viewerControl(); v.snapshot(snapshot); v.state('stale');
  for (let i = 0; i < 200; i++) v.wheel(i % 2 ? 10 : -10);
  v.click(); v.text('kept'); v.state('live'); await tick();
  assert.equal(v.sent[0].type, RC.CLICK);
  v.ack(v.sent[0]); await tick();
  assert.equal(v.sent[1].payload.text, 'kept');
  assert.deepEqual(v.errors, []);
});


function editableFrame() {
  const events = new Map(), loads = new Map();
  const doc = { addEventListener: (name, callback) => events.set(name, callback), querySelectorAll: () => [] };
  const field = { tagName: 'INPUT', type: 'text', ownerDocument: doc, selectionStart: 0, selectionEnd: 0, selectionDirection: 'none',
    closest() { return this; }, focus() { doc.activeElement = this; },
    setSelectionRange(start, end, direction) { this.selectionStart = start; this.selectionEnd = end; this.selectionDirection = direction; },
  };
  doc.elementFromPoint = () => field;
  const frame = { contentDocument: doc, addEventListener: (name, callback) => loads.set(name, callback),
    resolveNode: () => ({ rect: { left: 10, top: 10, width: 200, height: 30 } }),
    hostDocument: { body: {} },
  };
  return { frame, doc, field, events, loads };
}

test('editable clicks keep native caret focus; remote selection updates do not focus the hidden input', async () => {
  const { frame, doc, field, events } = editableFrame();
  frame.offset = { left: 100, top: 74 };
  const v = viewerControl(frame); v.snapshot(snapshot); v.state('live');
  events.get('click')({ target: field, clientX: 20, clientY: 20, preventDefault() {} });
  await tick();
  assert.equal(doc.activeElement, field);
  assert.equal(v.surface.focusCalls, 0);
  v.ack(v.sent[0]); await tick();
  v.focus({ ...snapshot, nid: 'field', start: 4, end: 4, direction: 'none' });
  assert.equal(field.selectionStart, 4);
  events.get('keydown')({ key: 'ArrowLeft', preventDefault() {} }); await tick();
  v.ack(v.sent[1]); await tick();
  v.focus({ ...snapshot, nid: 'field', start: 3, end: 3, direction: 'none' });
  assert.equal(field.selectionStart, 3);
  assert.equal(doc.activeElement, field);
  // Reading/editing the viewer URL must not be interrupted by focus updates.
  frame.hostDocument.activeElement = {};
  v.focus({ ...snapshot, nid: 'field', start: 1, end: 1, direction: 'none' });
  assert.equal(field.selectionStart, 3);
});

test('focused mirrored fields commit IME once and forward paste without editing only the mirror', async () => {
  const { frame, field, events } = editableFrame();
  const v = viewerControl(frame); v.snapshot(snapshot); v.state('live');
  events.get('click')({ target: field, clientX: 20, clientY: 20, preventDefault() {} }); await tick();
  v.ack(v.sent[0]); await tick();
  events.get('compositionstart')();
  events.get('beforeinput')({ target: field, inputType: 'insertCompositionText', data: 'intermediate', isComposing: true });
  assert.equal(v.sent.length, 1);
  events.get('compositionend')({ target: field, data: 'привет' });
  events.get('beforeinput')({ inputType: 'insertFromComposition', data: 'привет', preventDefault() {} });
  await tick();
  assert.equal(v.sent[1].payload.text, 'привет');
  v.ack(v.sent[1]); await tick();
  let prevented = false;
  events.get('paste')({ clipboardData: { getData: () => 'pasted text' }, preventDefault() { prevented = true; } });
  await tick();
  assert.ok(prevented);
  assert.equal(v.sent[2].payload.text, 'pasted text');
  assert.equal(v.sent.length, 3);
});
