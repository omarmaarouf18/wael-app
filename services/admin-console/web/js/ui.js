// Shared building blocks for the tab modules: error banner, pager, status
// badge, and the components of the owner redesign 2026-11-14 (buttons, chips,
// avatar initial, card, KPI card, copy-id chip, empty state, skeleton rows,
// side panel). Messages come from i18n by error kind; server text is never
// shown. Colours come from the tokens in style.css, never from here.

import { clear, h } from './dom.js';
import { errorText, formatDate, formatNumber, formatTime, t } from './i18n.js';

export function pageCount(total, limit) {
  if (!(total > 0) || !(limit > 0)) return 1;
  return Math.max(1, Math.ceil(total / limit));
}

/**
 * A wait the server asked for (Retry-After on a 429). While it runs, `active`
 * is true and the action it guards should stay disabled. Subscribers hear the
 * seconds left once a second, and 0 when it ends.
 */
export function createCooldown() {
  let until = 0;
  let timer = 0;
  const listeners = new Set();
  const remaining = () => Math.max(0, Math.ceil((until - Date.now()) / 1000));

  function tick() {
    timer = 0;
    const left = remaining();
    if (left > 0) timer = setTimeout(tick, 1000);
    else until = 0;
    for (const fn of [...listeners]) fn(left);
  }

  return {
    get active() {
      return remaining() > 0;
    },
    get remaining() {
      return remaining();
    },
    /** Starts (or restarts) the wait for `seconds`. */
    start(seconds) {
      if (timer) clearTimeout(timer);
      until = Date.now() + seconds * 1000;
      timer = setTimeout(tick, 1000);
    },
    /** Starts the wait when `res` is a rate-limited answer with a Retry-After. */
    arm(res) {
      if (res && res.kind === 'rate_limited' && res.retryAfter > 0) this.start(res.retryAfter);
    },
    stop() {
      if (timer) clearTimeout(timer);
      timer = 0;
      until = 0;
    },
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
}

// The countdown shown in a banner, so a repaint or a hide can drop it.
const watchers = new WeakMap(); // container -> unsubscribe
// One cooldown per failed answer when the caller supplies none, so repainting
// the same banner does not restart the wait.
const ownCooldowns = new WeakMap(); // res -> cooldown

function unwatch(container) {
  const off = watchers.get(container);
  if (off) off();
  watchers.delete(container);
}

/**
 * Stops Esc (the dialog's cancel event) from closing a dialog while its
 * request is in flight. Closing then would reset the dialog and let the same
 * action be submitted a second time.
 */
export function keepOpenWhile(dialog, isBusy) {
  dialog.addEventListener('cancel', (event) => {
    if (isBusy()) event.preventDefault();
  });
}

/** Fills and shows a banner. A retry button is offered unless the session ended. */
export function showBanner(container, kind, onRetry) {
  showError(container, { kind }, onRetry);
}

/**
 * Fills and shows a banner for a failed request. A known server error code
 * wins over the status kind; raw server text is never shown. A retry button
 * is offered unless the session ended. For a rate-limited answer with a
 * Retry-After the banner counts down ("try again in N seconds") and the retry
 * button stays disabled until it ends; pass the caller's `cooldown` (already
 * armed with arm(res)) so the action the banner belongs to stays disabled too.
 */
export function showError(container, res = {}, onRetry, cooldown) {
  const { code = null, kind = 'unavailable' } = res;
  unwatch(container);
  clear(container);
  let wait = cooldown;
  if (!wait && kind === 'rate_limited' && res.retryAfter > 0) {
    wait = ownCooldowns.get(res);
    if (!wait) {
      wait = createCooldown();
      wait.arm(res);
      ownCooldowns.set(res, wait);
    }
  }
  const text = h('span', { class: 'banner-text' });
  container.append(text);
  let retry = null;
  if (onRetry && kind !== 'unauthorized') {
    retry = h('button', { class: 'btn small', text: t('common.retry'), attrs: { type: 'button' }, on: { click: onRetry } });
    container.append(retry);
  }
  const paint = (left) => {
    text.textContent = left > 0 ? t('err.retryAfter', { n: formatNumber(left) }) : errorText(code, kind);
    if (retry) retry.disabled = left > 0;
  };
  paint(wait ? wait.remaining : 0);
  if (wait && wait.active) {
    const off = wait.subscribe((left) => {
      paint(left);
      if (left === 0) {
        off();
        watchers.delete(container);
      }
    });
    watchers.set(container, off);
  }
  container.hidden = false;
}

export function hideBanner(container) {
  unwatch(container);
  clear(container);
  container.hidden = true;
}

/** Chip tone per account status. */
export const STATUS_TONE = Object.freeze({
  active: 'success',
  suspended: 'warning',
  pending_deletion: 'danger',
  deleted: 'neutral',
});

export function statusBadge(status) {
  const known = Object.hasOwn(STATUS_TONE, status);
  return h('span', {
    class: `badge chip chip-${known ? STATUS_TONE[status] : 'neutral'} ${known ? `badge-${status}` : 'badge-unknown'}`,
    text: known ? t(`status.${status}`) : String(status ?? ''),
  });
}

/**
 * Binds a pager made of [data-role=prev], [data-role=next], [data-role=info]
 * and [data-role=total] elements. update() redraws it in the current language.
 */
export function createPager(root, onPage) {
  const prev = root.querySelector('[data-role=prev]');
  const next = root.querySelector('[data-role=next]');
  const info = root.querySelector('[data-role=info]');
  const total = root.querySelector('[data-role=total]');
  let page = 1;
  let pages = 1;
  prev.addEventListener('click', () => {
    if (page > 1) onPage(page - 1);
  });
  next.addEventListener('click', () => {
    if (page < pages) onPage(page + 1);
  });
  return {
    update({ page: p, total: n, limit }) {
      page = p;
      pages = pageCount(n, limit);
      prev.disabled = page <= 1;
      next.disabled = page >= pages;
      info.textContent = t('common.pageOf', { page: formatNumber(page), pages: formatNumber(pages) });
      total.textContent = t('common.total', { total: formatNumber(n) });
    },
  };
}

/** A table row with one full-width message cell. */
export function messageRow(columns, text) {
  return h('tr', {}, h('td', { class: 'empty', text, attrs: { colspan: columns } }));
}

/** A table cell with the date on one line and the time under it. */
export function dateCell(value) {
  return h(
    'td',
    { class: 'when' },
    h('div', { text: formatDate(value) }),
    h('div', { class: 'muted small', text: formatTime(value) }),
  );
}

const toastTimers = new Map(); // box id -> timer

function stopToastTimer(id) {
  const timer = toastTimers.get(id);
  if (timer) clearTimeout(timer);
  toastTimers.delete(id);
}

/**
 * Shows a short success note. It replaces any previous note in the same box.
 * The default box is the page toast; a dialog that stays open after a save
 * has its own box inside it (the page toast would sit under its backdrop).
 */
export function toast(doc, text, boxId = 'toast') {
  const box = doc.getElementById(boxId);
  if (!box) return;
  stopToastTimer(boxId);
  clear(box);
  box.append(h('span', { text }));
  box.hidden = false;
  toastTimers.set(boxId, setTimeout(() => {
    toastTimers.delete(boxId);
    clear(box);
    box.hidden = true;
  }, 4000));
}

/** Hides a toast immediately. */
export function hideToast(doc, boxId = 'toast') {
  stopToastTimer(boxId);
  const box = doc.getElementById(boxId);
  if (box) {
    clear(box);
    box.hidden = true;
  }
}

// ---------------------------------------------------------------------------
// Components (owner redesign 2026-11-14)
// ---------------------------------------------------------------------------

export const BUTTON_VARIANTS = Object.freeze(['primary', 'secondary', 'success', 'danger', 'danger-outline', 'link']);

/**
 * A button: button(text, { variant, small, onClick, attrs, class }).
 * The type is always "button" unless attrs.type says otherwise.
 */
export function button(text, { variant = 'secondary', small = false, onClick, attrs = {}, class: extra = '' } = {}) {
  const v = BUTTON_VARIANTS.includes(variant) ? variant : 'secondary';
  return h('button', {
    class: ['btn', v, small ? 'small' : '', extra].filter(Boolean).join(' '),
    text,
    attrs: { type: 'button', ...attrs },
    on: onClick ? { click: onClick } : {},
  });
}

export const CHIP_TONES = Object.freeze(['neutral', 'success', 'info', 'warning', 'danger', 'accent']);

/** A small status chip; the tone picks one colour token pair. */
export function chip(text, tone = 'neutral', extra = '') {
  const tn = CHIP_TONES.includes(tone) ? tone : 'neutral';
  return h('span', { class: ['chip', `chip-${tn}`, extra].filter(Boolean).join(' '), text });
}

/** The first letter of a name, for an avatar; '?' when there is none. */
export function initialOf(name) {
  const first = [...String(name ?? '').trim()][0];
  return first ? first.toLocaleUpperCase() : '?';
}

/** A round avatar with the name's initial. Decorative: the name is always shown beside it. */
export function avatar(name, size = 'md') {
  return h('span', { class: `avatar avatar-${size}`, text: initialOf(name), attrs: { 'aria-hidden': 'true' } });
}

/** A surface card. */
export function card(props = {}, ...children) {
  return h(props.tag ?? 'div', { ...props, class: ['card', props.class].filter(Boolean).join(' ') }, ...children);
}

/**
 * A KPI card: label, big value, a one-line hint and a decorative icon glyph.
 * tone tints the icon (neutral, success, info, warning, danger, accent).
 */
export function kpiCard({ id, label, value, hint = '', tone = 'neutral', icon = '•' }) {
  return h('div', { class: `kpi kpi-${tone}`, attrs: { id }, dataset: { kpi: id ?? '' } },
    h('div', { class: 'kpi-head' },
      h('span', { class: 'kpi-label', text: label }),
      h('span', { class: 'kpi-icon', text: icon, attrs: { 'aria-hidden': 'true' } })),
    h('span', { class: 'kpi-value', text: value }),
    hint ? h('span', { class: 'kpi-hint muted small', text: hint }) : null,
  );
}

/** First 8 characters of an id with an ellipsis; '' for none. */
export function shortId(id) {
  const s = String(id ?? '');
  return s.length > 10 ? `${s.slice(0, 8)}…` : s;
}

/** Copies text to the clipboard and confirms with a toast; silent when unavailable. */
export function copyText(doc, text) {
  const clip = globalThis.navigator?.clipboard;
  if (!clip || typeof clip.writeText !== 'function') return Promise.resolve(false);
  return clip.writeText(String(text)).then(() => {
    toast(doc, t('common.copied'));
    return true;
  }, () => false);
}

/** A copy-id chip: the short id (LTR) and a copy action for the full id. */
export function copyIdChip(doc, id) {
  return h('button', {
    class: 'copy-chip',
    attrs: { type: 'button', title: String(id ?? ''), 'aria-label': t('common.copyIdFull', { id: String(id ?? '') }) },
    on: { click: () => copyText(doc, id) },
  },
  h('bdi', { class: 'id ltr', text: shortId(id) }),
  h('span', { class: 'copy-chip-label', text: t('common.copy') }));
}

/** An empty state: a decorative icon and one line that explains it. */
export function emptyState(text, icon = '○') {
  return h('div', { class: 'empty-state' },
    h('span', { class: 'empty-icon', text: icon, attrs: { 'aria-hidden': 'true' } }),
    h('p', { class: 'muted', text }));
}

/** Placeholder rows while a list loads; announced once as "loading". */
export function skeletonRows(count = 3) {
  const box = h('div', { class: 'skeleton', attrs: { role: 'status' } },
    h('span', { class: 'visually-hidden', text: t('common.loading') }));
  for (let i = 0; i < count; i++) box.append(h('span', { class: 'skeleton-row', attrs: { 'aria-hidden': 'true' } }));
  return box;
}

/**
 * A side panel (drawer) already in the page: open() shows it and moves focus
 * to its close button, close() hides it and returns focus. Esc closes it.
 * On a wide screen it sits beside the list; below 900px the stylesheet turns
 * it into a full-screen drawer.
 */
export function createSidePanel({ root, closeButton, onClose = () => {} }) {
  let returnTo = null;
  let isOpen = false;
  function close() {
    if (!isOpen) return;
    isOpen = false;
    root.hidden = true;
    root.setAttribute('aria-hidden', 'true');
    onClose();
    if (returnTo && typeof returnTo.focus === 'function') returnTo.focus();
    returnTo = null;
  }
  closeButton?.addEventListener('click', close);
  root.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && isOpen) {
      event.preventDefault?.();
      close();
    }
  });
  root.hidden = true;
  return {
    get isOpen() {
      return isOpen;
    },
    open(from = null) {
      returnTo = from ?? root.ownerDocument?.activeElement ?? null;
      isOpen = true;
      root.hidden = false;
      root.setAttribute('aria-hidden', 'false');
      closeButton?.focus?.();
    },
    close,
    /** Hides without moving focus (sign-out, reset). */
    dismiss() {
      isOpen = false;
      returnTo = null;
      root.hidden = true;
      root.setAttribute('aria-hidden', 'true');
    },
  };
}
