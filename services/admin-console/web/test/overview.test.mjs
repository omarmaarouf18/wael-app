// Overview tab (owner redesign 2026-11-14): KPI cards from existing endpoints,
// a hidden card when its endpoint fails, the pending list with inline
// accept/reject through the Requests tab's bulk dialogs, and the latest
// academy activity as sentences.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES, formatMoney, formatNumber, setLang } = await import('../js/i18n.js');
const { countNewStudents, summarizeGroup, groupFlat } = await import('../js/overview.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;
const $ = (id) => doc.getElementById(id);
const t = (key, p) => {
  const raw = MESSAGES.ar[key];
  return p ? raw.replace(/\{(\w+)\}/g, (_m, n) => String(p[n])) : raw;
};
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const byId = (root, id) => root.findAll((c) => c.attrs?.get?.('id') === id)[0];

const NOW = Date.now();
const daysAgo = (n) => new Date(NOW - n * 24 * 60 * 60 * 1000).toISOString();

const GROUPS = {
  items: [
    { user_id: 'u-1', orders: [{ order_id: 'o1', source: 'web', total: 1100, items: [
      { request_id: 'r1', subject_id: 's1', title_ar: 'أحوال شخصية', level_name_ar: 'الفرقة الثانية', price: 550, status: 'pending', created_at: daysAgo(2) },
      { request_id: 'r2', subject_id: 's2', title_ar: 'قانون مدني', level_name_ar: 'الفرقة الثانية', price: 550, status: 'pending', created_at: daysAgo(1) },
    ] }] },
    { user_id: 'u-2', orders: [{ order_id: '', source: 'mobile', total: 550, items: [
      { request_id: 'r3', subject_id: 's3', title_ar: 'جنائي', level_name_ar: 'الفرقة الأولى', price: 550, status: 'pending', created_at: daysAgo(3) },
    ] }] },
  ],
  total_students: 2,
  pending_count: 3,
};

let bulkBodies = [];
function routes(over = {}) {
  return {
    'GET /api/whoami': { body: { name: 'wael' } },
    'GET /api/accounts': ({ url }) => {
      if (url.includes('ids=')) {
        return { body: { items: [{ id: 'u-1', full_name: 'رقية مصطفى', email: 'r@example.com' }, { id: 'u-2', full_name: 'ندى', email: 'n@example.com' }], total: 2 } };
      }
      if (url.includes('status=active')) return { body: { items: [], total: 42 } };
      return { body: { items: [{ id: 'a', created_at: daysAgo(1) }, { id: 'b', created_at: daysAgo(3) }, { id: 'c', created_at: daysAgo(30) }], total: 3 } };
    },
    'GET /api/requests': { body: { items: [], total: 3, pending_count: 3 } },
    'GET /api/request-groups': { body: GROUPS },
    'GET /api/subjects': ({ url }) => ({ body: url.includes('published=true') ? { items: [], total: 9 } : { items: [{ id: 's1', title_ar: 'أحوال شخصية' }], total: 1 } }),
    'GET /api/levels': { body: { levels: [] } },
    'GET /api/iap/packages': { body: { packages: [{ id: 'pkg4', product_id: 'com.wael.app.pkg.year4', title_ar: 'الفرقة الرابعة' }] } },
    'GET /api/audit': { body: { items: [
      { id: 'e1', actor_name: 'wael', action: 'iap_package_update', target_type: 'iap_package', target_id: 'pkg4', created_at: daysAgo(0) },
      { id: 'e2', actor_name: 'wael', action: 'request_accept', target_type: 'request', target_id: 'r9', detail: 'subject_id=s1 user_id=u-9', created_at: daysAgo(1) },
    ], total: 2 } },
    'POST /api/requests/bulk': ({ init }) => {
      const body = JSON.parse(init.body);
      bulkBodies.push(body);
      return { body: { results: body.ids.map((id) => ({ id, status: body.action === 'accept' ? 'accepted' : 'rejected' })) } };
    },
    ...over,
  };
}

const app = main(doc, win);

async function signIn(over) {
  setLang('ar');
  globalThis.fetch = makeFetch(routes(over));
  $('login-token').value = 'tok';
  $('login-form').fire('submit');
  await flush();
  await flush();
  await flush();
  assert.equal(isSignedIn(), true);
}

async function signOut() {
  $('sign-out').click();
  await flush();
}

test('countNewStudents counts the last 7 days and marks a full sample as a lower bound', () => {
  const now = new Date(NOW);
  assert.deepEqual(countNewStudents({ ok: true, data: { items: [{ created_at: daysAgo(1) }, { created_at: daysAgo(9) }], total: 2 } }, now), { n: 1, more: false });
  const full = Array.from({ length: 100 }, () => ({ created_at: daysAgo(1) }));
  assert.deepEqual(countNewStudents({ ok: true, data: { items: full, total: 500 } }, now), { n: 100, more: true });
  assert.equal(countNewStudents({ ok: false }), null);
});

test('summarizeGroup and groupFlat give one row per student with subject count, level, amount and the oldest request', () => {
  const row = summarizeGroup(GROUPS.items[0], new Map([['u-1', { full_name: 'رقية' }]]));
  assert.equal(row.name, 'رقية');
  assert.equal(row.subjects, 2);
  assert.equal(row.level, 'الفرقة الثانية');
  assert.equal(row.amount, 1100);
  assert.deepEqual(row.requests.map((r) => r.id), ['r1', 'r2']);
  assert.equal(Date.parse(row.oldest), Date.parse(daysAgo(2)));
  const flat = groupFlat([{ id: 'x1', user_id: 'u', price: 5 }, { id: 'x2', user_id: 'u', price: 7 }]);
  assert.equal(flat.length, 1);
  assert.equal(summarizeGroup(flat[0]).amount, 12);
});

test('the overview shows the four cards from existing endpoints, in Arabic digits', async () => {
  await signIn();
  assert.equal(app.activeTab, 'overview');
  const body = $('overview-body');
  const card = (id) => byId(body, id);
  assert.ok(card('kpi-pending').textContent.includes(formatNumber(3)));
  assert.ok(card('kpi-pending').textContent.includes('٣'));
  assert.ok(card('kpi-new').textContent.includes(formatNumber(2)), 'two accounts within 7 days');
  assert.ok(card('kpi-active').textContent.includes('٤٢'));
  assert.ok(card('kpi-published').textContent.includes('٩'));
  assert.ok($('overview-greeting').textContent.includes('wael'));
  const urls = globalThis.fetch.calls.map((c) => c.url);
  assert.ok(urls.includes('/api/accounts?status=active&page=1&limit=1'));
  assert.ok(urls.includes('/api/subjects?published=true&page=1&limit=1'));
  assert.ok(urls.includes('/api/accounts?page=1&limit=100'));
  assert.ok(urls.includes('/api/request-groups?status=pending'));
  assert.ok(urls.includes('/api/audit?source=academy&page=1&limit=6'));
});

test('owner rule: no Apple or in-app purchase card, number or warning on the overview', async () => {
  const grid = $('overview-body').findAll((c) => c.className === 'kpi-grid')[0];
  assert.equal(grid.findAll((c) => /^kpi kpi-/.test(c.className)).length, 4, 'exactly the four cards');
  for (const word of ['Apple', 'آبل', 'iOS', 'IAP', '⚠']) assert.ok(!grid.textContent.includes(word), word);
  assert.ok(!$('overview-body').textContent.includes('⚠'), 'no warning anywhere on the overview');
  assert.ok(!globalThis.fetch.calls.some((c) => c.path === '/api/iap/purchases' || c.path === '/api/iap/coverage'));
});

test('a card whose endpoint fails is hidden, not shown as zero', async () => {
  await signOut();
  await signIn({ 'GET /api/subjects': { status: 503, body: {} } });
  const body = $('overview-body');
  assert.equal(byId(body, 'kpi-published'), undefined);
  assert.ok(byId(body, 'kpi-pending'));
  assert.ok(byId(body, 'kpi-active'));
});

test('pending list: student, subject count, level, amount and age; names link to the student', async () => {
  await signOut();
  await signIn();
  const list = byId($('overview-body'), 'overview-pending-list');
  assert.equal(list.children.length, 2);
  const first = list.children[0].textContent;
  assert.ok(first.includes('رقية مصطفى'));
  assert.ok(first.includes(t('overview.subjects', { n: formatNumber(2) })));
  assert.ok(first.includes('الفرقة الثانية'));
  assert.ok(first.includes(formatMoney(1100)));
  assert.match(first, /يوم|أمس/);
  const nameBtn = buttonsIn(list.children[0]).find((b) => b.className.includes('name-link'));
  nameBtn.click();
  await flush();
  assert.equal(app.activeTab, 'accounts');
  $('tab-overview').click();
  await flush();
  await flush();
  await flush();
});

test('inline accept goes through the bulk confirm dialog with that student\'s requests, then reloads', async () => {
  bulkBodies = [];
  const list = () => byId($('overview-body'), 'overview-pending-list');
  const accept = buttonsIn(list().children[0]).find((b) => b.textContent === t('requests.action.accept'));
  accept.click();
  assert.equal($('request-bulk-accept-dialog').open, true, 'the existing confirm dialog opens');
  assert.ok($('bulk-accept-dialog-target').textContent.includes(formatNumber(2)));
  globalThis.fetch.calls.length = 0;
  $('request-bulk-accept-form').fire('submit');
  await flush();
  await flush();
  await flush();
  assert.deepEqual(bulkBodies, [{ action: 'accept', ids: ['r1', 'r2'] }]);
  assert.equal($('request-bulk-accept-dialog').open, false);
  assert.ok(globalThis.fetch.calls.some((c) => c.path === '/api/request-groups'), 'overview reloaded');
});

test('inline reject asks for a reason in the existing dialog', async () => {
  bulkBodies = [];
  const list = byId($('overview-body'), 'overview-pending-list');
  const reject = buttonsIn(list.children[1]).find((b) => b.textContent === t('requests.action.reject'));
  reject.click();
  assert.equal($('request-bulk-reject-dialog').open, true);
  $('bulk-reject-dialog-reason').value = 'التحويل مش واضح';
  $('request-bulk-reject-form').fire('submit');
  await flush();
  await flush();
  assert.equal(bulkBodies.length, 1);
  assert.equal(bulkBodies[0].action, 'reject');
  assert.deepEqual(bulkBodies[0].ids, ['r3']);
});

test('"View all" opens the requests tab; "Full log" opens the audit tab', async () => {
  $('tab-overview').click();
  await flush();
  await flush();
  byId($('overview-body'), 'overview-pending-all').click();
  await flush();
  assert.equal(app.activeTab, 'requests');
  $('tab-overview').click();
  await flush();
  await flush();
  byId($('overview-body'), 'overview-activity-all').click();
  await flush();
  assert.equal(app.activeTab, 'audit');
  $('tab-overview').click();
  await flush();
  await flush();
  await flush();
});

test('latest activity renders academy entries as sentences with resolved names', async () => {
  const act = byId($('overview-body'), 'overview-activity-list');
  assert.equal(act.children.length, 2);
  assert.ok(act.children[0].textContent.includes('wael'));
  assert.ok(act.children[0].textContent.includes(MESSAGES.ar['audit.verb.iap_package_update']));
  assert.ok(act.children[0].textContent.includes('الفرقة الرابعة'), 'package name resolved');
  assert.ok(act.children[1].textContent.includes('أحوال شخصية'), 'subject name resolved');
});

test('empty pending list shows an explanatory empty state', async () => {
  await signOut();
  await signIn({ 'GET /api/request-groups': { body: { items: [], total_students: 0, pending_count: 0 } }, 'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } } });
  const list = byId($('overview-body'), 'overview-pending-list');
  assert.ok(list.textContent.includes(t('overview.pending.empty')));
  assert.ok(byId($('overview-body'), 'kpi-pending').textContent.includes(t('overview.kpi.pendingNone')));
});

test('English renders the same cards in Latin digits', async () => {
  setLang('en');
  const body = $('overview-body');
  assert.ok(byId(body, 'kpi-active').textContent.includes('42'));
  assert.ok(byId(body, 'kpi-active').textContent.includes(MESSAGES.en['overview.kpi.active']));
  setLang('ar');
});

test('sign-out clears the overview', async () => {
  await signOut();
  assert.equal($('overview-body').children.length, 0);
  assert.equal($('overview-greeting').textContent, '');
});
