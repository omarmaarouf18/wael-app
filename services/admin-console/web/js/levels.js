// Levels view: the three study-type sections in fixed order, each level with
// its names, published badge and subject counts. Diplomas can be added,
// edited, (un)published and deleted (only when empty); seeded levels offer
// edit and publish only.

import { clear, h } from './dom.js';
import { formatNumber, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { hideBanner, hideToast, showError, toast } from './ui.js';

export const STUDY_ORDER = Object.freeze(['bachelor', 'diploma', 'vocational']);
export const NAME_MAX = 200;

export function studyTitle(studyType) {
  return t(`catalog.study.${studyType}`);
}

/** Counts characters (code points), like the server. */
export function validName(raw) {
  const value = String(raw ?? '').trim().replace(/[\r\n]/g, '');
  const length = [...value].length;
  if (length < 1) return { ok: false, error: 'required', value };
  if (length > NAME_MAX) return { ok: false, error: 'too_long', value };
  return { ok: true, error: null, value };
}

export function parseOrder(raw) {
  const text = String(raw ?? '').trim();
  if (text === '') return { ok: true, value: undefined };
  if (!/^-?\d+$/.test(text)) return { ok: false };
  return { ok: true, value: Number(text) };
}

export function mountLevels({ api, doc = document, confirm, onOpen }) {
  const toolbar = doc.getElementById('catalog-toolbar');
  const banner = doc.getElementById('catalog-banner');
  const content = doc.getElementById('catalog-content');
  const pager = doc.getElementById('catalog-pager');

  const state = { items: [], loaded: false, error: null };
  let seq = 0;

  const editor = createLevelEditor({ api, doc, onDone: load });

  function badge(published) {
    return h('span', {
      class: `badge ${published ? 'badge-published' : 'badge-draft'}`,
      text: published ? t('catalog.published') : t('catalog.draft'),
    });
  }

  function card(level) {
    const buttons = h('div', { class: 'card-actions' });
    buttons.append(
      h('button', {
        class: 'btn small primary', text: t('catalog.open'), attrs: { type: 'button' },
        on: { click: () => onOpen(level) },
      }),
    );
    buttons.append(
      h('button', {
        class: 'btn small secondary', text: t('catalog.edit'), attrs: { type: 'button' },
        on: { click: () => editor.open(level) },
      }),
    );
    buttons.append(
      h('button', {
        class: 'btn small secondary',
        text: level.published ? t('catalog.unpublish') : t('catalog.publish'),
        attrs: { type: 'button' },
        on: { click: (event) => togglePublished(level, event.target) },
      }),
    );
    if (level.study_type === 'diploma' && (level.subject_count ?? 0) === 0) {
      buttons.append(
        h('button', {
          class: 'btn small danger-outline', text: t('catalog.delete'), attrs: { type: 'button' },
          on: { click: () => askDelete(level) },
        }),
      );
    }
    const names = h('div', {},
      h('div', { class: 'strong', text: level.name_ar || '—' }),
      level.name_en ? h('div', { class: 'muted small ltr', text: level.name_en }) : null,
    );
    return h(
      'div',
      { class: 'card level-card' },
      h('div', { class: 'level-head' }, names, badge(level.published)),
      h('div', { class: 'muted small', text: t('catalog.counts', { total: formatNumber(level.subject_count ?? 0), published: formatNumber(level.published_subject_count ?? 0) }) }),
      buttons,
    );
  }

  async function togglePublished(level, button) {
    button.disabled = true;
    const res = await api.post('/api/levels/update', { id: level.key, published: !level.published });
    if (!isSignedIn()) return;
    if (res.ok) {
      toast(doc, t(level.published ? 'catalog.toast.unpublished' : 'catalog.toast.published'));
      await load();
      return;
    }
    if (res.kind !== 'unauthorized') {
      state.error = res;
      render();
    }
  }

  function askDelete(level) {
    confirm.open({
      titleKey: 'catalog.deleteLevelTitle',
      noteText: t('catalog.deleteLevelNote'),
      confirmKey: 'catalog.confirmDelete',
      tone: 'danger',
      path: '/api/levels/delete',
      body: { id: level.key },
      onDone: () => {
        toast(doc, t('catalog.toast.deleted'));
        load();
      },
    });
  }

  function render() {
    clear(toolbar);
    pager.hidden = true;
    clear(content);
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      content.append(h('p', { class: 'muted', text: t('common.loading') }));
      return;
    }
    const byType = new Map();
    for (const level of state.items) {
      if (!byType.has(level.study_type)) byType.set(level.study_type, []);
      byType.get(level.study_type).push(level);
    }
    // All three sections always render, even when a type has no levels yet:
    // the first diploma is created from the empty diploma section.
    const order = [...STUDY_ORDER, ...[...byType.keys()].filter((k) => !STUDY_ORDER.includes(k))];
    for (const studyType of order) {
      const levels = byType.get(studyType) ?? [];
      const section = h('section', { class: 'level-group' },
        h('div', { class: 'section-head' },
          h('h3', { text: studyTitle(studyType) }),
          studyType === 'diploma'
            ? h('button', {
              class: 'btn primary small', text: t('catalog.addDiploma'), attrs: { type: 'button' },
              on: { click: () => editor.open(null) },
            })
            : null,
        ),
      );
      if (levels.length === 0) section.append(h('p', { class: 'muted', text: t('common.empty') }));
      for (const level of levels) section.append(card(level));
      content.append(section);
    }
  }

  function paint() {
    render();
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    paint();
    const res = await api.get('/api/levels');
    if (mine !== seq || !isSignedIn()) return;
    if (!res.ok) {
      state.error = res;
      paint();
      return;
    }
    const data = res.data ?? {};
    state.items = Array.isArray(data.levels) ? data.levels : [];
    state.loaded = true;
    paint();
  }

  return {
    load,
    rerender() {
      paint();
      editor.rerender();
    },
    reset() {
      seq += 1;
      hideToast(doc);
      Object.assign(state, { items: [], loaded: false, error: null });
      paint();
    },
    show() {
      paint();
    },
    get items() {
      return state.items;
    },
  };
}

