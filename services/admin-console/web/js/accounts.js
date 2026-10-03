// Accounts tab: search, status filter, pagination, and the three actions.

import { createAccountDialog } from './account-dialog.js';
import { createEntitlementsDialog } from './entitlements-dialog.js';
import { clear, h } from './dom.js';
import { t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { createPager, dateCell, hideBanner, messageRow, showBanner, statusBadge } from './ui.js';

export const PAGE_SIZE = 15;
const COLUMNS = 6;

/** Actions the console offers for an account in a given status. */
export function actionsFor(status) {
  switch (status) {
    case 'active':
      return ['suspend', 'delete'];
    case 'suspended':
      return ['reactivate', 'delete'];
    default:
      return [];
  }
}

/** Query for GET /api/accounts; empty values are left out. */
export function listQuery({ search, status, page, limit }) {
  const query = { page, limit };
  const text = String(search ?? '').trim();
  if (text) query.search = text;
  if (status) query.status = status;
  return query;
}

export function mountAccounts({ api, doc = document }) {
  const form = doc.getElementById('accounts-filter');
  const searchInput = doc.getElementById('accounts-search');
  const statusSelect = doc.getElementById('accounts-status');
  const resetButton = doc.getElementById('accounts-reset');
  const banner = doc.getElementById('accounts-banner');
  const tbody = doc.querySelector('#accounts-table tbody');
  const pager = createPager(doc.getElementById('accounts-pager'), (page) => {
    state.page = page;
    load();
  });

  const state = { page: 1, search: '', status: '', total: 0, items: [], loaded: false, error: null };
  let seq = 0;

  const dialog = createAccountDialog({ api, doc, onDone: load });
  const entitlementsDialog = createEntitlementsDialog({ api, doc, onDone: load });

  function renderRows() {
    clear(tbody);
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      tbody.append(messageRow(COLUMNS, t('common.loading')));
      return;
    }
    if (state.items.length === 0) {
      tbody.append(messageRow(COLUMNS, t('common.empty')));
      return;
    }
    for (const account of state.items) tbody.append(row(account));
  }

  function row(account) {
    const actions = h('td', { class: 'actions' });
    for (const action of actionsFor(account.status)) {
      actions.append(
        h('button', {
          class: `btn small ${action === 'reactivate' ? 'secondary' : 'danger-outline'}`,
          text: t(`action.${action}`),
          attrs: { type: 'button' },
          on: { click: () => dialog.open(action, account) },
        }),
      );
    }
    if (account.status !== 'deleted') {
      actions.append(
        h('button', {
          class: 'btn small secondary',
          text: t('action.subjects'),
          attrs: { type: 'button' },
          on: { click: () => entitlementsDialog.open(account) },
        }),
      );
    }
    return h(
      'tr',
      {},
      h('td', {}, h('div', { class: 'strong', text: account.full_name || '—' }), h('div', { class: 'id ltr', text: account.id })),
      h('td', { class: 'ltr', text: account.email || '—' }),
      h('td', { class: 'ltr', text: account.phone || '—' }),
      h('td', {}, statusBadge(account.status)),
      dateCell(account.created_at),
      actions,
    );
  }

  function render() {
    renderRows();
    if (state.error) showBanner(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();
    const res = await api.get('/api/accounts', listQuery({ ...state, limit: PAGE_SIZE }));
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

  form.addEventListener('submit', (event) => {
    event.preventDefault();
    state.search = searchInput.value;
    state.status = statusSelect.value;
    state.page = 1;
    load();
  });
  resetButton.addEventListener('click', () => {
    searchInput.value = '';
    statusSelect.value = '';
    state.search = '';
    state.status = '';
    state.page = 1;
    load();
  });

  return {
    load,
    rerender() {
      render();
      dialog.rerender();
      entitlementsDialog.rerender();
    },
    /** Drops everything shown, used when the session ends. */
    reset() {
      seq += 1;
      dialog.close();
      entitlementsDialog.close();
      Object.assign(state, { page: 1, search: '', status: '', total: 0, items: [], loaded: false, error: null });
      searchInput.value = '';
      statusSelect.value = '';
      render();
    },
  };
}
