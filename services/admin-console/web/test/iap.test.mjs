// iOS packages tab (#56) on the fake DOM: tab render, create/edit dialog
// validation, delete refused with purchases, purchases list, the coverage
// warning in the Subjects view and the "iap" entitlement source label.
// Runs in its own process with its own main(), like app.test.mjs.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES, formatNumber } = await import('../js/i18n.js');
const { readPackageForm, groupSubjects, IAP_SUBJECTS_MAX } = await import('../js/iap-dialog.js');
const { uncoveredSet } = await import('../js/iap.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const STUDENT = '11111111-1111-4111-8111-111111111111';
const LEVELS = [
  { key: 'bachelor-y2', study_type: 'bachelor', name_ar: 'الفرقة الثانية', order: 2, published: true },
  { key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى', order: 1, published: true },
];
const SUBJECTS = [
  { id: 'sub-law', level_id: 'bachelor-y1', term: 'first', title_ar: 'قانون مدني', title_en: '', price: 0, access_expires_at: '2027-06-01T20:59:59.000Z', order: 0, published: true, video_count: 2 },
  { id: 'sub-const', level_id: 'bachelor-y1', term: 'second', title_ar: 'قانون دستوري', title_en: '', price: 0, access_expires_at: '2027-06-01T20:59:59.000Z', order: 1, published: true, video_count: 1 },
  { id: 'sub-draft', level_id: 'bachelor-y2', term: 'first', title_ar: 'مادة مسودة', title_en: '', price: 0, access_expires_at: '2027-06-01T20:59:59.000Z', order: 0, published: false, video_count: 0 },
];
const PACKAGES = [
  {
    id: 'pkg_aaaaaaaaaaaaaaaa', product_id: 'com.wael.app.pkg.year1', title_ar: 'الفرقة الأولى', title_en: 'Year 1',
    description_ar: '', description_en: '', subject_ids: ['sub-law'], available_until: '2027-01-20T21:59:59Z',
    published: true, position: 1, created_at: '2026-11-14T08:00:00Z', updated_at: '2026-11-14T08:00:00Z',
  },
  {
    id: 'pkg_bbbbbbbbbbbbbbbb', product_id: 'com.wael.app.pkg.year2', title_ar: 'الفرقة الثانية', title_en: 'Year 2',
    description_ar: '', description_en: '', subject_ids: [], available_until: null,
    published: false, position: 2, created_at: '2026-11-14T08:00:00Z', updated_at: '2026-11-14T08:00:00Z',
  },
];
const PURCHASES = [
  {
    id: 'pur-1', transaction_id: '2000000123456789', original_transaction_id: '2000000123456789', user_id: STUDENT,
    package_id: PACKAGES[0].id, product_id: PACKAGES[0].product_id, environment: 'Sandbox',
    purchase_date: '2026-11-14T09:00:00Z', granted_subject_ids: ['sub-law'], skipped: [], created_at: '2026-11-14T09:00:01Z',
  },
];

function routes(overrides = {}) {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { body: { items: [{ id: STUDENT, full_name: 'علي أحمد', email: 'ali@example.com', phone: '', status: 'active', created_at: '2026-10-01T00:00:00Z' }], total: 1 } },
    'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
    'GET /api/audit': { body: { items: [], total: 0 } },
    'GET /api/levels': { body: { levels: LEVELS } },
    'GET /api/subjects': { body: { items: SUBJECTS, total: SUBJECTS.length, page: 1, limit: 100 } },
    'GET /api/iap/packages': { body: { packages: PACKAGES } },
    'GET /api/iap/coverage': { body: { uncovered_subject_ids: ['sub-const'] } },
    'GET /api/iap/purchases': { body: { purchases: PURCHASES } },
    'POST /api/iap/packages/create': { status: 201, body: { ...PACKAGES[1], id: 'pkg_cccccccccccccccc' } },
    'POST /api/iap/packages/update': { body: PACKAGES[0] },
    'POST /api/iap/packages/delete': { status: 409, body: { error: 'SECRET server text', code: 'package_has_purchases', request_id: 'r1' } },
    'GET /api/entitlements': {
      body: {
        items: [{
          id: 'ent-iap', user_id: STUDENT, subject_id: 'sub-law', subject_title_ar: 'قانون مدني', level_name_ar: 'الفرقة الأولى',
          source: 'iap', is_revoked: false, is_expired: false, granted_at: '2026-11-14T09:00:01Z', expires_at: '2027-06-01T20:59:59.000Z',
        }],
        total: 1,
      },
    },
    ...overrides,
  };
}

const $ = (id) => doc.getElementById(id);
const t = (key, params) => {
  const raw = MESSAGES.ar[key];
  return params ? raw.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m)) : raw;
};
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const buttonByText = (root, text) => buttonsIn(root).find((b) => b.textContent === text);
const calls = (method, path) => globalThis.fetch.calls.filter((c) => c.method === method && c.path === path);
const iapDialog = () => $('iap-dialogs').children[0];
const openConfirm = () => $('catalog-dialogs').children.find((d) => d.tagName === 'DIALOG' && d.open);
const inputs = (root, type) => root.findAll((c) => c.tagName === 'INPUT' && c.getAttribute('type') === type);
const bannerText = (dialog) => dialog.findAll((c) => c.className === 'banner')[0];

