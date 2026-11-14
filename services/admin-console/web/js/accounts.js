// Students tab (tab id "accounts"): search, status filter as a segmented
// control, pagination, and a side panel per student (owner redesign
// 2026-11-14). The list rows show avatar initial, name, email · phone (LTR)
// and a status chip; no raw id. Clicking a row opens the panel: header (name,
// joined date, status), details (email, phone, id as a copy chip), sections
// Subjects (entitlements), Requests (this student's requests) and Activity
// (student account events), and the existing suspend / reactivate / delete
// dialogs in its footer. The auth payload carries no subject count or level,
// so the row and the details leave those out.

import { createAccountDialog } from './account-dialog.js';
import { createEntitlementsDialog } from './entitlements-dialog.js';
import { describe, sentenceNode } from './audit.js';
import { clear, h } from './dom.js';
import { formatCairoDate, formatDate, formatMoney, formatNumber, formatTime, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import {
  avatar, button, chip, copyIdChip, createPager, createSidePanel, emptyState, hideBanner, showError,
  skeletonRows, statusBadge, toast,
} from './ui.js';

export const PAGE_SIZE = 15;
export const STATUS_FILTERS = Object.freeze(['', 'active', 'suspended', 'pending_deletion', 'deleted']);
export const PANEL_SECTIONS = Object.freeze(['subjects', 'requests', 'activity']);
export const ACTIVITY_LIMIT = 10;
const REQUEST_STATUSES = ['pending', 'accepted', 'rejected'];

/** Actions the console offers for an account in a given status. */
export function actionsFor(status) {
  switch (status) {
    case 'active':
      return ['suspend', 'delete'];
    case 'suspended':
      return ['reactivate', 'delete'];
    case 'pending_deletion':
      return ['suspend', 'delete'];
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

/** i18n key of an entitlement source chip; every source is neutral grey (owner rule). */
export function sourceKey(source) {
  if (source === 'iap') return 'students.source.iap';
  if (source === 'request') return 'students.source.request';
  return 'students.source.manual';
}

/** "email · phone" for a row; '' parts are left out. */
export function contactLine(account) {
  return [account?.email, account?.phone].filter(Boolean).join(' · ');
}

/** Flattens grouped request answers into items, newest first. */
export function flattenRequests(groupsAnswers) {
  const out = [];
  for (const res of groupsAnswers) {
    if (!res?.ok || !Array.isArray(res.data?.items)) continue;
    for (const g of res.data.items) {
      for (const o of g.orders ?? []) {
        for (const it of o.items ?? []) out.push({ ...it, source: it.source || o.source });
      }
    }
  }
  return out.sort((a, b) => (Date.parse(b.created_at) || 0) - (Date.parse(a.created_at) || 0));
}

export function mountAccounts({ api, doc = document }) {
  const form = doc.getElementById('accounts-filter');
  const searchInput = doc.getElementById('accounts-search');
  const statusGroup = doc.getElementById('accounts-status');
  const resetButton = doc.getElementById('accounts-reset');
  const banner = doc.getElementById('accounts-banner');
  const count = doc.getElementById('accounts-count');
  const list = doc.getElementById('accounts-list');
  const panelRoot = doc.getElementById('student-panel');
  const panelBody = doc.getElementById('student-panel-body');
  const layout = doc.getElementById('students-layout');
  const pager = createPager(doc.getElementById('accounts-pager'), (page) => {
    state.page = page;
    load();
  });

  const state = { page: 1, search: '', status: '', total: 0, items: [], loaded: false, error: null, focusUser: '' };
  const panelState = () => ({
    account: null, section: 'subjects',
    ents: null, entsError: null, reqs: null, reqsError: null, events: null, eventsError: null,
  });
  let panel = panelState();
  let seq = 0;
  let panelSeq = 0;

  const statusButtons = STATUS_FILTERS.map((value) => {
    const btn = h('button', {
      class: 'btn small secondary',
      text: '',
      attrs: { type: 'button', 'aria-pressed': String(value === ''), 'data-status': value },
      on: { click: () => setStatus(value) },
    });
    return [value, btn];
  });
  clear(statusGroup);
  for (const [, btn] of statusButtons) statusGroup.append(btn);

  const sidePanel = createSidePanel({
    root: panelRoot,
    closeButton: doc.getElementById('student-panel-close'),
    onClose: () => {
      const id = panel.account?.id;
      panelSeq += 1;
      panel = panelState();
      layout?.setAttribute('data-panel', 'closed');
      markSelected();
      renderPanel();
      // Focus the student's current row (the list may have been redrawn since).
      const row = [...list.children].map((li) => li?.children?.[0]).find((b) => b?.dataset?.id === id);
      if (row && typeof row.focus === 'function') {
        row.focus();
        return true;
      }
      return false;
    },
  });

  const dialog = createAccountDialog({
    api, doc,
    onDone: (action) => {
      toast(doc, t(`accounts.toast.${action}`));
      load();
    },
  });
  const entitlementsDialog = createEntitlementsDialog({
    api, doc,
    onDone: () => {
      load();
      if (panel.account) loadPanel(panel.account);
    },
  });

  // --- list ------------------------------------------------------------------
  function renderStatus() {
    for (const [value, btn] of statusButtons) {
      btn.textContent = value ? t(`status.${value}`) : t('accounts.statusAll');
      btn.setAttribute('aria-pressed', String(state.status === value));
    }
  }

  function renderRows() {
    clear(list);
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      list.append(h('li', {}, skeletonRows(4)));
      return;
    }
    if (state.items.length === 0) {
      list.append(h('li', {}, emptyState(t(state.search || state.status ? 'students.emptyFiltered' : 'students.empty'), '○')));
      return;
    }
    for (const account of state.items) list.append(row(account));
  }

  function row(account) {
    const selected = panel.account?.id === account.id;
    const contact = contactLine(account);
    return h('li', { class: 'student-item' },
      h('button', {
        class: `student-row${selected ? ' is-selected' : ''}`,
        attrs: { type: 'button', 'aria-current': selected ? 'true' : 'false', 'aria-controls': 'student-panel' },
        dataset: { id: account.id },
        on: { click: (event) => openPanel(account, event?.target) },
      },
      avatar(account.full_name || account.email),
      h('span', { class: 'student-row-main' },
        h('bdi', { class: 'strong student-name', text: account.full_name || '—' }),
        h('bdi', { class: 'muted small ltr student-contact', text: contact || '—' })),
      statusBadge(account.status)),
    );
  }

  // Marks the open student's row without rebuilding the list, so the row
  // that opened the panel stays in the page and gets focus back on close.
  function markSelected() {
    for (const li of [...list.children]) {
      const btn = li?.children?.[0];
      if (!btn?.dataset?.id) continue;
      const on = panel.account?.id === btn.dataset.id;
      btn.className = `student-row${on ? ' is-selected' : ''}`;
      btn.setAttribute('aria-current', on ? 'true' : 'false');
    }
  }

  function render() {
    renderStatus();
    renderRows();
    if (count) count.textContent = state.loaded ? t('students.count', { n: formatNumber(state.total) }) : '';
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
    renderPanel();
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();
    const res = await api.get('/api/accounts', listQuery({ ...state, limit: PAGE_SIZE }));
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
    // Keep the open panel in step with the list (an action changes the status).
    if (panel.account) {
      const fresh = state.items.find((a) => a.id === panel.account.id);
      if (fresh) panel.account = fresh;
    }
    const focus = state.focusUser;
    state.focusUser = '';
    render();
    if (focus) {
      const target = state.items.find((a) => a.id === focus);
      if (target) openPanel(target);
    }
  }

  function setStatus(value) {
    if (!STATUS_FILTERS.includes(value)) return;
    state.status = value;
    state.search = searchInput.value;
    state.page = 1;
    load();
  }

  // --- panel -----------------------------------------------------------------
  function openPanel(account, from = null) {
    panel = { ...panelState(), account };
    layout?.setAttribute('data-panel', 'open');
    sidePanel.open(from);
    markSelected();
    loadPanel(account);
  }

  async function loadPanel(account) {
    const mine = ++panelSeq;
    Object.assign(panel, { ents: null, entsError: null, reqs: null, reqsError: null, events: null, eventsError: null });
    renderPanel();
    const [ents, events, ...groups] = await Promise.all([
      api.get('/api/entitlements', { user_id: account.id }),
      api.get('/api/audit', { source: 'student', page: 1, limit: ACTIVITY_LIMIT, student: account.id }),
      ...REQUEST_STATUSES.map((status) => api.get('/api/request-groups', { status, user_id: account.id })),
    ]);
    if (mine !== panelSeq || !isSignedIn() || panel.account?.id !== account.id) return;
    panel.ents = ents.ok && Array.isArray(ents.data?.items) ? ents.data.items : [];
    panel.entsError = ents.ok ? null : ents;
    panel.events = events.ok && Array.isArray(events.data?.items) ? events.data.items : [];
    panel.eventsError = events.ok ? null : events;
    panel.reqs = flattenRequests(groups);
    panel.reqsError = groups.every((g) => !g.ok) ? groups[0] : null;
    renderPanel();
  }

  function detail(labelKey, value) {
    return h('div', { class: 'sp-detail' }, h('dt', { class: 'muted small', text: t(labelKey) }), h('dd', {}, value));
  }

  function sectionTabs() {
    const bar = h('div', { class: 'sp-tabs', attrs: { role: 'tablist', 'aria-label': t('students.sections') } });
    for (const s of PANEL_SECTIONS) {
      const on = panel.section === s;
      bar.append(h('button', {
        class: 'sp-tab',
        text: t(`students.section.${s}`),
        attrs: { type: 'button', role: 'tab', id: `sp-tab-${s}`, 'aria-selected': String(on), 'aria-controls': 'sp-section', tabindex: on ? '0' : '-1' },
        on: {
          click: () => {
            panel.section = s;
            renderPanel();
          },
        },
      }));
    }
    return bar;
  }

  function loadingOr(value, error, build, emptyKey) {
    if (error) return emptyState(t('students.sectionFailed'), '!');
    if (value === null) return skeletonRows(2);
    if (value.length === 0) return emptyState(t(emptyKey), '○');
    return build(value);
  }

  function subjectsSection() {
    const account = panel.account;
    const box = h('div', { class: 'sp-list' });
    box.append(loadingOr(panel.ents, panel.entsError, (items) => h('ul', { class: 'sp-items' }, ...items.map((ent) => {
      const state2 = ent.is_revoked ? 'revoked' : ent.is_expired ? 'expired' : 'active';
      const until = ent.expires_at ? t('students.until', { date: formatCairoDate(ent.expires_at) }) : '';
      return h('li', { class: 'sp-item', dataset: { source: ent.source || '' } },
        h('div', { class: 'sp-item-main' },
          h('span', { class: 'strong', text: [ent.subject_title_ar, ent.level_name_ar].filter(Boolean).join(' · ') || '—' }),
          h('span', { class: 'muted small', text: until })),
        state2 === 'active' ? null : chip(t(`entitlements.status.${state2}`), 'danger'),
        chip(t(sourceKey(ent.source)), 'neutral', 'source-chip'));
    })), 'students.noSubjects'));
    if (account.status !== 'deleted') {
      box.append(button(t('students.manageSubjects'), {
        variant: 'secondary', small: true, class: 'sp-add',
        onClick: () => entitlementsDialog.open(account),
      }));
    }
    return box;
  }

  function requestsSection() {
    return loadingOr(panel.reqs, panel.reqsError, (items) => h('ul', { class: 'sp-items' }, ...items.map((it) => {
      const tone = it.status === 'accepted' ? 'success' : it.status === 'rejected' ? 'danger' : 'warning';
      return h('li', { class: 'sp-item' },
        h('div', { class: 'sp-item-main' },
          h('span', { class: 'strong', text: it.title_ar || it.subject_title_ar || '—' }),
          h('span', { class: 'muted small', text: [formatMoney(it.price), formatDate(it.created_at)].filter(Boolean).join(' · ') })),
        chip(t(`requests.status.${it.status}`), tone));
    })), 'students.noRequests');
  }

  function activitySection() {
    return loadingOr(panel.events, panel.eventsError, (items) => h('ul', { class: 'sp-items' }, ...items.map((e) => {
      const m = describe(e, 'student');
      return h('li', { class: `sp-item ov-act kind-${m.kind}` },
        h('span', { class: 'audit-icon', text: m.icon, attrs: { 'aria-hidden': 'true' } }),
        h('div', { class: 'audit-main' },
          sentenceNode(m, null),
          h('span', { class: 'muted small', text: [formatDate(e.created_at), formatTime(e.created_at)].filter(Boolean).join(' · ') })));
    })), 'students.noActivity');
  }

  function renderPanel() {
    if (!panelBody) return;
    clear(panelBody);
    const account = panel.account;
    if (!account) return;
    const sections = { subjects: subjectsSection, requests: requestsSection, activity: activitySection };
    const actions = h('div', { class: 'sp-actions', attrs: { id: 'student-panel-actions' } });
    for (const action of actionsFor(account.status)) {
      actions.append(button(t(`action.${action}`), {
        variant: action === 'reactivate' ? 'secondary' : 'danger-outline', small: true,
        onClick: () => dialog.open(action, account),
      }));
    }
    const parts = [
      h('div', { class: 'sp-head' },
        avatar(account.full_name || account.email, 'lg'),
        h('div', { class: 'sp-title' },
          h('h3', { attrs: { id: 'student-panel-name' } }, h('bdi', { text: account.full_name || account.email || '—' })),
          h('p', { class: 'muted small' },
            h('span', { text: t('students.joined', { date: formatCairoDate(account.created_at) || '—' }) }),
            ' ',
            statusBadge(account.status)))),
      h('dl', { class: 'sp-details' },
        detail('accounts.col.email', h('bdi', { class: 'ltr', text: account.email || '—' })),
        detail('accounts.col.phone', h('bdi', { class: 'ltr', text: account.phone || '—' })),
        detail('students.id', copyIdChip(doc, account.id))),
      sectionTabs(),
      h('div', { class: 'sp-section', attrs: { id: 'sp-section', role: 'tabpanel', 'aria-labelledby': `sp-tab-${panel.section}` } },
        sections[panel.section]()),
    ];
    if (actions.children.length) parts.push(actions);
    panelBody.append(...parts);
  }

  // --- events ----------------------------------------------------------------
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    state.search = searchInput.value;
    state.page = 1;
    load();
  });
  resetButton.addEventListener('click', () => {
    searchInput.value = '';
    state.search = '';
    state.status = '';
    state.page = 1;
    load();
  });

  return {
    load,
    /** Filters the list to one account id (from the audit log or overview) and opens its panel on the next load. */
    showUser(id) {
      searchInput.value = String(id ?? '');
      Object.assign(state, { search: searchInput.value, status: '', page: 1, focusUser: String(id ?? '') });
    },
    /** The account whose panel is open, if any. */
    get selected() {
      return panel.account;
    },
    rerender() {
      render();
      dialog.rerender();
      entitlementsDialog.rerender();
    },
    /** Drops everything shown, used when the session ends. */
    reset() {
      seq += 1;
      panelSeq += 1;
      dialog.close();
      entitlementsDialog.close();
      sidePanel.dismiss();
      panel = panelState();
      layout?.setAttribute('data-panel', 'closed');
      Object.assign(state, { page: 1, search: '', status: '', total: 0, items: [], loaded: false, error: null, focusUser: '' });
      searchInput.value = '';
      render();
    },
  };
}
