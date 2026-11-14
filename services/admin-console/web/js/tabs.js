// The tab registry, in side-navigation order. A tab whose API does not exist
// yet is declared with `enabled: false` and its button stays hidden; enabling
// one means adding its module and flipping `enabled`. Every tab is enabled
// since Files (SPEC Phase 5); "iOS packages" (iap) joined in #56; "Overview"
// joined first in the owner redesign of 2026-11-14 and is the default tab.

import { formatNumber } from './i18n.js';

export const TABS = Object.freeze([
  Object.freeze({ id: 'overview', enabled: true }),
  Object.freeze({ id: 'accounts', enabled: true }),
  Object.freeze({ id: 'audit', enabled: true }),
  Object.freeze({ id: 'requests', enabled: true, badge: true }),
  Object.freeze({ id: 'catalog', enabled: true }),
  Object.freeze({ id: 'iap', enabled: true }),
  Object.freeze({ id: 'settings', enabled: true }),
  Object.freeze({ id: 'files', enabled: true }),
  Object.freeze({ id: 'notifications', enabled: true }),
]);

/** The tab shown after sign-in. */
export const DEFAULT_TAB = 'overview';

export function visibleTabs() {
  return TABS.filter((tab) => tab.enabled);
}

export function isTabEnabled(id) {
  return TABS.some((tab) => tab.id === id && tab.enabled);
}

/** Text for a badge counter in the page's digits: empty when there is nothing to show. */
export function formatBadge(count) {
  if (!Number.isInteger(count) || count <= 0) return '';
  return count > 99 ? `${formatNumber(99)}+` : formatNumber(count);
}

/**
 * Index of the tab to focus after an arrow key. The navigation is a vertical
 * list, so Down/Up move forward/back; Left/Right follow the visual direction
 * as well (in right-to-left the Left arrow moves forward).
 */
export function nextTabIndex(current, count, key, dir) {
  if (count <= 0) return -1;
  if (key === 'Home') return 0;
  if (key === 'End') return count - 1;
  const forward = dir === 'rtl' ? 'ArrowLeft' : 'ArrowRight';
  const backward = dir === 'rtl' ? 'ArrowRight' : 'ArrowLeft';
  if (key === forward || key === 'ArrowDown') return (current + 1) % count;
  if (key === backward || key === 'ArrowUp') return (current - 1 + count) % count;
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
