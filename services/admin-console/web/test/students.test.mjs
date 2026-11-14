// Students tab (owner redesign 2026-11-14): list rows without raw ids, the
// segmented status filter, and the side panel with Subjects (source chips),
// Requests and Activity sections, footer actions and focus handling.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { MESSAGES, formatMoney, setLang } = await import('../js/i18n.js');
const { contactLine, flattenRequests, sourceKey } = await import('../js/accounts.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;
const $ = (id) => doc.getElementById(id);
const t = (k) => MESSAGES.ar[k];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const byClass = (root, cls) => root.findAll((c) => String(c.className).split(' ').includes(cls));

const U1 = '8031e2b7-1111-4111-8111-11111111c89a';
const U2 = '22222222-2222-4222-8222-222222222222';
const ACCOUNTS = [
  { id: U1, full_name: 'هالة وائل', email: 'student1@example.com', phone: '+201000000001', status: 'active', created_at: '2026-11-10T10:00:00Z' },
  { id: U2, full_name: 'يوسف عادل', email: 'student4@example.com', phone: '', status: 'suspended', created_at: '2026-10-01T10:00:00Z' },
];
const calls = () => globalThis.fetch.calls.map((c) => c.url);

function routes(over = {}) {
  return {
    'GET /api/whoami': { body: { name: 'wael' } },
    'GET /api/accounts': ({ url }) => {
      if (url.includes(`search=${U1}`)) return { body: { items: [ACCOUNTS[0]], total: 1 } };
      return { body: { items: ACCOUNTS, total: 2 } };
    },
    'GET /api/entitlements': { body: { items: [
      { id: 'e1', subject_title_ar: 'أحوال شخصية ٤', source: 'admin_grant', expires_at: '2027-06-30T20:59:59Z' },
      { id: 'e2', subject_title_ar: 'قانون مدني ٢', source: 'iap', expires_at: '2027-06-30T20:59:59Z' },
      { id: 'e3', subject_title_ar: 'جنائي', source: 'request', is_expired: true, expires_at: '2026-01-01T00:00:00Z' },
    ] } },
    'GET /api/request-groups': ({ url }) => {
      const st = new URL(url, 'http://x').searchParams.get('status');
      const item = { pending: { request_id: 'r1', title_ar: 'مادة معلقة', price: 550, status: 'pending', created_at: '2026-11-13T10:00:00Z' },
        accepted: { request_id: 'r2', title_ar: 'مادة مقبولة', price: 300, status: 'accepted', created_at: '2026-11-01T10:00:00Z' },
        rejected: null }[st];
      return { body: { items: item ? [{ user_id: U1, orders: [{ source: 'web', items: [item] }] }] : [], pending_count: 1 } };
    },
    'GET /api/audit': ({ url }) => ({ body: url.includes('source=student')
      ? { items: [{ id: 's1', user_id: U1, student: 'هالة وائل', type: 'login', device_label: 'iPhone 15', actor_type: 'student', created_at: '2026-11-14T08:00:00Z' }], total: 1 }
      : { items: [{ id: 'a1', actor_id: 'adm', actor_name: 'wael', action: 'entitlement_grant', target_type: 'entitlement', target_id: 'e1', detail: `subject_id=s1 user_id=${U1}`, created_at: '2026-11-14T08:00:00Z' }], total: 1 } }),
    'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
    'GET /api/subjects': { body: { items: [], total: 0 } },
    'GET /api/levels': { body: { levels: [] } },
    'GET /api/iap/packages': { body: { packages: [] } },
    'POST /api/accounts/suspend': { body: { status: 'ok' } },
    ...over,
  };
}

const app = main(doc, win);
const settle = async () => { for (let i = 0; i < 4; i++) await flush(); };

test('helpers: contact line, source labels and merged requests', () => {
  assert.equal(contactLine(ACCOUNTS[0]), 'student1@example.com · +201000000001');
  assert.equal(contactLine(ACCOUNTS[1]), 'student4@example.com');
  assert.equal(sourceKey('iap'), 'students.source.iap');
  assert.equal(sourceKey('admin_grant'), 'students.source.manual');
  assert.equal(sourceKey(undefined), 'students.source.manual');
  const merged = flattenRequests([
    { ok: true, data: { items: [{ orders: [{ source: 'web', items: [{ id: 'a', created_at: '2026-01-01T00:00:00Z' }] }] }] } },
    { ok: false },
    { ok: true, data: { items: [{ orders: [{ source: 'mobile', items: [{ id: 'b', created_at: '2026-02-01T00:00:00Z' }] }] }] } },
  ]);
  assert.deepEqual(merged.map((x) => [x.id, x.source]), [['b', 'mobile'], ['a', 'web']]);
});

test('the list shows avatar, name, email · phone and a status chip, with no raw id', async () => {
  setLang('ar');
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'tok';
  $('login-form').fire('submit');
  await settle();
  $('tab-accounts').click();
  await settle();
  const rows = $('accounts-list').children;
  assert.equal(rows.length, 2);
  const text = rows[0].textContent;
  assert.ok(text.includes('هالة وائل'));
  assert.ok(text.includes('student1@example.com · +201000000001'));
  assert.ok(text.includes(t('status.active')));
  assert.ok(!text.includes(U1));
  assert.equal(byClass(rows[0], 'avatar')[0].textContent, 'ه');
  assert.equal(byClass(rows[0], 'student-contact')[0].className.includes('ltr'), true, 'contact reads LTR');
  assert.ok($('accounts-count').textContent.includes('٢'));
});

test('status is a segmented control with all five choices; the pressed one is marked', () => {
  const seg = buttonsIn($('accounts-status'));
  assert.deepEqual(seg.map((b) => b.getAttribute('data-status')), ['', 'active', 'suspended', 'pending_deletion', 'deleted']);
  assert.equal(seg[0].textContent, t('accounts.statusAll'));
  assert.equal(seg[0].getAttribute('aria-pressed'), 'true');
});

test('clicking a row opens the side panel with header, details and the Subjects section', async () => {
  const rowBtn = $('accounts-list').children[0].children[0];
  rowBtn.click();
  assert.equal($('student-panel').hidden, false);
  assert.equal($('students-layout').getAttribute('data-panel'), 'open');
  assert.equal(doc.activeElement, $('student-panel-close'), 'focus moves to the close button');
  await settle();
  const body = $('student-panel-body');
  assert.ok(body.textContent.includes('هالة وائل'));
  assert.ok(body.textContent.includes(t('status.active')));
  assert.ok(body.textContent.includes('student1@example.com'));
  assert.ok(body.textContent.includes('8031e2b7…'), 'id as a short copy chip');
  assert.equal(byClass(body, 'copy-chip').length, 1);
  assert.ok(calls().includes(`/api/entitlements?user_id=${U1}`));
  assert.ok(calls().includes(`/api/audit?source=student&page=1&limit=10&student=${U1}`));
  for (const st of ['pending', 'accepted', 'rejected']) assert.ok(calls().includes(`/api/request-groups?status=${st}&user_id=${U1}`), st);

  // Subjects: manual and in-app sources, both neutral grey chips.
  const chips = byClass(body, 'source-chip');
  assert.deepEqual(chips.map((c) => c.textContent), [t('students.source.manual'), t('students.source.iap'), t('students.source.request')]);
  for (const c of chips) assert.match(c.className, /chip-neutral/);
  assert.ok(!body.textContent.includes('آبل'), 'no Apple wording or styling in the panel');
  assert.ok(body.textContent.includes(t('entitlements.status.expired')));
  assert.ok(rowBtn.className.includes('is-selected'), 'the open row is marked in place');
  assert.equal(rowBtn.getAttribute('aria-current'), 'true');
});

test('Requests and Activity sections switch in place', async () => {
  const body = $('student-panel-body');
  const tab = (id) => buttonsIn(body).find((b) => b.getAttribute('id') === `sp-tab-${id}`);
  assert.equal(tab('subjects').getAttribute('aria-selected'), 'true');
  tab('requests').click();
  assert.equal(tab('requests').getAttribute('aria-selected'), 'true');
  assert.ok(body.textContent.includes('مادة معلقة'));
  assert.ok(body.textContent.includes('مادة مقبولة'));
  assert.ok(body.textContent.includes(formatMoney(550)));
  assert.ok(body.textContent.indexOf('مادة معلقة') < body.textContent.indexOf('مادة مقبولة'), 'newest first');
  tab('activity').click();
  assert.ok(body.textContent.includes(MESSAGES.ar['audit.verb.login']));
  assert.ok(body.textContent.includes('iPhone 15'));
});

test('footer actions use the existing dialogs and the panel follows the new status', async () => {
  const actions = buttonsIn($('student-panel-body')).filter((b) => [t('action.suspend'), t('action.delete')].includes(b.textContent));
  assert.equal(actions.length, 2);
  globalThis.fetch = makeFetch(routes({ 'GET /api/accounts': { body: { items: [{ ...ACCOUNTS[0], status: 'suspended' }, ACCOUNTS[1]], total: 2 } } }));
  actions[0].click();
  assert.equal($('action-dialog').open, true);
  $('dialog-reason').value = 'سبب';
  $('dialog-reason').fire('input');
  $('action-form').fire('submit');
  await settle();
  assert.ok(calls().some((u) => u === '/api/accounts/suspend'));
  const after = buttonsIn($('student-panel-body')).map((b) => b.textContent);
  assert.ok(after.includes(t('action.reactivate')), 'panel shows reactivate after suspend');
});

test('Esc closes the panel and returns focus to the row', async () => {
  const rowBtn = $('accounts-list').children[0].children[0];
  rowBtn.click();
  await settle();
  $('student-panel').fire('keydown', { key: 'Escape' });
  assert.equal($('student-panel').hidden, true);
  assert.equal($('students-layout').getAttribute('data-panel'), 'closed');
  assert.equal($('student-panel-body').children.length, 0);
  assert.equal(doc.activeElement, rowBtn);
});

test('a student name in the audit log opens the students tab with that student\'s panel open', async () => {
  globalThis.fetch = makeFetch(routes());
  $('tab-audit').click();
  await settle();
  const link = $('audit-list').findAll((c) => c.className?.includes?.('name-link') && c.getAttribute('title') === U1)[0];
  assert.ok(link, 'student link in the audit row');
  link.click();
  await settle();
  assert.equal(app.activeTab, 'accounts');
  assert.equal($('accounts-search').value, U1);
  assert.equal($('student-panel').hidden, false);
  assert.ok($('student-panel-body').textContent.includes('هالة وائل'));
});

test('a failed section says so without breaking the panel; English labels', async () => {
  globalThis.fetch = makeFetch(routes({ 'GET /api/entitlements': { status: 503, body: {} } }));
  $('accounts-list').children[0].children[0].click();
  await settle();
  assert.ok($('student-panel-body').textContent.includes(t('students.sectionFailed')));
  setLang('en');
  assert.ok($('student-panel-body').textContent.includes(MESSAGES.en['students.section.subjects']));
  setLang('ar');
});

test('sign-out closes the panel and clears the list', async () => {
  $('sign-out').click();
  await flush();
  assert.equal($('student-panel').hidden, true);
  assert.equal($('student-panel-body').children.length, 0);
  assert.ok(!$('accounts-list').textContent.includes('هالة'));
});
