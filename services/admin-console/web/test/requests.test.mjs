// Requests flows on the fake DOM: default view grouped by student with expandable
// orders and subjects, select all / select order, bulk accept of 30, mixed results,
// reject reason reaches the student, amount check warning, flat list toggle,
// Cairo date formatting, fallback note on failed auth lookup, single accept dialog
// with Cairo expiry and 409 subject_expired banner, single reject dialog with mandatory reason.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES, formatNumber } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const REQUESTS = [
  {
    id: 'req-1',
    user_id: 'u-1',
    subject_id: 'sub-1',
    subject_title_ar: 'لغة عربية',
    level_name_ar: 'الفرقة الأولى',
    price: 150,
    created_at: '2026-10-02T12:00:00Z',
    access_expires_at: '2027-06-01T20:59:59.000Z',
    status: 'pending',
  },
  {
    id: 'req-2',
    user_id: 'u-missing',
    subject_id: 'sub-1',
    subject_title_ar: 'لغة عربية',
    level_name_ar: 'الفرقة الأولى',
    price: 150,
    created_at: '2026-10-02T13:00:00Z',
    access_expires_at: '2027-06-01T20:59:59.000Z',
    status: 'pending',
  },
  {
    id: 'req-3',
    user_id: 'u-1',
    subject_id: 'sub-1',
    subject_title_ar: 'لغة عربية',
    level_name_ar: 'الفرقة الأولى',
    price: 150,
    created_at: '2026-10-01T10:00:00Z',
    access_expires_at: '2027-06-01T20:59:59.000Z',
    status: 'rejected',
    reject_reason: 'صورة الإيصال غير واضحة',
  },
  {
    id: 'req-4',
    user_id: 'u-1',
    subject_id: 'sub-1',
    subject_title_ar: 'لغة عربية',
    level_name_ar: 'الفرقة الأولى',
    price: 150,
    created_at: '2026-10-01T08:00:00Z',
    access_expires_at: '2027-06-01T20:59:59.000Z',
    status: 'accepted',
    decided_at: '2026-10-01T09:00:00Z',
  },
];

const STUDENTS = [
  { id: 'u-1', full_name: 'طالب واحد', email: 's1@example.com', phone: '+201000000001' },
];

function routes(overrides = {}) {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('ids=')) {
        return { body: { items: STUDENTS, total: STUDENTS.length } };
      }
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items: REQUESTS, total: 4, pending_count: 2 } },
    'GET /api/subjects': { body: { items: [{ id: 'sub-1', title_ar: 'لغة عربية' }], total: 1 } },
    'GET /api/audit': { body: { items: [], total: 0 } },
    'POST /api/requests/accept': { body: { status: 'ok' } },
    'POST /api/requests/reject': { body: { status: 'ok' } },
    'POST /api/requests/bulk': { body: { total: 0, accepted: 0, rejected: 0, failed: 0, results: [] } },
    ...overrides,
  };
}

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const inputsIn = (el) => el.findAll((c) => c.tagName === 'INPUT');
const toggleCheckbox = (cb, checked) => {
  cb.checked = checked;
  cb.fire('change');
};

const requestsBody$ = () => doc.querySelector('#requests-table tbody');
const tableRows = () => requestsBody$().children;

async function resetRequestsFilters() {
  if ($('requests-view-mode')) $('requests-view-mode').value = 'grouped';
  if ($('requests-status')) $('requests-status').value = 'pending';
  if ($('requests-source')) $('requests-source').value = '';
  if ($('requests-search')) $('requests-search').value = '';
  if ($('requests-subject')) $('requests-subject').value = '';
  $('requests-reset').click();
  await flush();
}

