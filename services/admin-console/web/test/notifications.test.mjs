// Notifications tab: compose with preview count, confirm-then-send, history
// with delivered/failed counts and retry, plus the shared guards.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createApi } from '../js/api.js';
import { MESSAGES, formatNumber, setLang, t } from '../js/i18n.js';
import { audienceOf, mountNotifications, precheckCompose, statusLabel } from '../js/notifications.js';
import { FakeDocument } from './fake-dom.mjs';
import { flush, makeFetch } from './fake-dom.mjs';

let doc;
const $ = (id) => doc.getElementById(id);
const text = (el) => (el?.textContent ?? '');

const LEVELS = [{ key: 'bachelor-y1', name_ar: 'الفرقة الأولى', study_type: 'bachelor' }];
const SUBJECTS = [
  { id: 'sub-1', title_ar: 'رياضيات', published: true },
  { id: 'sub-2', title_ar: 'فيزياء', published: false },
];
const HISTORY = [
  {
    id: 'bc-1', audience: 'all', title_ar: 'عنوان', title_en: 'Title', body_ar: 'نص', body_en: 'Body',
    created_by: 'admin-1', created_at: '2026-10-12T10:00:00Z', status: 'done', total: 3, delivered: 3, failed: 0,
  },
  {
    id: 'bc-2', audience: 'subject:sub-1', title_ar: 'مادة', title_en: '', body_ar: 'درس', body_en: '',
    created_by: 'admin-1', created_at: '2026-10-12T11:00:00Z', status: 'partial', total: 2, delivered: 1, failed: 1,
  },
];

function setup(routes = {}) {
  setLang('ar');
  doc = new FakeDocument();
  globalThis.document = doc;
  const fetchFn = makeFetch({
    'GET /api/levels': { body: { levels: LEVELS } },
    'GET /api/subjects': { body: { items: SUBJECTS, total: 2, page: 1, limit: 100 } },
    'GET /api/broadcasts': { body: { items: HISTORY, total: 2, page: 1, limit: 15 } },
    'POST /api/broadcasts/preview': { body: { audience: 'all', recipient_count: 3 } },
    'POST /api/broadcasts/create': { status: 201, body: { id: 'bc-9', status: 'queued', total: 3, delivered: 0, failed: 0 } },
    'POST /api/broadcasts/retry': { body: { id: 'bc-2', status: 'sending', retried: 1, total: 2, delivered: 1, failed: 1 } },
    ...routes,
  });
  const api = createApi({ fetchFn, getToken: () => 'tok', onUnauthorized: () => {} });
  const mod = mountNotifications({ api, doc });
  return { mod, fetchFn };
}

const posts = (fetchFn, path) => fetchFn.calls.filter((c) => c.method === 'POST' && c.path === path);
const hostDialog = () => $('catalog-dialogs').children.find((d) => d.tagName === 'DIALOG' && d.open);

function fillForm() {
  $('notifications-title-ar').value = 'عنوان مهم';
  $('notifications-body-ar').value = 'نص الرسالة';
}

test('precheckCompose flags missing fields, lengths and a missing subject', () => {
  assert.equal(precheckCompose({ titleAr: '', bodyAr: 'ن', audience: 'all' }), 'notifications.check.title');
  assert.equal(precheckCompose({ titleAr: 'ع'.repeat(81), bodyAr: 'ن', audience: 'all' }), 'notifications.check.title');
  assert.equal(precheckCompose({ titleAr: 'ع', titleEn: 'e'.repeat(81), bodyAr: 'ن', audience: 'all' }), 'notifications.check.title');
  assert.equal(precheckCompose({ titleAr: 'ع', bodyAr: '', audience: 'all' }), 'notifications.check.body');
  assert.equal(precheckCompose({ titleAr: 'ع', bodyAr: 'ن'.repeat(501), audience: 'all' }), 'notifications.check.body');
  assert.equal(precheckCompose({ titleAr: 'ع', bodyAr: 'ن', audience: 'subject', subjectId: '' }), 'notifications.check.subject');
  assert.equal(precheckCompose({ titleAr: 'ع', bodyAr: 'ن', audience: 'all' }), null);
  assert.equal(precheckCompose({ titleAr: 'ع', bodyAr: 'ن', audience: 'subject', subjectId: 'sub-1' }), null);
  assert.equal(audienceOf({ audience: 'all' }), 'all');
  assert.equal(audienceOf({ audience: 'subject', subjectId: 'sub-1' }), 'subject:sub-1');
});

test('statusLabel covers every broadcast status in both languages', () => {
  setLang('ar');
  assert.equal(statusLabel('done'), 'تم');
  assert.equal(statusLabel('partial'), 'جزئي');
  setLang('en');
  assert.equal(statusLabel('done'), 'Done');
  assert.equal(statusLabel('sending'), 'Sending');
  assert.equal(statusLabel('bogus'), 'bogus');
  setLang('ar');
});

test('load renders levels and history with counts and a retry action on partial', async () => {
  const { mod } = setup();
  await mod.load();
  await flush();
  assert.equal(text($('notifications-preview-count')), '');
  const rows = $('notifications-history-body').children.filter((c) => c.tagName === 'TR');
  assert.equal(rows.length, 2);
  const partial = text(rows[1]);
  assert.ok(partial.includes('جزئي'), partial);
  assert.ok(partial.includes(t('notifications.retry')), partial);
  const done = text(rows[0]);
  assert.ok(done.includes('تم'), done);
  assert.ok(!done.includes(t('notifications.retry')), done);
});

