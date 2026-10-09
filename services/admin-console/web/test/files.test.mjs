import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, flush, makeFetch } from './fake-dom.mjs';

// Each test mounts the module on a fresh fake document, so listeners from an
// earlier test never fire; dom.js builds elements through the global.
let doc = new FakeDocument();
globalThis.document = doc;

const { mountFiles, precheckFile, formatSize, MAX_PDF_BYTES_HINT } = await import('../js/files.js');
const { createApi } = await import('../js/api.js');
const { MESSAGES, setLang } = await import('../js/i18n.js');
const { hasUnsavedChanges } = await import('../js/unsaved.js');

const SUBJECT = { id: 'subj-1', title_ar: 'القانون المدني', published: true, level_id: 'bachelor-y1' };
const DRAFT = { id: 'subj-2', title_ar: 'مادة مسودة', published: false, level_id: 'bachelor-y1' };
const FILE_ROW = { id: 'f-1', subject_id: 'subj-1', kind: 'note', title_ar: 'مذكرة ١', title_en: 'Notes 1', size_bytes: 2 * 1024 * 1024, created_at: '2026-10-09T08:00:00Z' };

const $ = (id) => doc.getElementById(id);

function text(el) {
  return el.textContent;
}

function pdfFile(name = 'notes.pdf', size = 1000) {
  const bytes = new Uint8Array(size);
  bytes.set(new TextEncoder().encode('%PDF-1.7'));
  return new File([bytes], name, { type: 'application/pdf' });
}

function setup(routes = {}) {
  setLang('ar');
  doc = new FakeDocument();
  globalThis.document = doc;
  $('files-file').files = [];
  const fetchFn = makeFetch({
    'GET /api/levels': { body: { levels: [{ key: 'bachelor-y1', name_ar: 'الفرقة الأولى', study_type: 'bachelor' }] } },
    'GET /api/subjects': { body: { items: [SUBJECT, DRAFT], total: 2, page: 1, limit: 100 } },
    'GET /api/files': { body: { files: [FILE_ROW] } },
    'POST /api/files/upload': { status: 201, body: { id: 'f-2' } },
    'POST /api/files/delete': { body: { status: 'ok' } },
    ...routes,
  });
  const api = createApi({ fetchFn, getToken: () => 'tok', onUnauthorized: () => {} });
  const mod = mountFiles({ api, doc });
  return { mod, fetchFn };
}

async function pickSubject(mod, subjectId = SUBJECT.id) {
  await mod.load();
  $('files-level').value = 'bachelor-y1';
  $('files-level').fire('change');
  await flush();
  await flush();
  $('files-subject').value = subjectId;
  $('files-subject').fire('change');
  await flush();
  await flush();
}

function fillForm({ kind = 'book', ar = 'كتاب القانون', en = 'Law book', file = pdfFile() } = {}) {
  $('files-kind').value = kind;
  $('files-title-ar').value = ar;
  $('files-title-en').value = en;
  $('files-file').files = file ? [file] : [];
  // Browsers show a chosen file in the input's value (and clear the
  // selection when the value is reset); the fake mirrors that.
  $('files-file').value = file ? `C:\\fakepath\\${file.name}` : '';
}

test('precheck: name, emptiness and the default size limit (UX only)', () => {
  assert.equal(precheckFile(null), 'files.check.noFile');
  assert.equal(precheckFile({ name: 'a.docx', size: 10 }), 'files.check.notPdf');
  assert.equal(precheckFile({ name: 'a.PDF', size: 0 }), 'files.check.empty');
  assert.equal(precheckFile({ name: 'a.pdf', size: MAX_PDF_BYTES_HINT + 1 }), 'files.check.tooLarge');
  assert.equal(precheckFile({ name: 'a.pdf', size: MAX_PDF_BYTES_HINT }), null);
  assert.equal(MAX_PDF_BYTES_HINT, 20 * 1024 * 1024);
});

test('sizes are shown in KB or MB with localized digits', () => {
  setLang('en');
  assert.equal(formatSize(500), '1 KB');
  assert.equal(formatSize(2 * 1024 * 1024), '2 MB');
  assert.equal(formatSize(1.25 * 1024 * 1024), '1.3 MB');
  setLang('ar');
  assert.ok(formatSize(2 * 1024 * 1024).includes('٢'));
});

