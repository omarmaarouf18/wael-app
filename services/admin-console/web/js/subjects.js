// Subjects view: the subjects of one level with a draft/published filter
// and pagination. Publish needs at least one video upstream; the button is
// disabled with a hint at zero videos. Drafts can be deleted (owner
// amendment 2026-11-02); published subjects offer unpublish only. A
// published subject that no published iOS package covers (GET
// /api/iap/coverage, #56) carries a ⚠️ badge; the badge is informative, so a
// failed coverage call just shows none.

import { clear, h } from './dom.js';
import { formatCairoDate } from './subject-dialog.js';
import { formatMoney, formatNumber, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { createCooldown, createPager, hideBanner, hideToast, messageRow, showError, toast } from './ui.js';
import { uncoveredSet } from './iap.js';

export const PAGE_SIZE = 15;
const COLUMNS = 7;

/** Query for GET /api/subjects; empty values are left out. */
export function subjectsQuery({ levelId, published, page, limit }) {
  const query = { page, limit };
  if (levelId) query.level_id = levelId;
  if (published) query.published = published;
  return query;
}

export function mountSubjects({ api, doc = document, dialog, confirm, levelsOf, onOpen }) {
  const toolbar = doc.getElementById('catalog-toolbar');
  const banner = doc.getElementById('catalog-banner');
  const content = doc.getElementById('catalog-content');
  const pagerEl = doc.getElementById('catalog-pager');
  const pager = createPager(pagerEl, (page) => {
    state.page = page;
    load();
  });

  const state = { level: null, published: '', page: 1, total: 0, items: [], loaded: false, error: null, uncovered: new Set() };
  let seq = 0;
  // A 429 with Retry-After keeps the publish buttons off until the wait ends.
  const lock = createCooldown();
  lock.subscribe((left) => {
    if (left === 0) paint();
  });

  function badge(published) {
    return h('span', {
      class: `badge ${published ? 'badge-published' : 'badge-draft'}`,
      text: published ? t('catalog.published') : t('catalog.draft'),
    });
  }

  function row(subject) {
    const actions = h('td', { class: 'actions' });
    actions.append(
      h('button', {
        class: 'btn small primary', text: t('catalog.open'), attrs: { type: 'button' },
        on: { click: () => onOpen(subject) },
      }),
    );
    actions.append(
      h('button', {
        class: 'btn small secondary', text: t('catalog.edit'), attrs: { type: 'button' },
        on: { click: () => dialog.open({ mode: 'edit', subject }, levelsOf()) },
      }),
    );
    if (subject.published) {
      actions.append(
        h('button', {
          class: 'btn small secondary', text: t('catalog.unpublish'), attrs: { type: 'button' },
          on: { click: (event) => askUnpublish(subject, event.target) },
        }),
      );
    } else {
      const canPublish = (subject.video_count ?? 0) > 0;
      const button = h('button', {
        class: 'btn small secondary', text: t('catalog.publish'), attrs: { type: 'button' },
        on: { click: (event) => publish(subject, event.target) },
      });
      button.disabled = !canPublish || lock.active;
      if (!canPublish) button.title = t('catalog.publishDisabledHint');
      actions.append(button);
      actions.append(
        h('button', {
          class: 'btn small danger-outline', text: t('catalog.delete'), attrs: { type: 'button' },
          on: { click: () => askDelete(subject) },
        }),
      );
      if (!canPublish) {
        actions.append(h('div', { class: 'muted small', text: t('catalog.publishDisabledHint') }));
      }
    }
    return h(
      'tr',
      {},
      h('td', {}, h('div', { class: 'strong', text: subject.title_ar || '—' }, coverageBadge(subject)), subject.title_en ? h('div', { class: 'muted small ltr', text: subject.title_en }) : null),
      h('td', {}, badge(subject.published)),
      h('td', { text: formatMoney(subject.price ?? 0) }),
      h('td', { text: formatCairoDate(subject.access_expires_at) || '—' }),
      h('td', { text: t('catalog.videos', { n: formatNumber(subject.video_count ?? 0) }) }),
      h('td', { class: 'muted small', text: termText(subject.term) }),
      actions,
    );
  }

  function coverageBadge(subject) {
    if (!state.uncovered.has(subject.id)) return null;
    const tip = t('iap.coverageBadge');
    return h('span', { class: 'badge badge-warn', text: ' ⚠️', attrs: { title: tip, 'aria-label': tip, role: 'img' }, dataset: { coverage: 'uncovered' } });
  }

  function termText(term) {
    if (term === 'first' || term === 'second') return t(`catalog.term.${term}`);
    return '—';
  }

  async function publish(subject, button) {
    if (lock.active) return;
    button.disabled = true;
    const res = await api.post('/api/subjects/publish', { id: subject.id });
    if (!isSignedIn()) return;
    if (res.ok) {
      toast(doc, t('catalog.toast.published'));
      await load();
      return;
    }
    if (res.kind !== 'unauthorized') {
      lock.arm(res);
      state.error = res;
      paint();
    }
  }

  function askUnpublish(subject) {
    confirm.open({
      titleKey: 'catalog.confirmUnpublish',
      noteText: t('catalog.unpublishNote'),
      confirmKey: 'catalog.confirmUnpublish',
      tone: 'danger',
      path: '/api/subjects/unpublish',
      body: { id: subject.id },
      onDone: () => {
        toast(doc, t('catalog.toast.unpublished'));
        load();
      },
    });
  }

  // Delete is offered on drafts only; the academy also refuses a subject
  // with students or requests (subject_has_students), shown in the dialog.
  function askDelete(subject) {
    confirm.open({
      titleKey: 'catalog.deleteSubjectTitle',
      noteText: t('catalog.deleteSubjectNote'),
      confirmKey: 'catalog.confirmDelete',
      tone: 'danger',
      path: '/api/subjects/delete',
      body: { id: subject.id },
      onDone: () => {
        toast(doc, t('catalog.toast.deleted'));
        load();
      },
    });
  }

  function renderToolbar() {
    clear(toolbar);
    const filter = h('select', { attrs: { 'aria-label': t('catalog.filter.label') } });
    for (const [value, key] of [['', 'catalog.filter.all'], ['true', 'catalog.filter.published'], ['false', 'catalog.filter.draft']]) {
      filter.append(h('option', { text: t(key), attrs: { value } }));
    }
    filter.value = state.published;
    filter.addEventListener('change', () => {
      state.published = filter.value;
      state.page = 1;
      load();
    });
    const add = h('button', {
      class: 'btn primary', text: t('catalog.addSubject'), attrs: { type: 'button' },
      on: { click: () => dialog.open({ mode: 'create', levelKey: state.level?.key }, levelsOf()) },
    });
    toolbar.append(
      h('div', { class: 'field' }, filter),
      add,
    );
  }

  function render() {
    renderToolbar();
    pagerEl.hidden = false;
    clear(content);
    if (state.error && !state.loaded) return;
    const table = h('table', { class: 'catalog-table' },
      h('thead', {}, h('tr', {},
        h('th', { attrs: { scope: 'col' }, text: t('catalog.field.titleAr') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.published') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.field.price') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.field.expires') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.col.videos') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.field.term') }),
        h('th', { attrs: { scope: 'col' }, text: t('catalog.col.actions') }),
      )),
    );
    const tbody = h('tbody', {});
    table.append(tbody);
    if (!state.loaded) tbody.append(messageRow(COLUMNS, t('common.loading')));
    else if (state.items.length === 0) tbody.append(messageRow(COLUMNS, t('common.empty')));
    else for (const subject of state.items) tbody.append(row(subject));
    content.append(h('div', { class: 'table-wrap' }, table));
  }

  function paint() {
    render();
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
    if (state.error) showError(banner, state.error, load, state.error.kind === 'rate_limited' ? lock : undefined);
    else hideBanner(banner);
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    paint();
    const [res, cover] = await Promise.all([
      api.get('/api/subjects', subjectsQuery({
        levelId: state.level?.key, published: state.published, page: state.page, limit: PAGE_SIZE,
      })),
      api.get('/api/iap/coverage'),
    ]);
    if (mine !== seq || !isSignedIn()) return;
    state.uncovered = cover.ok ? uncoveredSet(cover.data) : new Set();
    if (!res.ok) {
      state.error = res;
      paint();
      return;
    }
    const data = res.data ?? {};
    state.items = Array.isArray(data.items) ? data.items : [];
    state.total = Number.isInteger(data.total) ? data.total : state.items.length;
    state.loaded = true;
    paint();
  }

  return {
    open(level, keep) {
      state.level = level;
      if (!keep) {
        state.published = '';
        state.page = 1;
        state.loaded = false;
        state.items = [];
      }
      load();
    },
    reload: load,
    rerender: paint,
    get view() {
      return { level: state.level, published: state.published, page: state.page };
    },
    reset() {
      seq += 1;
      lock.stop();
      hideToast(doc);
      Object.assign(state, { level: null, published: '', page: 1, total: 0, items: [], loaded: false, error: null, uncovered: new Set() });
    },
  };
}
