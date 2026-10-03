// End-to-end flows of the real modules (app, auth, api, accounts, audit,
// dialog) on the fake DOM. One main() per process: the tests below share it
// and run in order.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const storageHits = [];
for (const name of ['localStorage', 'sessionStorage', 'indexedDB']) {
  globalThis[name] = new Proxy({}, { get(_t, p) { storageHits.push(`${name}.${String(p)}`); throw new Error(`${name} used`); } });
}

// Import first: app.js starts itself only when a global document exists.
const { main } = await import('../js/app.js');
const { getToken, isSignedIn } = await import('../js/auth.js');
const { MESSAGES } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;
Object.defineProperty(doc, 'cookie', { get() { storageHits.push('document.cookie'); throw new Error('cookie used'); }, set() { storageHits.push('document.cookie'); throw new Error('cookie used'); } });

const IDS = {
  active: '11111111-1111-4111-8111-111111111111',
  suspended: '22222222-2222-4222-8222-222222222222',
  deleted: '33333333-3333-4333-8333-333333333333',
};
const accountsBody = {
  items: [
    { id: IDS.active, full_name: 'علي أحمد', email: 'ali@example.com', phone: '+201001234567', status: 'active', created_at: '2026-09-01T10:00:00Z' },
    { id: IDS.suspended, full_name: 'Mona', email: 'mona@example.com', phone: '+201007654321', status: 'suspended', created_at: '2026-09-02T10:00:00Z' },
    { id: IDS.deleted, full_name: 'Gone', email: 'gone@example.com', phone: '', status: 'deleted', created_at: '2026-09-03T10:00:00Z' },
  ],
  total: 3,
  page: 1,
  limit: 15,
};

const $ = (id) => doc.getElementById(id);
const accountsBody$ = () => doc.querySelector('#accounts-table tbody');
const auditBody$ = () => doc.querySelector('#audit-table tbody');
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const rowButtons = (index) => buttonsIn(accountsBody$().children[index]).map((b) => b.textContent);
const t = (key) => MESSAGES[win.lang ?? 'ar'][key];

function routes(overrides = {}) {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { body: accountsBody },
    'GET /api/audit': {
      body: {
        items: [
          { id: 'a1', actor_name: 'Wael', action: 'account_suspend', target_type: 'user', target_id: IDS.active, detail: 'spam', created_at: '2026-10-01T09:00:00Z' },
          { id: 'a2', actor_name: 'Wael', action: 'something_new', target_type: 'widget', target_id: 'w1', created_at: '2026-10-01T08:00:00Z' },
        ],
        total: 2,
      },
    },
    'POST /api/accounts/suspend': { body: { status: 'ok' } },
    'POST /api/accounts/reactivate': { body: { status: 'ok' } },
    'POST /api/accounts/delete': { body: { status: 'ok' } },
    ...overrides,
  };
}

// After sign-out the tables hold at most a placeholder row: nothing about a student.
function assertNoStudentData() {
  const shown = accountsBody$().textContent + auditBody$().textContent;
  for (const secret of ['ali@example.com', 'mona@example.com', 'gone@example.com', 'علي أحمد', 'Mona', IDS.active, IDS.suspended, 'spam', 'something_new']) {
    assert.ok(!shown.includes(secret), `${secret} is still on screen`);
  }
}

async function submitLogin(token) {
  $('login-token').value = token;
  $('login-form').fire('submit');
  await flush();
}

async function signInOk() {
  globalThis.fetch = makeFetch(routes());
  await submitLogin('admin-token-xyz');
  assert.equal(isSignedIn(), true);
}

// The real page tags these in index.html; the fake document is not parsed from it.
$('sign-out').dataset.i18n = 'top.signOut';
$('lang-toggle').dataset.i18n = 'top.language';
$('audit-source-academy').dataset.i18n = 'audit.source.academy';
$('panel-audit').append($('audit-source-academy'));

