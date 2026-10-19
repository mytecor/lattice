import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Script, runInNewContext } from 'node:vm';
import { webcrypto } from 'node:crypto';
import { copyFormState, readFocus } from './capture.js';
import { captureSource } from './streams.js';

function clone(attributes = {}) {
  return { attributes: { ...attributes }, textContent: '',
    setAttribute(name, value) { this.attributes[name] = String(value); },
    removeAttribute(name) { delete this.attributes[name]; },
  };
}

test('snapshot serializes current form properties instead of initial HTML defaults', () => {
  const input = clone({ value: 'initial', checked: '' });
  copyFormState({ tagName: 'INPUT', type: 'text', value: 'привет "мир"', checked: false }, input);
  assert.equal(input.attributes.value, 'привет "мир"');
  assert.equal('checked' in input.attributes, false);
  const textarea = clone(); textarea.textContent = 'initial';
  copyFormState({ tagName: 'TEXTAREA', value: 'edited\ntext' }, textarea);
  assert.equal(textarea.textContent, 'edited\ntext');
  const option = clone({ selected: '' });
  copyFormState({ tagName: 'OPTION', selected: false }, option);
  assert.equal('selected' in option.attributes, false);
  copyFormState({ tagName: 'OPTION', selected: true }, option);
  assert.equal('selected' in option.attributes, true);
});

test('an existing agent tab replaces legacy capture once without reloading its document', () => {
  let stops = 0;
  const window = {
    top: {}, __latticeControlInstalled: true, __phantomStreamInjected: true,
    __phantomStreamStop: () => stops++,
    __latticePhantomQueue: { frames: ['old snapshot'], bytes: 12 },
  };
  const context = { window, document: {}, crypto: webcrypto };
  // The upstream script ignores subframes; this exercises the host bootstrap
  // against a legacy queue without requiring a DOM implementation in Node.
  runInNewContext(captureSource, context);
  assert.equal(stops, 1);
  assert.equal(window.__phantomStreamInjected, false);
  assert.equal(window.__latticePhantomQueue.frames.length, 0);
  const generation = window.__latticePhantomQueue.documentId;
  assert.ok(generation);
  runInNewContext(captureSource, context);
  assert.equal(stops, 1);
  assert.equal(window.__latticePhantomQueue.documentId, generation);
});

test('snapshot never copies a file input path and the generated capture script parses', () => {
  const file = clone();
  copyFormState({ tagName: 'INPUT', type: 'file', value: 'private-path', checked: false }, file);
  assert.equal('value' in file.attributes, false);
  assert.doesNotThrow(() => new Script(captureSource));
});


test('focus channel exposes caret positions and identity without reading field contents', () => {
  const field = { tagName: 'INPUT', selectionStart: 2, selectionEnd: 5, selectionDirection: 'backward',
    get value() { throw new Error('focus metadata must not read a value'); } };
  const result = runInNewContext(`(${readFocus.toString()})({ documentId: 'doc', snapshotId: 1 })`, {
    document: { activeElement: field }, window: { __phantomStreamGetNodeId: () => 'input-node' },
  });
  assert.equal(result.nid, 'input-node');
  assert.equal(result.start, 2);
  assert.equal(result.end, 5);
  assert.equal(result.documentId, 'doc');
  assert.equal('value' in result, false);
});
