// Requests tab (SPEC Phase 4.5, 6.3 part 2): lists purchase requests with
// student identity join from auth-service, pending badge counter, accept and
// reject dialogs.

import { clear, h } from './dom.js';
import { formatCairoDateTime, formatNumber, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { setBadge } from './tabs.js';
import { createCooldown, createPager, hideBanner, keepOpenWhile, messageRow, showError, statusBadge } from './ui.js';
import { validateReason } from './account-dialog.js';

export const PAGE_SIZE = 15;
const COLUMNS = 8;

export function requestsQuery({ status, subject_id, page, limit }) {
  const q = { page, limit };
  if (status) q.status = status;
  if (subject_id) q.subject_id = subject_id;
  return q;
}

export function mountRequests({ api, doc = document, win = window }) {
  const form = doc.getElementById('requests-filter');
  const statusSelect = doc.getElementById('requests-status');
  const subjectSelect = doc.getElementById('requests-subject');
  const resetButton = doc.getElementById('requests-reset');
  const banner = doc.getElementById('requests-banner');
  const tbody = doc.querySelector('#requests-table tbody');
  const pager = createPager(doc.getElementById('requests-pager'), (page) => {
    state.page = page;
    load();
  });

  // Accept dialog elements
  const acceptDialog = doc.getElementById('request-accept-dialog');
  const acceptForm = doc.getElementById('request-accept-form');
  const acceptTarget = doc.getElementById('accept-dialog-target');
  const acceptNote = doc.getElementById('accept-dialog-note');
  const acceptBanner = doc.getElementById('accept-dialog-banner');
  const acceptCancel = doc.getElementById('accept-dialog-cancel');
  const acceptConfirm = doc.getElementById('accept-dialog-confirm');

  // Reject dialog elements
  const rejectDialog = doc.getElementById('request-reject-dialog');
  const rejectForm = doc.getElementById('request-reject-form');
  const rejectTarget = doc.getElementById('reject-dialog-target');
  const rejectReason = doc.getElementById('reject-dialog-reason');
  const rejectHint = doc.getElementById('reject-dialog-hint');
  const rejectCount = doc.getElementById('reject-dialog-count');
  const rejectBanner = doc.getElementById('reject-dialog-banner');
  const rejectCancel = doc.getElementById('reject-dialog-cancel');
  const rejectConfirm = doc.getElementById('reject-dialog-confirm');

  const state = {
    page: 1,
    status: 'pending',
    subject_id: '',
    total: 0,
    items: [],
    studentMap: new Map(),
    subjects: [],
    loaded: false,
    error: null,
  };
  let seq = 0;
  let activeRequest = null;
  let inFlight = false;
  // A 429 with Retry-After keeps the accept/reject buttons off until it ends.
  const acceptCooldown = createCooldown();
  const rejectCooldown = createCooldown();
  acceptCooldown.subscribe((left) => {
    if (left === 0 && activeRequest && !inFlight) acceptConfirm.disabled = false;
  });
  rejectCooldown.subscribe((left) => {
    if (left === 0) renderRejectState();
  });

  async function loadSubjectsFilter() {
    const res = await api.get('/api/subjects', { limit: 100 });
    if (!res.ok || !res.data) return;
    const items = Array.isArray(res.data.items) ? res.data.items : [];
    state.subjects = items;
    if (subjectSelect) {
      const current = subjectSelect.value;
      clear(subjectSelect);
      subjectSelect.append(h('option', { value: '', text: t('requests.filterAllSubjects') }));
      for (const s of items) {
        subjectSelect.append(h('option', { value: s.id, text: s.title_ar }));
      }
      subjectSelect.value = current;
    }
  }

  function renderRows() {
    clear(tbody);
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      tbody.append(messageRow(COLUMNS, t('common.loading')));
      return;
    }
    if (state.items.length === 0) {
      tbody.append(messageRow(COLUMNS, t('common.empty')));
      return;
    }
    for (const req of state.items) {
      tbody.append(renderRow(req));
    }
  }

  function renderRow(req) {
    const student = state.studentMap.get(req.user_id);
    const studentCell = h('td', {});
    if (student) {
      studentCell.append(
        h('div', { class: 'strong', text: student.full_name || '—' }),
        h('div', { class: 'id ltr', text: req.user_id }),
      );
    } else {
      studentCell.append(
        h('div', { class: 'id ltr', text: req.user_id }),
        h('div', { class: 'small muted', text: t('requests.student_fetch_failed') }),
      );
    }

    const phoneCell = h('td', { class: 'ltr', text: student?.phone || '—' });
    const emailCell = h('td', { class: 'ltr', text: student?.email || '—' });

    const subjectText = [req.subject_title_ar, req.level_name_ar].filter(Boolean).join(' · ');
    const subjectCell = h('td', { text: subjectText || '—' });

    const priceCell = h('td', { text: t('catalog.price', { n: formatNumber(req.price) }) });
    const requestedCell = h('td', { text: formatCairoDateTime(req.created_at) });
    const statusCell = h('td', {}, statusBadge(req.status));

    const actionsCell = h('td', { class: 'actions' });
    if (req.status === 'pending') {
      actionsCell.append(
        h('button', {
          class: 'btn small primary',
          text: t('requests.action.accept'),
          attrs: { type: 'button' },
          on: { click: () => openAccept(req) },
        }),
        h('button', {
          class: 'btn small danger-outline',
          text: t('requests.action.reject'),
          attrs: { type: 'button' },
          on: { click: () => openReject(req) },
        }),
      );
    } else if (req.status === 'rejected' && req.reject_reason) {
      actionsCell.append(h('span', { class: 'small muted', text: req.reject_reason }));
    } else if (req.status === 'accepted' && req.decided_at) {
      actionsCell.append(h('span', { class: 'small muted', text: formatCairoDateTime(req.decided_at) }));
    }

    return h('tr', {}, studentCell, phoneCell, emailCell, subjectCell, priceCell, requestedCell, statusCell, actionsCell);
  }

  function render() {
    renderRows();
    if (state.error) showError(banner, state.error, load);
    else hideBanner(banner);
    pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();

    const res = await api.get('/api/requests', requestsQuery({
      status: state.status,
      subject_id: state.subject_id,
      page: state.page,
      limit: PAGE_SIZE,
    }));

    if (mine !== seq || !isSignedIn()) return;
    if (!res.ok) {
      state.error = res;
      render();
      return;
    }

    const data = res.data ?? {};
    state.items = Array.isArray(data.items) ? data.items : [];
    state.total = Number.isInteger(data.total) ? data.total : state.items.length;
    state.loaded = true;

    // Update pending badge
    if (Number.isInteger(data.pending_count)) {
      setBadge(doc, 'requests', data.pending_count);
    }

    // Join student identity via auth-service
    const userIDs = [...new Set(state.items.map((it) => it.user_id).filter(Boolean))];
    if (userIDs.length > 0) {
      const authRes = await api.get('/api/accounts', { ids: userIDs.join(','), limit: 100 });
      if (mine === seq && isSignedIn()) {
        if (authRes.ok && authRes.data && Array.isArray(authRes.data.items)) {
          state.studentMap = new Map(authRes.data.items.map((u) => [u.id, u]));
        } else {
          state.studentMap = new Map();
        }
      }
    } else {
      state.studentMap = new Map();
    }

    render();
  }

  // --- Accept Dialog ---
  function openAccept(req) {
    activeRequest = req;
    inFlight = false;
    acceptCooldown.stop();
    hideBanner(acceptBanner);

    const student = state.studentMap.get(req.user_id);
    const studentName = student ? (student.full_name || student.email || req.user_id) : req.user_id;
    acceptTarget.textContent = `${studentName} · ${req.subject_title_ar}`;
    acceptNote.textContent = t('dialog.accept.note', {
      price: formatNumber(req.price),
      expiry: formatCairoDateTime(req.access_expires_at),
    });

    acceptConfirm.disabled = false;
    acceptCancel.disabled = false;
    acceptDialog.showModal();
    acceptConfirm.focus();
  }

  if (acceptForm) {
    acceptForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!activeRequest || inFlight || acceptCooldown.active) return;
      inFlight = true;
      acceptConfirm.disabled = true;
      acceptCancel.disabled = true;
      hideBanner(acceptBanner);

      const res = await api.post('/api/requests/accept', { id: activeRequest.id });
      inFlight = false;
      acceptConfirm.disabled = false;
      acceptCancel.disabled = false;

      if (res.ok) {
        acceptDialog.close();
        activeRequest = null;
        load();
        return;
      }
      if (res.kind === 'unauthorized') {
        acceptDialog.close();
        return;
      }
      acceptCooldown.arm(res);
      acceptConfirm.disabled = acceptCooldown.active;
      showError(acceptBanner, res, () => acceptForm.requestSubmit(), acceptCooldown);
    });
  }

  if (acceptDialog) keepOpenWhile(acceptDialog, () => inFlight);
  if (rejectDialog) keepOpenWhile(rejectDialog, () => inFlight);

  if (acceptCancel) {
    acceptCancel.addEventListener('click', () => {
      if (!inFlight) {
        acceptDialog.close();
        activeRequest = null;
      }
    });
  }

  // --- Reject Dialog ---
  function openReject(req) {
    activeRequest = req;
    inFlight = false;
    rejectCooldown.stop();
    rejectReason.value = '';
    hideBanner(rejectBanner);

    const student = state.studentMap.get(req.user_id);
    const studentName = student ? (student.full_name || student.email || req.user_id) : req.user_id;
    rejectTarget.textContent = `${studentName} · ${req.subject_title_ar}`;

    renderRejectState();
    rejectDialog.showModal();
    rejectReason.focus();
  }

  function renderRejectState() {
    const val = rejectReason.value;
    const len = [...val].length;
    rejectHint.textContent = t('dialog.reasonHint', { max: formatNumber(1000) });
    rejectCount.textContent = t('dialog.reasonCount', { n: formatNumber(len), max: formatNumber(1000) });
    const valid = validateReason(val).ok;
    rejectConfirm.disabled = inFlight || rejectCooldown.active || !valid;
    rejectCancel.disabled = inFlight;
    rejectReason.disabled = inFlight;
  }

  if (rejectReason) {
    rejectReason.addEventListener('input', renderRejectState);
  }

  if (rejectForm) {
    rejectForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (!activeRequest || inFlight || rejectCooldown.active) return;
      const valid = validateReason(rejectReason.value);
      if (!valid.ok) return;

      inFlight = true;
      renderRejectState();
      hideBanner(rejectBanner);

      const res = await api.post('/api/requests/reject', {
        id: activeRequest.id,
        reason: valid.value,
      });

      inFlight = false;
      renderRejectState();

      if (res.ok) {
        rejectDialog.close();
        activeRequest = null;
        load();
        return;
      }
      if (res.kind === 'unauthorized') {
        rejectDialog.close();
        return;
      }
      rejectCooldown.arm(res);
      renderRejectState();
      showError(rejectBanner, res, () => rejectForm.requestSubmit(), rejectCooldown);
    });
  }

  if (rejectCancel) {
    rejectCancel.addEventListener('click', () => {
      if (!inFlight) {
        rejectDialog.close();
        activeRequest = null;
      }
    });
  }

  // Filter form
  if (form) {
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      state.status = statusSelect ? statusSelect.value : '';
      state.subject_id = subjectSelect ? subjectSelect.value : '';
      state.page = 1;
      load();
    });
  }

  if (resetButton) {
    resetButton.addEventListener('click', () => {
      if (statusSelect) statusSelect.value = 'pending';
      if (subjectSelect) subjectSelect.value = '';
      state.status = 'pending';
      state.subject_id = '';
      state.page = 1;
      load();
    });
  }

  return {
    load() {
      loadSubjectsFilter();
      return load();
    },
    refreshBadge: async () => {
      const res = await api.get('/api/requests', { limit: 1 });
      if (res.ok && res.data && Number.isInteger(res.data.pending_count)) {
        setBadge(doc, 'requests', res.data.pending_count);
      }
    },
    rerender() {
      render();
    },
    reset() {
      seq += 1;
      activeRequest = null;
      inFlight = false;
      if (acceptDialog && acceptDialog.open) acceptDialog.close();
      if (rejectDialog && rejectDialog.open) rejectDialog.close();
      Object.assign(state, {
        page: 1,
        status: 'pending',
        subject_id: '',
        total: 0,
        items: [],
        studentMap: new Map(),
        loaded: false,
        error: null,
      });
      if (statusSelect) statusSelect.value = 'pending';
      if (subjectSelect) subjectSelect.value = '';
      render();
    },
  };
}
