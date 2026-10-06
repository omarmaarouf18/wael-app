// Unsaved-changes guard. Forms and lists that hold edits the server has not
// seen register a "dirty" check here. Leaving the page (beforeunload) and
// leaving a view inside the console (tab switch, breadcrumb) then ask first.
// Nothing is stored: the registry lives in memory and is emptied when a form
// closes or the session ends.

import { h } from './dom.js';
import { t } from './i18n.js';

const sources = new Map(); // id -> () => boolean

/** Registers a dirty check; returns a function that removes it. */
export function trackDirty(id, isDirty) {
  sources.set(id, isDirty);
  return () => {
    if (sources.get(id) === isDirty) sources.delete(id);
  };
}

export function hasUnsavedChanges() {
  for (const isDirty of sources.values()) {
    if (isDirty()) return true;
  }
  return false;
}

/** Asks the browser to confirm leaving the page while something is unsaved. */
export function installUnloadGuard(win) {
  win.addEventListener('beforeunload', (event) => {
    if (!hasUnsavedChanges()) return;
    event.preventDefault();
    // Some browsers only prompt when returnValue is set.
    event.returnValue = '';
  });
}

let discardDialog = null;
let pending = null; // { resolve, promise } while the question is open

function buildDiscardDialog(doc) {
  const title = h('h2', { text: t('unsaved.title') });
  const note = h('p', { class: 'callout', text: t('unsaved.note') });
  const stay = h('button', { class: 'btn secondary', text: t('unsaved.stay'), attrs: { type: 'button' } });
  const discard = h('button', { class: 'btn danger', text: t('unsaved.discard'), attrs: { type: 'button' } });
  const form = h('form', { attrs: { method: 'dialog', novalidate: '' } }, title, note, h('div', { class: 'dialog-actions' }, stay, discard));
  const dialog = h('dialog', { class: 'dialog' }, form);
  doc.getElementById('catalog-dialogs').append(dialog);
  const state = { dialog, title, note, stay, discard, answer: false };
  form.addEventListener('submit', (event) => event.preventDefault());
  stay.addEventListener('click', () => dialog.close());
  discard.addEventListener('click', () => {
    state.answer = true;
    dialog.close();
  });
  // Esc and any other close mean "stay".
  dialog.addEventListener('close', () => {
    const waiting = pending;
    pending = null;
    if (waiting) waiting.resolve(state.answer);
    state.answer = false;
  });
  return state;
}

/**
 * Resolves true when the admin chooses to discard the unsaved changes, false
 * when they stay. A second call while the question is open shares its answer.
 */
export function confirmDiscard(doc) {
  if (pending) return pending.promise;
  if (!discardDialog || discardDialog.dialog.ownerDocument !== doc) discardDialog = buildDiscardDialog(doc);
  discardDialog.title.textContent = t('unsaved.title');
  discardDialog.note.textContent = t('unsaved.note');
  discardDialog.stay.textContent = t('unsaved.stay');
  discardDialog.discard.textContent = t('unsaved.discard');
  discardDialog.answer = false;
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  pending = { resolve, promise };
  discardDialog.dialog.showModal();
  discardDialog.stay.focus();
  return promise;
}

/**
 * Runs `proceed` right away when nothing is unsaved, otherwise only after the
 * admin agreed to discard. `onDiscard` runs first so the caller can drop the
 * edits it holds.
 */
export async function leaveIfClean(doc, proceed, onDiscard) {
  if (!hasUnsavedChanges()) {
    proceed();
    return true;
  }
  const discard = await confirmDiscard(doc);
  if (!discard) return false;
  if (onDiscard) onDiscard();
  proceed();
  return true;
}

/** Closes the discard question when the session ends. */
export function closeDiscardDialog() {
  if (discardDialog && discardDialog.dialog.open) discardDialog.dialog.close();
}

/**
 * Wires a dialog form to the guard. `snapshot()` returns a string of the
 * current field values; the dialog counts as dirty when it differs from the
 * snapshot taken by arm(). `isBusy()` blocks closing while a request is in
 * flight. Cancel, Esc and requestClose() ask first when dirty.
 */
export function guardDialog({ id, doc, dialog, cancel, snapshot, isBusy }) {
  let initial = '';
  let untrack = null;

  const isDirty = () => dialog.open && snapshot() !== initial;

  function disarm() {
    if (untrack) untrack();
    untrack = null;
  }

  async function requestClose() {
    if (isBusy()) return;
    if (isDirty()) {
      const discard = await confirmDiscard(doc);
      if (!discard || isBusy()) return;
    }
    dialog.close();
  }

  cancel.addEventListener('click', requestClose);
  // Esc: always stop the browser closing it, then ask when needed.
  dialog.addEventListener('cancel', (event) => {
    event.preventDefault();
    requestClose();
  });
  dialog.addEventListener('close', disarm);

  return {
    arm() {
      disarm();
      initial = snapshot();
      untrack = trackDirty(id, isDirty);
    },
    isDirty,
    requestClose,
  };
}