const app = main(doc, win);

test('starts on the sign-in page, Arabic and right-to-left, with the unfinished tabs hidden', () => {
  assert.equal($('login-view').hidden, false);
  assert.equal($('app-view').hidden, true);
  assert.equal(doc.documentElement.lang, 'ar');
  assert.equal(doc.documentElement.dir, 'rtl');
  for (const id of ['requests', 'files']) assert.equal($(`tab-${id}`).hidden, true, id);
  for (const id of ['accounts', 'audit', 'catalog']) assert.equal($(`tab-${id}`).hidden, false, id);
  assert.equal(getToken(), '');
});

test('an empty token is refused with a message and no request', async () => {
  globalThis.fetch = makeFetch({});
  await submitLogin('   ');
  assert.equal($('login-banner').hidden, false);
  assert.equal($('login-banner').textContent, MESSAGES.ar['login.empty']);
  assert.equal(globalThis.fetch.calls.length, 0);
});

test('a wrong token (401) keeps the sign-in page, clears the field and the token', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { status: 401, body: { error: 'SECRET detail' } } });
  await submitLogin('wrong-token');
  assert.equal($('login-view').hidden, false);
  assert.equal($('app-view').hidden, true);
  assert.equal($('login-banner').textContent, MESSAGES.ar['login.expired']);
  assert.ok(!$('login-banner').textContent.includes('SECRET'));
  assert.equal($('login-token').value, '');
  assert.equal(getToken(), '');
  assert.equal($('login-submit').disabled, false);
});

test('a temporary failure (503) shows a retry banner and keeps the typed token for the retry', async () => {
  globalThis.fetch = makeFetch({ 'GET /api/whoami': { status: 503, body: {} } });
  await submitLogin('right-token');
  assert.equal($('login-view').hidden, false);
  assert.equal($('login-banner').hidden, false);
  assert.ok($('login-banner').textContent.includes(MESSAGES.ar['err.unavailable']));
  assert.equal($('login-token').value, 'right-token');
  const retry = buttonsIn($('login-banner'));
  assert.equal(retry.length, 1);
  assert.equal(retry[0].textContent, MESSAGES.ar['common.retry']);

  globalThis.fetch = makeFetch(routes());
  retry[0].click();
  await flush();
  assert.equal(isSignedIn(), true);
  assert.equal($('app-view').hidden, false);
  $('sign-out').click();
  await flush();
});

test('signing in shows the app, the admin name and the accounts list; the field is cleared', async () => {
  await signInOk();
  assert.equal($('login-view').hidden, true);
  assert.equal($('app-view').hidden, false);
  assert.equal($('admin-name').textContent, 'Wael');
  assert.equal($('login-token').value, '');
  assert.equal($('login-banner').hidden, true);

  const listCall = globalThis.fetch.calls.find((c) => c.path === '/api/accounts');
  assert.ok(listCall, 'accounts list was requested');
  assert.equal(listCall.url, '/api/accounts?page=1&limit=15');
  assert.equal(listCall.init.headers['X-Admin-Token'], 'admin-token-xyz');
  for (const call of globalThis.fetch.calls) assert.ok(!call.url.includes('admin-token-xyz'));

  assert.equal(accountsBody$().children.length, 3);
  assert.deepEqual(rowButtons(0), [MESSAGES.ar['action.suspend'], MESSAGES.ar['action.delete']]);
  assert.deepEqual(rowButtons(1), [MESSAGES.ar['action.reactivate'], MESSAGES.ar['action.delete']]);
  assert.deepEqual(rowButtons(2), []);
  assert.equal(accountsBody$().children[0].textContent.includes('علي أحمد'), true);
  assert.equal(accountsBody$().children[0].textContent.includes(IDS.active), true);
});

