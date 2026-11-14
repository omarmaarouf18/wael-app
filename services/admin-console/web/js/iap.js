// "iOS packages" tab (#56): the App Store packages students buy in the iPhone
// app (ADR-0015). Lists every package (published or not), creates and edits
// them, deletes an unused one (a package with purchases is refused upstream
// with 409 package_has_purchases, shown in the confirm dialog), lists the
// purchases of one package, and warns how many published subjects no
// published package covers. Prices are not shown or edited: they live in App
// Store Connect only.

import { clear, h } from './dom.js';
import { formatCairoDateTime, formatNumber, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { createConfirm } from './confirm.js';
import { createIAPDialog } from './iap-dialog.js';
import { formatCairoDate } from './subject-dialog.js';
import { hideBanner, hideToast, messageRow, showError, toast } from './ui.js';

const COLUMNS = 6;
const PURCHASE_COLUMNS = 5;
const SUBJECT_PAGE = 100;
// Upper bound on subject pages read for the picker (100 x 50 subjects).
const MAX_SUBJECT_PAGES = 50;
export const PURCHASES_LIMIT = 50;

/**
 * Reads every subject through the paged /api/subjects route. Resolves
 * { ok: true, items } or the failed response.
 */
export async function fetchAllSubjects(api) {
  const items = [];
  for (let page = 1; page <= MAX_SUBJECT_PAGES; page++) {
    const res = await api.get('/api/subjects', { page, limit: SUBJECT_PAGE });
    if (!res.ok) return res;
    const batch = Array.isArray(res.data?.items) ? res.data.items : [];
    items.push(...batch);
    const total = Number.isInteger(res.data?.total) ? res.data.total : items.length;
    if (batch.length < SUBJECT_PAGE || items.length >= total) break;
  }
  return { ok: true, items };
}

/** The uncovered subject ids from a /api/iap/coverage answer, as a Set. */
export function uncoveredSet(data) {
  const ids = Array.isArray(data?.uncovered_subject_ids) ? data.uncovered_subject_ids : [];
  return new Set(ids.filter((id) => typeof id === 'string'));
}

export function mountIAP({ api, doc = document }) {
  const addButton = doc.getElementById('iap-add');
  const coverage = doc.getElementById('iap-coverage');
  const banner = doc.getElementById('iap-banner');
  const body = doc.getElementById('iap-body');
  const purchasesBox = doc.getElementById('iap-purchases');
  const purchasesTitle = doc.getElementById('iap-purchases-title');
  const purchasesBanner = doc.getElementById('iap-purchases-banner');
  const purchasesBody = doc.getElementById('iap-purchases-body');
  const purchasesClose = doc.getElementById('iap-purchases-close');

  const confirm = createConfirm({ api, doc });
  const dialog = createIAPDialog({
    api, doc,
    onDone: (key) => {
      toast(doc, t(key));
      load();
    },
  });

  const empty = () => ({
    packages: [], levels: [], subjects: [], uncovered: null, loaded: false, error: null,
    purchasesFor: null, purchases: [], purchasesLoaded: false, purchasesError: null,
  });
  let state = empty();
  let seq = 0;
  let purchasesSeq = 0;

  function statusBadge(pkg) {
    return h('span', {
      class: `badge ${pkg.published ? 'badge-published' : 'badge-draft'}`,
      text: t(pkg.published ? 'iap.published' : 'iap.draft'),
    });
  }

  function row(pkg) {
    const count = Array.isArray(pkg.subject_ids) ? pkg.subject_ids.length : 0;
    const actions = h('td', { class: 'actions' },
      h('button', {
        class: 'btn small secondary', text: t('iap.edit'), attrs: { type: 'button' },
        on: { click: () => dialog.open({ mode: 'edit', pkg }, state.levels, state.subjects) },
      }),
      h('button', {
        class: 'btn small secondary', text: t('iap.purchases'), attrs: { type: 'button' },
        on: { click: () => openPurchases(pkg) },
      }),
      h('button', {
        class: 'btn small danger-outline', text: t('iap.delete'), attrs: { type: 'button' },
        on: { click: () => askDelete(pkg) },
      }),
    );
    return h('tr', { dataset: { id: pkg.id } },
      h('td', {},
        h('div', { class: 'strong', text: pkg.title_ar || '—' }),
        pkg.title_en ? h('div', { class: 'muted small ltr', text: pkg.title_en }) : null),
      h('td', { class: 'ltr', text: pkg.product_id || '—' }),
      h('td', { text: t('iap.subjectCount', { n: formatNumber(count) }) }),
      h('td', { text: pkg.available_until ? (formatCairoDate(pkg.available_until) || '—') : t('iap.noEnd') }),
      h('td', {}, statusBadge(pkg)),
      actions,
    );
  }

  function askDelete(pkg) {
    confirm.open({
      titleKey: 'iap.deleteTitle',
      noteText: t('iap.deleteNote', { title: pkg.title_ar || pkg.product_id || pkg.id }),
      confirmKey: 'iap.confirmDelete',
      tone: 'danger',
      path: '/api/iap/packages/delete',
      body: { id: pkg.id },
      onDone: () => {
        toast(doc, t('iap.toast.deleted'));
        if (state.purchasesFor?.id === pkg.id) closePurchases();
        load();
      },
    });
  }

  function renderCoverage() {
    const n = state.uncovered ? state.uncovered.size : 0;
    coverage.textContent = n > 0 ? `⚠️ ${t('iap.coverage', { n: formatNumber(n) })}` : '';
    coverage.hidden = n === 0;
  }

  function render() {
    clear(body);
    if (!state.loaded) {
      if (!state.error) body.append(messageRow(COLUMNS, t('common.loading')));
    } else if (state.packages.length === 0) {
      body.append(messageRow(COLUMNS, t('common.empty')));
    } else {
      for (const pkg of state.packages) body.append(row(pkg));
    }
    addButton.disabled = !state.loaded;
    renderCoverage();
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
    renderPurchases();
  }

  function purchaseRow(p) {
    const env = p.environment === 'Sandbox' ? t('iap.env.sandbox') : t('iap.env.production');
    const granted = Array.isArray(p.granted_subject_ids) ? p.granted_subject_ids.length : 0;
    return h('tr', {},
      h('td', { text: formatCairoDateTime(p.purchase_date) || '—' }),
      h('td', { class: 'ltr small', text: p.user_id || '—' }),
      h('td', { class: 'ltr small', text: p.transaction_id || '—' }),
      h('td', { text: env }),
      h('td', { text: formatNumber(granted) }),
    );
  }

  function renderPurchases() {
    const pkg = state.purchasesFor;
    purchasesBox.hidden = !pkg;
    clear(purchasesBody);
    if (!pkg) {
      hideBanner(purchasesBanner);
      return;
    }
    purchasesTitle.textContent = t('iap.purchasesTitle', { title: pkg.title_ar || pkg.product_id || pkg.id });
    if (!state.purchasesLoaded) {
      if (!state.purchasesError) purchasesBody.append(messageRow(PURCHASE_COLUMNS, t('common.loading')));
    } else if (state.purchases.length === 0) {
      purchasesBody.append(messageRow(PURCHASE_COLUMNS, t('iap.purchasesEmpty')));
    } else {
      for (const p of state.purchases) purchasesBody.append(purchaseRow(p));
    }
    if (state.purchasesError) showError(purchasesBanner, state.purchasesError, () => openPurchases(pkg));
    else hideBanner(purchasesBanner);
  }

  async function openPurchases(pkg) {
    const mine = ++purchasesSeq;
    Object.assign(state, { purchasesFor: pkg, purchases: [], purchasesLoaded: false, purchasesError: null });
    renderPurchases();
    const res = await api.get('/api/iap/purchases', { package_id: pkg.id, limit: PURCHASES_LIMIT });
    if (mine !== purchasesSeq || !isSignedIn()) return;
    if (!res.ok) {
      state.purchasesError = res;
    } else {
      state.purchases = Array.isArray(res.data?.purchases) ? res.data.purchases : [];
      state.purchasesLoaded = true;
    }
    renderPurchases();
  }

  function closePurchases() {
    purchasesSeq += 1;
    Object.assign(state, { purchasesFor: null, purchases: [], purchasesLoaded: false, purchasesError: null });
    renderPurchases();
  }

  // Packages, levels and subjects are all needed before the table and the
  // dialog are usable; any failure shows the banner with a retry. Coverage is
  // informative only: when it fails the warning is simply not shown.
  async function load() {
    const mine = ++seq;
    state.error = null;
    render();
    const [packages, levels, subjects, cover] = await Promise.all([
      api.get('/api/iap/packages'),
      api.get('/api/levels'),
      fetchAllSubjects(api),
      api.get('/api/iap/coverage'),
    ]);
    if (mine !== seq || !isSignedIn()) return;
    const failed = [packages, levels, subjects].find((res) => !res.ok);
    if (failed) {
      state.error = failed;
      render();
      return;
    }
    state.packages = Array.isArray(packages.data?.packages) ? packages.data.packages : [];
    state.levels = Array.isArray(levels.data?.levels) ? levels.data.levels : [];
    state.subjects = subjects.items;
    state.uncovered = cover.ok ? uncoveredSet(cover.data) : null;
    state.loaded = true;
    render();
  }

  addButton.addEventListener('click', () => {
    if (!state.loaded) return;
    dialog.open({ mode: 'create' }, state.levels, state.subjects);
  });
  purchasesClose.addEventListener('click', closePurchases);

  return {
    load,
    rerender() {
      render();
      dialog.rerender();
      confirm.rerender();
    },
    reset() {
      seq += 1;
      purchasesSeq += 1;
      dialog.close();
      confirm.close();
      hideToast(doc);
      state = empty();
      clear(body);
      clear(purchasesBody);
      purchasesTitle.textContent = '';
      purchasesBox.hidden = true;
      coverage.hidden = true;
      coverage.textContent = '';
      hideBanner(banner);
      hideBanner(purchasesBanner);
    },
  };
}