test('picking a level and subject lists the subject files, drafts marked', async () => {
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  const subjectsCall = fetchFn.calls.find((c) => c.path === '/api/subjects');
  assert.match(subjectsCall.url, /level_id=bachelor-y1/);
  assert.match(subjectsCall.url, /limit=100/);
  assert.ok(!/published=/.test(subjectsCall.url), 'drafts are listed too');
  const options = $('files-subject').children.map(text);
  assert.ok(options.some((o) => o.includes(MESSAGES.ar['files.draft'])));
  const filesCall = fetchFn.calls.find((c) => c.path === '/api/files');
  assert.equal(filesCall.url, '/api/files?subject_id=subj-1');
  const body = text($('files-body'));
  assert.ok(body.includes('مذكرة ١'));
  assert.ok(body.includes('Notes 1'));
  assert.ok(body.includes(MESSAGES.ar['files.kind.note']));
  assert.equal($('files-submit').disabled, false);
});

test('without a subject the upload form is disabled and the list says why', async () => {
  const { mod } = setup();
  await mod.load();
  assert.equal($('files-submit').disabled, true);
  assert.equal($('files-title-ar').disabled, true);
  assert.ok(text($('files-body')).includes(MESSAGES.ar['files.pickSubjectFirst']));
});

test('upload sends the fields before the file, to the chosen subject, then reloads', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout'] });
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  fillForm();
  $('files-form').fire('submit');
  await flush();
  await flush();
  const call = fetchFn.calls.find((c) => c.path === '/api/files/upload');
  assert.equal(call.url, '/api/files/upload?subject_id=subj-1');
  assert.equal(call.init.method, 'POST');
  assert.ok(call.init.body instanceof FormData);
  assert.deepEqual([...call.init.body.keys()], ['kind', 'title_ar', 'title_en', 'file']);
  assert.equal(call.init.body.get('kind'), 'book');
  assert.equal(call.init.body.get('title_ar'), 'كتاب القانون');
  assert.equal(call.init.headers['Content-Type'], undefined, 'the browser sets the multipart boundary');
  assert.equal(call.init.headers['X-Admin-Token'], 'tok');
  assert.equal(text($('toast')), MESSAGES.ar['files.toast.uploaded']);
  assert.equal($('files-title-ar').value, '');
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files').length, 2, 'list reloaded');
  assert.equal(mod.isDirty(), false);
});

test('an empty English title is not sent', async () => {
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  fillForm({ en: '' });
  await mod.upload();
  const call = fetchFn.calls.find((c) => c.path === '/api/files/upload');
  assert.deepEqual([...call.init.body.keys()], ['kind', 'title_ar', 'file']);
});

test('pre-check failures stay in the page and send nothing', async () => {
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  fillForm({ file: new File(['x'], 'scan.png', { type: 'image/png' }) });
  await mod.upload();
  assert.equal(text($('files-form-banner')), MESSAGES.ar['files.check.notPdf']);
  fillForm({ ar: '   ' });
  await mod.upload();
  assert.equal(text($('files-form-banner')), MESSAGES.ar['files.check.title']);
  fillForm({ file: null });
  await mod.upload();
  assert.equal(text($('files-form-banner')), MESSAGES.ar['files.check.noFile']);
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files/upload').length, 0);
});

test('double submit sends one upload; cancel aborts it', async () => {
  let release;
  const { mod, fetchFn } = setup({
    'POST /api/files/upload': ({ init }) =>
      new Promise((resolve, reject) => {
        release = resolve;
        init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
      }),
  });
  await pickSubject(mod);
  fillForm();
  $('files-form').fire('submit');
  $('files-form').fire('submit');
  await flush();
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files/upload').length, 1);
  assert.equal($('files-submit').disabled, true);
  assert.equal(text($('files-submit')), MESSAGES.ar['files.uploading']);
  assert.equal($('files-cancel').hidden, false);
  assert.equal($('files-title-ar').disabled, true);
  assert.equal(mod.isDirty(), true, 'an upload in flight counts as unsaved');

  $('files-cancel').click();
  await flush();
  await flush();
  assert.equal(text($('files-form-banner')), MESSAGES.ar['files.cancelled']);
  assert.equal($('files-cancel').hidden, true);
  assert.equal($('files-submit').disabled, false);
  assert.equal($('files-title-ar').value, 'كتاب القانون', 'a cancelled upload keeps the form');
  assert.equal(typeof release, 'function');
});

test('server codes map to the page text, never to server text', async () => {
  for (const [status, code] of [[413, 'file_too_large'], [400, 'invalid_pdf'], [404, 'subject_not_found'], [400, 'invalid_kind']]) {
    const { mod } = setup({ 'POST /api/files/upload': { status, body: { code, error: 'SECRET server text' } } });
    await pickSubject(mod);
    fillForm();
    await mod.upload();
    const shown = text($('files-form-banner'));
    assert.equal(shown, MESSAGES.ar[`err.${code}`], code);
    assert.ok(!shown.includes('SECRET'));
  }
});

