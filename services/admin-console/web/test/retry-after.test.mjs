// Retry-After on a 429: the api reads it, banners count down ("try again in
// N seconds") and the action stays disabled until the wait ends. Time is
// mocked, so the countdown runs without waiting.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createApi, parseRetryAfter, RETRY_AFTER_MAX } from '../js/api.js';
import { FakeDocument, fakeWindow, flush, makeFetch } from './fake-dom.mjs';

const { main } = await import('../js/app.js');
const { isSignedIn } = await import('../js/auth.js');
const { MESSAGES } = await import('../js/i18n.js');

const doc = new FakeDocument();
const win = fakeWindow('');
globalThis.document = doc;

const LEVELS = [
  { key: 'bachelor-y1', study_type: 'bachelor', name_ar: 'الفرقة الأولى', name_en: '', order: 1, published: true, subject_count: 1, published_subject_count: 1 },
  { key: 'dip-a', study_type: 'diploma', name_ar: 'دبلومة الاختبار', name_en: '', order: 0, published: false, subject_count: 0, published_subject_count: 0 },
];

const base = () => ({
  'GET /api/whoami': { body: { name: 'Wael' } },
  'GET /api/accounts': { body: { items: [], total: 0 } },
  'GET /api/requests': { body: { items: [], total: 0, pending_count: 0 } },
  'GET /api/levels': { body: { levels: LEVELS } },
});
const limited = (seconds) => ({ status: 429, body: { code: 'rate_limited' }, headers: seconds === null ? {} : { 'Retry-After': seconds } });

const $ = (id) => doc.getElementById(id);
const t = (key) => MESSAGES.ar[key];
const waitText = (n) => t('err.retryAfter').replace('{n}', new Intl.NumberFormat('ar-EG').format(n));
const buttonsIn = (el) => el.findAll((c) => c.tagName === 'BUTTON');
const buttonByText = (root, text) => buttonsIn(root).find((b) => b.textContent === text);
const dialogs = () => $('catalog-dialogs').children.filter((c) => c.tagName === 'DIALOG');
const levelCard = (index) => $('catalog-content').findAll((c) => c.className === 'card level-card')[index];
const postsTo = (path) => globalThis.fetch.calls.filter((c) => c.method === 'POST' && c.path === path);

async function signIn(extra = {}) {
  globalThis.fetch = makeFetch({ ...base(), ...extra });
  $('login-token').value = 'admin-token-xyz';
  $('login-form').fire('submit');
  await flush();
  assert.equal(isSignedIn(), true);
}

test('parseRetryAfter accepts whole seconds only and caps long waits', () => {
  assert.equal(parseRetryAfter('30'), 30);
  assert.equal(parseRetryAfter(' 7 '), 7);
  assert.equal(parseRetryAfter('1'), 1);
  assert.equal(parseRetryAfter('0'), null);
  assert.equal(parseRetryAfter('-5'), null);
  assert.equal(parseRetryAfter('1.5'), null);
  assert.equal(parseRetryAfter('Wed, 21 Oct 2026 07:28:00 GMT'), null);
  assert.equal(parseRetryAfter(''), null);
  assert.equal(parseRetryAfter(null), null);
  assert.equal(parseRetryAfter(undefined), null);
  assert.equal(parseRetryAfter('999999'), RETRY_AFTER_MAX);
});

test('the api result carries retryAfter on a 429 only', async () => {
  const run = async (status, headers) => {
    const api = createApi({
      fetchFn: makeFetch({ 'POST /api/x': { status, body: { code: 'x' }, headers } }),
      getToken: () => 'tok',
      onUnauthorized: () => {},
    });
    return api.post('/api/x', {});
  };
  assert.equal((await run(429, { 'Retry-After': '12' })).retryAfter, 12);
  assert.equal((await run(429, {})).retryAfter, null);
  assert.equal((await run(429, { 'Retry-After': 'soon' })).retryAfter, null);
  assert.equal((await run(503, { 'Retry-After': '12' })).retryAfter, undefined);
});

main(doc, win);

