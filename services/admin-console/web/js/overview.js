// Overview tab (owner redesign 2026-11-14): the first tab after sign-in.
//
// - KPI cards from existing endpoints only: pending requests, new students in
//   the last 7 days, active students, published subjects. A card whose answer
//   fails (or is not a count) is hidden, never shown as zero.
// - "Requests waiting for you": the first 5 students with pending requests,
//   with inline accept/reject through the Requests tab's own confirm dialogs.
// - "Latest activity": the last 6 academy audit entries as sentences, with the
//   audit tab's renderer.
// Owner rule: nothing about Apple / in-app purchase is counted or warned
// about here.

import { getAdminName, isSignedIn } from './auth.js';
import { describe, loadAuditLookups, sentenceNode, buildLookups } from './audit.js';
import { clear, h } from './dom.js';
import { formatAge, formatLongDate, formatMoney, formatNumber, formatTime, t } from './i18n.js';
import { avatar, button, emptyState, hideBanner, kpiCard, showError, skeletonRows } from './ui.js';

export const PENDING_LIMIT = 5;
export const ACTIVITY_LIMIT = 6;
/** Accounts fetched (newest first) to count the last 7 days; more than this shows "100+". */
export const NEW_STUDENTS_SAMPLE = 100;
const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

/** A count from a list answer, or null when the answer is not usable. */
function totalOf(res, field = 'total') {
  return res && res.ok && Number.isInteger(res.data?.[field]) ? res.data[field] : null;
}

/**
 * New accounts in the last 7 days from the newest-first first page.
 * Returns { n, more } or null; more is true when the whole sample is new and
 * the server holds more accounts (the count is then a lower bound).
 */
export function countNewStudents(res, now = new Date()) {
  if (!res || !res.ok || !Array.isArray(res.data?.items)) return null;
  const since = now.getTime() - WEEK_MS;
  const items = res.data.items;
  const n = items.filter((a) => {
    const at = Date.parse(a?.created_at);
    return !Number.isNaN(at) && at >= since;
  }).length;
  const total = Number.isInteger(res.data.total) ? res.data.total : items.length;
  return { n, more: n === items.length && items.length >= NEW_STUDENTS_SAMPLE && total > items.length };
}

/** One overview row per student from grouped pending requests. */
export function summarizeGroup(group, accounts = new Map()) {
  const orders = Array.isArray(group?.orders) ? group.orders : [];
  const items = orders.flatMap((o) => (Array.isArray(o.items) ? o.items : []))
    .filter((it) => it && (it.status === undefined || it.status === 'pending'));
  const account = accounts.get(group?.user_id);
  const times = items.map((it) => Date.parse(it.created_at)).filter((x) => !Number.isNaN(x));
  const levels = [...new Set(items.map((it) => it.level_name_ar).filter(Boolean))];
  return {
    userId: group?.user_id ?? '',
    name: account?.full_name || account?.email || '',
    subjects: items.length,
    level: levels[0] ?? '',
    moreLevels: levels.length > 1,
    amount: items.reduce((sum, it) => sum + (Number(it.price) || 0), 0),
    oldest: times.length ? new Date(Math.min(...times)).toISOString() : '',
    requests: items.map((it) => ({
      id: it.request_id || it.id,
      price: Number(it.price) || 0,
      user_id: group?.user_id,
      title: it.title_ar || it.subject_title_ar || '',
    })).filter((r) => r.id),
  };
}

/** Groups flat pending requests by student, the shape of /api/request-groups. */
export function groupFlat(items) {
  const map = new Map();
  for (const it of Array.isArray(items) ? items : []) {
    if (!map.has(it.user_id)) map.set(it.user_id, { user_id: it.user_id, orders: [{ items: [] }] });
    map.get(it.user_id).orders[0].items.push({ ...it, request_id: it.id });
  }
  return [...map.values()];
}

