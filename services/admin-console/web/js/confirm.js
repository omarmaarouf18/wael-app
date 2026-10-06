// A generic confirmation dialog for the catalog tab (level delete, video
// delete, unpublish, force delete). The dialog element is built once and
// reused; every mutation disables its buttons while in flight.

import { clear, h } from './dom.js';
import { t } from './i18n.js';
import { createCooldown, hideBanner, keepOpenWhile, showError } from './ui.js';

export function createConfirm({ api, doc = document }) {
  const host = doc.getElementById('catalog-dialogs');
  const title = h('h2', {});
  const note = h('p', { class: 'callout' });
  const banner = h('div', { class: 'banner', attrs: { role: 'alert' } });
  banner.hidden = true;
  const cancel = h('button', { class: 'btn secondary', text: t('common.cancel'), attrs: { type: 'button' } });
  const confirm = h('button', { class: 'btn danger', attrs: { type: 'submit' } });
  const actions = h('div', { class: 'dialog-actions' }, cancel, confirm);
  const form = h('form', { attrs: { novalidate: '' } }, title, note, banner, actions);
  const dialog = h('dialog', { class: 'dialog' }, form);
  host.append(dialog);

  let current = null; // { titleKey, noteText, confirmKey, tone, path, body, onDone, onCode }
  let busy = false;
  // A 429 with Retry-After keeps the confirm button off until the wait ends.
  const cooldown = createCooldown();
  cooldown.subscribe((left) => {
    if (left === 0) render();
  });

  function render() {
    if (!current) return;
    title.textContent = t(current.titleKey);
    note.textContent = current.noteText;
    confirm.className = `btn ${current.tone}`;
    confirm.textContent = busy ? t('dialog.working') : t(current.confirmKey);
    confirm.disabled = busy || cooldown.active;
    cancel.disabled = busy;
  }

  async function submit() {
    if (!current || busy || cooldown.active) return;
    busy = true;
    hideBanner(banner);
    render();
    const res = await api.post(current.path, current.body);
    busy = false;
    if (res.ok) {
      const done = current.onDone;
      dialog.close();
      if (done) done();
      return;
    }
    if (res.kind === 'unauthorized') {
      dialog.close();
      return;
    }
    // onCode lets a flow intercept a specific server code (e.g. the video
    // force-delete gate) instead of showing it as an error. A handler that
    // returns true managed the dialog itself (it may have opened a
    // replacement); submit does nothing further.
    if (res.code && current.onCode && current.onCode(res)) {
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
  cancel.addEventListener('click', () => dialog.close());
  keepOpenWhile(dialog, () => busy);
  dialog.addEventListener('close', () => {
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    open(spec) {
      // A handler may open a replacement while this dialog is still open
      // (the video force flow): close first so showModal starts closed.
      if (dialog.open) dialog.close();
      current = { tone: 'danger', ...spec };
      busy = false;
      cooldown.stop();
      hideBanner(banner);
      render();
      dialog.showModal();
      cancel.focus();
    },
    /** Redraws in the current language (called when the language changes). */
    rerender: render,
    close() {
      if (dialog.open) dialog.close();
    },
    get isOpen() {
      return dialog.open;
    },
  };
}
