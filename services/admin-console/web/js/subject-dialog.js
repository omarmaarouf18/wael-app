// The add/edit dialog for a subject, plus the Africa/Cairo date helpers.
// The access end is entered as a calendar date and sent as 23:59:59
// Africa/Cairo converted to RFC3339 UTC; it is shown back in Cairo time.

import { clear, h } from './dom.js';
import { getLang, t } from './i18n.js';
import { createCooldown, hideBanner, showError } from './ui.js';
import { guardDialog } from './unsaved.js';

export const TITLE_MAX = 200;
export const DESC_MAX = 5000;
const CAIRO_TZ = 'Africa/Cairo';

/** Minutes Cairo is ahead of UTC at the given instant (handles DST). */
export function cairoOffsetMinutes(utcMs) {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat('en-US', {
      timeZone: CAIRO_TZ, hour12: false,
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    }).formatToParts(new Date(utcMs)).map((p) => [p.type, p.value]),
  );
  const asUTC = Date.UTC(
    Number(parts.year), Number(parts.month) - 1, Number(parts.day),
    Number(parts.hour) % 24, Number(parts.minute), Number(parts.second),
  );
  return Math.round((asUTC - utcMs) / 60000);
}

/**
 * Converts a YYYY-MM-DD calendar date to 23:59:59 in Cairo as an RFC3339 UTC
 * string. Returns null for a malformed date.
 */
export function cairoEndOfDayISO(dateStr) {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(dateStr ?? '').trim());
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]);
  const day = Number(m[3]);
  if (month < 1 || month > 12 || day < 1 || day > 31) return null;
  const probe = new Date(Date.UTC(year, month - 1, day));
  if (probe.getUTCFullYear() !== year || probe.getUTCMonth() !== month - 1 || probe.getUTCDate() !== day) return null;
  let guess = Date.UTC(year, month - 1, day, 21, 59, 59);
  for (let i = 0; i < 3; i++) {
    guess = Date.UTC(year, month - 1, day, 23, 59, 59) - cairoOffsetMinutes(guess) * 60000;
  }
  return new Date(guess).toISOString();
}

