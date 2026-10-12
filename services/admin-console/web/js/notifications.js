// Notifications tab (SPEC Phase 7, ADR-0013): compose a broadcast to all
// students or to one subject's owners, preview the exact recipient count,
// confirm, then watch history with delivered/failed counts and retry.
//
// The page checks only what helps the admin early (required fields, lengths,
// a chosen subject); the server is the authority on every rule and its error
// codes map to the page's own text. Delivery is in-app only: students see
// the row next time they open the app, or live over SSE.

import { api as defaultApi } from './api.js';
import { createConfirm } from './confirm.js';
import { clear, h } from './dom.js';
import { formatNumber, t } from './i18n.js';
import { createCooldown, createPager, dateCell, hideBanner, messageRow, showError, toast } from './ui.js';
import { leaveIfClean, trackDirty } from './unsaved.js';

export const PAGE_SIZE = 15;
export const TITLE_MAX = 80;
export const BODY_MAX = 500;
const COLUMNS = 7;

const STATUS_KEYS = Object.freeze({
  queued: 'notifications.status.queued',
  sending: 'notifications.status.sending',
  done: 'notifications.status.done',
  partial: 'notifications.status.partial',
  failed: 'notifications.status.failed',
});

export function statusLabel(status) {
  const key = STATUS_KEYS[status];
  return key ? t(key) : String(status ?? '');
}

/**
 * The UX pre-check for the compose form: returns an i18n key for the
 * problem, or null when the send may proceed. The server checks again.
 */
export function precheckCompose({ titleAr, titleEn, bodyAr, bodyEn, audience, subjectId }) {
  const ar = String(titleAr ?? '').trim();
  const en = String(titleEn ?? '').trim();
  if (!ar || ar.length > TITLE_MAX || en.length > TITLE_MAX) return 'notifications.check.title';
  const bar = String(bodyAr ?? '').trim();
  const ben = String(bodyEn ?? '').trim();
  if (!bar || bar.length > BODY_MAX || ben.length > BODY_MAX) return 'notifications.check.body';
  if (audience === 'subject' && !subjectId) return 'notifications.check.subject';
  return null;
}

export function audienceOf({ audience, subjectId }) {
  return audience === 'subject' ? `subject:${subjectId}` : 'all';
}

