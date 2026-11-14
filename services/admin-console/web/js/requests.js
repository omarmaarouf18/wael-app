// Requests tab (SPEC Phase 4.5, 6.3 part 2, and 2026-11-05 orders grouped view):
// default view groups pending requests by student, with expandable orders
// and subjects, select all / select order / select subject, bulk accept/reject,
// amount check warnings, and flat list available as a toggle.

import { clear, h } from './dom.js';
import { formatCairoDateTime, formatMoney, formatNumber, getLang, t } from './i18n.js';
import { isSignedIn } from './auth.js';
import { setBadge } from './tabs.js';
import { avatar, createCooldown, createPager, hideBanner, keepOpenWhile, messageRow, shortId, showError, statusBadge, toast } from './ui.js';
import { validateReason } from './account-dialog.js';

export const PAGE_SIZE = 15;
const FLAT_COLUMNS = 10;
const GROUPED_COLUMNS = 8;

export function requestsQuery({ status, subject_id, page, limit }) {
  const q = { page, limit };
  if (status) q.status = status;
  if (subject_id) q.subject_id = subject_id;
  return q;
}

/** Payment method label from i18n (wallet unless InstaPay). */
function methodLabelOf(method) {
  return t(method === 'instapay' ? 'requests.method.instapay' : 'requests.method.wallet');
}

/** "<label>: <value>" where the value (a reference or phone) stays Latin and LTR. */
function labelledLtr(labelKey, value, extra = '') {
  return h('span', { class: extra }, `${t(labelKey)}: `, h('bdi', { class: 'ltr', text: value || '—' }));
}

/** Tags each cell with its column name so narrow screens can show rows as labelled cards. */
function labelCells(cells, keys) {
  cells.forEach((cell, i) => {
    if (keys[i]) cell.setAttribute('data-label', t(keys[i]));
  });
}