test('search and status filter go to the server and reset to page 1', async () => {
  $('accounts-search').value = ' علي ';
  $('accounts-status').value = 'suspended';
  globalThis.fetch.calls.length = 0;
  $('accounts-filter').fire('submit');
  await flush();
  assert.equal(globalThis.fetch.calls[0].url, '/api/accounts?page=1&limit=15&search=%D8%B9%D9%84%D9%8A&status=suspended');

  globalThis.fetch.calls.length = 0;
  $('accounts-reset').click();
  await flush();
  assert.equal(globalThis.fetch.calls[0].url, '/api/accounts?page=1&limit=15');
  assert.equal($('accounts-search').value, '');
});

test('pagination moves between pages', async () => {
  globalThis.fetch = makeFetch(routes({ 'GET /api/accounts': { body: { ...accountsBody, total: 40 } } }));
  $('accounts-reset').click();
  await flush();
  const pager = $('accounts-pager');
  const next = pager.querySelector('[data-role=next]');
  const prev = pager.querySelector('[data-role=prev]');
  assert.equal(next.disabled, false);
  assert.equal(prev.disabled, true);
  globalThis.fetch.calls.length = 0;
  next.click();
  await flush();
  assert.equal(globalThis.fetch.calls[0].url, '/api/accounts?page=2&limit=15');
  assert.equal(prev.disabled, false);
  prev.click();
  await flush();
  assert.equal(globalThis.fetch.calls[1].url, '/api/accounts?page=1&limit=15');
});

test('suspend asks for a reason: the confirm button stays disabled until one is written', async () => {
  globalThis.fetch = makeFetch(routes());
  $('accounts-reset').click();
  await flush();
  buttonsIn(accountsBody$().children[0])[0].click(); // suspend
  const dialog = $('action-dialog');
  assert.equal(dialog.open, true);
  assert.equal($('dialog-title').textContent, MESSAGES.ar['dialog.suspend.title']);
  assert.equal($('dialog-reason-group').hidden, false);
  assert.equal($('dialog-confirm').disabled, true);

  for (const blank of ['', '   ', '\n\r\n']) {
    $('dialog-reason').value = blank;
    $('dialog-reason').fire('input');
    assert.equal($('dialog-confirm').disabled, true, JSON.stringify(blank));
  }
  // An over-long reason is refused too.
  $('dialog-reason').value = 'x'.repeat(1001);
  $('dialog-reason').fire('input');
  assert.equal($('dialog-confirm').disabled, true);

  // Submitting while invalid sends nothing.
  globalThis.fetch.calls.length = 0;
  $('action-form').fire('submit');
  await flush();
  assert.equal(globalThis.fetch.calls.length, 0);
  assert.equal(dialog.open, true);

  $('dialog-reason').value = '  spam account \n';
  $('dialog-reason').fire('input');
  assert.equal($('dialog-confirm').disabled, false);

  $('action-form').fire('submit');
  await flush();
  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.equal(post.path, '/api/accounts/suspend');
  assert.deepEqual(JSON.parse(post.init.body), { id: IDS.active, reason: 'spam account' });
  assert.equal(post.init.headers['X-Admin-Token'], 'admin-token-xyz');
  assert.equal(dialog.open, false, 'dialog closes on success');
  assert.equal($('dialog-reason').value, '', 'the reason is not kept after the dialog closes');
  const listAfter = globalThis.fetch.calls.filter((c) => c.path === '/api/accounts');
  assert.equal(listAfter.length, 1, 'list reloaded after the action');
});

test('reactivate needs no reason and sends only the id', async () => {
  globalThis.fetch = makeFetch(routes());
  $('accounts-reset').click();
  await flush();
  buttonsIn(accountsBody$().children[1])[0].click(); // reactivate
  assert.equal($('dialog-title').textContent, MESSAGES.ar['dialog.reactivate.title']);
  assert.equal($('dialog-reason-group').hidden, true);
  assert.equal($('dialog-confirm').disabled, false);
  $('action-form').fire('submit');
  await flush();
  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.equal(post.path, '/api/accounts/reactivate');
  assert.deepEqual(JSON.parse(post.init.body), { id: IDS.suspended });
});