function dialogFields() {
  const d = iapDialog();
  const text = inputs(d, 'text');
  const areas = d.findAll((c) => c.tagName === 'TEXTAREA');
  const boxes = inputs(d, 'checkbox');
  return {
    dialog: d,
    productId: text[0], titleAr: text[1], titleEn: text[2], descAr: areas[0], descEn: areas[1],
    subjects: boxes.filter((b) => b.getAttribute('value')), published: boxes.find((b) => !b.getAttribute('value')),
    until: inputs(d, 'date')[0], position: inputs(d, 'number')[0],
    form: d.children[0],
  };
}

async function signInOk(extra = {}) {
  globalThis.fetch = makeFetch(routes(extra));
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

async function openTab() {
  $('tab-iap').click();
  await flush();
  await flush();
}

const app = main(doc, win);

// --- pure helpers -----------------------------------------------------------

const VALID = {
  productId: 'com.wael.app.pkg.year1', titleAr: 'الفرقة الأولى', titleEn: 'Year 1', descAr: '', descEn: '',
  subjectIds: ['sub-law'], availableUntil: '', position: '', published: false,
};

test('dialog validation: product id, titles, descriptions, subjects, date and position', () => {
  assert.deepEqual(readPackageForm(VALID, 'create').body, {
    product_id: 'com.wael.app.pkg.year1', title_ar: 'الفرقة الأولى', title_en: 'Year 1', description_ar: '', description_en: '',
    subject_ids: ['sub-law'], available_until: null, position: 0, published: false,
  });
  for (const productId of ['', 'com.wael.app.year1', 'com.wael.app.pkg.', 'com.wael.app.pkg.Year1', 'com.wael.app.pkg.year 1']) {
    assert.equal(readPackageForm({ ...VALID, productId }, 'create').error, 'iap.err.productId', productId);
  }
  assert.equal(readPackageForm({ ...VALID, titleAr: '  ' }, 'create').error, 'iap.err.titles');
  assert.equal(readPackageForm({ ...VALID, titleEn: '' }, 'create').error, 'iap.err.titles');
  assert.equal(readPackageForm({ ...VALID, titleAr: 'س'.repeat(81) }, 'create').error, 'iap.err.titles');
  assert.ok(readPackageForm({ ...VALID, titleAr: 'س'.repeat(80) }, 'create').body);
  assert.equal(readPackageForm({ ...VALID, descEn: 'x'.repeat(1001) }, 'create').error, 'iap.err.description');
  const many = Array.from({ length: IAP_SUBJECTS_MAX + 1 }, (_, i) => `s${i}`);
  assert.equal(readPackageForm({ ...VALID, subjectIds: many }, 'create').error, 'iap.err.subjects');
  assert.equal(readPackageForm({ ...VALID, availableUntil: '2027-02-30' }, 'create').error, 'iap.err.date');
  assert.equal(readPackageForm({ ...VALID, availableUntil: '2027-01-20' }, 'create').body.available_until, '2027-01-20T21:59:59.000Z');
  for (const position of ['-1', '1.5', 'x']) assert.equal(readPackageForm({ ...VALID, position }, 'create').error, 'iap.err.position', position);
  assert.equal(readPackageForm({ ...VALID, position: '3' }, 'create').body.position, 3);
});

test('dialog validation on edit: product id is never sent and an empty date clears the end', () => {
  const read = readPackageForm({ ...VALID, productId: 'anything at all', published: true }, 'edit');
  assert.equal(read.error, undefined);
  assert.equal('product_id' in read.body, false);
  assert.equal(read.body.available_until, null);
  assert.equal(read.body.published, true);
});

test('subjects are grouped by level order, then term', () => {
  const groups = groupSubjects(LEVELS, SUBJECTS);
  assert.deepEqual(groups.map((g) => g.key), ['bachelor-y2', 'bachelor-y1']);
  assert.deepEqual(groups[1].terms.map((x) => x.term), ['first', 'second']);
  assert.deepEqual(groups[1].terms[0].subjects.map((s) => s.id), ['sub-law']);
  assert.deepEqual(uncoveredSet({ uncovered_subject_ids: ['a', 3, 'b'] }), new Set(['a', 'b']));
  assert.deepEqual(uncoveredSet(null), new Set());
});

// --- tab flows ----------------------------------------------------------------

test('the iOS packages tab renders every package with its Product ID, subject count, end date and status', async () => {
  await signInOk();
  assert.equal($('tab-iap').hidden, false);
  await openTab();
  assert.equal(app.activeTab, 'iap');
  assert.equal($('panel-iap').hidden, false);
  const rows = $('iap-body').children;
  assert.equal(rows.length, 2);
  assert.ok(rows[0].textContent.includes('الفرقة الأولى'));
  assert.ok(rows[0].textContent.includes('Year 1'));
  assert.ok(rows[0].textContent.includes('com.wael.app.pkg.year1'));
  assert.ok(rows[0].textContent.includes(t('iap.subjectCount', { n: formatNumber(1) })));
  assert.ok(rows[0].textContent.includes(t('iap.published')));
  assert.ok(rows[1].textContent.includes(t('iap.noEnd')));
  assert.ok(rows[1].textContent.includes(t('iap.draft')));
  // No price anywhere: prices live in App Store Connect only.
  assert.ok(!$('panel-iap').textContent.includes(MESSAGES.ar['catalog.field.price']));
  assert.equal($('iap-coverage').hidden, false);
  assert.ok($('iap-coverage').textContent.includes(t('iap.coverage', { n: formatNumber(1) })));
  assert.equal(calls('GET', '/api/subjects')[0].url.includes('limit=100'), true);
});

test('create: an invalid form is refused locally; a valid one posts the cleaned body', async () => {
  $('iap-add').click();
  const f = dialogFields();
  assert.equal(f.dialog.open, true);
  assert.equal(f.productId.readOnly, false);
  assert.ok(f.dialog.textContent.includes(t('iap.field.productIdHint')));
  // Grouped picker: level headings in level order, draft marked.
  const text = f.dialog.textContent;
  assert.ok(text.indexOf('الفرقة الثانية') < text.indexOf('قانون مدني'));
  assert.ok(text.includes(`مادة مسودة (${t('iap.subjectDraft')})`));
  assert.equal(f.subjects.length, 3);

  f.form.fire('submit');
  await flush();
  assert.equal(calls('POST', '/api/iap/packages/create').length, 0);
  assert.equal(bannerText(f.dialog).hidden, false);
  assert.equal(bannerText(f.dialog).textContent, t('iap.err.productId'));

  f.productId.value = 'com.wael.app.pkg.year2';
  f.form.fire('submit');
  await flush();
  assert.equal(bannerText(f.dialog).textContent, t('iap.err.titles'));
  assert.equal(calls('POST', '/api/iap/packages/create').length, 0);

  f.titleAr.value = ' الفرقة الثانية ';
  f.titleEn.value = 'Year 2';
  f.subjects.find((b) => b.getAttribute('value') === 'sub-draft').checked = true;
  f.until.value = '2027-01-20';
  f.position.value = '2';
  f.form.fire('submit');
  await flush();
  await flush();
  const posted = calls('POST', '/api/iap/packages/create');
  assert.equal(posted.length, 1);
  assert.deepEqual(JSON.parse(posted[0].init.body), {
    product_id: 'com.wael.app.pkg.year2', title_ar: 'الفرقة الثانية', title_en: 'Year 2', description_ar: '', description_en: '',
    subject_ids: ['sub-draft'], available_until: '2027-01-20T21:59:59.000Z', position: 2, published: false,
  });
  assert.equal(f.dialog.open, false);
  assert.equal($('toast').textContent, t('iap.toast.created'));
});

test('edit: the Product ID is read-only and not sent; chosen subjects are pre-checked', async () => {
  await flush();
  buttonByText($('iap-body').children[0], t('iap.edit')).click();
  const f = dialogFields();
  assert.equal(f.dialog.open, true);
  assert.equal(f.productId.readOnly, true);
  assert.equal(f.productId.value, 'com.wael.app.pkg.year1');
  assert.equal(f.until.value, '2027-01-20');
  assert.equal(f.published.checked, true);
  assert.deepEqual(f.subjects.filter((b) => b.checked).map((b) => b.getAttribute('value')), ['sub-law']);
  f.subjects.find((b) => b.getAttribute('value') === 'sub-const').checked = true;
  f.until.value = '';
  f.form.fire('submit');
  await flush();
  await flush();
  const posted = calls('POST', '/api/iap/packages/update');
  assert.equal(posted.length, 1);
  const body = JSON.parse(posted[0].init.body);
  assert.equal(body.id, 'pkg_aaaaaaaaaaaaaaaa');
  assert.equal('product_id' in body, false);
  assert.deepEqual(body.subject_ids.sort(), ['sub-const', 'sub-law']);
  assert.equal(body.available_until, null);
  assert.equal($('toast').textContent, t('iap.toast.updated'));
});

test('a server error code in the dialog shows its own message, never server text', async () => {
  globalThis.fetch = makeFetch(routes({
    'POST /api/iap/packages/create': { status: 409, body: { error: 'SECRET server text', code: 'duplicate_product_id', request_id: 'r2' } },
  }));
  $('iap-add').click();
  const f = dialogFields();
  f.productId.value = 'com.wael.app.pkg.year1';
  f.titleAr.value = 'x';
  f.titleEn.value = 'y';
  f.form.fire('submit');
  await flush();
  await flush();
  assert.equal(f.dialog.open, true);
  assert.ok(bannerText(f.dialog).textContent.includes(t('err.duplicate_product_id')));
  assert.ok(!f.dialog.textContent.includes('SECRET'));
  f.dialog.close();
});

test('delete of a package with purchases shows the 409 message in the confirm dialog', async () => {
  buttonByText($('iap-body').children[0], t('iap.delete')).click();
  const dialog = openConfirm();
  assert.ok(dialog, 'confirm dialog open');
  assert.ok(dialog.textContent.includes(t('iap.deleteTitle')));
  dialog.children[0].fire('submit');
  await flush();
  await flush();
  assert.equal(calls('POST', '/api/iap/packages/delete').length, 1);
  assert.deepEqual(JSON.parse(calls('POST', '/api/iap/packages/delete')[0].init.body), { id: 'pkg_aaaaaaaaaaaaaaaa' });
  assert.equal(dialog.open, true);
  assert.ok(dialog.textContent.includes(t('err.package_has_purchases')));
  assert.ok(!dialog.textContent.includes('SECRET'));
  dialog.close();
});

test('purchases of one package are listed with environment and granted count', async () => {
  buttonByText($('iap-body').children[0], t('iap.purchases')).click();
  await flush();
  await flush();
  const call = calls('GET', '/api/iap/purchases').at(-1);
  assert.ok(call.url.includes('package_id=pkg_aaaaaaaaaaaaaaaa'), call.url);
  assert.ok(call.url.includes('limit=50'), call.url);
  assert.equal($('iap-purchases').hidden, false);
  assert.ok($('iap-purchases-title').textContent.includes('الفرقة الأولى'));
  const rows = $('iap-purchases-body').children;
  assert.equal(rows.length, 1);
  assert.ok(rows[0].textContent.includes('2000000123456789'));
  assert.ok(rows[0].textContent.includes(t('iap.env.sandbox')));
  $('iap-purchases-close').click();
  assert.equal($('iap-purchases').hidden, true);
});

test('the Subjects view marks a published subject no published iOS package covers', async () => {
  $('tab-catalog').click();
  await flush();
  await flush();
  const content = $('catalog-content');
  buttonByText(content, MESSAGES.ar['catalog.open']).click();
  await flush();
  await flush();
  const badges = content.findAll((c) => c.dataset?.coverage === 'uncovered');
  assert.equal(badges.length, 1);
  assert.equal(badges[0].getAttribute('title'), 'مش داخلة في أي باكدج iOS منشورة');
  const rowOf = (title) => content.findAll((c) => c.tagName === 'TR').find((r) => r.textContent.includes(title));
  assert.ok(rowOf('قانون دستوري').findAll((c) => c.dataset?.coverage === 'uncovered').length === 1);
  assert.ok(rowOf('قانون مدني').findAll((c) => c.dataset?.coverage === 'uncovered').length === 0);
});

test('a failed coverage call shows no badge and no error', async () => {
  globalThis.fetch = makeFetch(routes({ 'GET /api/iap/coverage': { status: 503, body: {} } }));
  $('tab-iap').click();
  await flush();
  await flush();
  assert.equal($('iap-coverage').hidden, true);
  assert.equal($('iap-banner').hidden, true);
  assert.equal($('iap-body').children.length, 2);
});

test('the entitlements dialog labels an Apple purchase as "شراء من آبل (iOS)"', async () => {
  globalThis.fetch = makeFetch(routes());
  $('tab-accounts').click();
  await flush();
  await flush();
  const accountsBody = doc.querySelector('#accounts-table tbody');
  buttonByText(accountsBody.children[0], MESSAGES.ar['action.subjects']).click();
  await flush();
  await flush();
  const rows = doc.querySelector('#entitlements-table tbody').children;
  assert.equal(rows.length, 1);
  assert.ok(rows[0].textContent.includes('شراء من آبل (iOS)'));
  assert.equal(MESSAGES.en['entitlements.source.iap'], 'Apple purchase (iOS)');
});

test('the tab label is "باكدجات iOS" / "iOS packages" and sign-out clears the tab', async () => {
  assert.equal(MESSAGES.ar['tabs.iap'], 'باكدجات iOS');
  assert.equal(MESSAGES.en['tabs.iap'], 'iOS packages');
  $('sign-out').click();
  await flush();
  assert.equal(isSignedIn(), false);
  assert.equal($('iap-body').children.length, 0);
  assert.equal($('iap-purchases').hidden, true);
  assert.equal($('iap-dialogs').children[0].open, false);
});
