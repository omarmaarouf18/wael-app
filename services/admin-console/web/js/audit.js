// Audit log tab: newest first, paginated. Only the auth-service log is served
// today; the academy log is merged in when its endpoint exists (ADR-0008 s9).

import { clear, h } from './dom.js';
import { MESSAGES, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { createPager, dateCell, hideBanner, messageRow, showBanner } from './ui.js';

export const PAGE_SIZE = 20;
const COLUMNS = 5;

/** i18n key for a known audit action, or null so the raw action is shown. */
export function actionLabelKey(action) {
  const key = `audit.action.${action}`;
  return key in MESSAGES.en ? key : null;
}

export function mountAudit({ api, doc = document }) {
  const banner = doc.getElementById('audit-banner');
  const tbody = doc.querySelector('#audit-table tbody');
  const pager = createPager(doc.getElementById('audit-pager'), (page) => {
    state.page = page;
    load();
  });
  const refreshButton = doc.getElementById('audit-refresh');

  const state = { page: 1, total: 0, items: [], loaded: false, error: null };
  let seq = 0;

  function row(entry) {
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
    clear(tbody);
    if (!state.loaded && !state.error) tbody.append(messageRow(COLUMNS, t('common.loading')));
    else if (state.loaded && state.items.length === 0) tbody.append(messageRow(COLUMNS, t('common.empty')));
    else if (state.loaded) for (const entry of state.items) tbody.append(row(entry));
    if (state.error) showBanner(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();
    const res = await api.get('/api/audit', { page: state.page, limit: PAGE_SIZE });
    if (mine !== seq || !isSignedIn()) return;
    if (!res.ok) {
      state.error = res.kind;
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

  return {
    load,
    rerender: render,
    reset() {
      seq += 1;
      Object.assign(state, { page: 1, total: 0, items: [], loaded: false, error: null });
      render();
    },
  };
}