/** Renders an RFC3339 instant as a YYYY-MM-DD Cairo calendar date. */
export function cairoDateInputValue(iso) {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return '';
  return new Intl.DateTimeFormat('en-CA', { timeZone: CAIRO_TZ, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(ms));
}

/** Renders an RFC3339 instant as a Cairo date in the page language. */
export function formatCairoDate(iso) {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return '';
  return new Intl.DateTimeFormat(getLang() === 'ar' ? 'ar-EG' : 'en-GB', {
    timeZone: CAIRO_TZ, dateStyle: 'medium',
  }).format(new Date(ms));
}

export function validTitle(raw) {
  const value = String(raw ?? '').trim().replace(/[\r\n]/g, '');
  const length = [...value].length;
  if (length < 1) return { ok: false, value };
  if (length > TITLE_MAX) return { ok: false, value };
  return { ok: true, value };
}

export function validDescription(raw) {
  const value = String(raw ?? '').trim().replace(/[\r\n]/g, ' ');
  if ([...value].length > DESC_MAX) return { ok: false, value };
  return { ok: true, value };
}

export function parsePrice(raw) {
  const text = String(raw ?? '').trim();
  if (text === '') return { ok: true, value: undefined };
  if (!/^\d+$/.test(text)) return { ok: false };
  return { ok: true, value: Number(text) };
}

export function createSubjectDialog({ api, doc = document, onDone }) {
  const host = doc.getElementById('catalog-dialogs');
  const title = h('h2', {});
  const banner = h('div', { class: 'banner', attrs: { role: 'alert' } });
  banner.hidden = true;
  const level = h('select', {});
  const titleAr = h('input', { attrs: { type: 'text', maxlength: '200', autocomplete: 'off' } });
  const titleEn = h('input', { class: 'ltr', attrs: { type: 'text', maxlength: '200', autocomplete: 'off' } });
  const descAr = h('textarea', { attrs: { rows: '3', maxlength: '5000' } });
  const descEn = h('textarea', { class: 'ltr', attrs: { rows: '3', maxlength: '5000' } });
  const termRow = h('div', { class: 'field' });
  const term = h('select', {});
  const price = h('input', { class: 'ltr', attrs: { type: 'number', min: '0', step: '1', inputmode: 'numeric' } });
  const expires = h('input', { class: 'ltr', attrs: { type: 'date' } });
  const cancel = h('button', { class: 'btn secondary', text: t('common.cancel'), attrs: { type: 'button' } });
  const save = h('button', { class: 'btn primary', attrs: { type: 'submit' } });
  const form = h(
    'form',
    { attrs: { novalidate: '' } },
    title,
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.level') }), level),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.titleAr') }), titleAr),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.titleEn') }), titleEn),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.descAr') }), descAr),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.descEn') }), descEn),
    termRow,
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.price') }), price),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.expires') }), expires),
    banner,
    h('div', { class: 'dialog-actions' }, cancel, save),
  );
  const dialog = h('dialog', { class: 'dialog' }, form);
  host.append(dialog);

  let current = null; // { mode: 'create', levelKey } or { mode: 'edit', subject }
  let levels = [];
  let busy = false;
  // A 429 with Retry-After keeps the save button off until the wait ends.
  const cooldown = createCooldown();
  cooldown.subscribe((left) => {
    if (left === 0) render();
  });

  const guard = guardDialog({
    id: 'subject-dialog',
    doc,
    dialog,
    cancel,
    snapshot: () => JSON.stringify([level.value, titleAr.value, titleEn.value, descAr.value, descEn.value, term.value, price.value, expires.value]),
    isBusy: () => busy,
  });

  function fillTermRow() {
    const keep = term.value;
    clear(termRow);
    termRow.append(h('label', { text: t('catalog.field.term') }), term);
    clear(term);
    for (const value of ['first', 'second']) {
      const option = h('option', { text: t(`catalog.term.${value}`), attrs: { value } });
      term.append(option);
    }
    if (keep === 'first' || keep === 'second') term.value = keep;
  }

  function selectedLevel() {
    return levels.find((l) => l.key === level.value) ?? null;
  }

  function syncTermRow() {
    const vocational = selectedLevel()?.study_type === 'vocational';
    termRow.hidden = vocational;
  }

  function render() {
    fillTermRow();
    syncTermRow();
    title.textContent = t(current?.mode === 'edit' ? 'catalog.subjectEditTitle' : 'catalog.subjectCreateTitle');
    save.textContent = busy ? t('dialog.working') : t(current?.mode === 'edit' ? 'catalog.save' : 'catalog.create');
    save.disabled = busy || cooldown.active;
    cancel.disabled = busy;
    for (const input of [level, titleAr, titleEn, descAr, descEn, term, price, expires]) input.disabled = busy;
  }

  function readBody() {
    const titleCheck = validTitle(titleAr.value);
    if (!titleCheck.ok) return { error: 'catalog.required' };
    if (titleEn.value.trim() !== '' && !validTitle(titleEn.value).ok) return { error: 'catalog.tooLong' };
    const descArCheck = validDescription(descAr.value);
    const descEnCheck = validDescription(descEn.value);
    if (!descArCheck.ok || !descEnCheck.ok) return { error: 'catalog.tooLong' };
    const priceCheck = parsePrice(price.value);
    if (!priceCheck.ok) return { error: 'catalog.badPrice' };
    const iso = cairoEndOfDayISO(expires.value);
    if (!iso || new Date(iso).getTime() <= Date.now()) return { error: 'catalog.badDate' };
    const lvl = selectedLevel();
    if (!lvl) return { error: 'catalog.required' };
    const body = {
      level_id: lvl.key,
      title_ar: titleCheck.value,
      access_expires_at: iso,
    };
    body.title_en = titleEn.value.trim() === '' ? '' : validTitle(titleEn.value).value;
    body.description_ar = descArCheck.value;
    body.description_en = descEnCheck.value;
    if (!termRow.hidden) body.term = term.value;
    else if (current?.mode === 'create') body.term = '';
    if (priceCheck.value !== undefined) body.price = priceCheck.value;
    else if (current?.mode === 'create') body.price = 0;
    return { body };
  }

  async function submit() {
    if (busy || cooldown.active || !current) return;
    const read = readBody();
    if (read.error) {
      clear(banner);
      banner.append(h('span', { class: 'banner-text', text: t(read.error) }));
      banner.hidden = false;
      return;
    }
    busy = true;
    hideBanner(banner);
    render();
    const path = current.mode === 'edit' ? '/api/subjects/update' : '/api/subjects/create';
    if (current.mode === 'edit') read.body.id = current.subject.id;
    const res = await api.post(path, read.body);
    busy = false;
    if (res.ok) {
      const done = onDone;
      const created = current.mode !== 'edit';
      dialog.close();
      done(created ? 'catalog.toast.created' : 'catalog.toast.updated');
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
  level.addEventListener('change', syncTermRow);
  dialog.addEventListener('close', () => {
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    open(spec, allLevels) {
      current = spec;
      levels = Array.isArray(allLevels) ? allLevels : [];
      busy = false;
      cooldown.stop();
      clear(level);
      for (const l of levels) {
        level.append(h('option', { text: l.name_ar ?? l.key, attrs: { value: l.key } }));
      }
      const subject = spec.mode === 'edit' ? spec.subject : null;
      const initialKey = subject ? subject.level_id : spec.levelKey;
      level.value = levels.some((l) => l.key === initialKey) ? initialKey : (levels[0]?.key ?? '');
      titleAr.value = subject?.title_ar ?? '';
      titleEn.value = subject?.title_en ?? '';
      descAr.value = subject?.description_ar ?? '';
      descEn.value = subject?.description_en ?? '';
      price.value = subject && Number.isInteger(subject.price) ? String(subject.price) : '';
      expires.value = subject?.access_expires_at ? cairoDateInputValue(subject.access_expires_at) : '';
      hideBanner(banner);
      render();
      // render() rebuilds the term options, so apply the value afterwards.
      term.value = subject?.term === 'second' ? 'second' : 'first';
      syncTermRow();
      guard.arm();
      dialog.showModal();
      titleAr.focus();
    },
    rerender: render,
    close() {
      if (dialog.open) dialog.close();
    },
  };
}