export function mountNotifications({ api = defaultApi, doc = document } = {}) {
  const banner = doc.getElementById('notifications-banner');
  const form = doc.getElementById('notifications-form');
  const titleAr = doc.getElementById('notifications-title-ar');
  const titleEn = doc.getElementById('notifications-title-en');
  const bodyAr = doc.getElementById('notifications-body-ar');
  const bodyEn = doc.getElementById('notifications-body-en');
  const audienceAll = doc.getElementById('notifications-audience-all');
  const audienceSubject = doc.getElementById('notifications-audience-subject');
  const levelSelect = doc.getElementById('notifications-level');
  const subjectSelect = doc.getElementById('notifications-subject');
  const previewBtn = doc.getElementById('notifications-preview');
  const previewCount = doc.getElementById('notifications-preview-count');
  const submitBtn = doc.getElementById('notifications-submit');
  const historyBody = doc.getElementById('notifications-history-body');
  const pager = createPager(doc.getElementById('notifications-pager'), (page) => {
    state.hpage = page;
    loadHistory();
  });

  const confirm = createConfirm({ api, doc });
  const cooldown = createCooldown();

  const state = {
    levels: [],
    subjects: [],
    levelKey: '',
    subjectId: '',
    audience: 'all',
    previewed: null, // { audience, count } from the last dry run
    previewing: false,
    history: [],
    htotal: 0,
    hpage: 1,
    hloading: false,
  };
  let seq = 0;

  cooldown.subscribe(() => renderForm());

  // --- unsaved guard -------------------------------------------------------
  function isDirty() {
    return Boolean(
      (titleAr?.value ?? '').trim() ||
        (titleEn?.value ?? '').trim() ||
        (bodyAr?.value ?? '').trim() ||
        (bodyEn?.value ?? '').trim() ||
        state.subjectId,
    );
  }
  trackDirty('notifications', isDirty);

  function clearForm() {
    if (titleAr) titleAr.value = '';
    if (titleEn) titleEn.value = '';
    if (bodyAr) bodyAr.value = '';
    if (bodyEn) bodyEn.value = '';
    state.previewed = null;
    hideBanner(banner);
    renderPreview();
  }

  function snapshot() {
    return {
      titleAr: titleAr?.value ?? '',
      titleEn: titleEn?.value ?? '',
      bodyAr: bodyAr?.value ?? '',
      bodyEn: bodyEn?.value ?? '',
      audience: state.audience,
      subjectId: state.subjectId,
    };
  }

  // --- selects ---------------------------------------------------------------
  function option(value, text, selected) {
    return h('option', { text, attrs: { value, selected: selected || undefined } });
  }

  function renderLevels() {
    if (!levelSelect) return;
    clear(levelSelect);
    levelSelect.append(option('', t('notifications.pickLevel'), state.levelKey === ''));
    for (const lvl of state.levels) {
      levelSelect.append(option(lvl.key, lvl.name_ar || lvl.key, lvl.key === state.levelKey));
    }
    levelSelect.value = state.levelKey;
  }

  function subjectLabel(s) {
    const title = s.title_ar || s.id;
    return s.published ? title : `${title} (${t('catalog.draft')})`;
  }

  function renderSubjects() {
    if (!subjectSelect) return;
    clear(subjectSelect);
    subjectSelect.append(option('', t('notifications.pickSubject'), state.subjectId === ''));
    for (const s of state.subjects) {
      subjectSelect.append(option(s.id, subjectLabel(s), s.id === state.subjectId));
    }
    subjectSelect.value = state.subjectId;
    subjectSelect.disabled = state.previewing || cooldown.active || state.audience !== 'subject' || !state.levelKey;
  }

  function renderAudience() {
    if (audienceAll) audienceAll.checked = state.audience === 'all';
    if (audienceSubject) audienceSubject.checked = state.audience === 'subject';
    if (levelSelect) levelSelect.disabled = state.previewing || cooldown.active || state.audience !== 'subject';
    renderSubjects();
  }

  function renderPreview() {
    if (!previewCount) return;
    clear(previewCount);
    if (state.previewed) {
      previewCount.append(h('span', { class: 'muted small', text: t('notifications.previewCount', { n: formatNumber(state.previewed.count) }) }));
    }
    if (previewBtn) {
      previewBtn.disabled = state.previewing || cooldown.active;
      previewBtn.textContent = t('notifications.preview');
    }
  }

  function renderForm() {
    const busy = state.previewing;
    if (submitBtn) {
      submitBtn.disabled = busy || cooldown.active;
      submitBtn.textContent = t('notifications.send');
    }
    for (const el of [titleAr, titleEn, bodyAr, bodyEn, audienceAll, audienceSubject]) {
      if (el) el.disabled = busy || cooldown.active;
    }
    // The subject picker stays off unless the subject audience is chosen.
    renderAudience();
    renderPreview();
  }

  function audienceLabel(audience) {
    if (audience === 'all') return t('notifications.audienceAll');
    const id = audience.startsWith('subject:') ? audience.slice('subject:'.length) : audience;
    const found = state.subjects.find((s) => s.id === id);
    return found ? subjectLabel(found) : id;
  }

  // --- history ---------------------------------------------------------------
  function renderHistory() {
    if (!historyBody) return;
    clear(historyBody);
    if (state.hloading) {
      historyBody.append(messageRow(COLUMNS, t('common.loading')));
      return;
    }
    if (state.history.length === 0) {
      historyBody.append(messageRow(COLUMNS, t('notifications.empty')));
      return;
    }
    for (const b of state.history) {
      const actions = h('td', { class: 'actions' });
      if (b.status === 'partial' || b.status === 'failed') {
        actions.append(
          h('button', {
            class: 'btn secondary small',
            text: t('notifications.retry'),
            attrs: { type: 'button' },
            on: { click: () => retry(b.id) },
          }),
        );
      }
      historyBody.append(
        h(
          'tr',
          {},
          dateCell(b.created_at),
          h('td', {}, h('div', { class: 'strong', text: b.title_ar || '—' })),
          h('td', { text: audienceLabel(b.audience) }),
          h('td', { text: statusLabel(b.status) }),
          h('td', { text: formatNumber(b.delivered ?? 0) }),
          h('td', { text: formatNumber(b.failed ?? 0) }),
          actions,
        ),
      );
    }
  }

  async function loadHistory() {
    const mine = ++seq;
    state.hloading = true;
    renderHistory();
    const res = await api.get('/api/broadcasts', { page: state.hpage, limit: PAGE_SIZE });
    if (mine !== seq) return;
    state.hloading = false;
    if (!res.ok) {
      state.history = [];
      renderHistory();
      showError(banner, res, () => loadHistory());
      return;
    }
    hideBanner(banner);
    const data = res.data ?? {};
    state.history = Array.isArray(data.items) ? data.items : [];
    state.htotal = Number.isInteger(data.total) ? data.total : state.history.length;
    renderHistory();
    pager.update({ page: state.hpage, total: state.htotal, limit: PAGE_SIZE });
  }

  async function retry(id) {
    hideBanner(banner);
    const res = await api.post('/api/broadcasts/retry', { id });
    if (!res.ok) {
      const retryable = res.kind === 'unavailable' || res.kind === 'rate_limited';
      if (res.kind !== 'unauthorized') {
        cooldown.arm(res);
        showError(banner, res, retryable ? () => retry(id) : undefined, cooldown);
      }
      return;
    }
    toast(doc, t('notifications.toast.retried'));
    await loadHistory();
  }

  // --- loading ---------------------------------------------------------------
  async function loadLevels() {
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
    if (state.levelKey) await loadSubjects();
    else renderSubjects();
  }

  async function loadSubjects() {
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
        state.previewed = null;
        renderPreview();
      },
      () => {
        subjectSelect.value = state.subjectId;
      },
    );
  });

  audienceAll?.addEventListener('change', () => {
    if (!audienceAll.checked) return;
    changeSelection(
      () => {
        state.audience = 'all';
        state.previewed = null;
        renderAudience();
        renderPreview();
      },
      () => renderAudience(),
    );
  });

  audienceSubject?.addEventListener('change', () => {
    if (!audienceSubject.checked) return;
    changeSelection(
      () => {
        state.audience = 'subject';
        state.previewed = null;
        renderAudience();
        renderPreview();
      },
      () => renderAudience(),
    );
  });

  function showInline(key) {
    if (!banner) return;
    clear(banner);
    banner.append(h('span', { class: 'banner-text', text: t(key) }));
    banner.hidden = false;
  }

  // --- preview (dry run) -------------------------------------------------------
  async function preview() {
    if (state.previewing || cooldown.active) return;
    const audience = audienceOf({ audience: state.audience, subjectId: state.subjectId });
    state.previewing = true;
    hideBanner(banner);
    renderPreview();
    const res = await api.post('/api/broadcasts/preview', { audience });
    state.previewing = false;
    if (!res.ok) {
      state.previewed = null;
      if (res.kind === 'unauthorized') return;
      const retryable = res.kind === 'unavailable' || res.kind === 'rate_limited';
      cooldown.arm(res);
      renderForm();
      showError(banner, res, retryable ? () => preview() : undefined, cooldown);
      return;
    }
    const count = Number(res.data?.recipient_count);
    state.previewed = { audience, count: Number.isInteger(count) ? count : 0 };
    renderPreview();
  }

  previewBtn?.addEventListener('click', () => preview());

  // --- send --------------------------------------------------------------------
  async function send() {
    if (state.previewing || cooldown.active) return;
    const snap = snapshot();
    const problem = precheckCompose(snap);
    if (problem) {
      showInline(problem);
      return;
    }
    // The confirm dialog shows the exact recipient count from a dry run.
    hideBanner(banner);
    state.previewing = true;
    renderPreview();
    const audience = audienceOf(snap);
    const prev = await api.post('/api/broadcasts/preview', { audience });
    state.previewing = false;
    if (!prev.ok) {
      if (prev.kind === 'unauthorized') {
        renderPreview();
        return;
      }
      const retryable = prev.kind === 'unavailable' || prev.kind === 'rate_limited';
      cooldown.arm(prev);
      renderForm();
      showError(banner, prev, retryable ? () => send() : undefined, cooldown);
      return;
    }
    const count = Number(prev.data?.recipient_count);
    state.previewed = { audience, count: Number.isInteger(count) ? count : 0 };
    renderPreview();
    if (state.previewed.count === 0) {
      showInline('err.empty_audience');
      return;
    }
    confirm.open({
      titleKey: 'notifications.confirmTitle',
      noteText: t('notifications.confirmNote', { n: formatNumber(state.previewed.count) }),
      confirmKey: 'notifications.send',
      tone: 'primary',
      path: '/api/broadcasts/create',
      body: {
        audience,
        title_ar: snap.titleAr.trim(),
        title_en: snap.titleEn.trim(),
        body_ar: snap.bodyAr.trim(),
        body_en: snap.bodyEn.trim(),
      },
      onDone: () => {
        clearForm();
        renderForm();
        toast(doc, t('notifications.toast.sent'));
        state.hpage = 1;
        loadHistory();
      },
    });
  }

  form?.addEventListener('submit', (event) => {
    event.preventDefault();
    send();
  });

  // --- module contract (app.js) --------------------------------------------------
  function discard() {
    clearForm();
    renderForm();
  }

  function rerender() {
    renderLevels();
    renderSubjects();
    renderAudience();
    renderPreview();
    renderHistory();
    renderForm();
    confirm.rerender();
  }

  function reset() {
    state.levels = [];
    state.subjects = [];
    state.levelKey = '';
    state.subjectId = '';
    state.audience = 'all';
    state.previewed = null;
    state.previewing = false;
    state.history = [];
    state.htotal = 0;
    state.hpage = 1;
    state.hloading = false;
    cooldown.stop();
    confirm.close();
    clearForm();
    hideBanner(banner);
    rerender();
  }

  async function load() {
    await loadLevels();
    await loadHistory();
  }

  renderAudience();
  renderPreview();
  renderForm();

  return {
    load,
    send,
    preview,
    retry,
    discard,
    isDirty,
    rerender,
    reset,
    get state() {
      return state;
    },
  };
}