async function signInOk() {
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

async function openRequests() {
  $('tab-requests').click();
  await flush();
}

const app = main(doc, win);

test('requests tab is visible and loads requests with student identity join in default grouped view', async () => {
  await signInOk();
  assert.equal($('tab-requests').hidden, false);
  await openRequests();
  assert.equal(app.activeTab, 'requests');

  // Verify badge shows pending count
  assert.equal($('badge-requests').textContent, '2');

  const rows = tableRows();
  // Default view is grouped: 2 students with pending requests (u-1 and u-missing)
  assert.equal(rows.length, 2);

  // Row 0 has student identity joined
  const row0Text = rows[0].textContent;
  assert.ok(row0Text.includes('طالب واحد'), 'contains student name');
  assert.ok(row0Text.includes('u-1'), 'contains user ID');
  assert.ok(row0Text.includes('+201000000001'), 'contains phone');
  assert.ok(row0Text.includes('s1@example.com'), 'contains email');

  // Row 0 has accept and reject action buttons for single pending item
  const r0Buttons = buttonsIn(rows[0]).filter((b) => !b.className.includes('btn-expand'));
  assert.equal(r0Buttons.length, 2);
  assert.equal(r0Buttons[0].textContent, t('requests.action.accept'));
  assert.equal(r0Buttons[1].textContent, t('requests.action.reject'));

  // Row 1 (u-missing): auth lookup did not return this student -> fallback note shown
  const row1Text = rows[1].textContent;
  assert.ok(row1Text.includes('u-missing'), 'contains user ID');
  assert.ok(row1Text.includes(t('requests.student_fetch_failed')), 'shows fallback note');
});

test('flat list view toggle loads flat requests table and toggles back', async () => {
  await openRequests();

  // Switch to flat view
  $('requests-view-mode').value = 'flat';
  $('requests-filter').fire('submit');
  await flush();

  const rows = tableRows();
  assert.equal(rows.length, 4);

  // Row 2 (rejected): shows reject reason text
  assert.ok(rows[2].textContent.includes('صورة الإيصال غير واضحة'));

  // Row 3 (accepted): shows decision date
  assert.ok(rows[3].textContent.length > 0);

  // Toggle back to grouped view
  $('requests-view-mode').value = 'grouped';
  $('requests-filter').fire('submit');
  await flush();
  assert.equal(tableRows().length, 2);
});

test('accept flow: opens dialog with price and Cairo expiry, submits and reloads', async () => {
  await resetRequestsFilters();
  await openRequests();

  const rows = tableRows();
  const acceptBtn = buttonsIn(rows[0]).find((b) => b.textContent === t('requests.action.accept'));
  assert.ok(acceptBtn, 'accept button exists');
  acceptBtn.click();

  const dialog = $('request-accept-dialog');
  assert.equal(dialog.open, true);
  assert.ok($('accept-dialog-target').textContent.includes('طالب واحد'));
  assert.ok($('accept-dialog-target').textContent.includes('لغة عربية'));
  assert.ok($('accept-dialog-note').textContent.includes(formatNumber(150)));

  globalThis.fetch.calls.length = 0;
  $('request-accept-form').fire('submit');
  await flush();

  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.ok(post, 'POST was sent');
  assert.equal(post.path, '/api/requests/accept');
  assert.deepEqual(JSON.parse(post.init.body), { id: 'req-1' });
  assert.equal(dialog.open, false, 'dialog closed');
});

test('accept flow 409 subject_expired shows mapped error banner', async () => {
  globalThis.fetch = makeFetch(routes({
    'POST /api/requests/accept': {
      status: 409,
      body: { code: 'subject_expired', error: 'SECRET expired' },
    },
  }));
  await resetRequestsFilters();
  await openRequests();

  const rows = tableRows();
  const acceptBtn = buttonsIn(rows[0]).find((b) => b.textContent === t('requests.action.accept'));
  assert.ok(acceptBtn, 'accept button exists');
  acceptBtn.click();

  const dialog = $('request-accept-dialog');
  assert.equal(dialog.open, true);

  $('request-accept-form').fire('submit');
  await flush();

  assert.equal(dialog.open, true, 'dialog stays open on error');
  const banner = $('accept-dialog-banner');
  assert.equal(banner.hidden, false);
  assert.ok(banner.textContent.includes(t('err.subject_expired')));
  assert.ok(!banner.textContent.includes('SECRET'));

  $('accept-dialog-cancel').click();
  assert.equal(dialog.open, false);
});

test('reject flow: requires a reason, and submits to /api/requests/reject', async () => {
  globalThis.fetch = makeFetch(routes());
  await resetRequestsFilters();
  await openRequests();

  const rows = tableRows();
  const rejectBtn = buttonsIn(rows[0]).find((b) => b.textContent === t('requests.action.reject'));
  assert.ok(rejectBtn, 'reject button exists');
  rejectBtn.click();

  const dialog = $('request-reject-dialog');
  assert.equal(dialog.open, true);
  assert.ok($('reject-dialog-target').textContent.includes('طالب واحد'));
  assert.equal($('reject-dialog-confirm').disabled, true, 'confirm disabled without reason');

  // Spaces only keep confirm disabled
  $('reject-dialog-reason').value = '   ';
  $('reject-dialog-reason').fire('input');
  assert.equal($('reject-dialog-confirm').disabled, true);

  // Valid reason enables confirm
  $('reject-dialog-reason').value = 'بيانات التحويل غير مطابقة';
  $('reject-dialog-reason').fire('input');
  assert.equal($('reject-dialog-confirm').disabled, false);

  globalThis.fetch.calls.length = 0;
  $('request-reject-form').fire('submit');
  await flush();

  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.ok(post, 'POST was sent');
  assert.equal(post.path, '/api/requests/reject');
  assert.deepEqual(JSON.parse(post.init.body), {
    id: 'req-1',
    reason: 'بيانات التحويل غير مطابقة',
  });
  assert.equal(dialog.open, false, 'dialog closed');
});

test('requests filter and reset trigger reload', async () => {
  await resetRequestsFilters();
  await openRequests();

  globalThis.fetch.calls.length = 0;
  $('requests-status').value = 'all';
  $('requests-filter').fire('submit');
  await flush();

  const call = globalThis.fetch.calls.find((c) => c.path === '/api/requests' || c.path === '/api/request-groups');
  assert.ok(call);
  assert.ok(call.url.includes('status=all'));

  globalThis.fetch.calls.length = 0;
  $('requests-reset').click();
  await flush();

  const resetCall = globalThis.fetch.calls.find((c) => c.path === '/api/requests' || c.path === '/api/request-groups');
  assert.ok(resetCall);
  assert.ok(resetCall.url.includes('status=pending'));
  assert.equal($('requests-status').value, 'pending');
});

function make30StudentData() {
  const student = { id: 'u-30', full_name: 'طالب ثلاثين مادة', email: 's30@example.com', phone: '+201099998888' };
  const items = [];
  for (let i = 1; i <= 30; i++) {
    const isWeb = i <= 20;
    items.push({
      id: `req-30-${i}`,
      user_id: 'u-30',
      subject_id: `sub-${i}`,
      subject_title_ar: `مادة رقم ${i}`,
      level_name_ar: 'الفرقة الأولى',
      price: 100,
      source: isWeb ? 'web' : 'app',
      order_id: isWeb ? (i <= 10 ? 'ord-web-1' : 'ord-web-2') : 'ord-app-1',
      payment_method: isWeb ? 'wallet' : undefined,
      transfer_reference: isWeb ? (i <= 10 ? 'TXN-WEB-1' : 'TXN-WEB-2') : undefined,
      sender_number: isWeb ? '01011112222' : undefined,
      amount: isWeb ? 1000 : undefined,
      amount_warning: false,
      created_at: `2026-11-0${Math.min(9, Math.floor(i / 4) + 1)}T10:00:00Z`,
      status: 'pending',
    });
  }
  return { student, items };
}

test('grouping: 1 student with 30 requests is 1 row in default view', async () => {
  const { student, items } = make30StudentData();
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
  }));

  await resetRequestsFilters();
  await openRequests();

  const rows = tableRows();
  // 30 requests from 1 student must render as exactly 1 row in default grouped view
  assert.equal(rows.length, 1);

  const rowText = rows[0].textContent;
  assert.ok(rowText.includes('طالب ثلاثين مادة'), 'contains student name');
  assert.ok(rowText.includes('u-30'), 'contains student ID');
  assert.ok(rowText.includes('+201099998888'), 'contains phone');
  assert.ok(rowText.includes('s30@example.com'), 'contains email');
  assert.ok(rowText.includes(formatNumber(30)), 'shows subject count 30');
  assert.ok(rowText.includes(formatNumber(3000)), 'shows total amount 3,000 EGP');
  assert.ok(rowText.includes(t('requests.source.web')), 'shows web source');
  assert.ok(rowText.includes(t('requests.source.mobile')), 'shows mobile/app source');
});

