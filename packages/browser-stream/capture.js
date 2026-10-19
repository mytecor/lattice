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
