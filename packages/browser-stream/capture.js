import { getPlaywrightInjectSource } from '@full-self-browsing/phantom-stream/adapters/playwright';

// PhantomStream 0.2.1 serializes clone.innerHTML, which loses live properties
// such as input.value. Copy them before upstream sanitization/masking runs.
export function copyFormState(original, clone) {
  const tag = original.tagName.toLowerCase();
  if (tag === 'input') {
    if (original.type !== 'file') clone.setAttribute('value', original.value);
    if (original.checked) clone.setAttribute('checked', '');
    else clone.removeAttribute('checked');
  } else if (tag === 'textarea') {
    clone.textContent = original.value;
  } else if (tag === 'option') {
    if (original.selected) clone.setAttribute('selected', '');
    else clone.removeAttribute('selected');
  }
}

export function captureScript() {
  const source = getPlaywrightInjectSource();
  const anchor = "var tag = cl.tagName ? cl.tagName.toLowerCase() : '';";
  if (source.split(anchor).length !== 2) throw new Error('PhantomStream snapshot patch needs review');
  return source.replace(anchor, `${anchor}\n      (${copyFormState.toString()})(orig, cl);`);
}


// Metadata only: never copy a field value into the focus side channel.
export function readFocus(identity) {
  let element = document.activeElement;
  try {
    while (element?.tagName === 'IFRAME' && element.contentDocument) element = element.contentDocument.activeElement;
  } catch { /* cross-origin frame */ }
  const editable = element && (element.tagName === 'INPUT' || element.tagName === 'TEXTAREA' || element.isContentEditable);
  return { ...identity, nid: editable ? window.__phantomStreamGetNodeId?.(element) || null : null,
    start: editable && Number.isInteger(element.selectionStart) ? element.selectionStart : null,
    end: editable && Number.isInteger(element.selectionEnd) ? element.selectionEnd : null,
    direction: editable ? element.selectionDirection || 'none' : 'none' };
}