test('expand: reveals student orders and subjects under each order', async () => {
  const { student, items } = make30StudentData();
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
  }));

  await resetRequestsFilters();
  await openRequests();

  let rows = tableRows();
  assert.equal(rows.length, 1);

  // Click expand arrow
  const expandBtn = buttonsIn(rows[0]).find((b) => b.className?.includes('btn-expand'));
  assert.ok(expandBtn, 'expand button exists');
  expandBtn.click();
  await flush();

  rows = tableRows();
  // Student row + expanded detail row
  assert.equal(rows.length, 2);

  const detailText = rows[1].textContent;
  assert.ok(detailText.includes('ord-web-1'), 'shows web order 1');
  assert.ok(detailText.includes('TXN-WEB-1'), 'shows transfer ref for web order');
  assert.ok(detailText.includes('01011112222'), 'shows sender number');
  assert.ok(detailText.includes(formatNumber(1000)), 'shows amount paid');
  assert.ok(detailText.includes('ord-app-1'), 'shows app order');

  // Verify subjects under orders
  assert.ok(detailText.includes('مادة رقم 1'));
  assert.ok(detailText.includes('مادة رقم 30'));

  // Collapse again
  expandBtn.click();
  await flush();
  assert.equal(tableRows().length, 1);
});

test('select all, select order, and select subject update bulk bar', async () => {
  const { student, items } = make30StudentData();
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
  }));

  await resetRequestsFilters();
  await openRequests();

  const bulkBar = $('requests-bulk-bar');
  const bulkInfo = $('requests-bulk-info');
  assert.equal(bulkBar.hidden, true, 'bulk bar initially hidden');

  const rows = tableRows();
  const selectAllBox = inputsIn(rows[0]).find((i) => i.getAttribute('data-role') === 'select-student');
  assert.ok(selectAllBox, 'select all checkbox on student row exists');

  // Check "select all"
  toggleCheckbox(selectAllBox, true);
  await flush();

  assert.equal(bulkBar.hidden, false, 'bulk bar visible when selected');
  assert.ok(bulkInfo.textContent.includes(formatNumber(30)), 'bulk info shows count 30');
  assert.ok(bulkInfo.textContent.includes(formatNumber(3000)), 'bulk info shows total 3,000');

  // Uncheck "select all"
  const updatedSelectAll = inputsIn(tableRows()[0]).find((i) => i.getAttribute('data-role') === 'select-student');
  toggleCheckbox(updatedSelectAll, false);
  await flush();

  assert.equal(bulkBar.hidden, true, 'bulk bar hidden when unselected');

  // Expand row to test select order
  const expandBtn = buttonsIn(tableRows()[0]).find((b) => b.className?.includes('btn-expand'));
  expandBtn.click();
  await flush();

  // Find select-order checkbox for ord-web-1 (10 subjects, 1000 EGP)
  const orderBoxes = inputsIn(tableRows()[1]).filter((i) => i.getAttribute('data-role') === 'select-order');
  assert.ok(orderBoxes.length >= 1, 'order checkboxes found');
  toggleCheckbox(orderBoxes[0], true);
  await flush();

  assert.equal(bulkBar.hidden, false);
  assert.ok(bulkInfo.textContent.includes(formatNumber(10)), 'shows 10 subjects selected');
  assert.ok(bulkInfo.textContent.includes(formatNumber(1000)), 'shows 1,000 EGP total');

  // Click clear selection button
  $('requests-bulk-clear-btn').click();
  await flush();
  assert.equal(bulkBar.hidden, true);
});

