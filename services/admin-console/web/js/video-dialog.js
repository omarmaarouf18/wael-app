// The add/edit dialog for a video: Arabic title, YouTube URL or id, and an
// optional duration as mm:ss (converted to seconds for the server).

import { clear, h } from './dom.js';
import { t } from './i18n.js';
import { hideBanner, showError } from './ui.js';

export const TITLE_MAX = 200;

/**
 * Parses an mm:ss duration. Minutes are 1-5 digits, seconds 00-59.
 * Empty means omitted. Returns { ok, seconds }.
 */
export function parseDurationMMSS(raw) {
  const text = String(raw ?? '').trim();
  if (text === '') return { ok: true, seconds: undefined };
  const m = /^(\d{1,5}):([0-5]\d)$/.exec(text);
  if (!m) return { ok: false };
  return { ok: true, seconds: Number(m[1]) * 60 + Number(m[2]) };
}

/** Formats seconds as m:ss for display and for prefilling the dialog. */
export function formatDuration(seconds) {
  const n = Number(seconds ?? 0);
  if (!Number.isFinite(n) || n < 0) return '0:00';
  const whole = Math.floor(n);
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
}

export function validVideoTitle(raw) {
  const value = String(raw ?? '').trim().replace(/[\r\n]/g, '');
  const length = [...value].length;
  if (length < 1 || length > TITLE_MAX) return { ok: false, value };
  return { ok: true, value };
}

export function createVideoDialog({ api, doc = document, onDone }) {
  const host = doc.getElementById('catalog-dialogs');
  const title = h('h2', {});
  const banner = h('div', { class: 'banner', attrs: { role: 'alert' } });
  banner.hidden = true;
  const titleAr = h('input', { attrs: { type: 'text', maxlength: '200', autocomplete: 'off' } });
  const youtube = h('input', { class: 'ltr', attrs: { type: 'text', autocomplete: 'off', placeholder: 'youtube.com/watch?v=…' } });
  const duration = h('input', { class: 'ltr', attrs: { type: 'text', autocomplete: 'off', placeholder: '12:30' } });
  const cancel = h('button', { class: 'btn secondary', text: t('common.cancel'), attrs: { type: 'button' } });
  const save = h('button', { class: 'btn primary', attrs: { type: 'submit' } });
  const form = h(
    'form',
    { attrs: { novalidate: '' } },
    title,
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.titleAr') }), titleAr),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.youtube') }), youtube),
    h('div', { class: 'field' }, h('label', { text: t('catalog.field.duration') }), duration),
    banner,
    h('div', { class: 'dialog-actions' }, cancel, save),
  );
  const dialog = h('dialog', { class: 'dialog' }, form);
  host.append(dialog);

  let current = null; // { mode: 'create', subjectId } or { mode: 'edit', video }
  let busy = false;

  function render() {
    title.textContent = t(current?.mode === 'edit' ? 'catalog.videoEditTitle' : 'catalog.videoCreateTitle');
    save.textContent = busy ? t('dialog.working') : t(current?.mode === 'edit' ? 'catalog.save' : 'catalog.create');
    save.disabled = busy;
    cancel.disabled = busy;
    for (const input of [titleAr, youtube, duration]) input.disabled = busy;
  }

  function readBody() {
    const titleCheck = validVideoTitle(titleAr.value);
    if (!titleCheck.ok) return { error: 'catalog.required' };
    if (youtube.value.trim() === '') return { error: 'catalog.required' };
    const durationCheck = parseDurationMMSS(duration.value);
    if (!durationCheck.ok) return { error: 'catalog.badDuration' };
    const body = { title_ar: titleCheck.value, youtube: youtube.value.trim() };
    if (durationCheck.seconds !== undefined) body.duration_seconds = durationCheck.seconds;
    return { body };
  }

  async function submit() {
    if (busy || !current) return;
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
    let path;
    if (current.mode === 'edit') {
      path = '/api/videos/update';
      read.body.id = current.video.id;
    } else {
      path = '/api/videos/create';
      read.body.subject_id = current.subjectId;
    }
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
    showError(banner, res, submit);
    render();
  }

  form.addEventListener('submit', (event) => {
    event.preventDefault();
    submit();
  });
  cancel.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => {
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    open(spec) {
      current = spec;
      busy = false;
      const video = spec.mode === 'edit' ? spec.video : null;
      titleAr.value = video?.title_ar ?? '';
      youtube.value = '';
      duration.value = video && (video.duration_seconds ?? 0) > 0 ? formatDuration(video.duration_seconds) : '';
      hideBanner(banner);
      render();
      dialog.showModal();
      titleAr.focus();
    },
    rerender: render,
  };
}
