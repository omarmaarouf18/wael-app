import { api as defaultApi } from './api.js';
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
  const submitBtn = doc.getElementById('settings-submit');
  const metaSpan = doc.getElementById('settings-meta');

  let loadedState = null;
  let inFlight = false;
  const cooldown = createCooldown();

  cooldown.subscribe((left) => {
    if (left === 0 && !inFlight) {
      if (submitBtn) submitBtn.disabled = false;
    } else if (left > 0) {
      if (submitBtn) submitBtn.disabled = true;
    }
  });

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
      current.center_map_url !== (loadedState.center_map_url ?? '')
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
