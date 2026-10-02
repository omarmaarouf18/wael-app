// Tiny DOM helpers. Text is always set through textContent, never as HTML, so
// data from the API cannot inject markup (the page also forbids inline script
// through its CSP).

/**
 * h('button', { class: 'btn', text: 'Save', attrs: { type: 'button' },
 *                on: { click: fn } }, ...children)
 */
export function h(tag, props = {}, ...children) {
  const el = document.createElement(tag);
  if (props.class) el.className = props.class;
  if (props.text !== undefined) el.textContent = props.text;
  for (const [name, value] of Object.entries(props.attrs ?? {})) {
    if (value !== undefined && value !== null && value !== false) el.setAttribute(name, value === true ? '' : String(value));
  }
  for (const [name, value] of Object.entries(props.dataset ?? {})) {
    el.dataset[name] = value;
  }
  for (const [name, fn] of Object.entries(props.on ?? {})) {
    el.addEventListener(name, fn);
  }
  for (const child of children) {
    if (child) el.append(child);
  }
  return el;
}

export function clear(node) {
  node.replaceChildren();
}

export function setHidden(node, hidden) {
  if (node) node.hidden = hidden;
}