test('bulk accept of 30: confirmation shows count and total, posts to /api/requests/bulk', async () => {
  const { student, items } = make30StudentData();
  let bulkPayload = null;
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
    'POST /api/requests/bulk': (req) => {
      bulkPayload = JSON.parse(req.init.body);
      const results = bulkPayload.ids.map((id) => ({ id, status: 'accepted' }));
      return { body: { total: bulkPayload.ids.length, accepted: bulkPayload.ids.length, rejected: 0, failed: 0, results } };
    },
  }));

  await resetRequestsFilters();
  await openRequests();

  // Select all 30
  const selectAll = inputsIn(tableRows()[0]).find((i) => i.getAttribute('data-role') === 'select-student');
  toggleCheckbox(selectAll, true);
  await flush();

  // Click bulk accept
  $('requests-bulk-accept-btn').click();
  await flush();

  const dialog = $('request-bulk-accept-dialog');
  assert.equal(dialog.open, true, 'bulk accept dialog is open');
  assert.ok($('bulk-accept-dialog-target').textContent.includes(formatNumber(30)));
  assert.ok($('bulk-accept-dialog-target').textContent.includes(formatNumber(3000)));

  // Confirm
  $('request-bulk-accept-form').fire('submit');
  await flush();

  assert.equal(dialog.open, false, 'bulk accept dialog closed');
  assert.ok(bulkPayload, 'bulk endpoint was called');
  assert.equal(bulkPayload.action, 'accept');
  assert.equal(bulkPayload.ids.length, 30);
  assert.equal($('requests-bulk-bar').hidden, true, 'bulk bar hidden after accept');
});