test('a 413 without a usable code still says the file is too large', async () => {
  const { mod } = setup({ 'POST /api/files/upload': { status: 413, body: {} } });
  await pickSubject(mod);
  fillForm();
  await mod.upload();
  assert.equal(text($('files-form-banner')), MESSAGES.ar['err.too_large']);
});

test('a 429 with Retry-After keeps upload disabled until the wait ends', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const { mod, fetchFn } = setup({ 'POST /api/files/upload': { status: 429, body: { code: 'locked_out' }, headers: { 'Retry-After': '3' } } });
  await pickSubject(mod);
  fillForm();
  await mod.upload();
  assert.equal($('files-submit').disabled, true);
  await mod.upload();
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files/upload').length, 1);
  ctx.mock.timers.tick(3100);
  assert.equal($('files-submit').disabled, false);
});

test('the filled form is unsaved: changing subject asks first and Stay keeps it', async () => {
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  fillForm();
  assert.equal(hasUnsavedChanges(), true);
  const before = fetchFn.calls.length;
  $('files-subject').value = DRAFT.id;
  $('files-subject').fire('change');
  await flush();
  const dialog = doc.getElementById('catalog-dialogs').children.find((c) => c.tagName === 'DIALOG' && c.open);
  assert.ok(dialog, 'the discard question is open');
  dialog.close(); // Esc / Stay
  await flush();
  assert.equal($('files-subject').value, SUBJECT.id);
  assert.equal(mod.state.subjectId, SUBJECT.id);
  assert.equal(fetchFn.calls.length, before);
  mod.discard();
  assert.equal(hasUnsavedChanges(), false);
});

test('delete goes through the confirm dialog, then reloads with a toast', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout'] });
  const { mod, fetchFn } = setup();
  await pickSubject(mod);
  const button = $('files-body').findAll((el) => el.tagName === 'BUTTON')[0];
  button.click();
  const dialog = doc.getElementById('catalog-dialogs').children.find((c) => c.tagName === 'DIALOG' && c.open);
  assert.ok(dialog, 'confirm dialog open');
  assert.ok(text(dialog).includes('مذكرة ١'));
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files/delete').length, 0, 'nothing deleted before confirming');
  dialog.findAll((el) => el.tagName === 'FORM')[0].fire('submit');
  await flush();
  await flush();
  const del = fetchFn.calls.find((c) => c.path === '/api/files/delete');
  assert.deepEqual(JSON.parse(del.init.body), { subject_id: 'subj-1', id: 'f-1' });
  assert.equal(text($('toast')), MESSAGES.ar['files.toast.deleted']);
  assert.equal(fetchFn.calls.filter((c) => c.path === '/api/files').length, 2);
});

test('a failed list shows the banner with retry and no rows', async () => {
  const { mod } = setup({ 'GET /api/files': { status: 503, body: { code: 'unavailable' } } });
  await pickSubject(mod);
  assert.equal($('files-banner').hidden, false);
  assert.equal(text($('files-banner')).includes(MESSAGES.ar['err.unavailable']), true);
});

test('reset (sign-out) aborts an upload and clears everything', async () => {
  let aborted = false;
  const { mod } = setup({
    'POST /api/files/upload': ({ init }) =>
      new Promise((_resolve, reject) => {
        init.signal.addEventListener('abort', () => {
          aborted = true;
          reject(new DOMException('aborted', 'AbortError'));
        });
      }),
  });
  await pickSubject(mod);
  fillForm();
  const pending = mod.upload();
  await flush();
  mod.reset();
  await pending;
  assert.equal(aborted, true);
  assert.equal(mod.state.subjectId, '');
  assert.equal($('files-title-ar').value, '');
  assert.equal(hasUnsavedChanges(), false);
});

test('the language switch redraws labels in English', async () => {
  const { mod } = setup();
  await pickSubject(mod);
  setLang('en');
  mod.rerender();
  assert.ok(text($('files-body')).includes(MESSAGES.en['files.kind.note']));
  assert.equal(text($('files-submit')), MESSAGES.en['files.upload']);
  setLang('ar');
});

test('no payment wording in the Files texts', () => {
  const words = /دفع|ادفع|شراء|اشتري|استرداد|payment|\bpay\b|\bbuy\b|purchase|refund|price|سعر/i;
  for (const lang of ['ar', 'en']) {
    for (const [key, value] of Object.entries(MESSAGES[lang])) {
      if (key.startsWith('files.') || ['err.invalid_file_id', 'err.file_not_found', 'err.invalid_upload', 'err.invalid_kind', 'err.invalid_pdf', 'err.file_too_large', 'err.too_large'].includes(key)) {
        assert.ok(!words.test(value), `${lang} ${key}: ${value}`);
      }
    }
  }
});
