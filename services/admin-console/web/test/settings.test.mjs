import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, flush, makeFetch } from './fake-dom.mjs';

const doc = new FakeDocument();
globalThis.document = doc;

const { mountSettings } = await import('../js/settings.js');
const { createApi } = await import('../js/api.js');
const { MESSAGES } = await import('../js/i18n.js');

function setup(initialSettings = {}, routes = {}) {
  // Create mock DOM elements needed by settings
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
  const toastBox = doc.getElementById('toast');

  let lastSentUpdate = null;

  const defaultRoutes = {
    'GET /api/settings': {
      body: {
        show_prices: false,
        support_whatsapp: '201000000000',
        center_name_ar: 'المركز الأصلي',
        center_name_en: 'Original Center',
        center_address_ar: 'العنوان',
        center_address_en: 'Address',
        center_hours_ar: '٩ ص - ٩ م',
        center_hours_en: '9 AM - 9 PM',
        center_map_url: 'https://maps.google.com/?q=test',
        updated_at: '2026-10-08T10:00:00Z',
        updated_by: 'admin-1',
        ...initialSettings,
      },
    },
    'POST /api/settings/update': (req) => {
      lastSentUpdate = req.init?.body ? JSON.parse(req.init.body) : null;
      return {
        body: {
          ...(lastSentUpdate ?? {}),
          updated_at: '2026-10-08T10:30:00Z',
          updated_by: 'admin-1',
        },
      };
    },
    ...routes,
  };

  const fetchFn = makeFetch(defaultRoutes);
  // The real api client over the fake fetch, so the test sees exactly the
  // { ok, status, data, code, retryAfter } shape the console uses.
  const api = createApi({ fetchFn, getToken: () => 'test-token', onUnauthorized: () => {} });

  const mod = mountSettings({ api, doc });
  return {
    doc,
    mod,
    elements: {
      panel,
      banner,
      form,
      showPrices,
      supportWhatsApp,
      centerNameAr,
      centerNameEn,
      centerAddressAr,
      centerAddressEn,
      centerHoursAr,
      centerHoursEn,
      centerMapUrl,
      submitBtn,
      metaSpan,
      toastBox,
    },
    getLastSentUpdate: () => lastSentUpdate,
  };
}

test('load populates form fields and formatted meta time', async () => {
  const { mod, elements } = setup({
    show_prices: false,
    support_whatsapp: '201012345678',
    center_name_ar: 'مركز النور',
    updated_at: '2026-10-08T12:00:00Z',
  });

  await mod.load();

  assert.equal(elements.showPrices.checked, false);
  assert.equal(elements.supportWhatsApp.value, '201012345678');
  assert.equal(elements.centerNameAr.value, 'مركز النور');
  assert.ok(elements.metaSpan.textContent.length > 0);
  assert.equal(elements.banner.hidden, true);
  assert.equal(mod.isDirty(), false);
});

test('toggle show_prices makes form dirty and save sends updated boolean', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout'] });
  const { mod, elements, getLastSentUpdate } = setup({ show_prices: false });

  await mod.load();
  assert.equal(mod.isDirty(), false);

  elements.showPrices.checked = true;
  assert.equal(mod.isDirty(), true);

  await mod.save();
  assert.equal(mod.isDirty(), false);

  const sent = getLastSentUpdate();
  assert.ok(sent);
  assert.equal(sent.show_prices, true);
  assert.equal(elements.toastBox.hidden, false);
  assert.ok(elements.toastBox.textContent.includes(MESSAGES.ar['settings.toast.saved']));
});

test('discard resets form values and restores clean state', async () => {
  const { mod, elements } = setup({
    show_prices: false,
    support_whatsapp: '201000000000',
  });

  await mod.load();
  elements.showPrices.checked = true;
  elements.supportWhatsApp.value = '201999999999';
  assert.equal(mod.isDirty(), true);

  mod.discard();
  assert.equal(elements.showPrices.checked, false);
  assert.equal(elements.supportWhatsApp.value, '201000000000');
  assert.equal(mod.isDirty(), false);
});

test('server error code is displayed in banner using mapped localized text', async () => {
  const { mod, elements } = setup(
    {},
    {
      'POST /api/settings/update': {
        status: 400,
        body: { error: 'validation failed', code: 'settings_forbidden_word' },
      },
    },
  );

  await mod.load();
  elements.centerNameAr.value = 'دفع كاش';
  await mod.save();

  assert.equal(elements.banner.hidden, false);
  assert.ok(elements.banner.textContent.includes(MESSAGES.ar['err.settings_forbidden_word']));
  // A validation error needs an edit, not a retry.
  assert.ok(!elements.banner.textContent.includes(MESSAGES.ar['common.retry']));
});

test('a 503 or network failure on save keeps the Retry button', async () => {
  for (const failure of [{ status: 503, body: { error: 'x', code: 'service_unavailable' } }, new Error('offline')]) {
    const { mod, elements } = setup({}, { 'POST /api/settings/update': failure });
    await mod.load();
    elements.centerNameAr.value = 'مركز جديد';
    await mod.save();
    assert.equal(elements.banner.hidden, false);
    assert.ok(elements.banner.textContent.includes(MESSAGES.ar['common.retry']));
  }
});

test('double submit guard prevents duplicate save requests while in flight', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout'] });
  let postCount = 0;
  let gateResolve;
  const gatePromise = new Promise((resolve) => {
    gateResolve = resolve;
  });

  const { mod } = setup(
    {},
    {
      'POST /api/settings/update': async () => {
        postCount++;
        await gatePromise;
        return { body: { ok: true } };
      },
    },
  );

  await mod.load();
  const p1 = mod.save();
  const p2 = mod.save();
  gateResolve();
  await Promise.all([p1, p2]);

  assert.equal(postCount, 1);
});

test('retry-after 429 arms cooldown and keeps save button disabled', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const { mod, elements } = setup(
    {},
    {
      'POST /api/settings/update': {
        status: 429,
        headers: { 'Retry-After': '10' },
        body: { error: 'rate limited', code: 'rate_limited' },
      },
    },
  );

  await mod.load();
  await mod.save();

  assert.equal(elements.submitBtn.disabled, true);
  assert.equal(elements.banner.hidden, false);
});