// The add/edit dialog for a diploma (create) or any level (edit name/order).
function createLevelEditor({ api, doc, onDone }) {
  const host = doc.getElementById('catalog-dialogs');
  const title = h('h2', {});
  const banner = h('div', { class: 'banner', attrs: { role: 'alert' } });
  banner.hidden = true;
  const nameAr = h('input', { attrs: { type: 'text', maxlength: '200', autocomplete: 'off' } });
  const nameEn = h('input', { class: 'ltr', attrs: { type: 'text', maxlength: '200', autocomplete: 'off' } });
  const order = h('input', { class: 'ltr', attrs: { type: 'number', step: '1' } });
  const cancel = h('button', { class: 'btn secondary', text: t('common.cancel'), attrs: { type: 'button' } });
  const save = h('button', { class: 'btn primary', text: t('catalog.save'), attrs: { type: 'submit' } });
  const form = h(
    'form',
    { attrs: { novalidate: '' } },
    title,
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.nameAr') }), nameAr),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.nameEn') }), nameEn),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.order') }), order),
    banner,
    h('div', { class: 'dialog-actions' }, cancel, save),
  );
  const dialog = h('dialog', { class: 'dialog' }, form);
  host.append(dialog);

  let current = null; // null for create, level for edit
  let busy = false;

  function fieldError() {
    if (!validName(nameAr.value).ok) return 'catalog.required';
    if (nameEn.value.trim() !== '' && !validName(nameEn.value).ok) return 'catalog.tooLong';
    if (!parseOrder(order.value).ok) return 'catalog.required';
    return null;
  }

  function render() {
    title.textContent = t(current ? 'catalog.levelEditTitle' : 'catalog.levelCreateTitle');
    save.textContent = busy ? t('dialog.working') : t(current ? 'catalog.save' : 'catalog.create');
    save.disabled = busy;
    cancel.disabled = busy;
    nameAr.disabled = busy;
    nameEn.disabled = busy;
    order.disabled = busy;
  }

  async function submit() {
    if (busy) return;
    if (fieldError()) {
      showError(banner, { kind: 'bad_request' }, null);
      return;
    }
    const body = { name_ar: validName(nameAr.value).value };
    const en = nameEn.value.trim();
    if (en !== '') body.name_en = validName(nameEn.value).value;
    else if (current) body.name_en = '';
    const parsed = parseOrder(order.value);
    if (parsed.value !== undefined) body.order = parsed.value;
    let path;
    if (current) {
      path = '/api/levels/update';
      body.id = current.key;
    } else {
      path = '/api/levels/create';
      body.study_type = 'diploma';
    }
    busy = true;
    hideBanner(banner);
    render();
    const res = await api.post(path, body);
    busy = false;
    if (res.ok) {
      const created = !current;
      dialog.close();
      toast(doc, t(created ? 'catalog.toast.created' : 'catalog.toast.updated'));
      onDone();
      return;
    }
    if (res.kind === 'unauthorized') {
      dialog.close();
      return;
    }
    showError(banner, res, submit);
    render();
  }

  form.addEventListener('submit', (event) => {
    event.preventDefault();
    submit();
  });
  for (const input of [nameAr, nameEn, order]) input.addEventListener('input', () => hideBanner(banner));
  cancel.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    open(level) {
      current = level;
      busy = false;
      nameAr.value = level ? level.name_ar ?? '' : '';
      nameEn.value = level ? level.name_en ?? '' : '';
      order.value = level ? String(level.order ?? '') : '';
      hideBanner(banner);
      render();
      dialog.showModal();
      nameAr.focus();
    },
    rerender: render,
  };
}
