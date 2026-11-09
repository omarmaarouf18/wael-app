import { api as defaultApi } from './api.js';
import { clear, h } from './dom.js';
import { formatCairoDateTime, t } from './i18n.js';
import { createCooldown, hideBanner, showError, toast } from './ui.js';
import { trackDirty } from './unsaved.js';

export function mountSettings({ api = defaultApi, doc = document } = {}) {
  const panel = doc.getElementById('panel-settings');
  const banner = doc.getElementById('settings-banner');
  const form = doc.getElementById('settings-form');
  const showPrices = doc.getElementById('settings-show-prices');
  const supportWhatsApp = doc.getElementById('settings-support-whatsapp');
  const centerNameAr = doc.getElementById('settings-center-name-ar');
  const centerNameEn = doc.getElementById('settings-center-name-en');
  const centerAddressAr = doc.getElementById('settings-center-address-ar');
  const centerAddressEn = doc.getElementById('settings-center-address-en');
  const centerHoursAr = doc.getElementById('settings-center-hours-ar');
  const centerHoursEn = doc.getElementById('settings-center-hours-en');
  const centerMapUrl = doc.getElementById('settings-center-map-url');
  const teacherNameAr = doc.getElementById('settings-teacher-name-ar');
  const teacherNameEn = doc.getElementById('settings-teacher-name-en');
  const teacherNameArCount = doc.getElementById('settings-teacher-name-ar-count');
  const teacherNameEnCount = doc.getElementById('settings-teacher-name-en-count');
  const teacherTitlesAr = doc.getElementById('settings-teacher-titles-ar');
  const teacherTitlesEn = doc.getElementById('settings-teacher-titles-en');
  const teacherTitlesArCount = doc.getElementById('settings-teacher-titles-ar-count');
  const teacherTitlesEnCount = doc.getElementById('settings-teacher-titles-en-count');
  const teacherBioAr = doc.getElementById('settings-teacher-bio-ar');
  const teacherBioEn = doc.getElementById('settings-teacher-bio-en');
  const teacherBioArCount = doc.getElementById('settings-teacher-bio-ar-count');
  const teacherBioEnCount = doc.getElementById('settings-teacher-bio-en-count');
  const teacherPreviewName = doc.getElementById('teacher-preview-name');
  const teacherPreviewTitles = doc.getElementById('teacher-preview-titles');
  const teacherPreviewBio = doc.getElementById('teacher-preview-bio');
  const paymentMethodsContainer = doc.getElementById('settings-payment-methods');
  const addPaymentMethodBtn = doc.getElementById('settings-add-payment-method');
  const submitBtn = doc.getElementById('settings-submit');
  const metaSpan = doc.getElementById('settings-meta');

  let loadedState = null;
  let inFlight = false;
  const cooldown = createCooldown();

  function updateTeacherFieldCounts() {
    if (teacherNameArCount && teacherNameAr) teacherNameArCount.textContent = String(teacherNameAr.value.length);
    if (teacherNameEnCount && teacherNameEn) teacherNameEnCount.textContent = String(teacherNameEn.value.length);
    if (teacherTitlesArCount && teacherTitlesAr) teacherTitlesArCount.textContent = String(teacherTitlesAr.value.length);
    if (teacherTitlesEnCount && teacherTitlesEn) teacherTitlesEnCount.textContent = String(teacherTitlesEn.value.length);
    if (teacherBioArCount && teacherBioAr) teacherBioArCount.textContent = String(teacherBioAr.value.length);
    if (teacherBioEnCount && teacherBioEn) teacherBioEnCount.textContent = String(teacherBioEn.value.length);
  }

  function updateTeacherPreview() {
    updateTeacherFieldCounts();
    if (!teacherPreviewName) return;

    const isEn = (doc.documentElement?.lang === 'en');
    const nameAr = (teacherNameAr?.value ?? '').trim();
    const nameEn = (teacherNameEn?.value ?? '').trim();
    const titlesAr = (teacherTitlesAr?.value ?? '').trim();
    const titlesEn = (teacherTitlesEn?.value ?? '').trim();
    const bioAr = (teacherBioAr?.value ?? '').trim();
    const bioEn = (teacherBioEn?.value ?? '').trim();

    const defaultName = isEn ? 'Wael El Saeed' : 'وائل السعيد';
    const effectiveName = isEn
      ? (nameEn || nameAr || defaultName)
      : (nameAr || nameEn || defaultName);

    const effectiveTitles = isEn
      ? (titlesEn || titlesAr)
      : (titlesAr || titlesEn);

    const effectiveBio = isEn
      ? (bioEn || bioAr)
      : (bioAr || bioEn);

    teacherPreviewName.textContent = effectiveName;

    if (teacherPreviewTitles) {
      teacherPreviewTitles.textContent = effectiveTitles;
      teacherPreviewTitles.hidden = !effectiveTitles;
    }

    if (teacherPreviewBio) {
      teacherPreviewBio.textContent = effectiveBio;
      teacherPreviewBio.hidden = !effectiveBio;
    }
  }

  const teacherInputs = [
    teacherNameAr, teacherNameEn,
    teacherTitlesAr, teacherTitlesEn,
    teacherBioAr, teacherBioEn,
  ];
  for (const input of teacherInputs) {
    input?.addEventListener('input', updateTeacherPreview);
  }

  cooldown.subscribe((left) => {
    if (left === 0 && !inFlight) {
      if (submitBtn) submitBtn.disabled = false;
    } else if (left > 0) {
      if (submitBtn) submitBtn.disabled = true;
    }
  });

  function getPaymentMethodsValues() {
    if (!paymentMethodsContainer) return [];
    const rows = paymentMethodsContainer.children ?? [];
    const result = [];
    for (const r of rows) {
      if (!r || !r.children) continue;
      const kindSelect = r.children[0];
      const labelInput = r.children[1];
      const numberInput = r.children[2];
      const kind = (kindSelect?.value ?? 'wallet').trim();
      const label = (labelInput?.value ?? '').trim();
      const numAddr = (numberInput?.value ?? '').trim();
      if (label || numAddr) {
        result.push({ kind, label, number_or_address: numAddr });
      }
    }
    return result;
  }

  function createPaymentMethodRow(item = { kind: 'wallet', label: '', number_or_address: '' }) {
    const kindSelect = h('select', { class: 'payment-kind' },
      h('option', { text: t('settings.paymentKindWallet'), attrs: { value: 'wallet' } }),
      h('option', { text: t('settings.paymentKindInstapay'), attrs: { value: 'instapay' } }),
    );
    kindSelect.value = item.kind || 'wallet';

    const labelInput = h('input', {
      class: 'payment-label',
      attrs: { type: 'text', maxlength: 100, autocomplete: 'off', placeholder: t('settings.paymentLabel') },
    });
    labelInput.value = item.label || '';

    const numberInput = h('input', {
      class: 'payment-number',
      attrs: { type: 'text', dir: 'ltr', maxlength: 100, autocomplete: 'off', placeholder: t('settings.paymentNumberOrAddress') },
    });
    numberInput.value = item.number_or_address || '';

    const removeBtn = h('button', {
      class: 'btn small danger-outline',
      text: t('settings.removePaymentMethod'),
      attrs: { type: 'button' },
      on: {
        click: () => {
          row.remove();
        },
      },
    });

    const row = h('div', { class: 'settings-grid settings-payment-row' },
      kindSelect,
      labelInput,
      numberInput,
      removeBtn,
    );
    return row;
  }

  function renderPaymentMethods(methods = []) {
    if (!paymentMethodsContainer) return;
    clear(paymentMethodsContainer);
    for (const m of methods) {
      paymentMethodsContainer.append(createPaymentMethodRow(m));
    }
  }

  addPaymentMethodBtn?.addEventListener('click', () => {
    if (!paymentMethodsContainer) return;
    paymentMethodsContainer.append(createPaymentMethodRow());
  });

  function getTeacherProfileValues() {
    return {
      name_ar: (teacherNameAr?.value ?? '').trim(),
      name_en: (teacherNameEn?.value ?? '').trim(),
      titles_ar: (teacherTitlesAr?.value ?? '').trim(),
      titles_en: (teacherTitlesEn?.value ?? '').trim(),
      bio_ar: (teacherBioAr?.value ?? '').trim(),
      bio_en: (teacherBioEn?.value ?? '').trim(),
    };
  }

  function getFormValues() {
    return {
      show_prices: Boolean(showPrices?.checked),
      support_whatsapp: (supportWhatsApp?.value ?? '').trim(),
      center_name_ar: (centerNameAr?.value ?? '').trim(),
      center_name_en: (centerNameEn?.value ?? '').trim(),
      center_address_ar: (centerAddressAr?.value ?? '').trim(),
      center_address_en: (centerAddressEn?.value ?? '').trim(),
      center_hours_ar: (centerHoursAr?.value ?? '').trim(),
      center_hours_en: (centerHoursEn?.value ?? '').trim(),
      center_map_url: (centerMapUrl?.value ?? '').trim(),
      payment_methods: getPaymentMethodsValues(),
      teacher_profile: getTeacherProfileValues(),
    };
  }

  function applyState(state) {
    if (!state) return;
    if (showPrices) showPrices.checked = Boolean(state.show_prices);
    if (supportWhatsApp) supportWhatsApp.value = state.support_whatsapp ?? '';
    if (centerNameAr) centerNameAr.value = state.center_name_ar ?? '';
    if (centerNameEn) centerNameEn.value = state.center_name_en ?? '';
    if (centerAddressAr) centerAddressAr.value = state.center_address_ar ?? '';
    if (centerAddressEn) centerAddressEn.value = state.center_address_en ?? '';
    if (centerHoursAr) centerHoursAr.value = state.center_hours_ar ?? '';
    if (centerHoursEn) centerHoursEn.value = state.center_hours_en ?? '';
    if (centerMapUrl) centerMapUrl.value = state.center_map_url ?? '';
    renderPaymentMethods(state.payment_methods ?? []);

    const tp = state.teacher_profile ?? {};
    if (teacherNameAr) teacherNameAr.value = tp.name_ar ?? '';
    if (teacherNameEn) teacherNameEn.value = tp.name_en ?? '';
    if (teacherTitlesAr) teacherTitlesAr.value = tp.titles_ar ?? '';
    if (teacherTitlesEn) teacherTitlesEn.value = tp.titles_en ?? '';
    if (teacherBioAr) teacherBioAr.value = tp.bio_ar ?? '';
    if (teacherBioEn) teacherBioEn.value = tp.bio_en ?? '';
    updateTeacherPreview();

    updateMeta(state);
  }

  function updateMeta(state) {
    if (!metaSpan) return;
    if (state?.updated_at) {
      const timeStr = formatCairoDateTime(state.updated_at);
      metaSpan.textContent = t('settings.updatedAt', { time: timeStr });
    } else {
      metaSpan.textContent = '';
    }
  }

  function paymentMethodsEqual(a, b) {
    if (!a && !b) return true;
    if (!a || !b) return false;
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (a[i].kind !== b[i].kind || a[i].label !== b[i].label || a[i].number_or_address !== b[i].number_or_address) {
        return false;
      }
    }
    return true;
  }

  function teacherProfileEqual(a, b) {
    const tpA = a ?? {};
    const tpB = b ?? {};
    return (
      (tpA.name_ar ?? '') === (tpB.name_ar ?? '') &&
      (tpA.name_en ?? '') === (tpB.name_en ?? '') &&
      (tpA.titles_ar ?? '') === (tpB.titles_ar ?? '') &&
      (tpA.titles_en ?? '') === (tpB.titles_en ?? '') &&
      (tpA.bio_ar ?? '') === (tpB.bio_ar ?? '') &&
      (tpA.bio_en ?? '') === (tpB.bio_en ?? '')
    );
  }

  function isDirty() {
    if (!loadedState) return false;
    const current = getFormValues();
    return (
      current.show_prices !== Boolean(loadedState.show_prices) ||
      current.support_whatsapp !== (loadedState.support_whatsapp ?? '') ||
      current.center_name_ar !== (loadedState.center_name_ar ?? '') ||
      current.center_name_en !== (loadedState.center_name_en ?? '') ||
      current.center_address_ar !== (loadedState.center_address_ar ?? '') ||
      current.center_address_en !== (loadedState.center_address_en ?? '') ||
      current.center_hours_ar !== (loadedState.center_hours_ar ?? '') ||
      current.center_hours_en !== (loadedState.center_hours_en ?? '') ||
      current.center_map_url !== (loadedState.center_map_url ?? '') ||
      !paymentMethodsEqual(current.payment_methods, loadedState.payment_methods ?? []) ||
      !teacherProfileEqual(current.teacher_profile, loadedState.teacher_profile)
    );
  }

  trackDirty('settings', isDirty);

  async function load() {
    if (banner) hideBanner(banner);
    const res = await api.get('/api/settings');
    if (!res.ok) {
      if (banner) showError(banner, res, () => load(), cooldown);
      return;
    }
    loadedState = res.data ?? {};
    applyState(loadedState);
  }

  function discard() {
    if (loadedState) {
      applyState(loadedState);
    }
  }

  async function save() {
    if (inFlight || cooldown.active) return;
    const values = getFormValues();
    inFlight = true;
    if (submitBtn) {
      submitBtn.disabled = true;
      submitBtn.textContent = t('settings.saving');
    }
    if (banner) hideBanner(banner);

    try {
      const res = await api.post('/api/settings/update', values);
      if (!res.ok) {
        cooldown.arm(res);
        // Retry only helps when the failure was the server or the network
        // (503, offline) or a rate limit; a validation error (4xx such as
        // settings_forbidden_word) needs an edit, so it gets no Retry.
        const retryable = res.kind === 'unavailable' || res.kind === 'rate_limited';
        if (banner) showError(banner, res, retryable ? () => save() : undefined, cooldown);
        return;
      }
      loadedState = {
        ...values,
        updated_at: res.data?.updated_at ?? new Date().toISOString(),
        updated_by: res.data?.updated_by,
      };
      updateMeta(loadedState);
      toast(doc, t('settings.toast.saved'));
    } finally {
      inFlight = false;
      if (submitBtn) {
        submitBtn.disabled = cooldown.active;
        submitBtn.textContent = t('settings.save');
      }
    }
  }

  form?.addEventListener('submit', (e) => {
    e.preventDefault();
    save();
  });

  function rerender() {
    if (loadedState) {
      updateMeta(loadedState);
    }
    if (submitBtn && !inFlight) {
      submitBtn.textContent = t('settings.save');
    }
    updateTeacherPreview();
  }

  function reset() {
    loadedState = null;
    inFlight = false;
    cooldown.stop();
    if (showPrices) showPrices.checked = false;
    if (supportWhatsApp) supportWhatsApp.value = '';
    if (centerNameAr) centerNameAr.value = '';
    if (centerNameEn) centerNameEn.value = '';
    if (centerAddressAr) centerAddressAr.value = '';
    if (centerAddressEn) centerAddressEn.value = '';
    if (centerHoursAr) centerHoursAr.value = '';
    if (centerHoursEn) centerHoursEn.value = '';
    if (centerMapUrl) centerMapUrl.value = '';
    if (teacherNameAr) teacherNameAr.value = '';
    if (teacherNameEn) teacherNameEn.value = '';
    if (teacherTitlesAr) teacherTitlesAr.value = '';
    if (teacherTitlesEn) teacherTitlesEn.value = '';
    if (teacherBioAr) teacherBioAr.value = '';
    if (teacherBioEn) teacherBioEn.value = '';
    updateTeacherPreview();
    renderPaymentMethods([]);
    if (metaSpan) metaSpan.textContent = '';
    if (banner) hideBanner(banner);
  }

  return {
    load,
    save,
    discard,
    isDirty,
    rerender,
    reset,
  };
}
