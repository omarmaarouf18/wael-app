// Entitlements dialog flows on the fake DOM: active, expired, and revoked
// status badges, grant flow with level/subject cascading select, and revoke
// flow with mandatory reason.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES, formatNumber } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const ENTITLEMENTS = [
  {
    id: 'ent-1',
    user_id: 'u-1',
    subject_id: 'sub-1',
    subject_title_ar: 'رياضيات',
    level_name_ar: 'الفرقة الأولى',
    source: 'request',
    is_revoked: false,
    is_expired: false,
    granted_at: '2026-10-01T10:00:00Z',
    expires_at: '2027-06-01T20:59:59.000Z',
  },
  {
    id: 'ent-2',
    user_id: 'u-1',
    subject_id: 'sub-2',
    subject_title_ar: 'فيزياء',
    level_name_ar: 'الفرقة الأولى',
    source: 'admin_grant',
    is_revoked: false,
    is_expired: true,
    granted_at: '2025-10-01T10:00:00Z',
    expires_at: '2026-06-01T20:59:59.000Z',
  },
  {
    id: 'ent-3',
    user_id: 'u-1',
    subject_id: 'sub-3',
    subject_title_ar: 'كيمياء',
    level_name_ar: 'الفرقة الأولى',
    source: 'admin_grant',
    is_revoked: true,
    is_expired: false,
    granted_at: '2026-10-01T10:00:00Z',
    expires_at: '2027-06-01T20:59:59.000Z',
    revoked_at: '2026-10-02T10:00:00Z',
    revoked_by: 'Wael',
    revoke_reason: 'طلب استرداد',
  },
];

const STUDENTS = [
  {
    id: 'u-1',
    full_name: 'علي أحمد',
    email: 'ali@example.com',
    phone: '+201000000001',
    status: 'active',
    created_at: '2026-10-01T00:00:00Z',
  },
];

const LEVELS = [
  { key: 'lvl-1', name_ar: 'الفرقة الأولى' },
];

const SUBJECTS = [
  {
    id: 'sub-4',
    level_id: 'lvl-1',
    title_ar: 'أحياء',
    published: true,
    price: 200,
    access_expires_at: '2027-06-01T20:59:59.000Z',
  },
];

function routes(overrides = {}) {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { body: { items: STUDENTS, total: STUDENTS.length } },
    'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
    'GET /api/audit': { body: { items: [], total: 0 } },
    'GET /api/entitlements': { body: { items: ENTITLEMENTS, total: 3 } },
    'GET /api/levels': { body: { levels: LEVELS } },
    'GET /api/subjects': { body: { items: SUBJECTS, total: 1 } },
    'POST /api/entitlements/grant': { body: { status: 'ok' } },
    'POST /api/entitlements/revoke': { body: { status: 'ok' } },
    ...overrides,
  };
}

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const accountsBody$ = () => doc.querySelector('#accounts-table tbody');
const entitlementsBody$ = () => doc.querySelector('#entitlements-table tbody');

