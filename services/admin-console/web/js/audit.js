// Audit log tab (owner redesign 2026-11-14): newest first, paginated, one
// source at a time — the academy log (content, the default), the auth-service
// log (accounts), the notification-service log (broadcasts) or student
// account events. The logs are never merged: merged pagination over two
// sources is wrong without a shared cursor.
//
// Each entry reads as a sentence ("<actor> <verb> <target>") with an icon and
// colour per action kind, grouped under day headers. Names are resolved
// client-side from data the console already serves (iOS packages, subjects,
// levels); anything unknown falls back to a short id. Raw ids and the detail
// string sit in a collapsible LTR block with a copy button. Student and
// package names open the Accounts and iOS packages tabs.

import { clear, h } from './dom.js';
import { MESSAGES, cairoDayKey, formatLongDate, formatNumber, formatTime, getLang, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { fetchAllSubjects } from './iap.js';
import { createPager, hideBanner, showError, toast } from './ui.js';

export const PAGE_SIZE = 20;
export const SOURCES = Object.freeze(['auth', 'academy', 'notifications', 'student']);
export const DEFAULT_SOURCE = 'academy';
/** Below this width the list is shown as cards. */
export const NARROW_QUERY = '(max-width: 719.98px)';

/** Actions each source writes, in the order the action filter lists them. */
export const SOURCE_ACTIONS = Object.freeze({
  auth: Object.freeze(['account_suspend', 'account_reactivate', 'account_delete']),
  academy: Object.freeze([
    'iap_purchase', 'iap_package_create', 'iap_package_update', 'iap_package_delete',
    'request_accept', 'request_reject', 'request_bulk_accept', 'request_bulk_reject',
    'entitlement_grant', 'entitlement_revoke',
    'subject_create', 'subject_update', 'subject_publish', 'subject_unpublish', 'subject_delete',
    'video_create', 'video_update', 'video_delete', 'video_reorder',
    'level_create', 'level_update', 'level_delete',
    'file_upload', 'file_delete', 'settings_updated',
  ]),
  notifications: Object.freeze(['broadcast_send', 'broadcast_retry']),
  student: Object.freeze([
    'login', 'logout', 'session_revoked', 'session_ended_cap', 'password_changed', 'profile_changed',
    'email_change_requested', 'email_changed', 'deletion_requested', 'deletion_cancelled',
    'account_purged', 'account_suspended', 'account_reactivated', 'account_deleted',
  ]),
});

/** Icon per kind. The icon is decorative: the verb always carries the meaning. */
export const KIND_ICON = Object.freeze({
  create: '+',
  accept: '✓',
  update: '✎',
  danger: '✕',
  purchase: '★',
  broadcast: '✉',
  neutral: '•',
});

/** i18n key for a known audit action (noun label), or null. */
export function actionLabelKey(action) {
  const key = `audit.action.${action}`;
  return key in MESSAGES.en ? key : null;
}

/** i18n key for a known audit action's sentence verb, or null. */
export function verbKey(action) {
  const key = `audit.verb.${action}`;
  return key in MESSAGES.en ? key : null;
}

/** The colour/icon kind of an action: create, accept, update, danger, purchase, broadcast or neutral. */
export function actionKind(action) {
  const a = String(action ?? '');
  if (a === 'iap_purchase') return 'purchase';
  if (a.startsWith('broadcast_')) return 'broadcast';
  if (/(^|_)(accept|reactivate|reactivated|cancelled)$/.test(a)) return 'accept';
  if (/(_delete|_deleted|_reject|_revoke|_unpublish|_suspend|_suspended|_purged|_cap|deletion_requested)$/.test(a)) return 'danger';
  if (/(_create|_upload|_grant|_publish)$/.test(a)) return 'create';
  if (/(_update|_updated|_reorder|_changed|_requested)$/.test(a)) return 'update';
  return 'neutral';
}

/** First 8 characters of an id with an ellipsis; '' for none. */
export function shortId(id) {
  const s = String(id ?? '');
  return s.length > 10 ? `${s.slice(0, 8)}…` : s;
}

/** Parses "key=value key2=value2" audit detail strings (values have no spaces). */
export function parseDetail(detail) {
  const out = {};
  for (const m of String(detail ?? '').matchAll(/(?:^|\s)([a-z_]+)=(\S*)/g)) out[m[1]] = m[2];
  return out;
}

/** Name maps for packages, subjects and levels from their list answers. */
export function buildLookups({ packages = [], subjects = [], levels = [] } = {}) {
  const byId = (items, key = 'id') => new Map(items.filter((x) => x && x[key]).map((x) => [x[key], x]));
  return {
    packages: byId(packages),
    packagesByProduct: byId(packages, 'product_id'),
    subjects: byId(subjects),
    levels: byId(levels, 'key'),
  };
}

const EMPTY_LOOKUPS = buildLookups();

function localName(obj, field) {
  if (!obj) return '';
  const ar = obj[`${field}_ar`];
  const en = obj[`${field}_en`];
  return (getLang() === 'en' ? en || ar : ar || en) || '';
}

const userLink = (id) => (id ? { tab: 'accounts', user: id } : null);

function idTarget(id, link = null) {
  return id ? { text: shortId(id), title: String(id), link, isId: true } : null;
}

function nameTarget(name, id, link = null) {
  return name ? { text: name, title: id ? String(id) : '', link } : idTarget(id, link);
}

function adminTarget(entry, L, d) {
  const action = String(entry.action ?? '');
  const id = entry.target_id;
  const detailName = String(entry.detail ?? '').trim();
  switch (entry.target_type) {
    case 'user':
      return idTarget(id, userLink(id));
    case 'subject': {
      const fromDetail = action.startsWith('subject_') ? detailName : '';
      return nameTarget(localName(L.subjects.get(id), 'title') || fromDetail, id);
    }
    case 'level':
      return nameTarget(localName(L.levels.get(id), 'name') || (action.startsWith('level_') ? detailName : ''), id);
    case 'video':
    case 'file':
      return nameTarget(detailName, id);
    case 'iap_package': {
      const pkg = L.packages.get(id) || L.packagesByProduct.get(d.product);
      const fallback = action.startsWith('iap_package_') ? detailName : d.product || '';
      return nameTarget(localName(pkg, 'title') || fallback, id, id ? { tab: 'iap', packageId: id } : null);
    }
    case 'entitlement':
    case 'request':
      return d.subject_id ? nameTarget(localName(L.subjects.get(d.subject_id), 'title'), d.subject_id) : idTarget(id);
    case 'request_bulk':
      return { text: t('audit.count', { n: formatNumber(Number.parseInt(d.count, 10) || 0) }) };
    case 'settings':
    case 'broadcast':
      return null;
    default:
      return idTarget(id);
  }
}

/**
 * The display model of one entry: icon kind, sentence parts and raw details.
 * source is the log it came from; L the name lookups.
 */
export function describe(entry, source, L = EMPTY_LOOKUPS) {
  const e = entry ?? {};
  if (source === 'student') {
    const action = String(e.type ?? '');
    const extras = [];
    if (e.device_label) extras.push({ text: e.device_label, ltr: true });
    if (e.actor_type === 'admin') extras.push({ text: t('audit.byAdmin', { name: e.actor_name || t('audit.actor.admin') }) });
    else extras.push({ text: t('audit.actor.student'), muted: true });
    const raw = pick(e, ['user_id', 'student', 'type', 'device_label', 'actor_type', 'actor_id', 'actor_name', 'detail']);
    return finish({
      action,
      actor: { text: e.student || shortId(e.user_id) || '—', title: e.user_id || '', link: userLink(e.user_id) },
      target: null,
      extras,
      raw,
    });
  }
  const action = String(e.action ?? '');
  const d = parseDetail(e.detail);
  const purchase = action === 'iap_purchase';
  const actor = purchase
    ? { text: e.actor_name && e.actor_name !== e.actor_id ? e.actor_name : shortId(e.actor_id) || '—', title: e.actor_id || '', link: userLink(e.actor_id) }
    : { text: e.actor_name || shortId(e.actor_id) || '—', title: e.actor_id || '' };
  const extras = [];
  if (purchase && d.env) extras.push({ text: d.env === 'Sandbox' ? t('iap.env.sandbox') : t('iap.env.production'), pill: d.env === 'Sandbox' });
  if ((e.target_type === 'entitlement' || e.target_type === 'request') && d.user_id) {
    extras.push({ prefix: t('audit.forStudent'), text: shortId(d.user_id), title: d.user_id, link: userLink(d.user_id), isId: true });
  }
  const known = verbKey(action) !== null;
  const target = known ? adminTarget(e, L, d) : unknownTarget(e);
  const raw = pick(e, ['actor_id', 'actor_name', 'action', 'target_type', 'target_id', 'detail']);
  return finish({ action, actor, target, extras, raw });
}

function unknownTarget(e) {
  const type = String(e.target_type ?? '');
  const key = `audit.target.${type}`;
  const label = key in MESSAGES.en ? t(key) : type;
  const id = shortId(e.target_id);
  const text = [label, id].filter(Boolean).join(' ');
  return text ? { text, title: e.target_id || '' } : null;
}

function pick(e, keys) {
  return keys.filter((k) => e[k] !== undefined && e[k] !== null && e[k] !== '').map((k) => [k, String(e[k])]);
}

function finish(model) {
  const vk = verbKey(model.action);
  const kind = actionKind(model.action);
  const searchText = [
    model.actor.text, model.actor.title,
    model.target?.text, model.target?.title,
    ...model.extras.map((x) => `${x.text} ${x.title ?? ''}`),
    ...model.raw.map(([, v]) => v),
  ].filter(Boolean).join(' ').toLowerCase();
  return {
    ...model,
    kind,
    icon: KIND_ICON[kind],
    verb: vk ? t(vk) : model.action || '—',
    unknown: vk === null,
    searchText,
  };
}

/** True when the model passes the text search and the action filter. */
export function matchesFilter(model, { search = '', action = '' } = {}) {
  if (action && model.action !== action) return false;
  const q = String(search).trim().toLowerCase();
  return !q || model.searchText.includes(q);
}

/** Header for a Cairo day key: Today, Yesterday, or the long date. */
export function dayLabel(sample, now = new Date()) {
  const key = cairoDayKey(sample);
  if (!key) return '—';
  if (key === cairoDayKey(now)) return t('audit.today');
  if (key === cairoDayKey(new Date(now.getTime() - 24 * 60 * 60 * 1000))) return t('audit.yesterday');
  return formatLongDate(sample);
}

/** Groups items (already newest first) into [{ key, label, items }] by Cairo day. */
export function groupByDay(items, now = new Date(), at = (x) => x.created_at) {
  const groups = [];
  for (const item of items) {
    const key = cairoDayKey(at(item)) || 'unknown';
    let g = groups.at(-1);
    if (!g || g.key !== key) {
      g = { key, label: dayLabel(at(item), now), items: [] };
      groups.push(g);
    }
    g.items.push(item);
  }
  return groups;
}

/**
 * Query for GET /api/audit; the source is always sent explicitly. Only the
 * student source supports search (student) and type on the server.
 */
export function auditQuery({ source, page, limit, search, action }) {
  const q = { source, page, limit };
  if (source === 'student') {
    const text = String(search ?? '').trim();
    if (text) q.student = text;
    if (action) q.type = action;
  }
  return q;
}

/** Whether filtering for a source happens on the server (else on the loaded page). */
export function filtersOnServer(source) {
  return source === 'student';
}

/** A name in a sentence: a link button when it opens another tab, else text. */
function nameEl(part, cls, navigate) {
  const inner = h('bdi', { text: part.text, class: part.isId ? 'id' : '' });
  if (part.link && navigate) {
    return h('button', {
      class: `name-link ${cls}`,
      attrs: { type: 'button', title: part.title || undefined },
      on: { click: () => navigate(part.link) },
    }, inner);
  }
  return h('span', { class: cls, attrs: { title: part.title || undefined } }, inner);
}

/** The "<actor> <verb> <target> · extras" sentence of a described entry. Shared with the overview. */
export function sentenceNode(m, navigate) {
  const sentence = h('p', { class: 'audit-sentence' },
    nameEl(m.actor, 'audit-actor strong', navigate),
    ' ',
    h('span', { class: m.unknown ? 'audit-verb ltr id' : 'audit-verb', text: m.verb }),
  );
  if (m.target) sentence.append(' ', nameEl(m.target, 'audit-target strong', navigate));
  for (const x of m.extras) {
    sentence.append(h('span', { class: 'audit-sep', text: ' · ', attrs: { 'aria-hidden': 'true' } }));
    if (x.prefix) sentence.append(h('span', { class: 'muted', text: `${x.prefix} ` }));
    if (x.link) sentence.append(nameEl(x, 'audit-extra', navigate));
    else sentence.append(h('bdi', { class: `audit-extra${x.pill ? ' pill' : ''}${x.muted ? ' muted' : ''}${x.ltr ? ' ltr' : ''}`, text: x.text }));
  }
  return sentence;
}

/**
 * Names for the academy log from data the console already serves (iOS
 * packages, levels, subjects). Best effort: null when every call failed.
 */
export async function loadAuditLookups(api) {
  const [packages, levels, subjects] = await Promise.all([
    api.get('/api/iap/packages'),
    api.get('/api/levels'),
    fetchAllSubjects(api),
  ]);
  if (!packages.ok && !levels.ok && !subjects.ok) return null;
  return buildLookups({
    packages: packages.ok && Array.isArray(packages.data?.packages) ? packages.data.packages : [],
    levels: levels.ok && Array.isArray(levels.data?.levels) ? levels.data.levels : [],
    subjects: subjects.ok && Array.isArray(subjects.items) ? subjects.items : [],
  });
}

export function mountAudit({ api, doc = document, win = globalThis.window, navigate = () => {}, now = () => new Date() }) {
  const banner = doc.getElementById('audit-banner');
  const list = doc.getElementById('audit-list');
  const pager = createPager(doc.getElementById('audit-pager'), (page) => {
    state.page = page;
    load();
  });
  const refreshButton = doc.getElementById('audit-refresh');
  const sourceButtons = Object.fromEntries(SOURCES.map((s) => [s, doc.getElementById(`audit-source-${s}`)]));

  const filterForm = doc.getElementById('audit-filter');
  const searchInput = doc.getElementById('audit-search');
  const actionSelect = doc.getElementById('audit-action');
  const filterReset = doc.getElementById('audit-filter-reset');
  const filterNote = doc.getElementById('audit-filter-note');

  const mq = win && typeof win.matchMedia === 'function' ? win.matchMedia(NARROW_QUERY) : null;

  const fresh = () => ({
    source: DEFAULT_SOURCE, page: 1, total: 0, items: [], search: '', action: '',
    loaded: false, error: null, expanded: new Set(),
  });
  let state = fresh();
  let lookups = EMPTY_LOOKUPS;
  let lookupsLoaded = false;
  let seq = 0;

  function isNarrow() {
    return Boolean(mq && mq.matches);
  }

  function renderSource() {
    for (const [s, btn] of Object.entries(sourceButtons)) btn?.setAttribute('aria-pressed', String(state.source === s));
    renderActionOptions();
    if (filterNote) filterNote.textContent = t(filtersOnServer(state.source) ? 'audit.filterNote.server' : 'audit.filterNote.client');
  }

  function renderActionOptions() {
    if (!actionSelect) return;
    clear(actionSelect);
    actionSelect.append(h('option', { text: t('audit.typeAll'), attrs: { value: '' } }));
    for (const a of SOURCE_ACTIONS[state.source]) {
      const key = actionLabelKey(a);
      actionSelect.append(h('option', { text: key ? t(key) : a, attrs: { value: a, selected: a === state.action } }));
    }
    actionSelect.value = state.action;
  }

  function copyText(text) {
    const clip = globalThis.navigator?.clipboard;
    if (!clip || typeof clip.writeText !== 'function') return;
    clip.writeText(text).then(() => toast(doc, t('audit.copied')), () => {});
  }

  function row(entry, index) {
    const m = describe(entry, state.source, lookups);
    const rowKey = String(entry.id ?? `${state.page}-${index}`);
    const detailsId = `audit-d-${state.page}-${index}`;
    const open = state.expanded.has(rowKey);

    const sentence = sentenceNode(m, navigate);

    const rawText = m.raw.map(([k, v]) => `${k}: ${v}`).join('\n');
    const details = h('div', { class: 'audit-details ltr-block', attrs: { id: detailsId, dir: 'ltr' } },
      h('dl', {}, ...m.raw.flatMap(([k, v]) => [h('dt', { text: k }), h('dd', { class: 'id', text: v })])),
      h('button', {
        class: 'btn small secondary audit-copy', text: t('audit.copy'), attrs: { type: 'button' },
        on: { click: () => copyText(rawText) },
      }),
    );
    details.hidden = !open;
    const toggle = h('button', {
      class: 'btn link small audit-toggle',
      text: t(open ? 'audit.detailsHide' : 'audit.details'),
      attrs: { type: 'button', 'aria-expanded': String(open), 'aria-controls': detailsId },
      on: {
        click: () => {
          if (state.expanded.has(rowKey)) state.expanded.delete(rowKey);
          else state.expanded.add(rowKey);
          const nowOpen = state.expanded.has(rowKey);
          details.hidden = !nowOpen;
          toggle.setAttribute('aria-expanded', String(nowOpen));
          toggle.textContent = t(nowOpen ? 'audit.detailsHide' : 'audit.details');
        },
      },
    });

    const timeValue = entry.created_at ? String(entry.created_at) : undefined;
    return h('li', { class: `audit-row kind-${m.kind}`, dataset: { kind: m.kind, action: m.action } },
      h('span', { class: 'audit-icon', text: m.icon, attrs: { 'aria-hidden': 'true' } }),
      h('div', { class: 'audit-main' }, sentence, details),
      h('time', { class: 'audit-time muted small', text: formatTime(entry.created_at) || '—', attrs: { datetime: timeValue } }),
      toggle,
    );
  }

  function visibleItems() {
    if (filtersOnServer(state.source)) return state.items;
    return state.items.filter((entry) => matchesFilter(describe(entry, state.source, lookups), state));
  }

  function renderList() {
    clear(list);
    list.className = `audit-list${isNarrow() ? ' is-narrow' : ''}`;
    if (!state.loaded) {
      if (!state.error) list.append(h('p', { class: 'audit-empty muted', text: t('common.loading') }));
      return;
    }
    if (state.items.length === 0) {
      const filtered = filtersOnServer(state.source) && (state.search || state.action);
      list.append(h('p', { class: 'audit-empty muted', text: filtered ? t('common.empty') : t(`audit.empty.${state.source}`) }));
      return;
    }
    const items = visibleItems();
    if (items.length === 0) {
      list.append(h('p', { class: 'audit-empty muted', text: t('audit.noMatch') }));
      return;
    }
    let index = 0;
    for (const group of groupByDay(items, now())) {
      const rows = h('ol', { class: 'audit-rows' });
      for (const entry of group.items) rows.append(row(entry, index++));
      list.append(h('section', { class: 'audit-day' }, h('h3', { class: 'audit-day-head', text: group.label }), rows));
    }
  }

  function render() {
    renderSource();
    renderList();
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
  }

  async function load({ refreshNames = false } = {}) {
    const mine = ++seq;
    state.error = null;
    render();
    const wantNames = state.source === 'academy' && (!lookupsLoaded || refreshNames);
    const [res, names] = await Promise.all([
      api.get('/api/audit', auditQuery({ ...state, limit: PAGE_SIZE })),
      wantNames ? loadAuditLookups(api) : Promise.resolve(null),
    ]);
    if (mine !== seq || !isSignedIn()) return;
    if (names) {
      lookups = names;
      lookupsLoaded = true;
    }
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

  function setSource(source) {
    if (state.source === source) return;
    Object.assign(state, { source, page: 1, search: '', action: '', items: [], loaded: false, expanded: new Set() });
    if (searchInput) searchInput.value = '';
    load();
  }

  refreshButton.addEventListener('click', () => {
    state.page = 1;
    load({ refreshNames: true });
  });
  for (const [s, btn] of Object.entries(sourceButtons)) btn?.addEventListener('click', () => setSource(s));

  filterForm?.addEventListener('submit', (e) => {
    e.preventDefault();
    state.search = (searchInput?.value || '').trim();
    state.action = (actionSelect?.value || '').trim();
    if (filtersOnServer(state.source)) {
      state.page = 1;
      load();
    } else {
      render();
    }
  });
  filterReset?.addEventListener('click', () => {
    if (searchInput) searchInput.value = '';
    const had = state.search || state.action;
    state.search = '';
    state.action = '';
    if (filtersOnServer(state.source) && had) {
      state.page = 1;
      load();
    } else {
      render();
    }
  });

  if (mq && typeof mq.addEventListener === 'function') mq.addEventListener('change', () => renderList());

  return {
    load,
    rerender: render,
    reset() {
      seq += 1;
      state = fresh();
      lookups = EMPTY_LOOKUPS;
      lookupsLoaded = false;
      if (searchInput) searchInput.value = '';
      render();
    },
  };
}
