// The add/edit dialog for an iOS package (#56). The Product ID is typed once
// on create and shown read-only on edit (immutable upstream). Subjects are
// chosen with checkboxes grouped by level, then term. "Available until" is a
// calendar date sent as 23:59:59 Africa/Cairo, like a subject's access end;
// an empty date means no end (null). Prices are never entered here: they
// live in App Store Connect only.

import { clear, h } from './dom.js';
import { formatNumber, t } from './i18n.js';
import { cairoDateInputValue, cairoEndOfDayISO } from './subject-dialog.js';
import { createCooldown, hideBanner, showError } from './ui.js';
import { guardDialog } from './unsaved.js';

export const IAP_TITLE_MAX = 80;
export const IAP_DESC_MAX = 1000;
export const IAP_SUBJECTS_MAX = 100;
export const PRODUCT_ID_PATTERN = /^com\.wael\.app\.pkg\.[a-z0-9_.]+$/;

const cleanTitle = (raw) => String(raw ?? '').trim().replace(/[\r\n]/g, '');
const cleanDesc = (raw) => String(raw ?? '').trim().replace(/[\r\n]/g, ' ');
const runes = (s) => [...s].length;

/**
 * Validates the dialog values and builds the request body. mode is 'create'
 * or 'edit'. Returns { error: i18nKey } or { body }. On edit product_id is
 * never sent, and an empty date clears the end (null).
 */
export function readPackageForm(values, mode) {
  const body = {};
  if (mode === 'create') {
    const productId = String(values.productId ?? '').trim();
    if (!PRODUCT_ID_PATTERN.test(productId)) return { error: 'iap.err.productId' };
    body.product_id = productId;
  }
  const titleAr = cleanTitle(values.titleAr);
  const titleEn = cleanTitle(values.titleEn);
  for (const title of [titleAr, titleEn]) {
    if (runes(title) < 1 || runes(title) > IAP_TITLE_MAX) return { error: 'iap.err.titles' };
  }
  body.title_ar = titleAr;
  body.title_en = titleEn;
  const descAr = cleanDesc(values.descAr);
  const descEn = cleanDesc(values.descEn);
  if (runes(descAr) > IAP_DESC_MAX || runes(descEn) > IAP_DESC_MAX) return { error: 'iap.err.description' };
  body.description_ar = descAr;
  body.description_en = descEn;
  const subjectIds = [...new Set(Array.isArray(values.subjectIds) ? values.subjectIds : [])];
  if (subjectIds.length > IAP_SUBJECTS_MAX) return { error: 'iap.err.subjects' };
  body.subject_ids = subjectIds;
  const date = String(values.availableUntil ?? '').trim();
  if (date === '') {
    body.available_until = null;
  } else {
    const iso = cairoEndOfDayISO(date);
    if (!iso) return { error: 'iap.err.date' };
    body.available_until = iso;
  }
  const position = String(values.position ?? '').trim();
  if (position === '') body.position = 0;
  else if (/^\d{1,6}$/.test(position)) body.position = Number(position);
  else return { error: 'iap.err.position' };
  body.published = values.published === true;
  return { body };
}

const TERM_ORDER = ['first', 'second', ''];

/**
 * Groups subjects by level (in the levels' order), then by term. Subjects of
 * an unknown level go last under their level id.
 */
export function groupSubjects(levels, subjects) {
  const order = new Map((Array.isArray(levels) ? levels : []).map((l, i) => [l.key, { level: l, index: i }]));
  const byLevel = new Map();
  for (const subject of Array.isArray(subjects) ? subjects : []) {
    if (!subject || typeof subject.id !== 'string') continue;
    const key = subject.level_id ?? '';
    if (!byLevel.has(key)) byLevel.set(key, []);
    byLevel.get(key).push(subject);
  }
  const rank = (key) => order.get(key)?.index ?? Number.MAX_SAFE_INTEGER;
  return [...byLevel.keys()].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b)).map((key) => {
    const level = order.get(key)?.level ?? null;
    const terms = TERM_ORDER.map((term) => ({
      term,
      subjects: byLevel.get(key)
        .filter((s) => (s.term === 'first' || s.term === 'second' ? s.term : '') === term)
        .sort((a, b) => (a.order ?? 0) - (b.order ?? 0) || String(a.title_ar).localeCompare(String(b.title_ar))),
    })).filter((group) => group.subjects.length > 0);
    return { key, name: level?.name_ar || key, terms };
  });
}