test('preview shows the dry-run count', async () => {
  const { mod, fetchFn } = setup();
  await mod.load();
  await flush();
  fillForm();
  $('notifications-preview').click();
  await flush();
  assert.equal(posts(fetchFn, '/api/broadcasts/preview').length, 1);
  assert.equal(text($('notifications-preview-count')), t('notifications.previewCount', { n: formatNumber(3) }));
});

test('send previews, confirms with the exact count, then creates and toasts', async () => {
  const { mod, fetchFn } = setup();
  await mod.load();
  await flush();
  fillForm();
  $('notifications-form').fire('submit');
  await flush();
  await flush();
  assert.equal(posts(fetchFn, '/api/broadcasts/preview').length, 1);
  const dialog = hostDialog();
  assert.ok(dialog, 'confirm dialog opens');
  const note = text(dialog);
  assert.ok(note.includes(t('notifications.confirmNote', { n: formatNumber(3) })), `confirm note shows the count: ${note}`);
  const form = dialog.findAll((c) => c.tagName === 'FORM')[0];
  form.fire('submit');
  await flush();
  const creates = posts(fetchFn, '/api/broadcasts/create');
  assert.equal(creates.length, 1);
  const sent = JSON.parse(creates[0].init.body);
  assert.deepEqual(sent, { audience: 'all', title_ar: 'عنوان مهم', title_en: '', body_ar: 'نص الرسالة', body_en: '' });
  assert.ok(text($('toast')).includes(t('notifications.toast.sent')), text($('toast')));
  assert.equal($('notifications-title-ar').value, '', 'form clears after send');
});

test('send with an empty form shows the inline check, nothing posted', async () => {
  const { mod, fetchFn } = setup();
  await mod.load();
  await flush();
  $('notifications-form').fire('submit');
  await flush();
  assert.equal(posts(fetchFn, '/api/broadcasts/preview').length, 0);
  assert.ok(text($('notifications-banner')).includes(t('notifications.check.title')));
});

test('a second send during the preview changes nothing (double submit)', async () => {
  const gate = { open: () => {} };
  const wait = new Promise((resolve) => { gate.open = resolve; });
  const { mod, fetchFn } = setup({
    'POST /api/broadcasts/preview': async () => {
      await wait;
      return { body: { audience: 'all', recipient_count: 3 } };
    },
  });
  await mod.load();
  await flush();
  fillForm();
  $('notifications-form').fire('submit');
  $('notifications-form').fire('submit');
  await flush();
  assert.equal(posts(fetchFn, '/api/broadcasts/preview').length, 1);
  gate.open();
  await flush();
  await flush();
  assert.ok(hostDialog(), 'one confirm opens');
});

test('a 429 with Retry-After cools the tab down', async () => {
  const { mod } = setup({
    'POST /api/broadcasts/preview': { status: 429, body: { error: 'slow', code: 'rate_limited' }, headers: { 'Retry-After': '2' } },
  });
  await mod.load();
  await flush();
  fillForm();
  $('notifications-preview').click();
  await flush();
  assert.equal($('notifications-submit').disabled, true);
  assert.equal($('notifications-preview').disabled, true);
});

test('retry on a partial row posts once and toasts', async () => {
  const { mod, fetchFn } = setup();
  await mod.load();
  await flush();
  const rows = $('notifications-history-body').children.filter((c) => c.tagName === 'TR');
  const retryBtn = rows[1].findAll((c) => c.tagName === 'BUTTON')[0];
  retryBtn.click();
  await flush();
  const retries = posts(fetchFn, '/api/broadcasts/retry');
  assert.equal(retries.length, 1);
  assert.deepEqual(JSON.parse(retries[0].init.body), { id: 'bc-2' });
  assert.ok(text($('toast')).includes(t('notifications.toast.retried')));
});

test('unsaved guard: a filled form is dirty, discard clears it', async () => {
  const { mod } = setup();
  await mod.load();
  await flush();
  assert.equal(mod.isDirty(), false);
  fillForm();
  assert.equal(mod.isDirty(), true);
  mod.discard();
  assert.equal(mod.isDirty(), false);
  assert.equal($('notifications-title-ar').value, '');
});

test('tab name and hint read correctly in both languages', () => {
  setup();
  setLang('ar');
  assert.equal(t('tabs.notifications'), 'الإشعارات');
  assert.equal(t('notifications.hint'), MESSAGES.ar['notifications.hint']);
  assert.ok(!MESSAGES.ar['notifications.hint'].includes('push'));
  setLang('en');
  assert.equal(t('tabs.notifications'), 'Notifications');
  assert.equal(t('notifications.title'), 'Notifications');
  setLang('ar');
});

test('no payment wording in the Notifications texts', () => {
  const words = /دفع|ادفع|شراء|اشتري|استرداد|payment|\bpay\b|\bbuy\b|purchase|refund|price|سعر/i;
  for (const lang of ['ar', 'en']) {
    for (const [key, value] of Object.entries(MESSAGES[lang])) {
      if (key.startsWith('notifications.')) {
        assert.ok(!words.test(value), `${lang} ${key}: ${value}`);
      }
    }
  }
});
