// The confirmation dialog for suspend, reactivate and delete. Suspend and
// delete need a reason (1-1000 characters), reactivate does not.

import { createCooldown, hideBanner, keepOpenWhile, showError } from './ui.js';
import { formatNumber, t } from './i18n.js';

export const REASON_MAX = 1000;

export const ACTIONS = Object.freeze({
  suspend: Object.freeze({ path: '/api/accounts/suspend', needsReason: true, tone: 'danger' }),
  reactivate: Object.freeze({ path: '/api/accounts/reactivate', needsReason: false, tone: 'primary' }),
  delete: Object.freeze({ path: '/api/accounts/delete', needsReason: true, tone: 'danger' }),
});

/** Same cleaning the server applies: control characters become spaces, then trim. */
export function cleanReason(raw) {
  return String(raw ?? '')
    .replace(/\p{Cc}/gu, ' ')
    .trim();
}

/** Reasons count characters (code points), like the server. */
export function validateReason(raw) {
  const value = cleanReason(raw);
  const length = [...value].length;
  if (length < 1) return { ok: false, error: 'required', value };
  if (length > REASON_MAX) return { ok: false, error: 'too_long', value };
  return { ok: true, error: null, value };
}

/**
 * Builds the request for an action, or { error } when the input is not valid.
 * Reactivate sends the id only: the server takes no reason there.
 */
export function buildActionRequest(action, account, rawReason) {
  const spec = ACTIONS[action];
  if (!spec || !account || typeof account.id !== 'string' || account.id === '') return { error: 'invalid' };
  const body = { id: account.id };
  if (spec.needsReason) {
    const reason = validateReason(rawReason);
    if (!reason.ok) return { error: reason.error };
    body.reason = reason.value;
  }
  return { path: spec.path, body };
}

/** Wires the dialog element. open(action, account, onDone) shows it. */
export function createAccountDialog({ api, doc = document, onDone }) {
  const dialog = doc.getElementById('action-dialog');
  const form = doc.getElementById('action-form');
  const title = doc.getElementById('dialog-title');
  const target = doc.getElementById('dialog-target');
  const note = doc.getElementById('dialog-note');
  const reasonGroup = doc.getElementById('dialog-reason-group');
  const reason = doc.getElementById('dialog-reason');
  const hint = doc.getElementById('dialog-reason-hint');
  const count = doc.getElementById('dialog-reason-count');
  const banner = doc.getElementById('dialog-banner');
  const confirm = doc.getElementById('dialog-confirm');
  const cancel = doc.getElementById('dialog-cancel');

  let current = null; // { action, account }
  let busy = false;
  // A 429 with Retry-After keeps the confirm button off until the wait ends.
  const cooldown = createCooldown();
  cooldown.subscribe((left) => {
    if (left === 0) render();
  });

  function valid() {
    const spec = ACTIONS[current.action];
    return !spec.needsReason || validateReason(reason.value).ok;
  }

  function render() {
    if (!current) return;
    const { action, account } = current;
    const spec = ACTIONS[action];
    title.textContent = t(`dialog.${action}.title`);
    note.textContent = t(`dialog.${action}.note`);
    target.textContent = [account.full_name, account.email].filter(Boolean).join(' · ');
    reasonGroup.hidden = !spec.needsReason;
    hint.textContent = t('dialog.reasonHint', { max: formatNumber(REASON_MAX) });
    count.textContent = t('dialog.reasonCount', { n: formatNumber([...reason.value].length), max: formatNumber(REASON_MAX) });
    confirm.className = `btn ${spec.tone}`;
    confirm.textContent = busy ? t('dialog.working') : t(`dialog.${action}.confirm`);
    confirm.disabled = busy || cooldown.active || !valid();
    cancel.disabled = busy;
    reason.disabled = busy;
  }

  async function submit() {
    if (!current || busy || cooldown.active || !valid()) return;
    const req = buildActionRequest(current.action, current.account, reason.value);
    if (req.error) return;
    busy = true;
    hideBanner(banner);
    render();
    const res = await api.post(req.path, req.body);
    busy = false;
    if (res.ok) {
      const action = current.action;
      dialog.close();
      onDone(action);
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
  reason.addEventListener('input', render);
  cancel.addEventListener('click', () => dialog.close());
  keepOpenWhile(dialog, () => busy);
  dialog.addEventListener('close', () => {
    // The reason can be sensitive; do not keep it around once the dialog is gone.
    reason.value = '';
    current = null;
    busy = false;
    hideBanner(banner);
  });

  return {
    open(action, account) {
      current = { action, account };
      busy = false;
      cooldown.stop();
      reason.value = '';
      hideBanner(banner);
      render();
      dialog.showModal();
      (ACTIONS[action].needsReason ? reason : cancel).focus();
    },
    /** Redraws in the current language (called when the language changes). */
    rerender: render,
    close() {
      if (dialog.open) dialog.close();
    },
  };
}
