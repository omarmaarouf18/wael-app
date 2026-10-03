import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  DEFAULT_LANG, LANGS, MESSAGES, applyTranslations, dirOf, formatDate, formatTime, getLang, langFromHash, onLangChange, otherLang, setLang, t,
} from '../js/i18n.js';
import { TABS, formatBadge, isTabEnabled, nextTabIndex, setBadge, visibleTabs } from '../js/tabs.js';
import { pageCount } from '../js/ui.js';
import { FakeDocument, FakeElement } from './fake-dom.mjs';

test('Arabic is the default, right-to-left; English is left-to-right', () => {
  assert.equal(DEFAULT_LANG, 'ar');
  assert.deepEqual([...LANGS], ['ar', 'en']);
  assert.equal(dirOf('ar'), 'rtl');
  assert.equal(dirOf('en'), 'ltr');
  assert.equal(dirOf('fr'), 'rtl', 'unknown languages fall back to the default direction');
  assert.equal(getLang(), 'ar');
});

test('langFromHash accepts only the two languages', () => {
  assert.equal(langFromHash('#en'), 'en');
  assert.equal(langFromHash('#ar'), 'ar');
  assert.equal(langFromHash(''), 'ar');
  assert.equal(langFromHash('#fr'), 'ar');
  assert.equal(langFromHash('#en&x=1'), 'ar');
  assert.equal(langFromHash(undefined), 'ar');
});

test('both languages define exactly the same keys, all non-empty', () => {
  const ar = Object.keys(MESSAGES.ar).sort();
  const en = Object.keys(MESSAGES.en).sort();
  assert.deepEqual(ar, en);
  for (const lang of LANGS) {
    for (const [key, value] of Object.entries(MESSAGES[lang])) {
      assert.ok(typeof value === 'string' && value.trim() !== '', `${lang}:${key} is empty`);
    }
  }
});

test('Arabic messages are written in Arabic and placeholders match across languages', () => {
  const arabic = /[؀-ۿ]/;
  for (const [key, value] of Object.entries(MESSAGES.ar)) {
    if (key === 'app.brand' || key === 'top.language') continue; // brand name; the toggle names the other language
    if (!/\p{L}/u.test(value.replace(/\{\w+\}/g, ''))) continue; // pure numbers and placeholders
    assert.match(value, arabic, `ar:${key} has no Arabic text`);
  }
  const names = (s) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
  for (const key of Object.keys(MESSAGES.en)) {
    assert.deepEqual(names(MESSAGES.ar[key]), names(MESSAGES.en[key]), `placeholders differ for ${key}`);
  }
});

test('error kinds the console shows all have a message in both languages', () => {
  for (const kind of ['unauthorized', 'forbidden', 'not_found', 'conflict', 'rate_limited', 'bad_request', 'unavailable']) {
    assert.ok(MESSAGES.ar[`err.${kind}`] && MESSAGES.en[`err.${kind}`], kind);
  }
});

test('t translates, interpolates, and falls back', () => {
  setLang('ar');
  assert.equal(t('top.signOut'), 'تسجيل الخروج');
  assert.equal(t('common.pageOf', { page: 2, pages: 5 }), 'صفحة 2 من 5');
  assert.equal(t('no.such.key'), 'no.such.key');
  assert.equal(t('common.pageOf', { page: 2 }), 'صفحة 2 من {pages}');
  setLang('en');
  assert.equal(t('top.signOut'), 'Sign out');
  assert.equal(t('common.total', { total: 7 }), '7 results');
  setLang('ar');
});

test('setLang refuses unknown languages and notifies subscribers', () => {
  const seen = [];
  const off = onLangChange((l) => seen.push(l));
  assert.equal(setLang('fr'), false);
  assert.equal(setLang('en'), true);
  assert.equal(setLang('ar'), true);
  off();
  setLang('en');
  setLang('ar');
  assert.deepEqual(seen, ['en', 'ar']);
  assert.equal(otherLang('ar'), 'en');
  assert.equal(otherLang('en'), 'ar');
});