test('delete warns, requires a reason and uses the delete route', async () => {
  globalThis.fetch = makeFetch(routes());
  $('accounts-reset').click();
  await flush();
  buttonsIn(accountsBody$().children[0])[1].click(); // delete
  assert.equal($('dialog-title').textContent, MESSAGES.ar['dialog.delete.title']);
  assert.equal($('dialog-note').textContent, MESSAGES.ar['dialog.delete.note']);
  assert.equal($('dialog-confirm').disabled, true);
  $('dialog-reason').value = 'fraud';
  $('dialog-reason').fire('input');
  $('action-form').fire('submit');
  await flush();
  const post = globalThis.fetch.calls.find((c) => c.method === 'POST');
  assert.equal(post.path, '/api/accounts/delete');
  assert.deepEqual(JSON.parse(post.init.body), { id: IDS.active, reason: 'fraud' });
});

test('a failed action keeps the dialog open with a banner and a working retry; no raw server text', async () => {
  let attempt = 0;
  globalThis.fetch = makeFetch(routes({
    'POST /api/accounts/suspend': () => (++attempt === 1 ? { status: 409, body: { error: 'SECRET account is already suspended', code: 'conflict' } } : { status: 200, body: { status: 'ok' } }),
  }));
  $('accounts-reset').click();
  await flush();
  buttonsIn(accountsBody$().children[0])[0].click();
  $('dialog-reason').value = 'spam';
  $('dialog-reason').fire('input');
  $('action-form').fire('submit');
  await flush();

  assert.equal($('action-dialog').open, true);
  assert.equal($('dialog-banner').hidden, false);
  assert.ok($('dialog-banner').textContent.includes(MESSAGES.ar['err.conflict']));
  assert.ok(!$('dialog-banner').textContent.includes('SECRET'));
  assert.equal($('dialog-confirm').disabled, false);

  buttonsIn($('dialog-banner'))[0].click(); // retry
  await flush();
  assert.equal(attempt, 2);
  assert.equal($('action-dialog').open, false);
});

test('every error kind has a banner message in the page language', async () => {
  for (const [status, key] of [[400, 'bad_request'], [403, 'forbidden'], [404, 'not_found'], [409, 'conflict'], [429, 'rate_limited'], [503, 'unavailable']]) {
    globalThis.fetch = makeFetch(routes({ 'GET /api/accounts': { status, body: { error: 'SECRET' } } }));
    $('accounts-reset').click();
    await flush();
    const banner = $('accounts-banner');
    assert.equal(banner.hidden, false, String(status));
    assert.ok(banner.textContent.includes(MESSAGES.ar[`err.${key}`]), String(status));
    assert.ok(!banner.textContent.includes('SECRET'));
    assert.equal(buttonsIn(banner).length, 1, `retry offered for ${status}`);
    assert.equal(isSignedIn(), true, `${status} must not log out`);
  }
});

test('a 401 during an action closes the dialog and returns to sign-in', async () => {
  globalThis.fetch = makeFetch(routes({ 'POST /api/accounts/suspend': { status: 401, body: { code: 'unauthorized' } } }));
  $('accounts-reset').click();
  await flush();
  buttonsIn(accountsBody$().children[0])[0].click();
  $('dialog-reason').value = 'spam';
  $('dialog-reason').fire('input');
  $('action-form').fire('submit');
  await flush();

  assert.equal($('action-dialog').open, false);
  assert.equal($('app-view').hidden, true);
  assert.equal($('login-view').hidden, false);
  assert.equal($('login-banner').textContent, MESSAGES.ar['login.expired']);
  assert.equal(getToken(), '');
  assertNoStudentData();
});

