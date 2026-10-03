// Catalog flows of the real modules on the fake DOM: levels, subjects with
// the Cairo end-of-day date, videos with reorder and the force-delete flow.
// Runs in its own process with its own main(), like app.test.mjs.

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
  { key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى', name_en: 'Year 1', order: 1, published: true, subject_count: 2, published_subject_count: 1 },
  { key: 'dip-a', study_type: 'diploma', name_ar: 'دبلومة الاختبار', name_en: '', order: 0, published: false, subject_count: 0, published_subject_count: 0 },
  { key: 'vocational', study_type: 'vocational', name_ar: 'التعليم المهني', name_en: '', order: 1, published: true, subject_count: 1, published_subject_count: 1 },
];
const SUBJECTS = [
  { id: 'sub-draft', level_id: 'bachelor-y1', term: 'first', title_ar: 'مادة مسودة', title_en: '', description_ar: '', description_en: '', price: 100, currency: 'EGP', access_expires_at: '2027-06-01T20:59:59.000Z', order: 0, published: false, video_count: 0, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' },
  { id: 'sub-live', level_id: 'bachelor-y1', term: 'second', title_ar: 'مادة منشورة', title_en: '', description_ar: '', description_en: '', price: 0, currency: 'EGP', access_expires_at: '2027-06-01T20:59:59.000Z', order: 1, published: true, video_count: 2, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' },
];
const VIDEOS = [
  { id: 'vid-1', subject_id: 'sub-live', title_ar: 'الأول', youtube_video_id: 'dQw4w9WgXcQ', order: 1, duration_seconds: 750, published: true, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' },
  { id: 'vid-2', subject_id: 'sub-live', title_ar: 'الثاني', youtube_video_id: '9bZkp7q19f0', order: 2, duration_seconds: 61, published: true, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' },
];

function routes(overrides = {}) {
  return {
    'GET /api/whoami': { body: { name: 'Wael' } },
    'GET /api/accounts': { body: { items: [], total: 0 } },
    'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
    'GET /api/audit': { body: { items: [], total: 0 } },
    'GET /api/levels': { body: { levels: LEVELS } },
    'POST /api/levels/create': { status: 201, body: { key: 'dip-b' } },
    'POST /api/levels/update': { body: { key: 'dip-b' } },
    'POST /api/levels/delete': { body: { status: 'ok' } },
    'GET /api/subjects': { body: { items: SUBJECTS, total: 2, page: 1, limit: 15 } },
    'POST /api/subjects/create': { status: 201, body: { ...SUBJECTS[0], id: 'sub-new' } },
    'POST /api/subjects/update': { body: SUBJECTS[0] },
    'POST /api/subjects/publish': { body: { ...SUBJECTS[0], published: true } },
    'POST /api/subjects/unpublish': { body: { ...SUBJECTS[0], published: false } },
    'GET /api/videos': { body: { videos: VIDEOS } },
    'POST /api/videos/create': { status: 201, body: { ...VIDEOS[0], id: 'vid-3' } },
    'POST /api/videos/update': { body: VIDEOS[0] },
    'POST /api/videos/reorder': { body: { videos: VIDEOS } },
    'POST /api/videos/delete': { body: { status: 'ok' } },
    ...overrides,
  };
}

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const content = () => $('catalog-content');
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const buttonByText = (root, text) => buttonsIn(root).find((b) => b.textContent === text);
/** Body rows of the rendered table (skips the header row). */
const bodyRows = () => {
  const bodies = content().findAll((c) => c.tagName === 'TBODY');
  assert.ok(bodies.length >= 1, 'a table body is rendered');
  return bodies[0].children;
};
const clickText = (root, text) => {
  const b = buttonByText(root, text);
  assert.ok(b, `button ${text}`);
  b.click();
};
/** The dialog currently shown (dialogs are built once and reused). */
const openDialog = () => {
  const host = $('catalog-dialogs');
  const dialog = host.children.find((d) => d.open);
  assert.ok(dialog, 'a dialog is open');
  return dialog;
};
const postsTo = (path) => globalThis.fetch.calls.filter((c) => c.method === 'POST' && c.path === path);

async function signInOk(extra = {}) {
  globalThis.fetch = makeFetch(routes(extra));
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

async function openCatalog() {
  $('tab-catalog').click();
  await flush();
}

function openLevel() {
  clickText(content(), t('catalog.open'));
}

// The levels view renders one card per level; the nth card's buttons.
function levelCard(index) {
  const cards = content().findAll((c) => c.className === 'card level-card');
  assert.ok(cards[index], `level card ${index}`);
  return cards[index];
}

const app = main(doc, win);

test('the catalog tab is visible and loads the three sections in order', async () => {
  await signInOk();
  assert.equal($('tab-catalog').hidden, false);
  await openCatalog();
  assert.equal(app.activeTab, 'catalog');
  const levelsCall = globalThis.fetch.calls.find((c) => c.path === '/api/levels');
  assert.ok(levelsCall, 'levels were requested');
  const text = content().textContent;
  const bachelor = text.indexOf(t('catalog.study.bachelor'));
  const diploma = text.indexOf(t('catalog.study.diploma'));
  const vocational = text.indexOf(t('catalog.study.vocational'));
  assert.ok(bachelor >= 0 && diploma > bachelor && vocational > diploma, 'fixed section order');
  assert.ok(text.includes('الفرقة الأولى'));
  assert.ok(text.includes(t('catalog.published')));
  assert.ok(text.includes(t('catalog.draft')));
});

test('creating a diploma sends study_type and the name only, then toasts and reloads', async () => {
  await openCatalog();
  globalThis.fetch.calls.length = 0;
  clickText(content(), t('catalog.addDiploma'));
  const dialog = openDialog();
  assert.equal(dialog.tagName, 'DIALOG');
  const inputs = dialog.findAll((c) => c.tagName === 'INPUT');
  assert.equal(inputs.length, 3);
  inputs[0].value = 'دبلومة جديدة';
  dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
  await flush();
  const posts = postsTo('/api/levels/create');
  assert.equal(posts.length, 1);
  assert.deepEqual(JSON.parse(posts[0].init.body), { study_type: 'diploma', name_ar: 'دبلومة جديدة' });
  assert.equal($('toast').hidden, false);
  assert.ok($('toast').textContent.includes(t('catalog.toast.created')));
  assert.ok(globalThis.fetch.calls.some((c) => c.path === '/api/levels'), 'levels reloaded');
});

test('seeded levels show no delete button; an empty diploma does', async () => {
  await openCatalog();
  assert.equal(buttonByText(levelCard(0), t('catalog.delete')), undefined, 'seeded level must not offer delete');
  assert.ok(buttonByText(levelCard(1), t('catalog.delete')), 'empty diploma offers delete');
});

test('the diploma section renders with an add button even when no diploma exists yet', async () => {
  globalThis.fetch = makeFetch(routes({ 'GET /api/levels': { body: { levels: LEVELS.filter((l) => l.study_type !== 'diploma') } } }));
  await openCatalog();
  const text = content().textContent;
  assert.ok(text.includes(t('catalog.study.diploma')), 'diploma section is always present');
  assert.ok(buttonByText(content(), t('catalog.addDiploma')), 'the first diploma is created from the empty section');
});

test('opening a level lists its subjects with the level filter', async () => {
  await openCatalog();
  globalThis.fetch.calls.length = 0;
  openLevel();
  await flush();
  const listCall = globalThis.fetch.calls.find((c) => c.path === '/api/subjects');
  assert.ok(listCall, 'subjects requested');
  assert.ok(listCall.url.includes('level_id=bachelor-y1'), listCall.url);
  const text = content().textContent;
  assert.ok(text.includes('مادة مسودة'));
  assert.ok(text.includes('مادة منشورة'));
});

test('subject create sends the date as 23:59:59 Cairo in UTC', async () => {
  globalThis.fetch.calls.length = 0;
  clickText($('catalog-toolbar'), t('catalog.addSubject'));
  const dialog = openDialog();
  const get = (label) => dialog.findAll((c) => c.tagName === 'INPUT' || c.tagName === 'SELECT' || c.tagName === 'TEXTAREA')
    .find((el) => el.tagName === 'INPUT' && el.attrs.get('type') === 'date');
  const dateInput = get();
  dateInput.value = '2027-06-01';
  // Fill the required title: the first text input in the dialog.
  const titleInput = dialog.findAll((c) => c.tagName === 'INPUT' && c.attrs.get('type') === 'text')[0];
  titleInput.value = 'مادة جديدة';
  dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
  await flush();
  const posts = postsTo('/api/subjects/create');
  assert.equal(posts.length, 1);
  const body = JSON.parse(posts[0].init.body);
  // June in Cairo is UTC+3 (DST), independently of the implementation.
  assert.equal(body.access_expires_at, new Date('2027-06-01T23:59:59+03:00').toISOString());
  assert.equal(body.title_ar, 'مادة جديدة');
});

test('publish is disabled at zero videos with a hint, enabled otherwise', async () => {
  const rows = bodyRows();
  assert.ok(rows.length >= 2, 'two subject rows');
  const draftPublish = buttonByText(rows[0], t('catalog.publish'));
  assert.equal(draftPublish.disabled, true);
  assert.ok(rows[0].textContent.includes(t('catalog.publishDisabledHint')));
  const liveRow = rows[1];
  const livePublish = buttonByText(liveRow, t('catalog.unpublish'));
  assert.ok(livePublish, 'published subject offers unpublish');
  assert.equal(livePublish.disabled, false);
});

test('a server error code shows the mapped message, never raw text', async () => {
  await signInOk({ 'GET /api/subjects': { status: 409, body: { error: 'SECRET detail', code: 'level_has_subjects' } } });
  await openCatalog();
  openLevel();
  await flush();
  const banner = $('catalog-banner');
  assert.equal(banner.hidden, false);
  assert.ok(banner.textContent.includes(t('err.level_has_subjects')));
  assert.ok(!banner.textContent.includes('SECRET'));
});

test('videos show duration, the id as text and a plain watch link', async () => {
  await signInOk();
  await openCatalog();
  openLevel();
  await flush();
  // Open the published subject (second row) to reach its videos.
  const rows = bodyRows();
  clickText(rows[1], t('catalog.open'));
  await flush();
  const videosCall = globalThis.fetch.calls.find((c) => c.path === '/api/videos');
  assert.ok(videosCall && videosCall.url.includes('subject_id=sub-live'));
  const text = content().textContent;
  assert.ok(text.includes('12:30'), '750 seconds as mm:ss');
  assert.ok(text.includes('1:01'), '61 seconds as mm:ss');
  assert.ok(text.includes('dQw4w9WgXcQ'), 'id as text');
  const links = content().findAll((c) => c.tagName === 'A');
  assert.ok(links.length >= 2);
  assert.equal(links[0].getAttribute('href'), 'https://www.youtube.com/watch?v=dQw4w9WgXcQ');
  assert.equal(links[0].getAttribute('target'), '_blank');
  assert.equal(links[0].getAttribute('rel'), 'noopener noreferrer');
});

test('reorder sends the full id list and flags unsaved order as dirty', async () => {
  const rows = () => bodyRows();
  clickText(rows()[0], t('catalog.reorder.down'));
  const save = buttonByText(content(), t('catalog.reorder.save'));
  assert.ok(save && !save.disabled, 'save enabled once dirty');
  assert.ok(content().textContent.includes(t('catalog.reorder.dirty')));
  globalThis.fetch.calls.length = 0;
  save.click();
  await flush();
  const posts = postsTo('/api/videos/reorder');
  assert.equal(posts.length, 1);
  assert.deepEqual(JSON.parse(posts[0].init.body), { subject_id: 'sub-live', video_ids: ['vid-2', 'vid-1'] });
});

test('deleting the last video of a published subject needs a second force confirm', async () => {
  let attempts = 0;
  globalThis.fetch = makeFetch(routes({
    'POST /api/videos/delete': ({ init }) => {
      attempts += 1;
      const body = JSON.parse(init.body);
      if (!body.force) return { status: 409, body: { error: 'SECRET detail', code: 'last_video_of_published_subject' } };
      return { status: 200, body: { status: 'ok' } };
    },
  }));
  const rows = () => bodyRows();
  clickText(rows()[0], t('catalog.delete'));
  let dialog = openDialog();
  assert.ok(dialog.textContent.includes(t('catalog.deleteVideoNote')));
  dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
  await flush();
  assert.equal(attempts, 1);
  dialog = openDialog();
  assert.ok(dialog.textContent.includes(t('catalog.forceDeleteNote')), 'second confirm names the unpublish');
  assert.ok(!dialog.textContent.includes('SECRET'));
  dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
  await flush();
  assert.equal(attempts, 2);
  assert.equal($('toast').textContent.includes(t('catalog.toast.deleted')), true);
});

test('two quick publishes send one request (no double submit)', async () => {
  await signInOk();
  await openCatalog();
  openLevel();
  await flush();
  // The published row offers unpublish; use the diploma toggle instead:
  // toggle the diploma publish twice quickly.
  await openCatalog();
  const publishButton = buttonByText(levelCard(1), t('catalog.publish'));
  globalThis.fetch.calls.length = 0;
  publishButton.click();
  publishButton.click();
  await flush();
  assert.equal(postsTo('/api/levels/update').length, 1);
});

test('a 401 while loading returns to sign-in', async () => {
  await signInOk({ 'GET /api/levels': { status: 401, body: {} } });
  globalThis.fetch.calls.length = 0;
  await openCatalog();
  assert.equal($('app-view').hidden, true);
  assert.equal($('login-view').hidden, false);
});