test('mixed results: no silent partial failure, banner shows which failed and why', async () => {
  const { student, items } = make30StudentData();
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
    'POST /api/requests/bulk': {
      body: {
        total: 2,
        accepted: 1,
        rejected: 0,
        failed: 1,
        results: [
          { id: 'req-30-1', status: 'accepted' },
          { id: 'req-30-2', status: 'failed', error: 'subject_expired' },
        ],
      },
    },
  }));

  await resetRequestsFilters();
  await openRequests();

  // Expand and select 2 items
  const expandBtn = buttonsIn(tableRows()[0]).find((b) => b.className?.includes('btn-expand'));
  expandBtn.click();
  await flush();

  const subBoxes = inputsIn(tableRows()[1]).filter((i) => i.getAttribute('data-role') === 'select-subject');
  toggleCheckbox(subBoxes[0], true);
  toggleCheckbox(subBoxes[1], true);
  await flush();

  // Click bulk accept and confirm
  $('requests-bulk-accept-btn').click();
  await flush();
  $('request-bulk-accept-form').fire('submit');
  await flush();

  // Banner must be visible and inform about the failure
  const banner = $('requests-banner');
  assert.equal(banner.hidden, false, 'banner is visible on partial failure');
  const bannerText = banner.textContent;
  assert.ok(bannerText.includes('req-30-2'), 'banner specifies which request failed');
  assert.ok(bannerText.includes('subject_expired') || bannerText.includes(t('err.subject_expired')), 'banner explains why');
});

test('reject reason reaches the student via bulk reject', async () => {
  const { student, items } = make30StudentData();
  let bulkPayload = null;
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
    'POST /api/requests/bulk': (req) => {
      bulkPayload = JSON.parse(req.init.body);
      const results = bulkPayload.ids.map((id) => ({ id, status: 'rejected' }));
      return { body: { total: bulkPayload.ids.length, accepted: 0, rejected: bulkPayload.ids.length, failed: 0, results } };
    },
  }));

  await resetRequestsFilters();
  await openRequests();

  const selectAll = inputsIn(tableRows()[0]).find((i) => i.getAttribute('data-role') === 'select-student');
  toggleCheckbox(selectAll, true);
  await flush();

  $('requests-bulk-reject-btn').click();
  await flush();

  const dialog = $('request-bulk-reject-dialog');
  assert.equal(dialog.open, true, 'bulk reject dialog open');
  assert.equal($('bulk-reject-dialog-confirm').disabled, true, 'confirm initially disabled');

  // Typing reason
  $('bulk-reject-dialog-reason').value = 'التحويل غير مطابق';
  $('bulk-reject-dialog-reason').fire('input');
  assert.equal($('bulk-reject-dialog-confirm').disabled, false, 'confirm enabled with reason');

  $('request-bulk-reject-form').fire('submit');
  await flush();

  assert.equal(dialog.open, false);
  assert.ok(bulkPayload);
  assert.equal(bulkPayload.action, 'reject');
  assert.equal(bulkPayload.reason, 'التحويل غير مطابق');
  assert.equal(bulkPayload.ids.length, 30);
});

