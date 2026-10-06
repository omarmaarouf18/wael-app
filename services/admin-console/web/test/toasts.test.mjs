// Every save and confirm in the console ends with a success toast: the page
// toast, or the toast inside the student-subjects dialog when that dialog
// stays open (the page toast would sit under its backdrop).

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const ACCOUNT = { id: '11111111-1111-4111-8111-111111111111', full_name: 'طالب تجريبي', email: 's@example.com', phone: '', status: 'active', created_at: '2026-10-01T00:00:00Z' };
const REQUEST = { id: 'req-1', user_id: ACCOUNT.id, subject_id: 'sub-live', subject_title_ar: 'مادة منشورة', level_name_ar: 'الفرقة الأولى', price: 100, status: 'pending', created_at: '2026-10-01T00:00:00Z', access_expires_at: '2027-06-01T20:59:59.000Z' };
const SUBJECT = { id: 'sub-live', level_id: 'bachelor-y1', title_ar: 'مادة منشورة', price: 100, access_expires_at: '2027-06-01T20:59:59.000Z', published: true };
const ENTITLEMENT = { id: 'ent-1', subject_title_ar: 'مادة منشورة', level_name_ar: 'الفرقة الأولى', source: 'request', is_revoked: false, is_expired: false, granted_at: '2026-10-01T00:00:00Z', expires_at: '2027-06-01T20:59:59.000Z' };

const routes = () => ({
  'GET /api/whoami': { body: { name: 'Wael' } },
  'GET /api/accounts': { body: { items: [ACCOUNT], total: 1 } },
  'GET /api/requests': { body: { items: [REQUEST], total: 1, pending_count: 1 } },
  'GET /api/levels': { body: { levels: [{ key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى' }] } },
  'GET /api/subjects': { body: { items: [SUBJECT], total: 1 } },
  'GET /api/entitlements': { body: { items: [ENTITLEMENT] } },
  'POST /api/accounts/suspend': { body: { status: 'ok' } },
  'POST /api/accounts/reactivate': { body: { status: 'ok' } },
  'POST /api/accounts/delete': { body: { status: 'ok' } },
  'POST /api/requests/accept': { body: { status: 'ok' } },
  'POST /api/requests/reject': { body: { status: 'ok' } },
  'POST /api/entitlements/grant': { body: { status: 'ok' } },
  'POST /api/entitlements/revoke': { body: { status: 'ok' } },
});

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const clickText = (root, text) => {
  const b = buttonsIn(root).find((x) => x.textContent === text);
  assert.ok(b, `button ${text}`);
  b.click();
};

main(doc, win);

test('sign in', async () => {
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
});

for (const action of ['suspend', 'delete']) {
  test(`account ${action} shows a success toast`, async () => {
    $('tab-accounts').click();
    await flush();
    $('toast').hidden = true;
    clickText($('query:#accounts-table tbody'), t(`action.${action}`));
    $('dialog-reason').value = 'سبب';
    $('dialog-reason').fire('input');
    $('action-form').fire('submit');
    await flush();
    assert.equal($('toast').hidden, false);
    assert.ok($('toast').textContent.includes(t(`accounts.toast.${action}`)), $('toast').textContent);
  });
}

test('account reactivate shows a success toast', async () => {
  globalThis.fetch = makeFetch({ ...routes(), 'GET /api/accounts': { body: { items: [{ ...ACCOUNT, status: 'suspended' }], total: 1 } } });
  $('tab-accounts').click();
  await flush();
  $('toast').hidden = true;
  clickText($('query:#accounts-table tbody'), t('action.reactivate'));
  $('action-form').fire('submit');
  await flush();
  assert.ok($('toast').textContent.includes(t('accounts.toast.reactivate')), $('toast').textContent);
  globalThis.fetch = makeFetch(routes());
});

test('request accept and reject show a success toast', async () => {
  $('tab-requests').click();
  await flush();
  $('toast').hidden = true;
  clickText($('query:#requests-table tbody'), t('requests.action.accept'));
  $('request-accept-form').fire('submit');
  await flush();
  assert.ok($('toast').textContent.includes(t('requests.toast.accepted')), $('toast').textContent);

  $('toast').hidden = true;
  clickText($('query:#requests-table tbody'), t('requests.action.reject'));
  $('reject-dialog-reason').value = 'سبب';
  $('reject-dialog-reason').fire('input');
  $('request-reject-form').fire('submit');
  await flush();
  assert.ok($('toast').textContent.includes(t('requests.toast.rejected')), $('toast').textContent);
});

test('grant and revoke show their toast inside the open subjects dialog', async () => {
  $('tab-accounts').click();
  await flush();
  clickText($('query:#accounts-table tbody'), t('action.subjects'));
  await flush();
  assert.equal($('entitlements-dialog').open, true);

  $('entitlements-grant-btn').click();
  await flush();
  $('grant-level-select').value = 'bachelor-y1';
  $('grant-level-select').fire('change');
  $('grant-subject-select').value = 'sub-live';
  $('grant-subject-select').fire('change');
  $('entitlement-grant-form').fire('submit');
  await flush();
  assert.equal($('entitlements-toast').hidden, false);
  assert.ok($('entitlements-toast').textContent.includes(t('entitlements.toast.granted')), $('entitlements-toast').textContent);

  clickText($('query:#entitlements-table tbody'), t('entitlements.action.revoke'));
  $('revoke-dialog-reason').value = 'سبب';
  $('revoke-dialog-reason').fire('input');
  $('entitlement-revoke-form').fire('submit');
  await flush();
  assert.ok($('entitlements-toast').textContent.includes(t('entitlements.toast.revoked')), $('entitlements-toast').textContent);

  $('entitlements-close').click();
  assert.equal($('entitlements-toast').hidden, true, 'closing the dialog clears its toast');
});
