// Files tab (SPEC Phase 5): pick a subject, list its PDFs, upload one PDF at a
// time, delete with the shared confirm dialog. The level and subject lists
// come from the same catalog routes as the Catalog tab (drafts included, so a
// subject can get its files before it is published).
//
// The page checks only what helps the admin early (a .pdf name, not empty,
// at most the default MAX_PDF_BYTES); the server is the authority on every
// rule and its error codes map to the page's own text. The api client uses
// fetch, which reports no upload progress, so an upload shows an "uploading"
// state with a Cancel button (AbortController) instead of a percentage.

import { api as defaultApi } from './api.js';
import { createConfirm } from './confirm.js';
import { clear, h } from './dom.js';
import { formatNumber, t } from './i18n.js';
import { createCooldown, dateCell, hideBanner, messageRow, showError, toast } from './ui.js';
import { leaveIfClean, trackDirty } from './unsaved.js';

/** The page's pre-check mirrors the MAX_PDF_BYTES default (SPEC D14). */
export const MAX_PDF_BYTES_HINT = 20 * 1024 * 1024;

const KINDS = Object.freeze(['note', 'book']);
const COLUMNS = 5;

/** Size in MB (or KB under one MB), localized digits. */
export function formatSize(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return '';
  if (bytes < 1024 * 1024) {
    return t('files.size.kb', { n: formatNumber(Math.max(1, Math.round(bytes / 1024))) });
  }
  return t('files.size.mb', { n: formatNumber(Math.round((bytes / (1024 * 1024)) * 10) / 10) });
}

/**
 * The UX pre-check for a chosen file: returns an i18n key for the problem, or
 * null when the file may be sent. The server checks again (magic bytes, size).
 */
export function precheckFile(file, maxBytes = MAX_PDF_BYTES_HINT) {
  if (!file) return 'files.check.noFile';
  if (!/\.pdf$/i.test(String(file.name ?? ''))) return 'files.check.notPdf';
  if (!(file.size > 0)) return 'files.check.empty';
  if (file.size > maxBytes) return 'files.check.tooLarge';
  return null;
}

