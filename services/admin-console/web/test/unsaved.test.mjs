// The unsaved-changes guard on the catalog edit forms and the video order:
// closing a dirty form, switching tab, leaving a view and closing the page
// all ask first; a clean form never does.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
const unloadListeners = [];
const addListener = win.addEventListener;
win.addEventListener = (type, fn) => {
  if (type === 'beforeunload') unloadListeners.push(fn);
  addListener(type, fn);
};
globalThis.document = doc;

const LEVELS = [
  { key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى', name_en: 'Year 1', order: 1, published: true, subject_count: 2, published_subject_count: 1 },
  { key: 'dip-a', study_type: 'diploma', name_ar: 'دبلومة الاختبار', name_en: '', order: 0, published: false, subject_count: 0, published_subject_count: 0 },
];
const SUBJECTS = [
  { id: 'sub-live', level_id: 'bachelor-y1', term: 'second', title_ar: 'مادة منشورة', title_en: '', description_ar: '', description_en: '', price: 0, currency: 'EGP', access_expires_at: '2027-06-01T20:59:59.000Z', order: 1, published: true, video_count: 2 },
];
const VIDEOS = [
  { id: 'vid-1', subject_id: 'sub-live', title_ar: 'الأول', youtube_video_id: 'dQw4w9WgXcQ', order: 1, duration_seconds: 750 },
  { id: 'vid-2', subject_id: 'sub-live', title_ar: 'الثاني', youtube_video_id: '9bZkp7q19f0', order: 2, duration_seconds: 61 },
];

const routes = () => ({
  'GET /api/whoami': { body: { name: 'Wael' } },
  'GET /api/accounts': { body: { items: [], total: 0 } },
  'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
  'GET /api/levels': { body: { levels: LEVELS } },
  'POST /api/levels/create': { status: 201, body: { key: 'dip-b' } },
  'POST /api/levels/update': { body: { key: 'dip-a' } },
  'GET /api/subjects': { body: { items: SUBJECTS, total: 1, page: 1, limit: 15 } },
  'GET /api/videos': { body: { videos: VIDEOS } },
  'POST /api/videos/create': { status: 201, body: { ...VIDEOS[0], id: 'vid-3' } },
});

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const content = () => $('catalog-content');
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const buttonByText = (root, text) => buttonsIn(root).find((b) => b.textContent === text);
const clickText = (root, text) => {
  const b = buttonByText(root, text);
  assert.ok(b, `button ${text}`);
  b.click();
};
const dialogs = () => $('catalog-dialogs').children.filter((c) => c.tagName === 'DIALOG');
const discardDialog = () => dialogs().find((d) => buttonByText(d, t('unsaved.discard')));
const editDialog = () => dialogs().find((d) => d.open && d !== discardDialog());
const levelCard = (index) => content().findAll((c) => c.className === 'card level-card')[index];

async function signIn() {
  globalThis.fetch = makeFetch(routes());
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

async function openCatalog() {
  $('tab-catalog').click();
  await flush();
}

const app = main(doc, win);

/** Fires beforeunload on every installed listener and reports whether it was blocked. */
function unloadBlocked() {
  const event = { type: 'beforeunload', defaultPrevented: false, returnValue: undefined, preventDefault() { this.defaultPrevented = true; } };
  for (const fn of unloadListeners) fn(event);
  return event.defaultPrevented;
}

test('a clean form closes without asking, and the page may unload', async () => {
  await signIn();
  await openCatalog();
  clickText(levelCard(1), t('catalog.edit'));
  const dialog = editDialog();
  assert.ok(dialog.open);
  assert.equal(unloadBlocked(), false);
  clickText(dialog, t('common.cancel'));
  await flush();
  assert.equal(dialog.open, false);
  assert.equal(discardDialog()?.open ?? false, false);
});

test('a dirty level form asks before Cancel closes it; Stay keeps it, Discard closes it', async () => {
  await openCatalog();
  clickText(levelCard(1), t('catalog.edit'));
  const dialog = editDialog();
  dialog.findAll((c) => c.tagName === 'INPUT')[0].value = 'اسم معدّل';
  assert.equal(unloadBlocked(), true, 'closing the page is blocked while the form is dirty');

  clickText(dialog, t('common.cancel'));
  await flush();
  assert.equal(discardDialog().open, true);
  assert.equal(dialog.open, true);

  clickText(discardDialog(), t('unsaved.stay'));
  await flush();
  assert.equal(discardDialog().open, false);
  assert.equal(dialog.open, true, 'Stay keeps the form and its text');
  assert.equal(dialog.findAll((c) => c.tagName === 'INPUT')[0].value, 'اسم معدّل');

  clickText(dialog, t('common.cancel'));
  await flush();
  clickText(discardDialog(), t('unsaved.discard'));
  await flush();
  assert.equal(dialog.open, false);
  assert.equal(unloadBlocked(), false, 'nothing is held once the form is gone');
});

test('Esc on a dirty form is stopped and asks; Esc on a clean form just closes', async () => {
  await openCatalog();
  clickText(levelCard(1), t('catalog.edit'));
  const dialog = editDialog();
  const clean = dialog.fire('cancel');
  await flush();
  assert.equal(clean.defaultPrevented, true, 'the browser close is replaced by our own');
  assert.equal(dialog.open, false);
  assert.equal(discardDialog().open, false);

  clickText(levelCard(1), t('catalog.edit'));
  dialog.findAll((c) => c.tagName === 'INPUT')[1].value = 'English';
  const dirty = dialog.fire('cancel');
  await flush();
  assert.equal(dirty.defaultPrevented, true);
  assert.equal(dialog.open, true);
  assert.equal(discardDialog().open, true);
  // Esc on the question itself means stay.
  discardDialog().fire('cancel');
  discardDialog().close();
  await flush();
  assert.equal(dialog.open, true);
  dialog.close();
});

test('the subject and video forms are guarded the same way', async () => {
  await openCatalog();
  clickText(content(), t('catalog.open'));
  await flush();
  clickText($('catalog-toolbar'), t('catalog.addSubject'));
  const subjectDialog = editDialog();
  subjectDialog.findAll((c) => c.tagName === 'INPUT')[0].value = 'مادة جديدة';
  clickText(subjectDialog, t('common.cancel'));
  await flush();
  assert.equal(discardDialog().open, true);
  clickText(discardDialog(), t('unsaved.discard'));
  await flush();
  assert.equal(subjectDialog.open, false);

  // Open the subject's videos and the add-video form.
  clickText(content(), t('catalog.open'));
  await flush();
  clickText(content(), t('catalog.addVideo'));
  const videoDialog = editDialog();
  videoDialog.findAll((c) => c.tagName === 'INPUT')[1].value = 'dQw4w9WgXcQ';
  clickText(videoDialog, t('common.cancel'));
  await flush();
  assert.equal(discardDialog().open, true);
  clickText(discardDialog(), t('unsaved.stay'));
  await flush();
  assert.equal(videoDialog.open, true);
  videoDialog.close();
});

test('an unsaved video order blocks a tab switch until the admin discards it', async () => {
  await openCatalog();
  clickText(content(), t('catalog.open'));
  await flush();
  clickText(content(), t('catalog.open'));
  await flush();
  const rows = content().findAll((c) => c.tagName === 'TBODY')[0].children;
  clickText(rows[0], t('catalog.reorder.down'));
  assert.equal(unloadBlocked(), true);

  $('tab-accounts').click();
  await flush();
  assert.equal(app.activeTab, 'catalog', 'the switch waits for the answer');
  assert.equal(discardDialog().open, true);
  clickText(discardDialog(), t('unsaved.stay'));
  await flush();
  assert.equal(app.activeTab, 'catalog');
  assert.equal(buttonByText(content(), t('catalog.reorder.save')).disabled, false, 'the order is kept');

  $('tab-accounts').click();
  await flush();
  clickText(discardDialog(), t('unsaved.discard'));
  await flush();
  assert.equal(app.activeTab, 'accounts');
  assert.equal(unloadBlocked(), false);
});

test('the breadcrumb asks too, and a saved order does not', async () => {
  await openCatalog();
  clickText(content(), t('catalog.open'));
  await flush();
  clickText(content(), t('catalog.open'));
  await flush();
  const rows = () => content().findAll((c) => c.tagName === 'TBODY')[0].children;
  clickText(rows()[0], t('catalog.reorder.down'));
  const crumb = $('catalog-crumb');
  clickText(crumb, t('catalog.title'));
  await flush();
  assert.equal(discardDialog().open, true);
  clickText(discardDialog(), t('unsaved.stay'));
  await flush();
  assert.equal(content().textContent.includes(t('catalog.reorder.dirty')), true, 'still on the videos view');

  globalThis.fetch = makeFetch({ ...routes(), 'POST /api/videos/reorder': { body: { videos: VIDEOS } } });
  clickText(content(), t('catalog.reorder.save'));
  await flush();
  assert.equal(unloadBlocked(), false, 'a saved order is clean');
  clickText(crumb, t('catalog.title'));
  await flush();
  assert.equal(discardDialog().open, false);
});

test('signing out closes an open form and clears the guard', async () => {
  await openCatalog();
  clickText(levelCard(1), t('catalog.edit'));
  const dialog = editDialog();
  dialog.findAll((c) => c.tagName === 'INPUT')[0].value = 'x';
  assert.equal(unloadBlocked(), true);
  $('sign-out').click();
  await flush();
  assert.equal(dialog.open, false);
  assert.equal(unloadBlocked(), false);
});
