// Double-submit guards: while a save or confirm is in flight its button is
// off, a second submit sends nothing, and Esc cannot close the dialog (closing
// would reset it and allow the same action to be sent again).

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const LEVELS = [
  { key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى', name_en: '', order: 1, published: true, subject_count: 1, published_subject_count: 1 },
  { key: 'dip-a', study_type: 'diploma', name_ar: 'دبلومة الاختبار', name_en: '', order: 0, published: false, subject_count: 0, published_subject_count: 0 },
];
const SUBJECTS = [
  { id: 'sub-live', level_id: 'bachelor-y1', term: 'second', title_ar: 'مادة منشورة', title_en: '', description_ar: '', description_en: '', price: 0, currency: 'EGP', access_expires_at: '2027-06-01T20:59:59.000Z', order: 1, published: true, video_count: 2 },
];
const VIDEOS = [
  { id: 'vid-1', subject_id: 'sub-live', title_ar: 'الأول', youtube_video_id: 'dQw4w9WgXcQ', order: 1, duration_seconds: 750 },
  { id: 'vid-2', subject_id: 'sub-live', title_ar: 'الثاني', youtube_video_id: '9bZkp7q19f0', order: 2, duration_seconds: 61 },
];
const ACCOUNT = { id: '11111111-1111-4111-8111-111111111111', full_name: 'طالب تجريبي', email: 's@example.com', phone: '', status: 'active', created_at: '2026-10-01T00:00:00Z' };
const REQUEST = { id: 'req-1', user_id: ACCOUNT.id, subject_id: 'sub-live', subject_title_ar: 'مادة منشورة', level_name_ar: 'الفرقة الأولى', price: 100, status: 'pending', created_at: '2026-10-01T00:00:00Z', access_expires_at: '2027-06-01T20:59:59.000Z' };

/** A handler that waits for gate.open() before answering. */
function gated(body, status = 200) {
  const gate = { open: () => {} };
  const wait = new Promise((resolve) => { gate.open = resolve; });
  return { gate, handler: async () => { await wait; return { status, body }; } };
}

const base = () => ({
  'GET /api/whoami': { body: { name: 'Wael' } },
  'GET /api/accounts': { body: { items: [ACCOUNT], total: 1 } },
  'GET /api/requests': { body: { items: [REQUEST], total: 1, pending_count: 1 } },
  'GET /api/levels': { body: { levels: LEVELS } },
  'GET /api/subjects': { body: { items: SUBJECTS, total: 1, page: 1, limit: 15 } },
  'GET /api/videos': { body: { videos: VIDEOS } },
  'GET /api/entitlements': { body: { items: [] } },
});

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const buttonByText = (root, text) => buttonsIn(root).find((b) => b.textContent === text);
const clickText = (root, text) => {
  const b = buttonByText(root, text);
  assert.ok(b, `button ${text}`);
  b.click();
};
const posts = (path) => globalThis.fetch.calls.filter((c) => c.method === 'POST' && c.path === path);
const gets = (path) => globalThis.fetch.calls.filter((c) => c.method === 'GET' && c.path === path);
const hostDialog = () => $('catalog-dialogs').children.find((d) => d.tagName === 'DIALOG' && d.open);

async function signIn(extra = {}) {
  globalThis.fetch = makeFetch({ ...base(), ...extra });
  if (!isSignedIn()) {
    $('login-token').value = 'admin-token-xyz';
    $('login-form').fire('submit');
    await flush();
  }
  assert.equal(isSignedIn(), true);
}

main(doc, win);

test('suspend: a second submit and Esc during the request change nothing', async () => {
  const { gate, handler } = gated({ status: 'ok' });
  await signIn({ 'POST /api/accounts/suspend': handler });
  $('tab-accounts').click();
  await flush();
  clickText($('query:#accounts-table tbody'), t('action.suspend'));
  const dialog = $('action-dialog');
  assert.equal(dialog.open, true);
  $('dialog-reason').value = 'سبب';
  $('dialog-reason').fire('input');
  $('action-form').fire('submit');
  await flush();
  assert.equal($('dialog-confirm').disabled, true);
  $('action-form').fire('submit');
  const esc = dialog.fire('cancel');
  await flush();
  assert.equal(esc.defaultPrevented, true, 'Esc is stopped while the request is in flight');
  assert.equal(dialog.open, true);
  assert.equal(posts('/api/accounts/suspend').length, 1);
  gate.open();
  await flush();
  assert.equal(dialog.open, false);
  assert.equal(posts('/api/accounts/suspend').length, 1);
  assert.equal(dialog.fire('cancel').defaultPrevented, false, 'Esc works again once idle');
});

test('catalog confirm: delete is sent once and Esc cannot close it meanwhile', async () => {
  const { gate, handler } = gated({ status: 'ok' });
  await signIn({ 'POST /api/levels/delete': handler });
  $('tab-catalog').click();
  await flush();
  const card = $('catalog-content').findAll((c) => c.className === 'card level-card')[1];
  clickText(card, t('catalog.delete'));
  const dialog = hostDialog();
  const form = dialog.findAll((c) => c.tagName === 'FORM')[0];
  form.fire('submit');
  form.fire('submit');
  const esc = dialog.fire('cancel');
  await flush();
  assert.equal(esc.defaultPrevented, true);
  assert.equal(dialog.open, true);
  assert.equal(posts('/api/levels/delete').length, 1);
  gate.open();
  await flush();
  assert.equal(dialog.open, false);
});

test('catalog form: Save is sent once and Esc cannot close the form meanwhile', async () => {
  const { gate, handler } = gated({ key: 'dip-a' });
  await signIn({ 'POST /api/levels/update': handler });
  $('tab-catalog').click();
  await flush();
  const card = $('catalog-content').findAll((c) => c.className === 'card level-card')[1];
  clickText(card, t('catalog.edit'));
  const dialog = hostDialog();
  const form = dialog.findAll((c) => c.tagName === 'FORM')[0];
  dialog.findAll((c) => c.tagName === 'INPUT')[0].value = 'اسم جديد';
  form.fire('submit');
  form.fire('submit');
  const esc = dialog.fire('cancel');
  await flush();
  assert.equal(esc.defaultPrevented, true);
  assert.equal(dialog.open, true);
  assert.equal(posts('/api/levels/update').length, 1);
  gate.open();
  await flush();
  assert.equal(dialog.open, false);
});

test('request accept: one request, Esc stopped, dialog closes after the answer', async () => {
  const { gate, handler } = gated({ status: 'ok' });
  await signIn({ 'POST /api/requests/accept': handler, 'GET /api/accounts': { body: { items: [ACCOUNT], total: 1 } } });
  $('tab-requests').click();
  await flush();
  clickText($('query:#requests-table tbody'), t('requests.action.accept'));
  const dialog = $('request-accept-dialog');
  assert.equal(dialog.open, true);
  $('request-accept-form').fire('submit');
  $('request-accept-form').fire('submit');
  const esc = dialog.fire('cancel');
  await flush();
  assert.equal(esc.defaultPrevented, true);
  assert.equal(posts('/api/requests/accept').length, 1);
  gate.open();
  await flush();
  assert.equal(dialog.open, false);
});

test('opening the grant dialog twice quickly fetches once', async () => {
  await signIn();
  $('tab-accounts').click();
  await flush();
  clickText($('query:#accounts-table tbody'), t('action.subjects'));
  await flush();
  globalThis.fetch.calls.length = 0;
  $('entitlements-grant-btn').click();
  $('entitlements-grant-btn').click();
  await flush();
  assert.equal(gets('/api/levels').length, 1);
  assert.equal($('entitlement-grant-dialog').open, true);
});

test('reordering is frozen while the order is being saved', async () => {
  const { gate, handler } = gated({ videos: VIDEOS });
  await signIn({ 'POST /api/videos/reorder': handler });
  $('tab-catalog').click();
  await flush();
  clickText($('catalog-content'), t('catalog.open'));
  await flush();
  clickText($('catalog-content'), t('catalog.open'));
  await flush();
  const rows = () => $('catalog-content').findAll((c) => c.tagName === 'TBODY')[0].children;
  clickText(rows()[0], t('catalog.reorder.down'));
  clickText($('catalog-content'), t('catalog.reorder.save'));
  await flush();
  assert.ok(rows()[0].textContent.includes('الثاني'));
  buttonByText(rows()[0], t('catalog.reorder.down'))?.click();
  assert.ok(rows()[0].textContent.includes('الثاني'), 'the order shown is the order being saved');
  assert.equal(posts('/api/videos/reorder').length, 1);
  gate.open();
  await flush();
});