test('applyTranslations sets lang, dir and every data-i18n text', () => {
  const doc = new FakeDocument();
  const text = new FakeElement('span', doc);
  text.dataset.i18n = 'top.signOut';
  const input = new FakeElement('input', doc);
  input.dataset.i18nPlaceholder = 'accounts.search';
  const nav = new FakeElement('nav', doc);
  nav.dataset.i18nAriaLabel = 'tabs.label';
  const root = {
    ownerDocument: doc,
    querySelectorAll(sel) {
      return { '[data-i18n]': [text], '[data-i18n-placeholder]': [input], '[data-i18n-aria-label]': [nav] }[sel] ?? [];
    },
  };

  setLang('ar');
  applyTranslations(root);
  assert.equal(doc.documentElement.lang, 'ar');
  assert.equal(doc.documentElement.dir, 'rtl');
  assert.equal(text.textContent, 'تسجيل الخروج');
  assert.equal(input.getAttribute('placeholder'), MESSAGES.ar['accounts.search']);
  assert.equal(nav.getAttribute('aria-label'), MESSAGES.ar['tabs.label']);

  setLang('en');
  applyTranslations(root);
  assert.equal(doc.documentElement.lang, 'en');
  assert.equal(doc.documentElement.dir, 'ltr');
  assert.equal(text.textContent, 'Sign out');
  assert.equal(doc.title, MESSAGES.en['app.title']);
  setLang('ar');
});

test('date and time formatting tolerates bad input and is split in two', () => {
  for (const f of [formatDate, formatTime]) {
    assert.equal(f('not a date'), '');
    assert.equal(f(undefined), '');
    assert.notEqual(f('2026-10-02T10:00:00Z'), '');
  }
  assert.notEqual(formatDate('2026-10-02T10:00:00Z'), formatTime('2026-10-02T10:00:00Z'));
});

test('Requests and Files exist in code but stay hidden until their APIs exist', () => {
  assert.deepEqual(TABS.map((x) => x.id), ['accounts', 'audit', 'requests', 'catalog', 'files']);
  assert.deepEqual(visibleTabs().map((x) => x.id), ['accounts', 'audit', 'catalog']);
  assert.equal(isTabEnabled('catalog'), true);
  for (const id of ['requests', 'files']) assert.equal(isTabEnabled(id), false, id);
  assert.equal(isTabEnabled('accounts'), true);
  assert.equal(isTabEnabled('nope'), false);
  assert.equal(TABS.find((x) => x.id === 'requests').badge, true);
  assert.throws(() => { 'use strict'; TABS[0].enabled = false; }, TypeError, 'the registry is frozen');
});

test('badge counter text', () => {
  assert.equal(formatBadge(0), '');
  assert.equal(formatBadge(-3), '');
  assert.equal(formatBadge(1.5), '');
  assert.equal(formatBadge(undefined), '');
  assert.equal(formatBadge('5'), '');
  assert.equal(formatBadge(7), '7');
  assert.equal(formatBadge(99), '99');
  assert.equal(formatBadge(100), '99+');
});

test('setBadge shows and hides the counter', () => {
  const doc = new FakeDocument();
  const badge = doc.getElementById('badge-requests');
  badge.hidden = true;
  setBadge(doc, 'requests', 4);
  assert.deepEqual([badge.textContent, badge.hidden], ['4', false]);
  setBadge(doc, 'requests', 0);
  assert.deepEqual([badge.textContent, badge.hidden], ['', true]);
  assert.doesNotThrow(() => setBadge({ getElementById: () => null }, 'missing', 3));
});

test('arrow keys follow the visual direction', () => {
  assert.equal(nextTabIndex(0, 3, 'ArrowRight', 'ltr'), 1);
  assert.equal(nextTabIndex(0, 3, 'ArrowLeft', 'ltr'), 2);
  assert.equal(nextTabIndex(0, 3, 'ArrowLeft', 'rtl'), 1);
  assert.equal(nextTabIndex(0, 3, 'ArrowRight', 'rtl'), 2);
  assert.equal(nextTabIndex(1, 3, 'Home', 'ltr'), 0);
  assert.equal(nextTabIndex(0, 3, 'End', 'rtl'), 2);
  assert.equal(nextTabIndex(1, 3, 'a', 'ltr'), 1);
  assert.equal(nextTabIndex(0, 0, 'ArrowRight', 'ltr'), -1);
});

test('pageCount', () => {
  assert.equal(pageCount(0, 15), 1);
  assert.equal(pageCount(1, 15), 1);
  assert.equal(pageCount(15, 15), 1);
  assert.equal(pageCount(16, 15), 2);
  assert.equal(pageCount(100, 20), 5);
  assert.equal(pageCount(undefined, 15), 1);
  assert.equal(pageCount(10, 0), 1);
});
