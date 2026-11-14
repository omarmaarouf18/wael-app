// The phone is optional at signup (owner amendment 2026-11-03, App Store
// 5.1.1(v)). A student without a phone shows "—" in the accounts list and in
// the access requests table, whether auth sends "" or null; search by phone
// still goes to the server as typed.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const ACCOUNTS = [
  { id: '11111111-1111-4111-8111-111111111111', full_name: 'بدون هاتف', email: 'nophone@example.com', phone: '', status: 'active', created_at: '2026-11-01T10:00:00Z' },
  { id: '22222222-2222-4222-8222-222222222222', full_name: 'Null Phone', email: 'nullphone@example.com', phone: null, status: 'active', created_at: '2026-11-02T10:00:00Z' },
  { id: '33333333-3333-4333-8333-333333333333', full_name: 'Has Phone', email: 'hasphone@example.com', phone: '+201000000009', status: 'active', created_at: '2026-11-03T10:00:00Z' },
];
const REQUESTS = [
  {
    id: 'req-np',
    user_id: ACCOUNTS[0].id,
    subject_id: 'sub-1',
    subject_title_ar: 'لغة عربية',
    level_name_ar: 'الفرقة الأولى',
    price: 150,
    created_at: '2026-11-02T12:00:00Z',
    access_expires_at: '2027-06-01T20:59:59.000Z',
    status: 'pending',
  },
];

const accountQueries = [];
globalThis.fetch = makeFetch({
  'GET /api/whoami': { body: { name: 'Wael' } },
  'GET /api/accounts': (req) => {
    const url = req.url || '';
    accountQueries.push(url);
    if (url.includes('ids=')) return { body: { items: [ACCOUNTS[0]], total: 1 } };
    return { body: { items: ACCOUNTS, total: ACCOUNTS.length, page: 1, limit: 15 } };
  },
  'GET /api/requests': { body: { items: REQUESTS, total: 1, pending_count: 1 } },
  'GET /api/subjects': { body: { items: [{ id: 'sub-1', title_ar: 'لغة عربية' }], total: 1 } },
  'GET /api/audit': { body: { items: [], total: 0 } },
});

const $ = (id) => doc.getElementById(id);
// Third column of the accounts table and second of the requests table hold the phone.
const phoneCell = (tableId, row, col) => doc.querySelector(`#${tableId} tbody`).children[row].children[col].textContent;

main(doc, win);

test('accounts list shows — for a student without a phone', async () => {
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
  $('tab-accounts').click();
  await flush();
  assert.equal(phoneCell('accounts-table', 0, 2), '—');
  assert.equal(phoneCell('accounts-table', 1, 2), '—');
  assert.equal(phoneCell('accounts-table', 2, 2), '+201000000009');
});

test('search by phone still reaches the server', async () => {
  $('accounts-search').value = '01000000009';
  $('accounts-filter').fire('submit');
  await flush();
  assert.ok(accountQueries.some((u) => u.includes('search=01000000009')), accountQueries.join(' | '));
});

test('access requests show — for a student without a phone', async () => {
  $('tab-requests').click();
  await flush();
  assert.equal(phoneCell('requests-table', 0, 1), '—');
  assert.ok(doc.querySelector('#requests-table tbody').children[0].textContent.includes('nophone@example.com'));
});