export function mountOverview({
  api, doc = document, now = () => new Date(),
  decide = () => {}, openTab = () => {}, navigate = () => {},
} = {}) {
  const greeting = doc.getElementById('overview-greeting');
  const date = doc.getElementById('overview-date');
  const body = doc.getElementById('overview-body');
  const notice = doc.getElementById('overview-banner');

  const fresh = () => ({
    loaded: false,
    kpis: { pending: null, newStudents: null, active: null, published: null },
    groups: [], accounts: new Map(), pendingError: null, oldestPending: '',
    activity: [], activityError: null, lookups: buildLookups(),
  });
  let state = fresh();
  let seq = 0;

  function renderHead() {
    greeting.textContent = t('overview.greeting', { name: getAdminName() || t('top.role') });
    date.textContent = t('overview.date', { date: formatLongDate(now()) });
  }

  function kpis() {
    const k = state.kpis;
    const grid = h('section', { class: 'kpi-grid', attrs: { 'aria-label': t('overview.kpis') } });
    if (!state.loaded) {
      grid.append(skeletonRows(1));
      return grid;
    }
    if (k.pending !== null) {
      grid.append(kpiCard({
        id: 'kpi-pending', label: t('overview.kpi.pending'), value: formatNumber(k.pending), tone: 'accent', icon: '✉',
        hint: k.pending > 0 && state.oldestPending ? t('overview.kpi.pendingOldest', { age: formatAge(state.oldestPending, now()) }) : t('overview.kpi.pendingNone'),
      }));
    }
    if (k.newStudents !== null) {
      const v = k.newStudents;
      grid.append(kpiCard({
        id: 'kpi-new', label: t('overview.kpi.newStudents'), tone: 'info', icon: '+',
        value: v.more ? `${formatNumber(v.n)}+` : formatNumber(v.n), hint: t('overview.kpi.newStudentsHint'),
      }));
    }
    if (k.active !== null) {
      grid.append(kpiCard({
        id: 'kpi-active', label: t('overview.kpi.active'), value: formatNumber(k.active), tone: 'success', icon: '✓',
        hint: t('overview.kpi.activeHint'),
      }));
    }
    if (k.published !== null) {
      grid.append(kpiCard({
        id: 'kpi-published', label: t('overview.kpi.published'), value: formatNumber(k.published), tone: 'neutral', icon: '▤',
        hint: t('overview.kpi.publishedHint'),
      }));
    }
    return grid;
  }

  function pendingRow(row) {
    const meta = [
      t('overview.subjects', { n: formatNumber(row.subjects) }),
      row.level ? (row.moreLevels ? t('overview.levelMore', { level: row.level }) : row.level) : '',
      formatMoney(row.amount),
      row.oldest ? formatAge(row.oldest, now()) : '',
    ].filter(Boolean).join(' · ');
    const name = row.name
      ? h('bdi', { class: 'strong', text: row.name })
      : h('bdi', { class: 'strong id ltr', text: row.userId.slice(0, 8) });
    return h('li', { class: 'ov-row', dataset: { user: row.userId } },
      avatar(row.name || '?'),
      h('div', { class: 'ov-row-main' },
        h('button', {
          class: 'name-link', attrs: { type: 'button' },
          on: { click: () => navigate({ tab: 'accounts', user: row.userId }) },
        }, name),
        h('span', { class: 'muted small', text: meta })),
      h('div', { class: 'ov-row-actions' },
        button(t('requests.action.accept'), { variant: 'success', small: true, onClick: () => decide('accept', row.requests) }),
        button(t('requests.action.reject'), { variant: 'danger-outline', small: true, onClick: () => decide('reject', row.requests) })),
    );
  }

  function pendingPanel() {
    const banner = h('div', { class: 'banner', attrs: { role: 'alert', id: 'overview-pending-banner' } });
    banner.hidden = true;
    const list = h('ol', { class: 'ov-list', attrs: { id: 'overview-pending-list' } });
    const panel = h('section', { class: 'panel ov-pending', attrs: { 'aria-labelledby': 'overview-pending-title' } },
      h('div', { class: 'panel-head' },
        h('h3', { text: t('overview.pending.title'), attrs: { id: 'overview-pending-title' } }),
        button(t('overview.pending.all'), { variant: 'link', small: true, onClick: () => openTab('requests'), attrs: { id: 'overview-pending-all' } })),
      banner,
      h('div', { class: 'panel-body' }, list));
    if (!state.loaded) {
      list.append(skeletonRows(3));
    } else if (state.pendingError) {
      showError(banner, state.pendingError, () => load());
    } else {
      const rows = state.groups.slice(0, PENDING_LIMIT).map((g) => summarizeGroup(g, state.accounts)).filter((r) => r.requests.length > 0);
      if (rows.length === 0) list.append(emptyState(t('overview.pending.empty'), '✓'));
      for (const r of rows) list.append(pendingRow(r));
    }
    return panel;
  }

  function activityPanel() {
    const list = h('ol', { class: 'ov-activity', attrs: { id: 'overview-activity-list' } });
    const panel = h('section', { class: 'panel ov-activity-panel', attrs: { 'aria-labelledby': 'overview-activity-title' } },
      h('div', { class: 'panel-head' },
        h('h3', { text: t('overview.activity.title'), attrs: { id: 'overview-activity-title' } }),
        button(t('overview.activity.all'), { variant: 'link', small: true, onClick: () => openTab('audit'), attrs: { id: 'overview-activity-all' } })),
      h('div', { class: 'panel-body' }, list));
    if (!state.loaded) {
      list.append(skeletonRows(3));
    } else if (state.activityError) {
      list.append(emptyState(t('overview.activity.failed'), '!'));
    } else if (state.activity.length === 0) {
      list.append(emptyState(t('audit.empty.academy'), '○'));
    } else {
      for (const entry of state.activity.slice(0, ACTIVITY_LIMIT)) {
        const m = describe(entry, 'academy', state.lookups);
        list.append(h('li', { class: `ov-act kind-${m.kind}`, dataset: { action: m.action } },
          h('span', { class: 'audit-icon', text: m.icon, attrs: { 'aria-hidden': 'true' } }),
          h('div', { class: 'audit-main' },
            sentenceNode(m, navigate),
            h('time', {
              class: 'muted small', text: activityWhen(entry.created_at),
              attrs: { datetime: entry.created_at ? String(entry.created_at) : undefined },
            }))));
      }
    }
    return panel;
  }

  function activityWhen(value) {
    const age = formatAge(value, now());
    const time = formatTime(value);
    return [age, time].filter(Boolean).join(' · ') || '—';
  }

  function render() {
    renderHead();
    clear(body);
    body.append(kpis(), h('div', { class: 'ov-columns' }, pendingPanel(), activityPanel()));
  }

  async function loadGroups() {
    const res = await api.get('/api/request-groups', { status: 'pending' });
    if (res.ok) return { ok: true, groups: Array.isArray(res.data?.items) ? res.data.items : [], pending: totalOf(res, 'pending_count') };
    if (res.status !== 404) return { ok: false, res };
    const flat = await api.get('/api/requests', { status: 'pending', page: 1, limit: 100 });
    if (!flat.ok) return { ok: false, res: flat };
    return { ok: true, groups: groupFlat(flat.data?.items), pending: totalOf(flat, 'pending_count') };
  }

  async function loadAccounts(groups) {
    const ids = [...new Set(groups.slice(0, PENDING_LIMIT).map((g) => g.user_id).filter(Boolean))];
    if (ids.length === 0) return new Map();
    const res = await api.get('/api/accounts', { ids: ids.join(','), limit: 100 });
    const map = new Map();
    if (res.ok && Array.isArray(res.data?.items)) for (const a of res.data.items) map.set(a.id, a);
    return map;
  }

  async function load({ keepNotice = false } = {}) {
    const mine = ++seq;
    if (!keepNotice) hideBanner(notice);
    state.loaded = false;
    render();
    const [pendingCount, recent, active, published, groups, audit, lookups] = await Promise.all([
      api.get('/api/requests', { status: 'pending', page: 1, limit: 1 }),
      api.get('/api/accounts', { page: 1, limit: NEW_STUDENTS_SAMPLE }),
      api.get('/api/accounts', { status: 'active', page: 1, limit: 1 }),
      api.get('/api/subjects', { published: 'true', page: 1, limit: 1 }),
      loadGroups(),
      api.get('/api/audit', { source: 'academy', page: 1, limit: ACTIVITY_LIMIT }),
      loadAuditLookups(api),
    ]);
    if (mine !== seq || !isSignedIn()) return;
    const accounts = groups.ok ? await loadAccounts(groups.groups) : new Map();
    if (mine !== seq || !isSignedIn()) return;

    const pending = totalOf(pendingCount, 'pending_count') ?? totalOf(pendingCount) ?? (groups.ok ? groups.pending : null);
    const pendingItems = groups.ok ? groups.groups.flatMap((g) => (g.orders ?? []).flatMap((o) => o.items ?? [])) : [];
    const times = pendingItems.map((it) => Date.parse(it.created_at)).filter((x) => !Number.isNaN(x));
    state = {
      ...state,
      loaded: true,
      kpis: {
        pending,
        newStudents: countNewStudents(recent, now()),
        active: totalOf(active),
        published: totalOf(published),
      },
      groups: groups.ok ? groups.groups : [],
      pendingError: groups.ok ? null : groups.res,
      oldestPending: times.length ? new Date(Math.min(...times)).toISOString() : '',
      accounts,
      activity: audit.ok && Array.isArray(audit.data?.items) ? audit.data.items : [],
      activityError: audit.ok ? null : audit,
      lookups: lookups ?? state.lookups,
    };
    render();
  }

  return {
    load,
    rerender: render,
    /** Shows the outcome of a decision that only partly went through (text from the Requests tab). */
    showNotice(text) {
      clear(notice);
      notice.append(h('span', { class: 'banner-text', text }));
      notice.hidden = false;
    },
    reset() {
      seq += 1;
      state = fresh();
      hideBanner(notice);
      greeting.textContent = '';
      date.textContent = '';
      clear(body);
    },
  };
}