test('a catalog form counts down after a 429 and enables Save again when it ends', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  try {
    await signIn({ 'POST /api/levels/update': limited('3') });
    $('tab-catalog').click();
    await flush();
    buttonByText(levelCard(1), t('catalog.edit')).click();
    const dialog = dialogs().find((d) => d.open);
    const save = buttonByText(dialog, t('catalog.save'));
    save.click?.();
    dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
    await flush();
    assert.equal(postsTo('/api/levels/update').length, 1);
    const banner = dialog.findAll((c) => c.className === 'banner')[0];
    assert.equal(banner.hidden, false);
    assert.ok(banner.textContent.includes(waitText(3)), banner.textContent);
    assert.equal(buttonByText(dialog, t('catalog.save')).disabled, true, 'Save is off during the wait');
    assert.equal(buttonByText(banner, t('common.retry')).disabled, true, 'Retry is off during the wait');

    // Enter inside the form must not send a second request either.
    dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
    await flush();
    assert.equal(postsTo('/api/levels/update').length, 1);

    ctx.mock.timers.tick(1000);
    assert.ok(banner.textContent.includes(waitText(2)), banner.textContent);
    ctx.mock.timers.tick(2000);
    assert.equal(banner.textContent.includes(waitText(1)), false);
    assert.ok(banner.textContent.includes(t('err.rate_limited')), 'the normal message returns');
    assert.equal(buttonByText(dialog, t('catalog.save')).disabled, false);
    assert.equal(buttonByText(banner, t('common.retry')).disabled, false);
    dialog.close();
  } finally {
    ctx.mock.timers.reset();
  }
});

test('a 429 without Retry-After keeps the plain message and the button enabled', async () => {
  await signIn({ 'POST /api/levels/update': limited(null) });
  buttonByText(levelCard(1), t('catalog.edit')).click();
  const dialog = dialogs().find((d) => d.open);
  dialog.findAll((c) => c.tagName === 'FORM')[0].fire('submit');
  await flush();
  const banner = dialog.findAll((c) => c.className === 'banner')[0];
  assert.ok(banner.textContent.includes(t('err.rate_limited')));
  assert.equal(buttonByText(dialog, t('catalog.save')).disabled, false);
  dialog.close();
});

test('a publish button is off during the wait and a list load shows the countdown', async (ctx) => {
  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  try {
    await signIn({ 'POST /api/levels/update': limited('2') });
    const publish = () => buttonByText(levelCard(1), t('catalog.publish'));
    publish().click();
    await flush();
    assert.equal(postsTo('/api/levels/update').length, 1);
    assert.equal(publish().disabled, true);
    assert.ok($('catalog-banner').textContent.includes(waitText(2)));
    publish().click();
    await flush();
    assert.equal(postsTo('/api/levels/update').length, 1, 'no request while waiting');
    ctx.mock.timers.tick(2000);
    assert.equal(publish().disabled, false);
  } finally {
    ctx.mock.timers.reset();
  }

  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  try {
    await signIn({ 'GET /api/accounts': limited('4') });
    $('tab-accounts').click();
    await flush();
    const banner = $('accounts-banner');
    assert.ok(banner.textContent.includes(waitText(4)), banner.textContent);
    assert.equal(buttonByText(banner, t('common.retry')).disabled, true);
    ctx.mock.timers.tick(4000);
    assert.equal(buttonByText(banner, t('common.retry')).disabled, false);
  } finally {
    ctx.mock.timers.reset();
  }
});

test('a rate-limited sign-in disables the button until the wait ends', async (ctx) => {
  // Sign out first: stopping the idle lock clears real timers, which the mocked clearTimeout would mishandle.
  $('sign-out').click();
  await flush();
  ctx.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  try {
    globalThis.fetch = makeFetch({ 'GET /api/whoami': limited('5') });
    $('login-token').value = 'admin-token-xyz';
    $('login-form').fire('submit');
    await flush();
    assert.equal(isSignedIn(), false);
    assert.ok($('login-banner').textContent.includes(waitText(5)), $('login-banner').textContent);
    assert.equal($('login-submit').disabled, true);
    $('login-form').fire('submit');
    await flush();
    assert.equal(globalThis.fetch.calls.length, 1, 'no second attempt while waiting');
    ctx.mock.timers.tick(5000);
    assert.equal($('login-submit').disabled, false);
  } finally {
    ctx.mock.timers.reset();
  }
});
