// The tab registry. Requests and Files are declared but not enabled: their
// buttons stay hidden until the APIs behind them exist (SPEC Phase 4.5/4.6
// and Phase 5). Enabling one means adding its module and flipping `enabled`.

export const TABS = Object.freeze([
  Object.freeze({ id: 'accounts', enabled: true }),
  Object.freeze({ id: 'audit', enabled: true }),
  Object.freeze({ id: 'requests', enabled: true, badge: true }),
  Object.freeze({ id: 'catalog', enabled: true }),
  Object.freeze({ id: 'files', enabled: false }),
]);

export function visibleTabs() {
  return TABS.filter((tab) => tab.enabled);
}

export function isTabEnabled(id) {
  return TABS.some((tab) => tab.id === id && tab.enabled);
}

/** Text for a badge counter: empty when there is nothing to show. */
export function formatBadge(count) {
  if (!Number.isInteger(count) || count <= 0) return '';
  return count > 99 ? '99+' : String(count);
}

/**
 * Index of the tab to focus after an arrow key. Arrow keys follow the visual
 * direction, so in right-to-left the Left arrow moves forward.
 */
export function nextTabIndex(current, count, key, dir) {
  if (count <= 0) return -1;
  if (key === 'Home') return 0;
  if (key === 'End') return count - 1;
  const forward = dir === 'rtl' ? 'ArrowLeft' : 'ArrowRight';
  const backward = dir === 'rtl' ? 'ArrowRight' : 'ArrowLeft';
  if (key === forward) return (current + 1) % count;
  if (key === backward) return (current - 1 + count) % count;
  return current;
}

/** Shows a count on a tab's badge, or hides the badge when there is none. */
export function setBadge(doc, id, count) {
  const badge = doc.getElementById(`badge-${id}`);
  if (!badge) return;
  const text = formatBadge(count);
  badge.textContent = text;
  badge.hidden = text === '';
}
