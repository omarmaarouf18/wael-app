// Owner redesign 2026-11-14, parts A and B: the side navigation shell, the
// narrow-screen drawer, the one numeral rule, and the shared components.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const i18n = await import('../js/i18n.js');
const ui = await import('../js/ui.js');

const { MESSAGES, setLang } = i18n;
const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;
const $ = (id) => doc.getElementById(id);

function routes() {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { body: { items: [], total: 0 } },
    'GET /api/requests': { body: { items: [], total: 0, pending_count: 13 } },
    'GET /api/subjects': { body: { items: [], total: 0 } },
    'GET /api/levels': { body: { levels: [] } },
    'GET /api/audit': { body: { items: [], total: 0 } },
    'GET /api/iap/packages': { body: { packages: [] } },
    'GET /api/request-groups': { body: { items: [], total_students: 0, pending_count: 13 } },
  };
}

const app = main(doc, win);

test('after sign-in the overview is the default tab and the pending badge uses Arabic digits', async () => {
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
  assert.equal(app.activeTab, 'overview');
  assert.equal($('tab-overview').getAttribute('aria-selected'), 'true');
  assert.equal($('panel-overview').hidden, false);
  for (const id of ['accounts', 'audit', 'requests', 'catalog', 'iap', 'settings', 'files', 'notifications']) {
    assert.equal($(`panel-${id}`).hidden, true, id);
    assert.equal($(`tab-${id}`).getAttribute('aria-selected'), 'false', id);
  }
  assert.equal($('badge-requests').textContent, '١٣');
  assert.equal($('badge-requests').hidden, false);
  assert.equal($('admin-name').textContent, 'Wael');
  assert.equal($('admin-initial').textContent, 'W');
});

test('arrow Down moves to the next tab in the vertical navigation', async () => {
  $('tab-overview').focus();
  const event = $('tabs').fire('keydown', { key: 'ArrowDown' });
  await flush();
  assert.equal(event.defaultPrevented, true);
  assert.equal(app.activeTab, 'accounts');
  $('tab-overview').click();
  await flush();
  assert.equal(app.activeTab, 'overview');
});

test('narrow screens: the menu button opens the navigation as a drawer, Esc and the scrim close it', async () => {
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');
  assert.equal($('nav-open').getAttribute('aria-expanded'), 'false');
  assert.equal($('nav-scrim').hidden, true);

  $('nav-open').click();
  assert.equal($('app-view').getAttribute('data-nav'), 'open');
  assert.equal($('nav-open').getAttribute('aria-expanded'), 'true');
  assert.equal($('nav-scrim').hidden, false);
  assert.equal(doc.activeElement, $('tab-overview'), 'focus moves into the drawer');

  const esc = $('sidenav').fire('keydown', { key: 'Escape' });
  assert.equal(esc.defaultPrevented, true);
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');
  assert.equal(doc.activeElement, $('nav-open'), 'focus returns to the menu button');

  $('nav-open').click();
  $('nav-scrim').click();
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');

  $('nav-open').click();
  $('nav-close').click();
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');
});

test('choosing a tab from the drawer closes it', async () => {
  $('nav-open').click();
  $('tab-audit').click();
  await flush();
  assert.equal(app.activeTab, 'audit');
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');
  $('tab-overview').click();
  await flush();
});

test('sign-out closes the drawer and clears the admin name and initial', async () => {
  $('nav-open').click();
  $('sign-out').click();
  await flush();
  assert.equal(isSignedIn(), false);
  assert.equal($('app-view').getAttribute('data-nav'), 'closed');
  assert.equal($('admin-initial').textContent, '');
});

// --- numerals ---------------------------------------------------------------

test('one numeral rule: Arabic-Indic digits in Arabic, Latin in English', () => {
  setLang('ar');
  assert.equal(i18n.formatNumber(1650), '١٬٦٥٠');
  assert.equal(i18n.formatMoney(550), '٥٥٠ جنيه');
  assert.match(i18n.formatDate('2026-11-14T10:00:00Z'), /[٠-٩]/);
  assert.doesNotMatch(i18n.formatDate('2026-11-14T10:00:00Z'), /[0-9]/);
  assert.match(i18n.formatTime('2026-11-14T10:00:00Z'), /[٠-٩]/);
  assert.equal(i18n.locale(), 'ar-EG');
  setLang('en');
  assert.equal(i18n.formatNumber(1650), '1,650');
  assert.equal(i18n.formatMoney(550), '550 EGP');
  assert.doesNotMatch(i18n.formatDate('2026-11-14T10:00:00Z'), /[٠-٩]/);
  assert.equal(i18n.locale(), 'en-GB');
  setLang('ar');
});