async function signInOk() {
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

const app = main(doc, win);

test('accounts tab row "المواد" opens entitlements dialog with active, expired, and revoked items', async () => {
  await signInOk();
  assert.equal(accountsBody$().children.length, 1);

  // Student row buttons: [suspend, delete, subjects]
  const rowButtons = buttonsIn(accountsBody$().children[0]);
  assert.equal(rowButtons.length, 3);
  assert.equal(rowButtons[2].textContent, t('action.subjects'));

  rowButtons[2].click();
  await flush();

  const dialog = $('entitlements-dialog');
  assert.equal(dialog.open, true);
  assert.ok($('entitlements-dialog-title').textContent.includes('علي أحمد'));

  const rows = entitlementsBody$().children;
  assert.equal(rows.length, 3);

  // Row 0 (active): shows subject title, active status badge, revoke button
  assert.ok(rows[0].textContent.includes('رياضيات'));
  assert.ok(rows[0].textContent.includes(t('entitlements.status.active')));
  const r0Buttons = buttonsIn(rows[0]);
  assert.equal(r0Buttons.length, 1);
  assert.equal(r0Buttons[0].textContent, t('entitlements.action.revoke'));

  // Row 1 (expired): shows expired badge, no revoke button
  assert.ok(rows[1].textContent.includes('فيزياء'));
  assert.ok(rows[1].textContent.includes(t('entitlements.status.expired')));
  assert.equal(buttonsIn(rows[1]).length, 0);

  // Row 2 (revoked): shows revoked badge, revocation details (Wael and reason)
  assert.ok(rows[2].textContent.includes('كيمياء'));
  assert.ok(rows[2].textContent.includes(t('entitlements.status.revoked')));
  assert.ok(rows[2].textContent.includes('Wael'));
  assert.ok(rows[2].textContent.includes('طلب استرداد'));
  assert.equal(buttonsIn(rows[2]).length, 0);
});

test('grant subject flow: selects level then published subject, displays price and Cairo expiry, submits and reloads', async () => {
  $('entitlements-grant-btn').click();
  await flush();

  const grantDialog = $('entitlement-grant-dialog');
  assert.equal(grantDialog.open, true);
  assert.ok($('grant-dialog-target').textContent.includes('علي أحمد'));
  assert.equal($('grant-dialog-confirm').disabled, true, 'confirm initially disabled');

  // Select level
  $('grant-level-select').value = 'lvl-1';
  $('grant-level-select').fire('change');
  assert.equal($('grant-dialog-confirm').disabled, true);

  // Select subject
  $('grant-subject-select').value = 'sub-4';
  $('grant-subject-select').fire('change');

  // Note displays formatted price and confirm is enabled
  assert.equal($('grant-dialog-note').hidden, false);
  assert.ok($('grant-dialog-note').textContent.includes(formatNumber(200)));
  assert.equal($('grant-dialog-confirm').disabled, false);

  globalThis.fetch.calls.length = 0;
  $('entitlement-grant-form').fire('submit');
  await flush();

  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.ok(post, 'POST sent');
  assert.equal(post.path, '/api/entitlements/grant');
  assert.deepEqual(JSON.parse(post.init.body), {
    user_id: 'u-1',
    subject_id: 'sub-4',
  });
  assert.equal(grantDialog.open, false, 'grant dialog closed');
});

test('revoke subject flow: requires reason, states payment record is kept, submits and reloads', async () => {
  const rows = entitlementsBody$().children;
  buttonsIn(rows[0])[0].click(); // Revoke button on active entitlement

  const revokeDialog = $('entitlement-revoke-dialog');
  assert.equal(revokeDialog.open, true);
  assert.ok($('revoke-dialog-target').textContent.includes('علي أحمد'));
  assert.ok($('revoke-dialog-target').textContent.includes('رياضيات'));
  assert.equal($('revoke-dialog-confirm').disabled, true, 'confirm initially disabled');

  // Spaces only keep confirm disabled
  $('revoke-dialog-reason').value = '   ';
  $('revoke-dialog-reason').fire('input');
  assert.equal($('revoke-dialog-confirm').disabled, true);

  // Valid reason enables confirm
  $('revoke-dialog-reason').value = 'استرداد المبلغ';
  $('revoke-dialog-reason').fire('input');
  assert.equal($('revoke-dialog-confirm').disabled, false);

  globalThis.fetch.calls.length = 0;
  $('entitlement-revoke-form').fire('submit');
  await flush();

  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.ok(post, 'POST sent');
  assert.equal(post.path, '/api/entitlements/revoke');
  assert.deepEqual(JSON.parse(post.init.body), {
    id: 'ent-1',
    reason: 'استرداد المبلغ',
  });
  assert.equal(revokeDialog.open, false, 'revoke dialog closed');
});
