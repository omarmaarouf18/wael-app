// Shared building blocks for the tab modules: error banner, pager, status
// badge. Messages come from i18n by error kind; server text is never shown.

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

export function statusBadge(status) {
  const known = status === 'active' || status === 'suspended' || status === 'deleted' || status === 'pending_deletion';
  return h('span', {
    class: `badge ${known ? `badge-${status}` : 'badge-unknown'}`,
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

let toastTimer = 0;

/** Shows a short success note. It replaces any previous toast. */
export function toast(doc, text) {
  const box = doc.getElementById('toast');
  if (!box) return;
  if (toastTimer) {
    clearTimeout(toastTimer);
    toastTimer = 0;
  }
  clear(box);
  box.append(h('span', { text }));
  box.hidden = false;
  toastTimer = setTimeout(() => {
    toastTimer = 0;
    clear(box);
    box.hidden = true;
  }, 4000);
}

/** Hides the toast immediately. */
export function hideToast(doc) {
  if (toastTimer) {
    clearTimeout(toastTimer);
    toastTimer = 0;
  }
  const box = doc.getElementById('toast');
  if (box) {
    clear(box);
    box.hidden = true;
  }
}