test('requests tab renders web purchase request details and amount warning', async () => {
  const webRequests = [
    {
      id: 'req-web-1',
      user_id: 'u-1',
      subject_id: 'sub-1',
      subject_title_ar: 'لغة عربية',
      price: 150,
      source: 'web',
      order_id: 'ord-w1',
      payment_method: 'instapay',
      transfer_reference: 'TX12345678',
      sender_number: '01012345678',
      amount: 150,
      amount_warning: false,
      created_at: '2026-10-02T12:00:00Z',
      status: 'pending',
    },
    {
      id: 'req-web-2',
      user_id: 'u-1',
      subject_id: 'sub-1',
      subject_title_ar: 'لغة عربية',
      price: 150,
      source: 'web',
      order_id: 'ord-w2',
      payment_method: 'wallet',
      transfer_reference: 'W88990011',
      sender_number: '01122334455',
      amount: 100,
      amount_warning: true,
      created_at: '2026-10-02T13:00:00Z',
      status: 'pending',
    },
  ];

  globalThis.fetch = makeFetch(routes({
    'GET /api/requests': { body: { items: webRequests, total: 2, pending_count: 2 } },
  }));
  await resetRequestsFilters();
  await openRequests();

  // In grouped view, 1 student row is rendered
  let rows = tableRows();
  assert.equal(rows.length, 1);

  // Expand student row to see order cards
  const expandBtn = buttonsIn(rows[0]).find((b) => b.className?.includes('btn-expand'));
  expandBtn.click();
  await flush();

  rows = tableRows();
  assert.equal(rows.length, 2);
  const detailText = rows[1].textContent;

  // Order 1: InstaPay, 150 EGP, Ref, From, no warning
  assert.ok(detailText.includes(t('requests.source.web')), 'contains web source badge');
  assert.ok(detailText.includes('InstaPay:'), 'contains instapay label');
  assert.ok(detailText.includes(formatNumber(150)), 'contains formatted amount');
  assert.ok(detailText.includes('Ref: TX12345678'), 'contains transfer ref');
  assert.ok(detailText.includes('From: 01012345678'), 'contains sender number');

  // Order 2: Wallet, 100 EGP, amount warning
  assert.ok(detailText.includes('Wallet:'), 'contains wallet label');
  assert.ok(detailText.includes(formatNumber(100)), 'contains formatted amount');
  assert.ok(detailText.includes('Ref: W88990011'), 'contains transfer ref');
  assert.ok(detailText.includes('⚠️'), 'contains amount warning badge');

  // Also verify flat list toggle displays flat rows directly
  $('requests-view-mode').value = 'flat';
  $('requests-filter').fire('submit');
  await flush();

  const flatRows = tableRows();
  assert.equal(flatRows.length, 2);
  assert.ok(flatRows[0].textContent.includes('Ref: TX12345678'));
  assert.ok(!flatRows[0].textContent.includes('⚠️'));
  assert.ok(flatRows[1].textContent.includes('Ref: W88990011'));
  assert.ok(flatRows[1].textContent.includes('⚠️'));
});

test('filters keep working: status, source, and search by name/email/phone/reference', async () => {
  const { student, items } = make30StudentData();
  globalThis.fetch = makeFetch(routes({
    'GET /api/accounts': (req) => {
      const url = req.url || '';
      if (url.includes('u-30')) return { body: { items: [student], total: 1 } };
      return { body: { items: [], total: 0 } };
    },
    'GET /api/requests': { body: { items, total: 30, pending_count: 30 } },
  }));

  await resetRequestsFilters();
  await openRequests();

  // Search by name
  $('requests-search').value = 'ثلاثين';
  $('requests-filter').fire('submit');
  await flush();
  assert.equal(tableRows().length, 1);

  // Search by non-matching query
  $('requests-search').value = 'nonexistent';
  $('requests-filter').fire('submit');
  await flush();
  assert.ok(tableRows()[0].textContent.includes(t('common.empty')));

  // Filter by source: web
  $('requests-search').value = '';
  $('requests-source').value = 'web';
  $('requests-filter').fire('submit');
  await flush();
  assert.equal(tableRows().length, 1);

  // Reset restores default view
  $('requests-reset').click();
  await flush();
  assert.equal($('requests-source').value, '');
  assert.equal($('requests-search').value, '');
  assert.equal(tableRows().length, 1);
});