test('a 401 while loading a list also returns to sign-in', async () => {
  await signInOk();
  globalThis.fetch = makeFetch(routes({ 'GET /api/accounts': { status: 401, body: {} } }));
  $('accounts-reset').click();
  await flush();
  assert.equal($('app-view').hidden, true);
  assert.equal($('login-view').hidden, false);
  assert.equal(getToken(), '');
  assert.equal($('login-banner').textContent, MESSAGES.ar['login.expired']);
});

test('after a 401 no further request is made until the admin signs in again', async () => {
  globalThis.fetch = makeFetch(routes());
  $('accounts-reset').click();
  $('accounts-filter').fire('submit');
  await flush();
  assert.equal(globalThis.fetch.calls.length, 0);
});

test('the audit tab loads newest first with localized labels and falls back to the raw action', async () => {
  await signInOk();
  globalThis.fetch.calls.length = 0;
  $('tab-audit').click();
  await flush();
  assert.equal(globalThis.fetch.calls[0].url, '/api/audit?source=auth&page=1&limit=20');
  assert.equal($('panel-audit').hidden, false);
  assert.equal($('panel-accounts').hidden, true);
  assert.equal($('tab-audit').getAttribute('aria-selected'), 'true');
  const rows = auditBody$().children;
  assert.equal(rows.length, 2);
  assert.ok(rows[0].textContent.includes(MESSAGES.ar['audit.action.account_suspend']));
  assert.ok(rows[0].textContent.includes(MESSAGES.ar['audit.target.user']));
  assert.ok(rows[0].textContent.includes('spam'));
  assert.ok(rows[1].textContent.includes('something_new'));
  assert.ok(rows[1].textContent.includes('widget'));
});

test('the audit source switch loads the academy log without merging pages', async () => {
  globalThis.fetch.calls.length = 0;
  $('audit-source-academy').click();
  await flush();
  const call = globalThis.fetch.calls.find((c) => c.path === '/api/audit');
  assert.equal(call.url, '/api/audit?source=academy&page=1&limit=20');
  assert.equal($('audit-source-academy').getAttribute('aria-pressed'), 'true');
  assert.equal($('audit-source-auth').getAttribute('aria-pressed'), 'false');
  $('audit-source-auth').click();
  await flush();
  const back = globalThis.fetch.calls.filter((c) => c.path === '/api/audit').pop();
  assert.equal(back.url, '/api/audit?source=auth&page=1&limit=20');
});

test('hidden tabs cannot be activated', () => {
  const before = globalThis.fetch.calls.length;
  app.activate('requests');
  app.activate('files');
  assert.equal(globalThis.fetch.calls.length, before);
  assert.equal(app.activeTab, 'audit');
});

test('the language toggle flips direction and keeps the choice in the URL hash only', () => {
  $('lang-toggle').click();
  assert.equal(doc.documentElement.lang, 'en');
  assert.equal(doc.documentElement.dir, 'ltr');
  assert.equal(win.location.hash, '#en');
  assert.equal($('sign-out').textContent, MESSAGES.en['top.signOut']);
  assert.ok($('panel-audit').textContent.includes(MESSAGES.en['audit.source.academy']));
  $('lang-toggle').click();
  assert.equal(doc.documentElement.dir, 'rtl');
  assert.equal(win.location.hash, '#ar');
  assert.equal($('sign-out').textContent, MESSAGES.ar['top.signOut']);
  assert.ok($('panel-audit').textContent.includes(MESSAGES.ar['audit.source.academy']));
});

test('sign out clears the token and everything on screen', async () => {
  $('sign-out').click();
  await flush();
  assert.equal($('app-view').hidden, true);
  assert.equal($('login-view').hidden, false);
  assert.equal(getToken(), '');
  assert.equal($('admin-name').textContent, '');
  assertNoStudentData();
  assert.equal($('login-banner').hidden, true, 'signing out is not an error');
});

test('browser storage and cookies were never touched', () => {
  assert.deepEqual(storageHits, []);
});