export function mountRequests({ api, doc = document, win = window, onDecided = () => {} }) {
  const form = doc.getElementById('requests-filter');
  const viewModeSelect = doc.getElementById('requests-view-mode');
  const statusSelect = doc.getElementById('requests-status');
  const sourceSelect = doc.getElementById('requests-source');
  const subjectSelect = doc.getElementById('requests-subject');
  const searchInput = doc.getElementById('requests-search');
  const resetButton = doc.getElementById('requests-reset');

  const bulkBar = doc.getElementById('requests-bulk-bar');
  const bulkInfo = doc.getElementById('requests-bulk-info');
  const bulkAcceptBtn = doc.getElementById('requests-bulk-accept-btn');
  const bulkRejectBtn = doc.getElementById('requests-bulk-reject-btn');
  const bulkClearBtn = doc.getElementById('requests-bulk-clear-btn');

  const banner = doc.getElementById('requests-banner');
  const table = doc.getElementById('requests-table');
  const thead = doc.querySelector('#requests-table thead');
  const tbody = doc.querySelector('#requests-table tbody');
  const pager = createPager(doc.getElementById('requests-pager'), (page) => {
    state.page = page;
    load();
  });

  // Single Accept dialog elements
  const acceptDialog = doc.getElementById('request-accept-dialog');
  const acceptForm = doc.getElementById('request-accept-form');
  const acceptTarget = doc.getElementById('accept-dialog-target');
  const acceptNote = doc.getElementById('accept-dialog-note');
  const acceptBanner = doc.getElementById('accept-dialog-banner');
  const acceptCancel = doc.getElementById('accept-dialog-cancel');
  const acceptConfirm = doc.getElementById('accept-dialog-confirm');

  // Single Reject dialog elements
  const rejectDialog = doc.getElementById('request-reject-dialog');
  const rejectForm = doc.getElementById('request-reject-form');
  const rejectTarget = doc.getElementById('reject-dialog-target');
  const rejectReason = doc.getElementById('reject-dialog-reason');
  const rejectHint = doc.getElementById('reject-dialog-hint');
  const rejectCount = doc.getElementById('reject-dialog-count');
  const rejectBanner = doc.getElementById('reject-dialog-banner');
  const rejectCancel = doc.getElementById('reject-dialog-cancel');
  const rejectConfirm = doc.getElementById('reject-dialog-confirm');

  // Bulk Accept dialog elements
  const bulkAcceptDialog = doc.getElementById('request-bulk-accept-dialog');
  const bulkAcceptForm = doc.getElementById('request-bulk-accept-form');
  const bulkAcceptTarget = doc.getElementById('bulk-accept-dialog-target');
  const bulkAcceptNote = doc.getElementById('bulk-accept-dialog-note');
  const bulkAcceptBanner = doc.getElementById('bulk-accept-dialog-banner');
  const bulkAcceptCancel = doc.getElementById('bulk-accept-dialog-cancel');
  const bulkAcceptConfirm = doc.getElementById('bulk-accept-dialog-confirm');

  // Bulk Reject dialog elements
  const bulkRejectDialog = doc.getElementById('request-bulk-reject-dialog');
  const bulkRejectForm = doc.getElementById('request-bulk-reject-form');
  const bulkRejectTarget = doc.getElementById('bulk-reject-dialog-target');
  const bulkRejectNote = doc.getElementById('bulk-reject-dialog-note');
  const bulkRejectReason = doc.getElementById('bulk-reject-dialog-reason');
  const bulkRejectHint = doc.getElementById('bulk-reject-dialog-hint');
  const bulkRejectCount = doc.getElementById('bulk-reject-dialog-count');
  const bulkRejectBanner = doc.getElementById('bulk-reject-dialog-banner');
  const bulkRejectCancel = doc.getElementById('bulk-reject-dialog-cancel');
  const bulkRejectConfirm = doc.getElementById('bulk-reject-dialog-confirm');

  const state = {
    viewMode: 'grouped', // 'grouped' (default) | 'flat'
    status: 'pending',
    source: '',
    subject_id: '',
    search: '',
    page: 1,
    total: 0,
    items: [],
    studentMap: new Map(),
    subjects: [],
    expandedStudents: new Set(),
    selectedRequests: new Map(), // id -> { id, price, title, user_id, order_id }
    loaded: false,
    error: null,
    partialError: null,
  };

  let seq = 0;
  let activeRequest = null;
  let inFlight = false;

  const acceptCooldown = createCooldown();
  const rejectCooldown = createCooldown();
  const bulkAcceptCooldown = createCooldown();
  const bulkRejectCooldown = createCooldown();

  acceptCooldown.subscribe((left) => {
    if (left === 0 && activeRequest && !inFlight && acceptConfirm) acceptConfirm.disabled = false;
  });
  rejectCooldown.subscribe((left) => {
    if (left === 0) renderRejectState();
  });
  bulkAcceptCooldown.subscribe((left) => {
    if (left === 0 && !inFlight && bulkAcceptConfirm) bulkAcceptConfirm.disabled = false;
  });
  bulkRejectCooldown.subscribe((left) => {
    if (left === 0) renderBulkRejectState();
  });

  async function loadSubjectsFilter() {
    const res = await api.get('/api/subjects', { limit: 100 });
    if (!res.ok || !res.data) return;
    const items = Array.isArray(res.data.items) ? res.data.items : [];
    state.subjects = items;
    if (subjectSelect) {
      const current = subjectSelect.value;
      clear(subjectSelect);
      subjectSelect.append(h('option', { text: t('requests.filterAllSubjects'), attrs: { value: '' } }));
      for (const s of items) {
        subjectSelect.append(h('option', { text: s.title_ar, attrs: { value: s.id } }));
      }
      subjectSelect.value = current;
    }
  }

  function renderHeader() {
    if (!thead) return;
    clear(thead);
    if (state.viewMode === 'flat') {
      thead.append(h('tr', {},
        h('th', { scope: 'col', text: t('requests.col.student') }),
        h('th', { scope: 'col', text: t('requests.col.phone') }),
        h('th', { scope: 'col', text: t('requests.col.email') }),
        h('th', { scope: 'col', text: t('requests.col.subject') }),
        h('th', { scope: 'col', text: t('requests.col.source') }),
        h('th', { scope: 'col', text: t('requests.col.paymentDetails') }),
        h('th', { scope: 'col', text: t('requests.col.price') }),
        h('th', { scope: 'col', text: t('requests.col.requested') }),
        h('th', { scope: 'col', text: t('requests.col.status') }),
        h('th', { scope: 'col', text: t('requests.col.actions') }),
      ));
    } else {
      thead.append(h('tr', {},
        h('th', { scope: 'col', text: t('requests.col.student') }),
        h('th', { scope: 'col', text: t('requests.col.phone') }),
        h('th', { scope: 'col', text: t('requests.col.email') }),
        h('th', { scope: 'col', text: t('requests.col.subjectCount') }),
        h('th', { scope: 'col', text: t('requests.col.totalAmount') }),
        h('th', { scope: 'col', text: t('requests.col.oldestRequest') }),
        h('th', { scope: 'col', text: t('requests.col.sources') }),
        h('th', { scope: 'col', text: t('requests.col.actions') }),
      ));
    }
  }

  function getFilteredStudentGroups() {
    const query = state.search ? state.search.toLowerCase() : '';
    const sourceFilter = state.source;
    const subjectFilter = state.subject_id;

    const result = [];
    for (const sg of state.items) {
      const student = state.studentMap.get(sg.user_id);
      const nameMatch = student?.full_name?.toLowerCase().includes(query) ?? false;
      const emailMatch = student?.email?.toLowerCase().includes(query) ?? false;
      const phoneMatch = student?.phone?.includes(query) ?? false;
      const userIdMatch = sg.user_id?.toLowerCase().includes(query) ?? false;

      const matchedOrders = [];
      for (const order of (sg.orders || [])) {
        if (sourceFilter) {
          const orderSrc = (order.source === 'mobile' || order.source === 'app') ? 'mobile' : 'web';
          if (orderSrc !== sourceFilter) continue;
        }

        const matchedItems = [];
        for (const it of (order.items || [])) {
          if (subjectFilter && it.subject_id !== subjectFilter) continue;
          matchedItems.push(it);
        }
        if (matchedItems.length === 0) continue;

        const orderRefMatch = order.transfer_reference?.toLowerCase().includes(query) ?? false;
        const orderSenderMatch = order.sender_number?.includes(query) ?? false;
        const orderIdMatch = order.order_id?.toLowerCase().includes(query) ?? false;
        const anyItemMatch = matchedItems.some((it) => (it.request_id || it.id)?.toLowerCase().includes(query) || (it.title_ar || it.subject_title_ar)?.toLowerCase().includes(query));

        if (!query || nameMatch || emailMatch || phoneMatch || userIdMatch || orderRefMatch || orderSenderMatch || orderIdMatch || anyItemMatch) {
          matchedOrders.push({ ...order, items: matchedItems });
        }
      }

      if (matchedOrders.length > 0) {
        result.push({ ...sg, orders: matchedOrders });
      }
    }
    return result;
  }

  function updateBulkBar() {
    if (!bulkBar) return;
    const count = state.selectedRequests.size;
    if (count === 0) {
      bulkBar.hidden = true;
      return;
    }
    let total = 0;
    for (const it of state.selectedRequests.values()) {
      total += (it.price || 0);
    }
    bulkBar.hidden = false;
    if (bulkInfo) {
      bulkInfo.textContent = t('requests.bulk.selected', {
        count: formatNumber(count),
        total: formatNumber(total),
      });
    }
    if (bulkAcceptBtn) {
      bulkAcceptBtn.textContent = t('requests.bulk.accept', { count: formatNumber(count) });
      bulkAcceptBtn.disabled = inFlight || bulkAcceptCooldown.active;
    }
    if (bulkRejectBtn) {
      bulkRejectBtn.textContent = t('requests.bulk.reject', { count: formatNumber(count) });
      bulkRejectBtn.disabled = inFlight || bulkRejectCooldown.active;
    }
  }

  function renderSelectionState() {
    render();
  }

  function renderGroupedRow(sg) {
    const student = state.studentMap.get(sg.user_id);
    const allOrders = sg.orders || [];
    const allItems = [];
    for (const o of allOrders) {
      for (const it of o.items) {
        allItems.push({ ...it, order_source: o.source, order_id: o.order_id });
      }
    }
    const subjectCount = allItems.length;
    const totalAmount = allItems.reduce((acc, it) => acc + (it.price || 0), 0);

    let oldestDate = null;
    const sourceSet = new Set();
    for (const it of allItems) {
      const d = it.created_at ? new Date(it.created_at) : null;
      if (d && !isNaN(d)) {
        if (!oldestDate || d < oldestDate) oldestDate = d;
      }
      if (it.source === 'web' || it.order_source === 'web') sourceSet.add('web');
      else sourceSet.add('app');
    }

    const pendingItems = allItems.filter((it) => it.status === 'pending');
    const selectedPendingCount = pendingItems.filter((it) => state.selectedRequests.has(it.request_id || it.id)).length;
    const isAllSelected = pendingItems.length > 0 && selectedPendingCount === pendingItems.length;
    const isIndeterminate = selectedPendingCount > 0 && selectedPendingCount < pendingItems.length;

    const selectAllBox = h('input', {
      attrs: {
        type: 'checkbox',
        'data-role': 'select-student',
        'data-user-id': sg.user_id,
        'aria-label': t('requests.selectAll'),
      },
      on: {
        change: (e) => {
          const checked = e.target.checked;
          for (const it of pendingItems) {
            const id = it.request_id || it.id;
            if (checked) {
              state.selectedRequests.set(id, { id, price: it.price || 0, user_id: sg.user_id, title: it.title_ar || it.subject_title_ar });
            } else {
              state.selectedRequests.delete(id);
            }
          }
          updateBulkBar();
          renderSelectionState();
        },
      },
    });
    selectAllBox.checked = isAllSelected;
    selectAllBox.indeterminate = isIndeterminate;
    if (pendingItems.length === 0) selectAllBox.disabled = true;

    const isExpanded = state.expandedStudents.has(sg.user_id);
    const expandBtn = h('button', {
      class: 'btn-expand',
      text: isExpanded ? '▼' : '▶',
      attrs: {
        type: 'button',
        'aria-expanded': String(isExpanded),
        title: isExpanded ? t('requests.collapse') : t('requests.expand'),
      },
      on: {
        click: () => {
          if (state.expandedStudents.has(sg.user_id)) {
            state.expandedStudents.delete(sg.user_id);
          } else {
            state.expandedStudents.add(sg.user_id);
          }
          render();
        },
      },
    });

    const studentCell = h('td', {},
      h('div', { class: 'order-meta' },
        selectAllBox,
        expandBtn,
        avatar(student?.full_name || student?.email),
        h('div', {},
          h('div', { class: 'strong', text: student?.full_name || '—' }),
          student ? null : h('bdi', { class: 'id ltr', text: shortId(sg.user_id), attrs: { title: sg.user_id } }),
          student ? null : h('div', { class: 'small muted', text: t('requests.student_fetch_failed') }),
        ),
      ),
    );

    const phoneCell = h('td', { class: 'ltr', text: student?.phone || '—' });
    const emailCell = h('td', { class: 'ltr', text: student?.email || '—' });
    const countCell = h('td', { text: formatNumber(subjectCount) });
    const totalCell = h('td', { text: formatMoney(totalAmount) });
    const dateCell = h('td', { text: oldestDate ? formatCairoDateTime(oldestDate.toISOString()) : '—' });

    const sourcesCell = h('td', {});
    if (sourceSet.has('web')) {
      sourcesCell.append(h('span', { class: 'badge', text: t('requests.source.web') }));
    }
    if (sourceSet.has('app')) {
      sourcesCell.append(h('span', { class: 'badge muted', text: t('requests.source.mobile') }));
    }
    if (sourceSet.size === 0) {
      sourcesCell.append(h('span', { class: 'muted', text: '—' }));
    }

    const actionsCell = h('td', { class: 'actions' });
    if (pendingItems.length === 1) {
      const req = pendingItems[0];
      actionsCell.append(
        h('button', {
          class: 'btn small success',
          text: t('requests.action.accept'),
          attrs: { type: 'button' },
          on: { click: () => openAccept({ ...req, id: req.request_id || req.id, user_id: sg.user_id }) },
        }),
        h('button', {
          class: 'btn small danger-outline',
          text: t('requests.action.reject'),
          attrs: { type: 'button' },
          on: { click: () => openReject({ ...req, id: req.request_id || req.id, user_id: sg.user_id }) },
        }),
      );
    } else if (pendingItems.length > 1) {
      const all = pendingItems.map((it) => ({ id: it.request_id || it.id, price: it.price || 0, user_id: sg.user_id, title: it.title_ar || it.subject_title_ar }));
      actionsCell.append(
        h('button', {
          class: 'btn small success',
          text: t('requests.acceptAll', { n: formatNumber(all.length) }),
          attrs: { type: 'button', 'data-role': 'accept-all' },
          on: { click: () => decideRequests('accept', all) },
        }),
        h('button', {
          class: 'btn small danger-outline',
          text: t('requests.rejectAll'),
          attrs: { type: 'button', 'data-role': 'reject-all' },
          on: { click: () => decideRequests('reject', all) },
        }),
        h('button', {
          class: 'btn small secondary',
          text: isExpanded ? t('requests.collapse') : t('requests.expand'),
          attrs: { type: 'button' },
          on: { click: () => expandBtn.click() },
        }),
      );
    }

    labelCells(
      [studentCell, phoneCell, emailCell, countCell, totalCell, dateCell, sourcesCell, actionsCell],
      ['requests.col.student', 'requests.col.phone', 'requests.col.email', 'requests.col.subjectCount', 'requests.col.totalAmount', 'requests.col.oldestRequest', 'requests.col.sources', ''],
    );
    const studentRow = h('tr', { class: 'student-row request-card' }, studentCell, phoneCell, emailCell, countCell, totalCell, dateCell, sourcesCell, actionsCell);

    if (!isExpanded) {
      return [studentRow];
    }

    // Expanded detail row
    const detailRow = h('tr', { class: 'expanded-detail-row' },
      h('td', { attrs: { colspan: String(GROUPED_COLUMNS) }, class: 'orders-container-cell' },
        renderStudentOrders(sg, student, allOrders),
      ),
    );

    return [studentRow, detailRow];
  }

  function renderStudentOrders(sg, student, allOrders) {
    const ordersContainer = h('div', { class: 'student-orders' });

    for (const order of allOrders) {
      const orderCard = h('div', { class: 'order-card' });
      const orderPending = order.items.filter((it) => it.status === 'pending');
      const orderSelectedCount = orderPending.filter((it) => state.selectedRequests.has(it.request_id || it.id)).length;
      const orderAllSelected = orderPending.length > 0 && orderSelectedCount === orderPending.length;
      const orderIndeterminate = orderSelectedCount > 0 && orderSelectedCount < orderPending.length;

      const orderCheckbox = h('input', {
        attrs: {
          type: 'checkbox',
          'data-role': 'select-order',
          'data-order-id': order.order_id || '',
          'data-user-id': sg.user_id,
        },
        on: {
          change: (e) => {
            const checked = e.target.checked;
            for (const it of orderPending) {
              const id = it.request_id || it.id;
              if (checked) {
                state.selectedRequests.set(id, { id, price: it.price || 0, user_id: sg.user_id, title: it.title_ar || it.subject_title_ar });
              } else {
                state.selectedRequests.delete(id);
              }
            }
            updateBulkBar();
            renderSelectionState();
          },
        },
      });
      orderCheckbox.checked = orderAllSelected;
      orderCheckbox.indeterminate = orderIndeterminate;
      if (orderPending.length === 0) orderCheckbox.disabled = true;

      const isWeb = order.source === 'web';
      const orderTitleText = order.order_id
        ? `${isWeb ? t('requests.webOrder') : t('requests.appOrder')} #${order.order_id}`
        : (isWeb ? t('requests.webOrder') : t('requests.appOrder'));

      const orderHeader = h('div', { class: 'order-header' },
        h('div', { class: 'order-meta' },
          h('label', { class: 'checkbox-label' },
            orderCheckbox,
            h('span', { text: t('requests.selectOrder') }),
          ),
          h('span', { class: isWeb ? 'badge' : 'badge muted', text: isWeb ? t('requests.source.web') : t('requests.source.mobile') }),
          h('strong', { text: orderTitleText }),
          h('span', { class: 'small muted', text: formatCairoDateTime(order.created_at) }),
          h('span', { class: 'strong', text: formatMoney(order.total) }),
        ),
      );

      if (isWeb) {
        const webDetails = h('div', { class: 'order-web-info' },
          h('span', { text: `${methodLabelOf(order.payment_method)}: ${formatMoney(order.amount_paid || 0)}` }),
          labelledLtr('requests.refLabel', order.transfer_reference),
          labelledLtr('requests.fromLabel', order.sender_number, 'muted'),
        );
        if (order.amount_mismatch || (order.amount_paid !== undefined && order.amount_paid !== order.total)) {
          webDetails.append(h('span', {
            class: 'badge danger small',
            text: t('requests.amountWarning', { amount: formatNumber(order.amount_paid), price: formatNumber(order.total) }),
          }));
        }
        orderHeader.append(webDetails);
      }

      orderCard.append(orderHeader);

      // Subjects table inside order
      const subThead = h('thead', {},
        h('tr', {},
          h('th', { scope: 'col' }, ''),
          h('th', { scope: 'col', text: t('requests.col.subject') }),
          h('th', { scope: 'col', text: t('requests.col.price') }),
          h('th', { scope: 'col', text: t('requests.col.source') }),
          h('th', { scope: 'col', text: t('requests.col.status') }),
          h('th', { scope: 'col', text: t('requests.col.actions') }),
        ),
      );
      const subTbody = h('tbody', {});
      const subTable = h('table', { class: 'order-subjects-table' }, subThead, subTbody);

      for (const it of order.items) {
        const id = it.request_id || it.id;
        const isPending = it.status === 'pending';
        const subCheckbox = h('input', {
          attrs: {
            type: 'checkbox',
            'data-role': 'select-subject',
            'data-id': id,
            'data-user-id': sg.user_id,
            'data-order-id': order.order_id || '',
            'aria-label': it.title_ar || it.subject_title_ar || id,
          },
          on: {
            change: (e) => {
              if (e.target.checked) {
                state.selectedRequests.set(id, { id, price: it.price || 0, user_id: sg.user_id, title: it.title_ar || it.subject_title_ar });
              } else {
                state.selectedRequests.delete(id);
              }
              updateBulkBar();
              renderSelectionState();
            },
          },
        });
        subCheckbox.checked = state.selectedRequests.has(id);
        if (!isPending) subCheckbox.disabled = true;

        const subTitle = [it.title_ar || it.subject_title_ar, it.level_name_ar].filter(Boolean).join(' · ');
        const actions = h('td', { class: 'actions' });
        if (isPending) {
          actions.append(
            h('button', {
              class: 'btn small primary',
              text: t('requests.action.accept'),
              attrs: { type: 'button' },
              on: { click: () => openAccept({ ...it, id, user_id: sg.user_id }) },
            }),
            h('button', {
              class: 'btn small danger-outline',
              text: t('requests.action.reject'),
              attrs: { type: 'button' },
              on: { click: () => openReject({ ...it, id, user_id: sg.user_id }) },
            }),
          );
        } else if (it.status === 'rejected' && it.reject_reason) {
          actions.append(h('span', { class: 'small muted', text: it.reject_reason }));
        } else if (it.status === 'accepted' && it.decided_at) {
          actions.append(h('span', { class: 'small muted', text: formatCairoDateTime(it.decided_at) }));
        }

        subTbody.append(h('tr', {},
          h('td', {}, subCheckbox),
          h('td', { text: subTitle || '—' }),
          h('td', { text: formatMoney(it.price) }),
          h('td', {}, h('span', { class: order.source === 'web' ? 'badge' : 'badge muted', text: order.source === 'web' ? t('requests.source.web') : t('requests.source.mobile') })),
          h('td', {}, statusBadge(it.status)),
          actions,
        ));
      }

      orderCard.append(subTable);
      ordersContainer.append(orderCard);
    }

    return ordersContainer;
  }

  function renderFlatRow(req) {
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

    const subjectText = [req.subject_title_ar || req.title_ar, req.level_name_ar].filter(Boolean).join(' · ');
    const subjectCell = h('td', { text: subjectText || '—' });

    const isWeb = req.source === 'web';
    const sourceCell = h('td', {},
      h('span', { class: isWeb ? 'badge' : 'badge muted', text: isWeb ? t('requests.source.web') : t('requests.source.mobile') }),
    );

    const paymentCell = h('td', {});
    if (isWeb) {
      paymentCell.append(
        h('div', { class: 'strong', text: `${methodLabelOf(req.payment_method)}: ${formatMoney(req.amount || 0)}` }),
        h('div', { class: 'small' }, labelledLtr('requests.refLabel', req.transfer_reference)),
        h('div', { class: 'small' }, labelledLtr('requests.fromLabel', req.sender_number, 'muted')),
      );
      if (req.amount_warning) {
        paymentCell.append(
          h('div', {
            class: 'badge danger small',
            text: t('requests.amountWarning', { amount: formatNumber(req.amount), price: formatNumber(req.price) }),
          }),
        );
      }
    } else {
      paymentCell.append(h('span', { class: 'muted', text: '—' }));
    }

    const priceCell = h('td', { text: formatMoney(req.price) });
    const requestedCell = h('td', { text: formatCairoDateTime(req.created_at) });
    const statusCell = h('td', {}, statusBadge(req.status));

    const actionsCell = h('td', { class: 'actions' });
    if (req.status === 'pending') {
      actionsCell.append(
        h('button', {
          class: 'btn small success',
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

    labelCells(
      [studentCell, phoneCell, emailCell, subjectCell, sourceCell, paymentCell, priceCell, requestedCell, statusCell, actionsCell],
      ['requests.col.student', 'requests.col.phone', 'requests.col.email', 'requests.col.subject', 'requests.col.source', 'requests.col.paymentDetails', 'requests.col.price', 'requests.col.requested', 'requests.col.status', ''],
    );
    return h('tr', { class: 'request-card' }, studentCell, phoneCell, emailCell, subjectCell, sourceCell, paymentCell, priceCell, requestedCell, statusCell, actionsCell);
  }

  function renderRows() {
    if (!tbody) return;
    clear(tbody);
    renderHeader();
    if (state.error && !state.loaded) return;
    if (!state.loaded) {
      const colSpan = state.viewMode === 'flat' ? FLAT_COLUMNS : GROUPED_COLUMNS;
      tbody.append(messageRow(colSpan, t('common.loading')));
      return;
    }

    if (state.viewMode === 'flat') {
      let filtered = state.items;
      if (state.source) {
        filtered = filtered.filter((it) => {
          const itSrc = (it.source === 'mobile' || it.source === 'app') ? 'mobile' : 'web';
          return itSrc === state.source;
        });
      }
      if (state.search) {
        const q = state.search.toLowerCase();
        filtered = filtered.filter((it) => {
          const student = state.studentMap.get(it.user_id);
          const nameMatch = student?.full_name?.toLowerCase().includes(q) ?? false;
          const emailMatch = student?.email?.toLowerCase().includes(q) ?? false;
          const phoneMatch = student?.phone?.includes(q) ?? false;
          const userMatch = it.user_id?.toLowerCase().includes(q) ?? false;
          const refMatch = it.transfer_reference?.toLowerCase().includes(q) ?? false;
          return nameMatch || emailMatch || phoneMatch || userMatch || refMatch;
        });
      }

      if (filtered.length === 0) {
        tbody.append(messageRow(FLAT_COLUMNS, t('common.empty')));
        return;
      }
      for (const req of filtered) {
        tbody.append(renderFlatRow(req));
      }
    } else {
      const groups = getFilteredStudentGroups();
      if (groups.length === 0) {
        tbody.append(messageRow(GROUPED_COLUMNS, t('common.empty')));
        return;
      }
      for (const sg of groups) {
        const rows = renderGroupedRow(sg);
        for (const r of rows) tbody.append(r);
      }
    }
  }

  function render() {
    renderRows();
    updateBulkBar();
    if (state.error) {
      showError(banner, state.error, load);
    } else if (state.partialError) {
      clear(banner);
      banner.append(h('span', { class: 'banner-text', text: state.partialError }));
      banner.hidden = false;
    } else {
      hideBanner(banner);
    }
    if (state.viewMode === 'flat') {
      pager.update({ page: state.page, total: state.total, limit: PAGE_SIZE });
    } else {
      pager.update({ page: 1, total: state.items.length, limit: Math.max(1, state.items.length) });
    }
  }

  function groupFlatItems(flatItems) {
    const map = new Map();
    for (const item of flatItems) {
      let sg = map.get(item.user_id);
      if (!sg) {
        sg = { user_id: item.user_id, orders: [] };
        map.set(item.user_id, sg);
      }
      const orderKey = item.order_id || (item.source === 'web' ? 'web-legacy' : 'app-legacy');
      let order = sg.orders.find((o) => (o.order_id || o._key) === orderKey);
      if (!order) {
        order = {
          _key: orderKey,
          order_id: item.order_id || '',
          source: item.source || 'mobile',
          status: item.status,
          created_at: item.created_at,
          payment_method: item.payment_method,
          transfer_reference: item.transfer_reference,
          sender_number: item.sender_number,
          amount_paid: item.amount,
          amount_mismatch: item.amount_warning,
          total: 0,
          items: [],
        };
        sg.orders.push(order);
      }
      order.items.push({
        request_id: item.id || item.request_id,
        id: item.id || item.request_id,
        subject_id: item.subject_id,
        title_ar: item.subject_title_ar || item.title_ar,
        title_en: item.title_en,
        level_name_ar: item.level_name_ar,
        price: item.price || 0,
        status: item.status,
        created_at: item.created_at,
        decided_at: item.decided_at,
        reject_reason: item.reject_reason,
        access_expires_at: item.access_expires_at,
        source: item.source || 'mobile',
      });
      order.total += (item.price || 0);
    }
    return [...map.values()];
  }

  async function joinStudentAccounts(userIDs, currentSeq) {
    if (userIDs.length === 0) {
      state.studentMap = new Map();
      return;
    }
    const map = new Map();
    for (let i = 0; i < userIDs.length; i += 100) {
      const chunk = userIDs.slice(i, i + 100);
      const authRes = await api.get('/api/accounts', { ids: chunk.join(','), limit: 100 });
      if (currentSeq !== seq || !isSignedIn()) return;
      if (authRes.ok && authRes.data && Array.isArray(authRes.data.items)) {
        for (const u of authRes.data.items) {
          map.set(u.id, u);
        }
      }
    }
    state.studentMap = map;
  }

  async function load() {
    const mine = ++seq;
    state.error = null;
    render();

    if (state.viewMode === 'grouped') {
      const q = {};
      if (state.status) q.status = state.status;
      const res = await api.get('/api/request-groups', q);

      if (mine !== seq || !isSignedIn()) return;
      if (!res.ok) {
        if (res.status === 404) {
          // Fallback to flat list query and group it client-side
          const flatRes = await api.get('/api/requests', requestsQuery({
            status: state.status,
            subject_id: state.subject_id,
            page: 1,
            limit: 100,
          }));
          if (mine !== seq || !isSignedIn()) return;
          if (!flatRes.ok) {
            state.error = flatRes;
            render();
            return;
          }
          const flatData = flatRes.data ?? {};
          if (Number.isInteger(flatData.pending_count)) {
            setBadge(doc, 'requests', flatData.pending_count);
          }
          const flatItems = Array.isArray(flatData.items) ? flatData.items : [];
          state.items = groupFlatItems(flatItems);
          state.total = state.items.length;
          state.loaded = true;
          const userIDs = [...new Set(state.items.map((it) => it.user_id).filter(Boolean))];
          await joinStudentAccounts(userIDs, mine);
          render();
          return;
        }
        state.error = res;
        render();
        return;
      }

      const data = res.data ?? {};
      state.items = Array.isArray(data.items) ? data.items : [];
      state.total = Number.isInteger(data.total_students) ? data.total_students : state.items.length;
      state.loaded = true;

      if (Number.isInteger(data.pending_count)) {
        setBadge(doc, 'requests', data.pending_count);
      }

      const userIDs = [...new Set(state.items.map((it) => it.user_id).filter(Boolean))];
      await joinStudentAccounts(userIDs, mine);
      render();
    } else {
      // Flat view
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

      if (Number.isInteger(data.pending_count)) {
        setBadge(doc, 'requests', data.pending_count);
      }

      const userIDs = [...new Set(state.items.map((it) => it.user_id).filter(Boolean))];
      await joinStudentAccounts(userIDs, mine);
      render();
    }
  }

  // --- Single Accept Dialog ---
  function openAccept(req) {
    activeRequest = req;
    inFlight = false;
    acceptCooldown.stop();
    hideBanner(acceptBanner);

    const student = state.studentMap.get(req.user_id);
    const studentName = student ? (student.full_name || student.email || req.user_id) : req.user_id;
    const title = req.subject_title_ar || req.title_ar || '';
    acceptTarget.textContent = `${studentName} · ${title}`;
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
        toast(doc, t('requests.toast.accepted'));
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

  if (acceptCancel) {
    acceptCancel.addEventListener('click', () => {
      if (!inFlight) {
        acceptDialog.close();
        activeRequest = null;
      }
    });
  }

  // --- Single Reject Dialog ---
  function openReject(req) {
    activeRequest = req;
    inFlight = false;
    rejectCooldown.stop();
    rejectReason.value = '';
    hideBanner(rejectBanner);

    const student = state.studentMap.get(req.user_id);
    const studentName = student ? (student.full_name || student.email || req.user_id) : req.user_id;
    const title = req.subject_title_ar || req.title_ar || '';
    rejectTarget.textContent = `${studentName} · ${title}`;

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
        toast(doc, t('requests.toast.rejected'));
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

  // --- Bulk Accept Dialog ---
  function openBulkAccept() {
    inFlight = false;
    bulkAcceptCooldown.stop();
    hideBanner(bulkAcceptBanner);

    const count = state.selectedRequests.size;
    let total = 0;
    for (const it of state.selectedRequests.values()) {
      total += (it.price || 0);
    }

    bulkAcceptTarget.textContent = t('requests.bulk.selected', {
      count: formatNumber(count),
      total: formatNumber(total),
    });
    bulkAcceptNote.textContent = t('requests.bulk.acceptNote', {
      count: formatNumber(count),
      total: formatNumber(total),
    });
    bulkAcceptConfirm.textContent = t('requests.bulk.acceptConfirm', {
      count: formatNumber(count),
    });

    bulkAcceptConfirm.disabled = false;
    bulkAcceptCancel.disabled = false;
    bulkAcceptDialog.showModal();
    bulkAcceptConfirm.focus();
  }

  if (bulkAcceptBtn) {
    bulkAcceptBtn.addEventListener('click', () => {
      if (state.selectedRequests.size === 0) return;
      openBulkAccept();
    });
  }

  if (bulkAcceptForm) {
    bulkAcceptForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (state.selectedRequests.size === 0 || inFlight || bulkAcceptCooldown.active) return;
      inFlight = true;
      bulkAcceptConfirm.disabled = true;
      bulkAcceptCancel.disabled = true;
      hideBanner(bulkAcceptBanner);

      const ids = [...state.selectedRequests.keys()];
      const res = await api.post('/api/requests/bulk', {
        action: 'accept',
        ids,
      });

      inFlight = false;
      bulkAcceptConfirm.disabled = false;
      bulkAcceptCancel.disabled = false;

      if (res.ok && res.data && Array.isArray(res.data.results)) {
        bulkAcceptDialog.close();
        const results = res.data.results;
        const failed = results.filter((r) => r.status === 'failed');
        const accepted = results.filter((r) => r.status === 'accepted');

        if (failed.length === 0) {
          state.selectedRequests.clear();
          updateBulkBar();
          toast(doc, t('requests.bulk.toastAccepted', { count: formatNumber(accepted.length) }));
          load();
          onDecided({ ok: true });
          return;
        }

        // Partial failure
        for (const a of accepted) {
          state.selectedRequests.delete(a.id);
        }
        updateBulkBar();
        if (accepted.length > 0) {
          toast(doc, t('requests.bulk.toastAccepted', { count: formatNumber(accepted.length) }));
        }

        const failDetails = failed.map((f) => {
          const errCode = f.error_code || f.error || 'failed';
          const localizedKey = `err.${errCode}`;
          const localized = t(localizedKey);
          const msg = getLang() === 'ar' ? (f.message_ar || (localized !== localizedKey ? localized : errCode)) : (f.message_en || (localized !== localizedKey ? localized : errCode));
          return `${f.id}: ${msg}`;
        }).join(' · ');
        const messageText = `${t('requests.bulk.partialFailure', { failed: formatNumber(failed.length), total: formatNumber(results.length) })} ${failDetails}`;
        state.partialError = messageText;
        load();
        onDecided({ ok: false, partial: messageText });
        return;
      }

      if (res.kind === 'unauthorized') {
        bulkAcceptDialog.close();
        return;
      }

      bulkAcceptCooldown.arm(res);
      bulkAcceptConfirm.disabled = bulkAcceptCooldown.active;
      showError(bulkAcceptBanner, res, () => bulkAcceptForm.requestSubmit(), bulkAcceptCooldown);
    });
  }

  if (bulkAcceptCancel) {
    bulkAcceptCancel.addEventListener('click', () => {
      if (!inFlight) bulkAcceptDialog.close();
    });
  }

  // --- Bulk Reject Dialog ---
  function openBulkReject() {
    inFlight = false;
    bulkRejectCooldown.stop();
    bulkRejectReason.value = '';
    hideBanner(bulkRejectBanner);

    const count = state.selectedRequests.size;
    let total = 0;
    for (const it of state.selectedRequests.values()) {
      total += (it.price || 0);
    }

    bulkRejectTarget.textContent = t('requests.bulk.selected', {
      count: formatNumber(count),
      total: formatNumber(total),
    });
    bulkRejectNote.textContent = t('requests.bulk.rejectNote', {
      count: formatNumber(count),
      total: formatNumber(total),
    });

    renderBulkRejectState();
    bulkRejectDialog.showModal();
    bulkRejectReason.focus();
  }

  function renderBulkRejectState() {
    const val = bulkRejectReason.value;
    const len = [...val].length;
    bulkRejectHint.textContent = t('dialog.reasonHint', { max: formatNumber(1000) });
    bulkRejectCount.textContent = t('dialog.reasonCount', { n: formatNumber(len), max: formatNumber(1000) });
    const valid = validateReason(val).ok;
    const count = state.selectedRequests.size;
    bulkRejectConfirm.textContent = t('requests.bulk.rejectConfirm', { count: formatNumber(count) });
    bulkRejectConfirm.disabled = inFlight || bulkRejectCooldown.active || !valid;
    bulkRejectCancel.disabled = inFlight;
    bulkRejectReason.disabled = inFlight;
  }

  if (bulkRejectReason) {
    bulkRejectReason.addEventListener('input', renderBulkRejectState);
  }

  if (bulkRejectBtn) {
    bulkRejectBtn.addEventListener('click', () => {
      if (state.selectedRequests.size === 0) return;
      openBulkReject();
    });
  }

  if (bulkRejectForm) {
    bulkRejectForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      if (state.selectedRequests.size === 0 || inFlight || bulkRejectCooldown.active) return;
      const valid = validateReason(bulkRejectReason.value);
      if (!valid.ok) return;

      inFlight = true;
      renderBulkRejectState();
      hideBanner(bulkRejectBanner);

      const ids = [...state.selectedRequests.keys()];
      const res = await api.post('/api/requests/bulk', {
        action: 'reject',
        ids,
        reason: valid.value,
      });

      inFlight = false;
      renderBulkRejectState();

      if (res.ok && res.data && Array.isArray(res.data.results)) {
        bulkRejectDialog.close();
        const results = res.data.results;
        const failed = results.filter((r) => r.status === 'failed');
        const rejected = results.filter((r) => r.status === 'rejected');

        if (failed.length === 0) {
          state.selectedRequests.clear();
          updateBulkBar();
          toast(doc, t('requests.bulk.toastRejected', { count: formatNumber(rejected.length) }));
          load();
          onDecided({ ok: true });
          return;
        }

        // Partial failure
        for (const a of rejected) {
          state.selectedRequests.delete(a.id);
        }
        updateBulkBar();
        if (rejected.length > 0) {
          toast(doc, t('requests.bulk.toastRejected', { count: formatNumber(rejected.length) }));
        }

        const failDetails = failed.map((f) => {
          const errCode = f.error_code || f.error || 'failed';
          const localizedKey = `err.${errCode}`;
          const localized = t(localizedKey);
          const msg = getLang() === 'ar' ? (f.message_ar || (localized !== localizedKey ? localized : errCode)) : (f.message_en || (localized !== localizedKey ? localized : errCode));
          return `${f.id}: ${msg}`;
        }).join(' · ');
        const messageText = `${t('requests.bulk.partialFailure', { failed: formatNumber(failed.length), total: formatNumber(results.length) })} ${failDetails}`;
        state.partialError = messageText;
        load();
        onDecided({ ok: false, partial: messageText });
        return;
      }

      if (res.kind === 'unauthorized') {
        bulkRejectDialog.close();
        return;
      }

      bulkRejectCooldown.arm(res);
      renderBulkRejectState();
      showError(bulkRejectBanner, res, () => bulkRejectForm.requestSubmit(), bulkRejectCooldown);
    });
  }

  if (bulkRejectCancel) {
    bulkRejectCancel.addEventListener('click', () => {
      if (!inFlight) bulkRejectDialog.close();
    });
  }

  if (bulkClearBtn) {
    bulkClearBtn.addEventListener('click', () => {
      state.selectedRequests.clear();
      updateBulkBar();
      renderSelectionState();
    });
  }

  if (acceptDialog) keepOpenWhile(acceptDialog, () => inFlight);
  if (rejectDialog) keepOpenWhile(rejectDialog, () => inFlight);
  if (bulkAcceptDialog) keepOpenWhile(bulkAcceptDialog, () => inFlight);
  if (bulkRejectDialog) keepOpenWhile(bulkRejectDialog, () => inFlight);

  // Filter form controls
  if (viewModeSelect) {
    viewModeSelect.addEventListener('change', () => {
      state.viewMode = viewModeSelect.value;
      state.page = 1;
      state.selectedRequests.clear();
      updateBulkBar();
      load();
    });
  }

  if (sourceSelect) {
    sourceSelect.addEventListener('change', () => {
      state.source = sourceSelect.value;
      if (state.viewMode === 'flat') {
        state.page = 1;
        load();
      } else {
        render();
      }
    });
  }

  if (searchInput) {
    searchInput.addEventListener('input', () => {
      state.search = searchInput.value.trim();
      render();
    });
  }

  if (form) {
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      state.viewMode = viewModeSelect ? viewModeSelect.value : 'grouped';
      state.status = statusSelect ? statusSelect.value : '';
      state.source = sourceSelect ? sourceSelect.value : '';
      state.subject_id = subjectSelect ? subjectSelect.value : '';
      state.search = searchInput ? searchInput.value.trim() : '';
      state.partialError = null;
      state.page = 1;
      load();
    });
  }

  if (resetButton) {
    resetButton.addEventListener('click', () => {
      if (viewModeSelect) viewModeSelect.value = 'grouped';
      if (statusSelect) statusSelect.value = 'pending';
      if (sourceSelect) sourceSelect.value = '';
      if (subjectSelect) subjectSelect.value = '';
      if (searchInput) searchInput.value = '';
      state.viewMode = 'grouped';
      state.status = 'pending';
      state.source = '';
      state.subject_id = '';
      state.search = '';
      state.partialError = null;
      state.page = 1;
      state.selectedRequests.clear();
      state.expandedStudents.clear();
      updateBulkBar();
      load();
    });
  }

  /** Opens the bulk confirm dialog for exactly these requests (replaces the selection). */
  function decideRequests(action, requests) {
    const list = Array.isArray(requests) ? requests.filter((r) => r && r.id) : [];
    if (list.length === 0) return;
    state.selectedRequests.clear();
    for (const r of list) {
      state.selectedRequests.set(r.id, { id: r.id, price: r.price || 0, user_id: r.user_id, title: r.title });
    }
    updateBulkBar();
    if (action === 'accept') openBulkAccept();
    else if (action === 'reject') openBulkReject();
  }

  return {
    load() {
      loadSubjectsFilter();
      return load();
    },
    /**
     * Accepts or rejects the given requests ([{ id, price, user_id, title }])
     * through the same bulk confirm dialogs the tab uses (the overview's
     * inline buttons). It replaces the current selection with these requests.
     */
    decide: decideRequests,
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
      if (bulkAcceptDialog && bulkAcceptDialog.open) bulkAcceptDialog.close();
      if (bulkRejectDialog && bulkRejectDialog.open) bulkRejectDialog.close();
      Object.assign(state, {
        viewMode: 'grouped',
        status: 'pending',
        source: '',
        subject_id: '',
        search: '',
        partialError: null,
        page: 1,
        total: 0,
        items: [],
        studentMap: new Map(),
        expandedStudents: new Set(),
        selectedRequests: new Map(),
        loaded: false,
        error: null,
      });
      if (viewModeSelect) viewModeSelect.value = 'grouped';
      if (statusSelect) statusSelect.value = 'pending';
      if (sourceSelect) sourceSelect.value = '';
      if (subjectSelect) subjectSelect.value = '';
      if (searchInput) searchInput.value = '';
      updateBulkBar();
      render();
    },
  };
}
