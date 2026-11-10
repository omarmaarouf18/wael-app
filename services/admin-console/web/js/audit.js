// Audit log tab: newest first, paginated. The page shows one source at a
// time: the auth-service log (accounts), the academy log (content), the
// notification-service log (broadcasts) or student account events. The logs
// are never merged: merged pagination over two sources is wrong without a shared cursor.

import { clear, h } from './dom.js';
import { MESSAGES, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { createPager, dateCell, hideBanner, messageRow, showError } from './ui.js';

export const PAGE_SIZE = 20;
const COLUMNS = 5;

/** i18n key for a known audit action, or null so the raw action is shown. */
export function actionLabelKey(action) {
  const key = `audit.action.${action}`;
  return key in MESSAGES.en ? key : null;
}

/** Query for GET /api/audit; the source is always sent explicitly. */
export function auditQuery({ source, page, limit, student, type }) {
  const q = { source, page, limit };
  if (student) q.student = student;
  if (type) q.type = type;
  return q;
}

export function mountAudit({ api, doc = document }) {
  const banner = doc.getElementById('audit-banner');
  const tbody = doc.querySelector('#audit-table tbody');
  const pager = createPager(doc.getElementById('audit-pager'), (page) => {
    state.page = page;
    load();
  });
  const refreshButton = doc.getElementById('audit-refresh');
  const sourceAuth = doc.getElementById('audit-source-auth');
  const sourceAcademy = doc.getElementById('audit-source-academy');
  const sourceNotifications = doc.getElementById('audit-source-notifications');
  const sourceStudent = doc.getElementById('audit-source-student');

  const filterForm = doc.getElementById('audit-filter');
  const studentInput = doc.getElementById('audit-student');
  const typeSelect = doc.getElementById('audit-type');
  const filterReset = doc.getElementById('audit-filter-reset');

  const thTime = doc.getElementById('audit-th-time');
  const thActor = doc.getElementById('audit-th-actor');
  const thAction = doc.getElementById('audit-th-action');
  const thTarget = doc.getElementById('audit-th-target');
  const thDetail = doc.getElementById('audit-th-detail');

  const state = { source: 'auth', page: 1, total: 0, items: [], student: '', type: '', loaded: false, error: null };
  let seq = 0;

  function renderHeaders() {
    if (!thTime) return;
    if (state.source === 'student') {
      thTime.textContent = t('audit.col.time');
      thActor.textContent = t('audit.col.student');
      thAction.textContent = t('audit.col.event');
      thTarget.textContent = t('audit.col.device');
      thDetail.textContent = t('audit.col.actor');
    } else {
      thTime.textContent = t('audit.col.time');
      thActor.textContent = t('audit.col.actor');
      thAction.textContent = t('audit.col.action');
      thTarget.textContent = t('audit.col.target');
      thDetail.textContent = t('audit.col.detail');
    }
  }

  function renderSource() {
    sourceAuth.setAttribute('aria-pressed', String(state.source === 'auth'));
    sourceAcademy.setAttribute('aria-pressed', String(state.source === 'academy'));
    if (sourceNotifications) sourceNotifications.setAttribute('aria-pressed', String(state.source === 'notifications'));
    if (sourceStudent) sourceStudent.setAttribute('aria-pressed', String(state.source === 'student'));
    if (filterForm) filterForm.hidden = state.source !== 'student';
    renderHeaders();
  }

  function row(entry) {
    if (state.source === 'student') {
      const key = actionLabelKey(entry.type);
      const eventLabel = key ? t(key) : String(entry.type ?? '');
      const actorLabel = entry.actor_type === 'admin'
        ? (entry.actor_name ? `${t('audit.actor.admin')}: ${entry.actor_name}` : t('audit.actor.admin'))
        : t('audit.actor.student');
      return h(
        'tr',
        {},
        dateCell(entry.created_at),
        h('td', {}, h('div', { text: entry.student || '—' }), entry.user_id ? h('div', { class: 'id ltr muted small', text: entry.user_id }) : ''),
        h('td', { text: eventLabel }),
        h('td', { class: 'ltr', text: entry.device_label || '—' }),
        h('td', { text: actorLabel }),
      );
    }
    const key = actionLabelKey(entry.action);
    const targetKey = `audit.target.${entry.target_type}`;
    const targetLabel = targetKey in MESSAGES.en ? t(targetKey) : String(entry.target_type ?? '');
    return h(
      'tr',
      {},
      dateCell(entry.created_at),
      h('td', { text: entry.actor_name || '—' }),
      h('td', { text: key ? t(key) : String(entry.action ?? '') }),
      h('td', {}, h('div', { text: targetLabel }), h('div', { class: 'id ltr', text: entry.target_id || '' })),
      h('td', { class: 'detail', text: entry.detail || '' }),
    );
  }

  function render() {
    renderSource();
    clear(tbody);
    if (!state.loaded && !state.error) tbody.append(messageRow(COLUMNS, t('common.loading')));
    else if (state.loaded && state.items.length === 0) tbody.append(messageRow(COLUMNS, t('common.empty')));
    else if (state.loaded) for (const entry of state.items) tbody.append(row(entry));
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();
    const query = auditQuery({
      source: state.source,
      page: state.page,
      limit: PAGE_SIZE,
      student: state.student,
      type: state.type,
    });
    const res = await api.get('/api/audit', query);
    if (mine !== seq || !isSignedIn()) return;
    if (!res.ok) {
      state.error = res;
      render();
      return;
    }
    const data = res.data ?? {};
    state.items = Array.isArray(data.items) ? data.items : [];
    state.total = Number.isInteger(data.total) ? data.total : state.items.length;
    state.loaded = true;
    render();
  }

  refreshButton.addEventListener('click', () => {
    state.page = 1;
    load();
  });
  sourceAuth.addEventListener('click', () => {
    if (state.source === 'auth') return;
    state.source = 'auth';
    state.page = 1;
    state.student = '';
    state.type = '';
    load();
  });
  sourceAcademy.addEventListener('click', () => {
    if (state.source === 'academy') return;
    state.source = 'academy';
    state.page = 1;
    state.student = '';
    state.type = '';
    load();
  });
  sourceNotifications?.addEventListener('click', () => {
    if (state.source === 'notifications') return;
    state.source = 'notifications';
    state.page = 1;
    state.student = '';
    state.type = '';
    load();
  });
  sourceStudent?.addEventListener('click', () => {
    if (state.source === 'student') return;
    state.source = 'student';
    state.page = 1;
    load();
  });

  filterForm?.addEventListener('submit', (e) => {
    e.preventDefault();
    state.student = (studentInput?.value || '').trim();
    state.type = (typeSelect?.value || '').trim();
    state.page = 1;
    load();
  });
  filterReset?.addEventListener('click', () => {
    if (studentInput) studentInput.value = '';
    if (typeSelect) typeSelect.value = '';
    state.student = '';
    state.type = '';
    state.page = 1;
    load();
  });

  return {
    load,
    rerender: render,
    reset() {
      seq += 1;
      Object.assign(state, { source: 'auth', page: 1, total: 0, items: [], student: '', type: '', loaded: false, error: null });
      if (studentInput) studentInput.value = '';
      if (typeSelect) typeSelect.value = '';
      render();
    },
  };
}
