// A very small DOM stand-in for the node tests: enough surface for the
// console's modules (no jsdom, no npm). getElementById and querySelector
// create the element on first use, so tests do not have to rebuild
// index.html; static.test.mjs separately checks that every id the JS asks for
// exists in index.html.

export class FakeElement {
  constructor(tag = 'div', ownerDocument = null) {
    this.tagName = tag.toUpperCase();
    this.ownerDocument = ownerDocument;
    this.children = [];
    this.attrs = new Map();
    this.dataset = {};
    this.listeners = new Map();
    this.hidden = false;
    this.disabled = false;
    this.open = false;
    this.value = '';
    this.className = '';
    this.tabIndex = 0;
    this.lang = '';
    this.dir = '';
    this._text = '';
    this._queries = new Map();
  }

  get textContent() {
    return this._text + this.children.map((c) => (typeof c === 'string' ? c : c.textContent)).join('');
  }

  set textContent(v) {
    this.children = [];
    this._text = String(v);
  }

  setAttribute(name, value) {
    this.attrs.set(name, String(value));
  }

  getAttribute(name) {
    return this.attrs.has(name) ? this.attrs.get(name) : null;
  }

  append(...nodes) {
    for (const n of nodes) this.children.push(n);
  }

  replaceChildren(...nodes) {
    this.children = [...nodes];
    this._text = '';
  }

  addEventListener(type, fn) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(fn);
  }

  /** Runs the listeners for type and returns the event object they received. */
  fire(type, extra = {}) {
    const event = { type, target: this, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, ...extra };
    for (const fn of this.listeners.get(type) ?? []) fn(event);
    return event;
  }

  click() {
    if (!this.disabled) this.fire('click');
  }

  focus() {
    if (this.ownerDocument) this.ownerDocument.activeElement = this;
  }

  showModal() {
    this.open = true;
  }

  close() {
    if (!this.open) return;
    this.open = false;
    this.fire('close');
  }

  requestSubmit() {
    this.fire('submit');
  }

  querySelector(selector) {
    if (!this._queries.has(selector)) this._queries.set(selector, new FakeElement('div', this.ownerDocument));
    return this._queries.get(selector);
  }

  querySelectorAll() {
    return [];
  }

  /** All descendants (depth first) matching predicate. */
  findAll(predicate, out = []) {
    for (const c of this.children) {
      if (typeof c === 'string') continue;
      if (predicate(c)) out.push(c);
      c.findAll(predicate, out);
    }
    return out;
  }
}

export class FakeDocument {
  constructor() {
    this.elements = new Map();
    this.documentElement = new FakeElement('html', this);
    this.activeElement = null;
    this.title = '';
  }

  getElementById(id) {
    if (!this.elements.has(id)) this.elements.set(id, new FakeElement('div', this));
    return this.elements.get(id);
  }

  querySelector(selector) {
    return this.getElementById(`query:${selector}`);
  }

  /** Supports the three [data-i18n*] selectors: every element tagged with that dataset key. */
  querySelectorAll(selector) {
    const key = { '[data-i18n]': 'i18n', '[data-i18n-placeholder]': 'i18nPlaceholder', '[data-i18n-aria-label]': 'i18nAriaLabel' }[selector];
    if (!key) return [];
    return [...this.elements.values()].filter((el) => el.dataset[key] !== undefined);
  }

  createElement(tag) {
    return new FakeElement(tag, this);
  }
}

export function fakeWindow(hash = '') {
  const listeners = new Map();
  const win = {
    location: { hash },
    history: {
      replaceState(_state, _title, url) {
        win.location.hash = url;
      },
    },
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    removeEventListener(type, fn) {
      const list = listeners.get(type) ?? [];
      const idx = list.indexOf(fn);
      if (idx !== -1) list.splice(idx, 1);
    },
    setTimeout(...args) {
      const t = setTimeout(...args);
      if (t && typeof t.unref === 'function') t.unref();
      return t;
    },
    clearTimeout(...args) { return clearTimeout(...args); },
    setInterval(...args) {
      const t = setInterval(...args);
      if (t && typeof t.unref === 'function') t.unref();
      return t;
    },
    clearInterval(...args) { return clearInterval(...args); },
  };
  return win;
}

/** Resolves after pending promise callbacks and timers have run. */
export function flush() {
  return new Promise((resolve) => setImmediate(resolve));
}

/** A fetch stand-in. routes maps "METHOD /path" to a response spec or function. */
export function makeFetch(routes) {
  const calls = [];
  async function fetchFn(url, init = {}) {
    const method = init.method ?? 'GET';
    const path = String(url).split('?')[0];
    calls.push({ method, url: String(url), path, init });
    const handler = routes[`${method} ${path}`];
    if (!handler) return jsonResponse(404, { code: 'not_found', error: 'SECRET not found text' });
    const spec = typeof handler === 'function' ? await handler({ method, url: String(url), init }) : handler;
    if (spec instanceof Error) throw spec;
    return jsonResponse(spec.status ?? 200, spec.body ?? {}, spec.headers);
  }
  fetchFn.calls = calls;
  return fetchFn;
}

export function jsonResponse(status, body, headers = {}) {
  const lower = Object.fromEntries(Object.entries(headers).map(([name, value]) => [name.toLowerCase(), String(value)]));
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (name) => lower[String(name).toLowerCase()] ?? null },
    async json() {
      return body;
    },
  };
}