export function createIAPDialog({ api, doc = document, onDone }) {
  const host = doc.getElementById('iap-dialogs');
  const title = h('h2', {});
  const banner = h('div', { class: 'banner', attrs: { role: 'alert' } });
  banner.hidden = true;
  const productId = h('input', { class: 'ltr', attrs: { type: 'text', maxlength: '200', autocomplete: 'off', spellcheck: 'false' } });
  const productHint = h('span', { class: 'muted small' });
  const titleAr = h('input', { attrs: { type: 'text', maxlength: String(IAP_TITLE_MAX), autocomplete: 'off' } });
  const titleEn = h('input', { class: 'ltr', attrs: { type: 'text', maxlength: String(IAP_TITLE_MAX), autocomplete: 'off' } });
  const descAr = h('textarea', { attrs: { rows: '3', maxlength: String(IAP_DESC_MAX) } });
  const descEn = h('textarea', { class: 'ltr', attrs: { rows: '3', maxlength: String(IAP_DESC_MAX) } });
  const subjectsBox = h('div', { class: 'iap-subjects' });
  const subjectsCount = h('span', { class: 'muted small' });
  const until = h('input', { class: 'ltr', attrs: { type: 'date' } });
  const position = h('input', { class: 'ltr', attrs: { type: 'number', min: '0', step: '1', inputmode: 'numeric' } });
  const published = h('input', { attrs: { type: 'checkbox' } });
  const labels = {};
  const label = (key) => {
    labels[key] = h('label', { text: t(key) });
    return labels[key];
  };
  const publishedText = h('span', {});
  const cancel = h('button', { class: 'btn secondary', text: t('common.cancel'), attrs: { type: 'button' } });
  const save = h('button', { class: 'btn primary', attrs: { type: 'submit' } });
  const form = h(
    'form',
    { attrs: { novalidate: '' } },
    title,
    h('div', { class: 'field' }, label('iap.field.productId'), productId, productHint),
    h('div', { class: 'field' }, label('iap.field.titleAr'), titleAr),
    h('div', { class: 'field' }, label('iap.field.titleEn'), titleEn),
    h('div', { class: 'field' }, label('iap.field.descAr'), descAr),
    h('div', { class: 'field' }, label('iap.field.descEn'), descEn),
    h('div', { class: 'field' }, label('iap.field.subjects'), subjectsBox, subjectsCount),
    h('div', { class: 'field' }, label('iap.field.availableUntil'), until),
    h('div', { class: 'field' }, label('iap.field.position'), position),
    h('div', { class: 'field' }, h('label', { class: 'checkbox-label' }, published, publishedText)),
    banner,
    h('div', { class: 'dialog-actions' }, cancel, save),
  );
  const dialog = h('dialog', { class: 'dialog dialog-wide' }, form);
  host.append(dialog);

  let current = null; // { mode: 'create' } or { mode: 'edit', pkg }
  let levels = [];
  let subjects = [];
  let boxes = []; // [{ id, input }]
  let busy = false;
  const cooldown = createCooldown();
  cooldown.subscribe((left) => {
    if (left === 0) render();
  });

  const selectedIds = () => boxes.filter((b) => b.input.checked).map((b) => b.id);
  const guard = guardDialog({
    id: 'iap-dialog',
    doc,
    dialog,
    cancel,
    snapshot: () => JSON.stringify([productId.value, titleAr.value, titleEn.value, descAr.value, descEn.value, selectedIds(), until.value, position.value, published.checked]),
    isBusy: () => busy,
  });

  function renderCount() {
    subjectsCount.textContent = t('iap.field.subjectsCount', {
      n: formatNumber(selectedIds().length), max: formatNumber(IAP_SUBJECTS_MAX),
    });
  }

  function buildSubjects(chosen) {
    clear(subjectsBox);
    boxes = [];
    const groups = groupSubjects(levels, subjects);
    if (groups.length === 0) {
      subjectsBox.append(h('p', { class: 'muted small', text: t('iap.noSubjects') }));
    }
    for (const group of groups) {
      const section = h('div', { class: 'iap-subject-group' }, h('h3', { class: 'strong', text: group.name }));
      for (const termGroup of group.terms) {
        section.append(h('h4', { text: termGroup.term ? t(`catalog.term.${termGroup.term}`) : t('iap.term.none') }));
        for (const subject of termGroup.subjects) {
          const input = h('input', { attrs: { type: 'checkbox', value: subject.id } });
          input.checked = chosen.has(subject.id);
          input.addEventListener('change', renderCount);
          boxes.push({ id: subject.id, input });
          const name = subject.title_ar || subject.id;
          section.append(h('label', { class: 'checkbox-label' }, input,
            h('span', { text: subject.published === false ? `${name} (${t('iap.subjectDraft')})` : name })));
        }
      }
      subjectsBox.append(section);
    }
    renderCount();
  }

  function render() {
    const editing = current?.mode === 'edit';
    title.textContent = t(editing ? 'iap.editTitle' : 'iap.createTitle');
    for (const [key, el] of Object.entries(labels)) el.textContent = t(key);
    productHint.textContent = t('iap.field.productIdHint');
    publishedText.textContent = t('iap.field.published');
    cancel.textContent = t('common.cancel');
    save.textContent = busy ? t('dialog.working') : t(editing ? 'iap.save' : 'iap.create');
    save.disabled = busy || cooldown.active;
    cancel.disabled = busy;
    productId.readOnly = editing;
    productId.disabled = busy;
    for (const input of [titleAr, titleEn, descAr, descEn, until, position, published, ...boxes.map((b) => b.input)]) input.disabled = busy;
    renderCount();
  }

  function values() {
    return {
      productId: productId.value,
      titleAr: titleAr.value,
      titleEn: titleEn.value,
      descAr: descAr.value,
      descEn: descEn.value,
      subjectIds: selectedIds(),
      availableUntil: until.value,
      position: position.value,
      published: published.checked === true,
    };
  }

  async function submit() {
    if (busy || cooldown.active || !current) return;
    const read = readPackageForm(values(), current.mode);
    if (read.error) {
      clear(banner);
      banner.append(h('span', { class: 'banner-text', text: t(read.error) }));
      banner.hidden = false;
      return;
    }
    const editing = current.mode === 'edit';
    if (editing) read.body.id = current.pkg.id;
    busy = true;
    hideBanner(banner);
    render();
    const res = await api.post(editing ? '/api/iap/packages/update' : '/api/iap/packages/create', read.body);
    busy = false;
    if (res.ok) {
      dialog.close();
      onDone(editing ? 'iap.toast.updated' : 'iap.toast.created');
      return;
    }
    if (res.kind === 'unauthorized') {
      dialog.close();
      return;
    }
    cooldown.arm(res);
    showError(banner, res, submit, cooldown);
    render();
  }

  form.addEventListener('submit', (event) => {
    event.preventDefault();
    submit();
  });
  dialog.addEventListener('close', () => {
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    /** spec: { mode: 'create' } | { mode: 'edit', pkg }. */
    open(spec, allLevels, allSubjects) {
      current = spec;
      levels = Array.isArray(allLevels) ? allLevels : [];
      subjects = Array.isArray(allSubjects) ? allSubjects : [];
      busy = false;
      cooldown.stop();
      const pkg = spec.mode === 'edit' ? spec.pkg : null;
      productId.value = pkg?.product_id ?? '';
      titleAr.value = pkg?.title_ar ?? '';
      titleEn.value = pkg?.title_en ?? '';
      descAr.value = pkg?.description_ar ?? '';
      descEn.value = pkg?.description_en ?? '';
      until.value = pkg?.available_until ? cairoDateInputValue(pkg.available_until) : '';
      position.value = pkg && Number.isInteger(pkg.position) ? String(pkg.position) : '';
      published.checked = pkg?.published === true;
      buildSubjects(new Set(Array.isArray(pkg?.subject_ids) ? pkg.subject_ids : []));
      hideBanner(banner);
      render();
      guard.arm();
      dialog.showModal();
      (pkg ? titleAr : productId).focus();
    },
    rerender() {
      if (!current) return;
      const chosen = new Set(selectedIds());
      buildSubjects(chosen);
      render();
    },
    close() {
      if (dialog.open) dialog.close();
    },
    get isOpen() {
      return dialog.open;
    },
  };
}