export function mountFiles({ api = defaultApi, doc = document, FormDataCtor = globalThis.FormData, AbortCtor = globalThis.AbortController } = {}) {
  const levelSelect = doc.getElementById('files-level');
  const subjectSelect = doc.getElementById('files-subject');
  const banner = doc.getElementById('files-banner');
  const body = doc.getElementById('files-body');
  const form = doc.getElementById('files-form');
  const kindSelect = doc.getElementById('files-kind');
  const titleAr = doc.getElementById('files-title-ar');
  const titleEn = doc.getElementById('files-title-en');
  const fileInput = doc.getElementById('files-file');
  const formBanner = doc.getElementById('files-form-banner');
  const submitBtn = doc.getElementById('files-submit');
  const cancelBtn = doc.getElementById('files-cancel');
  const status = doc.getElementById('files-status');
  const limitHint = doc.getElementById('files-limit');

  const confirm = createConfirm({ api, doc });
  const cooldown = createCooldown();

  const state = {
    levels: [],
    subjects: [],
    levelKey: '',
    subjectId: '',
    files: [],
    loading: false,
    uploading: null, // AbortController while an upload is in flight
  };

  cooldown.subscribe(() => renderForm());

  // --- unsaved guard -------------------------------------------------------
  function isDirty() {
    return Boolean(
      state.uploading ||
        (titleAr?.value ?? '').trim() ||
        (titleEn?.value ?? '').trim() ||
        // A chosen file shows in the input's value; clearing it unselects.
        (fileInput?.value ?? ''),
    );
  }
  trackDirty('files', isDirty);

  function clearForm() {
    if (titleAr) titleAr.value = '';
    if (titleEn) titleEn.value = '';
    if (kindSelect) kindSelect.value = KINDS[0];
    if (fileInput) fileInput.value = '';
    if (formBanner) hideBanner(formBanner);
  }

  // --- selects ---------------------------------------------------------------
  function option(value, text, selected) {
    return h('option', { text, attrs: { value, selected: selected || undefined } });
  }

  function renderLevels() {
    if (!levelSelect) return;
    clear(levelSelect);
    levelSelect.append(option('', t('files.pickLevel'), state.levelKey === ''));
    for (const lvl of state.levels) {
      levelSelect.append(option(lvl.key, lvl.name_ar || lvl.key, lvl.key === state.levelKey));
    }
    levelSelect.value = state.levelKey;
  }

  function subjectLabel(s) {
    const title = s.title_ar || s.id;
    return s.published ? title : `${title} (${t('files.draft')})`;
  }

  function renderSubjects() {
    if (!subjectSelect) return;
    clear(subjectSelect);
    subjectSelect.append(option('', t('files.pickSubject'), state.subjectId === ''));
    for (const s of state.subjects) {
      subjectSelect.append(option(s.id, subjectLabel(s), s.id === state.subjectId));
    }
    subjectSelect.value = state.subjectId;
    subjectSelect.disabled = !state.levelKey;
  }

  function renderKinds() {
    if (!kindSelect) return;
    const current = kindSelect.value || KINDS[0];
    clear(kindSelect);
    for (const kind of KINDS) kindSelect.append(option(kind, t(`files.kind.${kind}`), kind === current));
    kindSelect.value = current;
  }

  // --- list ------------------------------------------------------------------
  function renderList() {
    if (!body) return;
    clear(body);
    if (!state.subjectId) {
      body.append(messageRow(COLUMNS, t('files.pickSubjectFirst')));
      return;
    }
    if (state.loading) {
      body.append(messageRow(COLUMNS, t('common.loading')));
      return;
    }
    if (state.files.length === 0) {
      body.append(messageRow(COLUMNS, t('files.empty')));
      return;
    }
    for (const f of state.files) {
      const remove = h('button', {
        class: 'btn danger small',
        text: t('files.delete'),
        attrs: { type: 'button' },
        on: { click: () => askDelete(f) },
      });
      body.append(
        h(
          'tr',
          {},
          h('td', { text: KINDS.includes(f.kind) ? t(`files.kind.${f.kind}`) : String(f.kind ?? '') }),
          h('td', {}, h('div', { class: 'strong', text: f.title_ar || '—' }), f.title_en ? h('div', { class: 'muted small ltr', text: f.title_en }) : null),
          h('td', { text: formatSize(f.size_bytes) }),
          dateCell(f.created_at),
          h('td', { class: 'actions' }, remove),
        ),
      );
    }
  }

  function renderForm() {
    const busy = Boolean(state.uploading);
    const noSubject = !state.subjectId;
    if (submitBtn) {
      submitBtn.disabled = busy || noSubject || cooldown.active;
      submitBtn.textContent = busy ? t('files.uploading') : t('files.upload');
    }
    if (cancelBtn) {
      cancelBtn.hidden = !busy;
      cancelBtn.disabled = !busy;
    }
    for (const el of [kindSelect, titleAr, titleEn, fileInput]) if (el) el.disabled = busy || noSubject;
    if (status) status.textContent = busy ? t('files.uploadingNote') : '';
    if (limitHint) limitHint.textContent = t('files.limit', { size: formatSize(MAX_PDF_BYTES_HINT) });
  }

  // --- loading -----------------------------------------------------------------
  async function loadLevels() {
    hideBanner(banner);
    const res = await api.get('/api/levels');
    if (!res.ok) {
      showError(banner, res, () => loadLevels());
      return;
    }
    state.levels = Array.isArray(res.data?.levels) ? res.data.levels : [];
    if (state.levelKey && !state.levels.some((l) => l.key === state.levelKey)) {
      state.levelKey = '';
      state.subjectId = '';
    }
    renderLevels();
    if (state.levelKey) {
      await loadSubjects();
    } else {
      renderSubjects();
      await loadFiles();
    }
  }

  async function loadSubjects() {
    hideBanner(banner);
    const items = [];
    for (let page = 1; page <= 50; page += 1) {
      const res = await api.get('/api/subjects', { level_id: state.levelKey, page, limit: 100 });
      if (!res.ok) {
        showError(banner, res, () => loadSubjects());
        return;
      }
      const batch = Array.isArray(res.data?.items) ? res.data.items : [];
      items.push(...batch);
      if (batch.length < 100) break;
    }
    state.subjects = items;
    if (state.subjectId && !items.some((s) => s.id === state.subjectId)) state.subjectId = '';
    renderSubjects();
    await loadFiles();
  }

  async function loadFiles() {
    renderForm();
    if (!state.subjectId) {
      state.files = [];
      renderList();
      return;
    }
    state.loading = true;
    renderList();
    const subjectId = state.subjectId;
    const res = await api.get('/api/files', { subject_id: subjectId });
    if (subjectId !== state.subjectId) return; // the admin picked another subject meanwhile
    state.loading = false;
    if (!res.ok) {
      state.files = [];
      renderList();
      showError(banner, res, () => loadFiles());
      return;
    }
    hideBanner(banner);
    state.files = Array.isArray(res.data?.files) ? res.data.files : [];
    renderList();
  }

  // Changing level or subject with a filled form asks first, like a tab switch.
  async function changeSelection(apply, revert) {
    const moved = await leaveIfClean(doc, apply, () => discard());
    if (!moved) revert();
  }

  levelSelect?.addEventListener('change', () => {
    const next = levelSelect.value;
    changeSelection(
      () => {
        state.levelKey = next;
        state.subjectId = '';
        state.subjects = [];
        renderSubjects();
        if (next) loadSubjects();
        else loadFiles();
      },
      () => {
        levelSelect.value = state.levelKey;
      },
    );
  });

  subjectSelect?.addEventListener('change', () => {
    const next = subjectSelect.value;
    changeSelection(
      () => {
        state.subjectId = next;
        loadFiles();
      },
      () => {
        subjectSelect.value = state.subjectId;
      },
    );
  });

  // --- upload --------------------------------------------------------------------
  async function upload() {
    if (state.uploading || cooldown.active || !state.subjectId) return;
    hideBanner(formBanner);
    const file = fileInput?.files?.[0] ?? null;
    const problem = precheckFile(file);
    if (problem) {
      showInline(problem);
      return;
    }
    const ar = (titleAr?.value ?? '').trim();
    const en = (titleEn?.value ?? '').trim();
    if (!ar || ar.length > 200 || en.length > 200) {
      showInline('files.check.title');
      return;
    }
    const kind = KINDS.includes(kindSelect?.value) ? kindSelect.value : KINDS[0];

    // Field order matters: the server reads kind and titles before the file.
    const data = new FormDataCtor();
    data.append('kind', kind);
    data.append('title_ar', ar);
    if (en) data.append('title_en', en);
    data.append('file', file, file.name);

    const controller = new AbortCtor();
    state.uploading = controller;
    const subjectId = state.subjectId;
    renderForm();
    let res;
    try {
      res = await api.upload('/api/files/upload', { subject_id: subjectId }, data, controller.signal);
    } finally {
      state.uploading = null;
    }
    if (res.ok) {
      clearForm();
      renderForm();
      toast(doc, t('files.toast.uploaded'));
      if (subjectId === state.subjectId) await loadFiles();
      return;
    }
    // Arm the Retry-After wait before redrawing, so Upload stays disabled.
    cooldown.arm(res);
    renderForm();
    if (res.kind === 'aborted') {
      showInline('files.cancelled');
      return;
    }
    if (res.kind === 'unauthorized') return;
    const retryable = res.kind === 'unavailable' || res.kind === 'rate_limited';
    showError(formBanner, res, retryable ? () => upload() : undefined, cooldown);
  }

  function showInline(key) {
    if (!formBanner) return;
    clear(formBanner);
    formBanner.append(h('span', { class: 'banner-text', text: t(key) }));
    formBanner.hidden = false;
  }

  form?.addEventListener('submit', (event) => {
    event.preventDefault();
    upload();
  });
  cancelBtn?.addEventListener('click', () => {
    if (state.uploading) state.uploading.abort();
  });

  // --- delete --------------------------------------------------------------------
  function askDelete(f) {
    const subjectId = state.subjectId;
    confirm.open({
      titleKey: 'files.deleteTitle',
      noteText: t('files.deleteNote', { title: f.title_ar || f.id }),
      confirmKey: 'files.delete',
      path: '/api/files/delete',
      body: { subject_id: subjectId, id: f.id },
      onDone: () => {
        toast(doc, t('files.toast.deleted'));
        if (subjectId === state.subjectId) loadFiles();
      },
    });
  }

  // --- module contract (app.js) --------------------------------------------------
  function discard() {
    if (state.uploading) state.uploading.abort();
    clearForm();
    renderForm();
  }

  function rerender() {
    renderLevels();
    renderSubjects();
    renderKinds();
    renderList();
    renderForm();
    confirm.rerender();
  }

  function reset() {
    if (state.uploading) state.uploading.abort();
    state.uploading = null;
    state.levels = [];
    state.subjects = [];
    state.levelKey = '';
    state.subjectId = '';
    state.files = [];
    state.loading = false;
    cooldown.stop();
    confirm.close();
    clearForm();
    hideBanner(banner);
    rerender();
  }

  renderKinds();
  renderForm();

  return {
    load: () => loadLevels(),
    upload,
    discard,
    isDirty,
    rerender,
    reset,
    get state() {
      return state;
    },
  };
}
