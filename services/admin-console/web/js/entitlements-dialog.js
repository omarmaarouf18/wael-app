// Entitlements dialog (SPEC Phase 4.6, SPEC 6.3 part 2): views a student's
// active, expired, and revoked entitlements, allows manual grant of published
// subjects, and revocation with mandatory reason.

import { clear, h } from './dom.js';
import { formatCairoDateTime, formatNumber, t } from './i18n.js';
import { hideBanner, messageRow, showBanner, showError } from './ui.js';
import { validateReason } from './account-dialog.js';

const COLUMNS = 6;

export function createEntitlementsDialog({ api, doc = document, onDone }) {
  const dialog = doc.getElementById('entitlements-dialog');
  const title = doc.getElementById('entitlements-dialog-title');
  const grantBtn = doc.getElementById('entitlements-grant-btn');
  const closeBtn = doc.getElementById('entitlements-close');
  const banner = doc.getElementById('entitlements-banner');
  const tbody = doc.querySelector('#entitlements-table tbody');

  // Grant dialog elements
  const grantDialog = doc.getElementById('entitlement-grant-dialog');
  const grantForm = doc.getElementById('entitlement-grant-form');
  const grantTarget = doc.getElementById('grant-dialog-target');
  const grantLevelSelect = doc.getElementById('grant-level-select');
  const grantSubjectSelect = doc.getElementById('grant-subject-select');
  const grantNote = doc.getElementById('grant-dialog-note');
  const grantBanner = doc.getElementById('grant-dialog-banner');
  const grantCancel = doc.getElementById('grant-dialog-cancel');
  const grantConfirm = doc.getElementById('grant-dialog-confirm');

  // Revoke dialog elements
  const revokeDialog = doc.getElementById('entitlement-revoke-dialog');
  const revokeForm = doc.getElementById('entitlement-revoke-form');
  const revokeTarget = doc.getElementById('revoke-dialog-target');
  const revokeReason = doc.getElementById('revoke-dialog-reason');
  const revokeHint = doc.getElementById('revoke-dialog-hint');
  const revokeCount = doc.getElementById('revoke-dialog-count');
  const revokeBanner = doc.getElementById('revoke-dialog-banner');
  const revokeCancel = doc.getElementById('revoke-dialog-cancel');
  const revokeConfirm = doc.getElementById('revoke-dialog-confirm');

  let currentAccount = null;
  let activeEntitlement = null;
  let items = [];
  let levels = [];
  let subjects = [];
  let loading = false;
  let inFlight = false;

  async function loadEntitlements() {
    if (!currentAccount) return;
    loading = true;
    clear(tbody);
    tbody.append(messageRow(COLUMNS, t('common.loading')));
    hideBanner(banner);

    const res = await api.get('/api/entitlements', { user_id: currentAccount.id });
    loading = false;
    clear(tbody);

    if (!res.ok) {
      showBanner(banner, res.kind, loadEntitlements);
      return;
    }

    items = Array.isArray(res.data?.items) ? res.data.items : [];
    if (items.length === 0) {
      tbody.append(messageRow(COLUMNS, t('entitlements.empty')));
      return;
    }

    for (const ent of items) {
      tbody.append(renderEntitlementRow(ent));
    }
  }

  function renderEntitlementRow(ent) {
    const subjectText = [ent.subject_title_ar, ent.level_name_ar].filter(Boolean).join(' · ');
    const subjectCell = h('td', { text: subjectText || '—' });

    let statusText = t('entitlements.status.active');
    let statusClass = 'status-badge success';
    if (ent.is_revoked) {
      statusText = t('entitlements.status.revoked');
      statusClass = 'status-badge danger';
    } else if (ent.is_expired) {
      statusText = t('entitlements.status.expired');
      statusClass = 'status-badge danger';
    }
    const statusCell = h('td', {}, h('span', { class: statusClass, text: statusText }));

    const sourceText = ent.source === 'request'
      ? t('entitlements.source.request')
      : t('entitlements.source.admin_grant');
    const sourceCell = h('td', { text: sourceText });

    const grantedCell = h('td', { text: formatCairoDateTime(ent.granted_at) });
    const expiresCell = h('td', { text: formatCairoDateTime(ent.expires_at) });

    const actionsCell = h('td', { class: 'actions' });
    if (!ent.is_revoked && !ent.is_expired) {
      actionsCell.append(
        h('button', {
          class: 'btn small danger-outline',
          text: t('entitlements.action.revoke'),
          attrs: { type: 'button' },
          on: { click: () => openRevoke(ent) },
        }),
      );
    } else if (ent.is_revoked && ent.revoke_reason) {
      const detail = t('entitlements.revocationDetail', {
        date: formatCairoDateTime(ent.revoked_at),
        by: ent.revoked_by || '—',
        reason: ent.revoke_reason,
      });
      actionsCell.append(h('span', { class: 'small muted', text: detail }));
    }

    return h('tr', {}, subjectCell, statusCell, sourceCell, grantedCell, expiresCell, actionsCell);
  }

  // --- Grant Flow ---
  async function openGrant() {
    if (!currentAccount) return;
    inFlight = false;
    hideBanner(grantBanner);

    const name = currentAccount.full_name || currentAccount.email || currentAccount.id;
    grantTarget.textContent = name;
    grantNote.hidden = true;
    grantConfirm.disabled = true;

    // Load levels and published subjects
    const [levelsRes, subjectsRes] = await Promise.all([
      api.get('/api/levels'),
      api.get('/api/subjects', { published: 'true', limit: 100 }),
    ]);

    levels = Array.isArray(levelsRes.data?.levels) ? levelsRes.data.levels : (Array.isArray(levelsRes.data?.items) ? levelsRes.data.items : []);
    subjects = Array.isArray(subjectsRes.data?.items) ? subjectsRes.data.items : [];

    clear(grantLevelSelect);
    grantLevelSelect.append(h('option', { value: '', text: t('dialog.grant.selectLevel') }));
    for (const lvl of levels) {
      const id = lvl.key || lvl.id;
      grantLevelSelect.append(h('option', { value: id, text: lvl.name_ar || lvl.title_ar || id }));
    }

    clear(grantSubjectSelect);
    grantSubjectSelect.append(h('option', { value: '', text: t('dialog.grant.selectSubject') }));

    grantDialog.showModal();
    grantLevelSelect.focus();
  }

  function onGrantLevelChange() {
    const selectedLevel = grantLevelSelect.value;
    clear(grantSubjectSelect);
    grantNote.hidden = true;
    grantConfirm.disabled = true;

    if (!selectedLevel) {
      grantSubjectSelect.append(h('option', { value: '', text: t('dialog.grant.selectSubject') }));
      return;
    }

    const available = subjects.filter((s) => s.level_id === selectedLevel || s.level_key === selectedLevel);
    if (available.length === 0) {
      grantSubjectSelect.append(h('option', { value: '', text: t('dialog.grant.noSubjects') }));
      return;
    }

    grantSubjectSelect.append(h('option', { value: '', text: t('dialog.grant.selectSubject') }));
    for (const s of available) {
      grantSubjectSelect.append(h('option', { value: s.id, text: s.title_ar }));
    }
  }

  function onGrantSubjectChange() {
    const selectedSubjID = grantSubjectSelect.value;
    if (!selectedSubjID) {
      grantNote.hidden = true;
      grantConfirm.disabled = true;
      return;
    }

    const s = subjects.find((sub) => sub.id === selectedSubjID);
    if (!s) return;

    grantNote.textContent = t('dialog.grant.note', {
      price: formatNumber(s.price),
      expiry: formatCairoDateTime(s.access_expires_at),
    });
    grantNote.hidden = false;
    grantConfirm.disabled = false;
  }

  if (grantLevelSelect) {
    grantLevelSelect.addEventListener('change', onGrantLevelChange);
  }
  if (grantSubjectSelect) {
    grantSubjectSelect.addEventListener('change', onGrantSubjectChange);
  }

  if (grantForm) {
    grantForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!currentAccount || inFlight) return;
      const subjID = grantSubjectSelect.value;
      if (!subjID) return;

      inFlight = true;
      grantConfirm.disabled = true;
      grantCancel.disabled = true;
      hideBanner(grantBanner);

      const res = await api.post('/api/entitlements/grant', {
        user_id: currentAccount.id,
        subject_id: subjID,
      });

      inFlight = false;
      grantConfirm.disabled = false;
      grantCancel.disabled = false;

      if (res.ok) {
        grantDialog.close();
        loadEntitlements();
        if (typeof onDone === 'function') onDone();
        return;
      }
      if (res.kind === 'unauthorized') {
        grantDialog.close();
        return;
      }
      showError(grantBanner, res, () => grantForm.requestSubmit());
    });
  }

  if (grantCancel) {
    grantCancel.addEventListener('click', () => {
      if (!inFlight) grantDialog.close();
    });
  }

  // --- Revoke Flow ---
  function openRevoke(ent) {
    activeEntitlement = ent;
    inFlight = false;
    revokeReason.value = '';
    hideBanner(revokeBanner);

    const name = currentAccount.full_name || currentAccount.email || currentAccount.id;
    revokeTarget.textContent = `${name} · ${ent.subject_title_ar}`;

    renderRevokeState();
    revokeDialog.showModal();
    revokeReason.focus();
  }

  function renderRevokeState() {
    const val = revokeReason.value;
    const len = [...val].length;
    revokeHint.textContent = t('dialog.reasonHint', { max: formatNumber(1000) });
    revokeCount.textContent = t('dialog.reasonCount', { n: formatNumber(len), max: formatNumber(1000) });
    const valid = validateReason(val).ok;
    revokeConfirm.disabled = inFlight || !valid;
    revokeCancel.disabled = inFlight;
    revokeReason.disabled = inFlight;
  }

  if (revokeReason) {
    revokeReason.addEventListener('input', renderRevokeState);
  }

  if (revokeForm) {
    revokeForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!activeEntitlement || inFlight) return;
      const valid = validateReason(revokeReason.value);
      if (!valid.ok) return;

      inFlight = true;
      renderRevokeState();
      hideBanner(revokeBanner);

      const res = await api.post('/api/entitlements/revoke', {
        id: activeEntitlement.id,
        reason: valid.value,
      });

      inFlight = false;
      renderRevokeState();

      if (res.ok) {
        revokeDialog.close();
        activeEntitlement = null;
        loadEntitlements();
        if (typeof onDone === 'function') onDone();
        return;
      }
      if (res.kind === 'unauthorized') {
        revokeDialog.close();
        return;
      }
      showError(revokeBanner, res, () => revokeForm.requestSubmit());
    });
  }

  if (revokeCancel) {
    revokeCancel.addEventListener('click', () => {
      if (!inFlight) {
        revokeDialog.close();
        activeEntitlement = null;
      }
    });
  }

  if (grantBtn) {
    grantBtn.addEventListener('click', openGrant);
  }
  if (closeBtn) {
    closeBtn.addEventListener('click', () => dialog.close());
  }

  return {
    open(account) {
      currentAccount = account;
      const name = account.full_name || account.email || account.id;
      title.textContent = t('entitlements.title', { name });
      loadEntitlements();
      dialog.showModal();
    },
    close() {
      if (dialog && dialog.open) dialog.close();
      if (grantDialog && grantDialog.open) grantDialog.close();
      if (revokeDialog && revokeDialog.open) revokeDialog.close();
      currentAccount = null;
      activeEntitlement = null;
      items = [];
    },
    rerender() {
      if (currentAccount) {
        const name = currentAccount.full_name || currentAccount.email || currentAccount.id;
        title.textContent = t('entitlements.title', { name });
      }
    },
  };
}
