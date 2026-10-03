// Videos view: the ordered videos of one subject with title, duration,
// YouTube id as text and a plain watch link. Reorder uses up/down buttons
// plus one save that sends the full id list; deleting the last video of a
// published subject offers the force flow.

import { clear, h } from './dom.js';
import { formatDuration } from './video-dialog.js';
import { t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { hideBanner, hideToast, showError, toast } from './ui.js';

// The only third-party URL on the page: a plain link, never fetched,
// embedded or previewed (static.test.mjs allows exactly this prefix).
export const WATCH_BASE = 'https://www.youtube.com/watch?v=';
const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/;

export function watchURL(id) {
  if (!YOUTUBE_ID.test(String(id ?? ''))) return null;
  return `${WATCH_BASE}${id}`;
}

export function mountVideos({ api, doc = document, dialog, confirm, onChanged }) {
  const toolbar = doc.getElementById('catalog-toolbar');
  const banner = doc.getElementById('catalog-banner');
  const content = doc.getElementById('catalog-content');
  const pagerEl = doc.getElementById('catalog-pager');

  const state = { subject: null, items: [], order: [], loaded: false, error: null, saving: false };
  let seq = 0;

  const dirty = () => state.order.join(',') !== state.items.map((v) => v.id).join(',');

  function byId(id) {
    return state.items.find((v) => v.id === id) ?? null;
  }

  function row(video, index) {
    const actions = h('td', { class: 'actions' });
    const up = h('button', {
      class: 'btn small secondary', text: t('catalog.reorder.up'), attrs: { type: 'button' },
      on: { click: () => move(index, -1) },
    });
    const down = h('button', {
      class: 'btn small secondary', text: t('catalog.reorder.down'), attrs: { type: 'button' },
      on: { click: () => move(index, 1) },
    });
    up.disabled = index === 0;
    down.disabled = index === state.order.length - 1;
    actions.append(up, down);
    actions.append(
      h('button', {
        class: 'btn small secondary', text: t('catalog.edit'), attrs: { type: 'button' },
        on: { click: () => dialog.open({ mode: 'edit', video }) },
      }),
    );
    actions.append(
      h('button', {
        class: 'btn small danger-outline', text: t('catalog.delete'), attrs: { type: 'button' },
        on: { click: () => askDelete(video) },
      }),
    );
    const link = watchURL(video.youtube_video_id);
    const idCell = h('td', { class: 'ltr' },
      h('div', { class: 'id', text: video.youtube_video_id || '—' }),
      link ? h('a', { class: 'link', text: t('catalog.watch'), attrs: { href: link, target: '_blank', rel: 'noopener noreferrer' } }) : null,
    );
    return h(
      'tr',
      {},
      h('td', { class: 'ltr', text: String(index + 1) }),
      h('td', { text: video.title_ar || '—' }),
      h('td', { class: 'ltr', text: formatDuration(video.duration_seconds) }),
      idCell,
      actions,
    );
  }

  function move(index, delta) {
    const next = index + delta;
    if (next < 0 || next >= state.order.length) return;
    const ids = [...state.order];
    [ids[index], ids[next]] = [ids[next], ids[index]];
    state.order = ids;
    paint();
  }

  async function saveOrder(button) {
    if (state.saving || !dirty()) return;
    state.saving = true;
    paint();
    const res = await api.post('/api/videos/reorder', { subject_id: state.subject.id, video_ids: state.order });
    state.saving = false;
    if (!isSignedIn()) return;
    if (res.ok) {
      toast(doc, t('catalog.toast.reordered'));
      await load();
      return;
    }
    if (res.kind !== 'unauthorized') {
      state.error = res;
      paint();
    }
  }

  function afterDelete() {
    toast(doc, t('catalog.toast.deleted'));
    load();
    if (onChanged) onChanged();
  }

  function askDelete(video) {
    confirm.open({
      titleKey: 'catalog.deleteVideoTitle',
      noteText: t('catalog.deleteVideoNote'),
      confirmKey: 'catalog.confirmDelete',
      tone: 'danger',
      path: '/api/videos/delete',
      body: { id: video.id },
      onDone: afterDelete,
      // Deleting the last video of a published subject needs force, which
      // also unpublishes the subject: offer it as a second confirm.
      onCode: (res) => {
        if (res.code !== 'last_video_of_published_subject') return false;
        confirm.open({
          titleKey: 'catalog.deleteVideoTitle',
          noteText: t('catalog.forceDeleteNote'),
          confirmKey: 'catalog.confirmDelete',
          tone: 'danger',
          path: '/api/videos/delete',
          body: { id: video.id, force: true },
          onDone: afterDelete,
        });
        return true;
      },
    });
  }

  function render() {
    clear(toolbar);
    pagerEl.hidden = true;
    clear(content);
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      content.append(h('p', { class: 'muted', text: t('common.loading') }));
      return;
    }
    if (state.items.length === 0) {
      content.append(h('p', { class: 'muted', text: t('common.empty') }));
    } else {
      const tbody = h('tbody', {});
      for (let i = 0; i < state.order.length; i++) {
        const video = byId(state.order[i]);
        if (video) tbody.append(row(video, i));
      }
      content.append(h('div', { class: 'table-wrap' }, h('table', {}, tbody)));
    }
    const bar = h('div', { class: 'toolbar' });
    bar.append(
      h('button', {
        class: 'btn primary', text: t('catalog.addVideo'), attrs: { type: 'button' },
        on: { click: () => dialog.open({ mode: 'create', subjectId: state.subject.id }) },
      }),
    );
    if (state.order.length > 1) {
      const save = h('button', {
        class: 'btn secondary', text: state.saving ? t('dialog.working') : t('catalog.reorder.save'),
        attrs: { type: 'button' },
        on: { click: (event) => saveOrder(event.target) },
      });
      save.disabled = state.saving || !dirty();
      bar.append(save);
      if (dirty()) bar.append(h('span', { class: 'muted small', text: t('catalog.reorder.dirty') }));
    }
    content.append(bar);
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
    const subjectId = state.subject?.id;
    if (!subjectId) return;
    const res = await api.get('/api/videos', { subject_id: subjectId });
    if (mine !== seq || !isSignedIn()) return;
    if (!res.ok) {
      state.error = res;
      paint();
      return;
    }
    const data = res.data ?? {};
    state.items = Array.isArray(data.videos) ? data.videos : [];
    state.order = state.items.map((v) => v.id);
    state.loaded = true;
    paint();
  }

  return {
    open(subject) {
      state.subject = subject;
      state.items = [];
      state.order = [];
      state.loaded = false;
      load();
    },
    reload: load,
    rerender: paint,
    reset() {
      seq += 1;
      hideToast(doc);
      Object.assign(state, { subject: null, items: [], order: [], loaded: false, error: null, saving: false });
    },
    // Exposed for tests.
    get order() {
      return [...state.order];
    },
    get isDirty() {
      return dirty();
    },
  };
}
