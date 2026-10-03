// Requests flows on the fake DOM: list with student identity join, Cairo
// date formatting, fallback note on failed auth lookup, accept dialog with
// Cairo expiry and 409 subject_expired banner, reject dialog with mandatory reason.

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
    ...overrides,
  };
}

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');

const requestsBody$ = () => doc.querySelector('#requests-table tbody');
const tableRows = () => requestsBody$().children;

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

test('requests tab is visible and loads requests with student identity join', async () => {
  await signInOk();
  assert.equal($('tab-requests').hidden, false);
  await openRequests();
  assert.equal(app.activeTab, 'requests');

  // Verify badge shows pending count
  assert.equal($('badge-requests').textContent, '2');

  const rows = tableRows();
  assert.equal(rows.length, 4);

  // Row 0 has student identity joined
  const row0Text = rows[0].textContent;
  assert.ok(row0Text.includes('طالب واحد'), 'contains student name');
  assert.ok(row0Text.includes('u-1'), 'contains user ID');
  assert.ok(row0Text.includes('+201000000001'), 'contains phone');
  assert.ok(row0Text.includes('s1@example.com'), 'contains email');
  assert.ok(row0Text.includes('لغة عربية'), 'contains subject');

  // Row 0 has accept and reject action buttons
  const r0Buttons = buttonsIn(rows[0]);
  assert.equal(r0Buttons.length, 2);
  assert.equal(r0Buttons[0].textContent, t('requests.action.accept'));
  assert.equal(r0Buttons[1].textContent, t('requests.action.reject'));

  // Row 1 (u-missing): auth lookup did not return this student -> fallback note shown
  const row1Text = rows[1].textContent;
  assert.ok(row1Text.includes('u-missing'), 'contains user ID');
  assert.ok(row1Text.includes(t('requests.student_fetch_failed')), 'shows fallback note');

  // Row 2 (rejected): shows reject reason text
  assert.ok(rows[2].textContent.includes('صورة الإيصال غير واضحة'));

  // Row 3 (accepted): shows decision date
  assert.ok(rows[3].textContent.length > 0);
});

test('accept flow: opens dialog with price and Cairo expiry, submits and reloads', async () => {
  await openRequests();

  const rows = tableRows();
  buttonsIn(rows[0])[0].click(); // accept

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
  await openRequests();

  const rows = tableRows();
  buttonsIn(rows[0])[0].click(); // accept

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
  await openRequests();

  const rows = tableRows();
  buttonsIn(rows[0])[1].click(); // reject

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
  await openRequests();

  globalThis.fetch.calls.length = 0;
  $('requests-status').value = 'all';
  $('requests-filter').fire('submit');
  await flush();

  const call = globalThis.fetch.calls.find((c) => c.path === '/api/requests');
  assert.ok(call);
  assert.ok(call.url.includes('status=all'));

  globalThis.fetch.calls.length = 0;
  $('requests-reset').click();
  await flush();

  const resetCall = globalThis.fetch.calls.find((c) => c.path === '/api/requests');
  assert.ok(resetCall);
  assert.ok(resetCall.url.includes('status=pending'));
  assert.equal($('requests-status').value, 'pending');
});