test('formatAge reads minutes, hours, then days, in the page digits', () => {
  const now = new Date('2026-11-14T12:00:00Z');
  setLang('en');
  assert.equal(i18n.formatAge('2026-11-14T11:55:00Z', now), '5 minutes ago');
  assert.equal(i18n.formatAge('2026-11-14T09:00:00Z', now), '3 hours ago');
  assert.equal(i18n.formatAge('2026-11-12T12:00:00Z', now), '2 days ago');
  assert.equal(i18n.formatAge('nope', now), '');
  setLang('ar');
  assert.match(i18n.formatAge('2026-11-11T12:00:00Z', now), /٣/);
  assert.equal(i18n.formatMoney('not a number'), '٠ جنيه');
});

test('ids, phones and emails are never formatted: they stay as given', () => {
  setLang('ar');
  assert.equal(ui.shortId('8031e2b7-aaaa-4bbb-8ccc-0123456789ab'), '8031e2b7…');
  const chipEl = ui.copyIdChip(doc, '8031e2b7-aaaa-4bbb-8ccc-0123456789ab');
  assert.ok(chipEl.textContent.includes('8031e2b7…'));
  assert.ok(!/[٠-٩]/.test(chipEl.textContent));
});

// --- components -------------------------------------------------------------

test('button variants fall back to secondary and always have type=button', () => {
  const b = ui.button('Go', { variant: 'success', small: true });
  assert.equal(b.className, 'btn success small');
  assert.equal(b.getAttribute('type'), 'button');
  assert.equal(ui.button('x', { variant: 'weird' }).className, 'btn secondary');
});

test('status chip, avatar initial, empty state and skeleton', () => {
  setLang('ar');
  const active = ui.statusBadge('active');
  assert.match(active.className, /chip-success/);
  assert.equal(active.textContent, MESSAGES.ar['status.active']);
  assert.match(ui.statusBadge('suspended').className, /chip-warning/);
  assert.match(ui.statusBadge('zzz').className, /chip-neutral/);
  assert.equal(ui.chip('x', 'nope').className, 'chip chip-neutral');

  assert.equal(ui.initialOf(' هالة وائل'), 'ه');
  assert.equal(ui.initialOf('wael'), 'W');
  assert.equal(ui.initialOf(''), '?');
  assert.equal(ui.avatar('Mona').getAttribute('aria-hidden'), 'true');

  const empty = ui.emptyState('لا يوجد شيء');
  assert.ok(empty.textContent.includes('لا يوجد شيء'));
  const sk = ui.skeletonRows(4);
  assert.equal(sk.getAttribute('role'), 'status');
  assert.equal(sk.findAll((c) => c.className === 'skeleton-row').length, 4);
});

test('KPI card shows label, value and hint', () => {
  const k = ui.kpiCard({ id: 'kpi-x', label: 'L', value: '١٣', hint: 'H', tone: 'accent', icon: '!' });
  assert.equal(k.className, 'kpi kpi-accent');
  assert.ok(k.textContent.includes('L'));
  assert.ok(k.textContent.includes('١٣'));
  assert.ok(k.textContent.includes('H'));
});

test('copy chip copies the full id and confirms with a toast', async () => {
  const written = [];
  const nav = globalThis.navigator;
  Object.defineProperty(globalThis, 'navigator', { value: { clipboard: { writeText: async (s) => { written.push(s); } } }, configurable: true });
  try {
    ui.copyIdChip(doc, 'abcdef0123456789').click();
    await flush();
    assert.deepEqual(written, ['abcdef0123456789']);
    assert.equal($('toast').hidden, false);
    assert.ok($('toast').textContent.includes(MESSAGES.ar['common.copied']));
  } finally {
    Object.defineProperty(globalThis, 'navigator', { value: nav, configurable: true });
    ui.hideToast(doc);
  }
});

test('side panel opens, moves focus to close, and Esc closes and returns focus', () => {
  const root = doc.createElement('aside');
  const closeBtn = doc.createElement('button');
  const opener = doc.createElement('button');
  let closed = 0;
  const panel = ui.createSidePanel({ root, closeButton: closeBtn, onClose: () => { closed += 1; } });
  assert.equal(root.hidden, true);
  panel.open(opener);
  assert.equal(panel.isOpen, true);
  assert.equal(root.hidden, false);
  assert.equal(doc.activeElement, closeBtn);
  root.fire('keydown', { key: 'Escape' });
  assert.equal(panel.isOpen, false);
  assert.equal(root.hidden, true);
  assert.equal(closed, 1);
  assert.equal(doc.activeElement, opener);
  panel.open(opener);
  closeBtn.click();
  assert.equal(closed, 2);
});
