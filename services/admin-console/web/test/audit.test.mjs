// Audit log tab (owner redesign 2026-11-14) on the fake DOM: sentence
// rendering, unknown-action fallback, icons and kinds, day grouping, search
// and action filters, empty states per source, the narrow (card) layout
// class, right-to-left isolation, details toggle and copy, and navigation.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { FakeDocument, flush } from './fake-dom.mjs';

const doc = new FakeDocument();
globalThis.document = doc;

const { signIn } = await import('../js/auth.js');
const { MESSAGES, setLang } = await import('../js/i18n.js');
const {
  DEFAULT_SOURCE, KIND_ICON, SOURCE_ACTIONS, actionKind, auditQuery, buildLookups, dayLabel,
  describe, groupByDay, matchesFilter, mountAudit, parseDetail, shortId,
} = await import('../js/audit.js');

await signIn('test-token', async () => ({ ok: true, data: { name: 'Wael' } }));

const ar = (k) => MESSAGES.ar[k];
const en = (k) => MESSAGES.en[k];
const STUDENT = '11111111-1111-4111-8111-111111111111';
const NOW = new Date('2026-11-14T12:00:00Z'); // 14:00 in Cairo

const LOOKUPS = buildLookups({
  packages: [
    { id: 'pkg_y4', product_id: 'com.wael.app.pkg.year4', title_ar: 'الفرقة 4', title_en: 'Year 4' },
    { id: 'pkg_y2', product_id: 'com.wael.app.pkg.year2', title_ar: 'الفرقة 2', title_en: 'Year 2' },
  ],
  subjects: [{ id: 'sub-law', title_ar: 'قانون مدني', title_en: 'Civil law' }],
  levels: [{ key: 'diploma-x', name_ar: 'دبلومة', name_en: 'Diploma' }],
});

const sentence = (m) => [m.actor.text, m.verb, m.target?.text].filter(Boolean).join(' ');

// --- pure helpers -----------------------------------------------------------

test('admin actions read as "<actor> <verb> <target name>" with names resolved from lookups', () => {
  setLang('ar');
  const upd = describe({ actor_name: 'wael', action: 'iap_package_update', target_type: 'iap_package', target_id: 'pkg_y4', detail: 'com.wael.app.pkg.year4' }, 'academy', LOOKUPS);
  assert.equal(sentence(upd), 'wael عدّل باكدج الفرقة 4');
  assert.deepEqual(upd.target.link, { tab: 'iap', packageId: 'pkg_y4' });

  const buy = describe({ actor_id: STUDENT, actor_name: STUDENT, action: 'iap_purchase', target_type: 'iap_package', target_id: 'pkg_y2', detail: 'product=com.wael.app.pkg.year2 env=Sandbox tx=2000000123 subjects=3' }, 'academy', LOOKUPS);
  assert.equal(sentence(buy), `${shortId(STUDENT)} اشترى باكدج الفرقة 2`);
  assert.deepEqual(buy.actor.link, { tab: 'accounts', user: STUDENT });
  assert.equal(buy.extras[0].text, ar('iap.env.sandbox'));
  assert.equal(buy.kind, 'neutral', 'owner rule: an in-app purchase is neutral grey');

  const grant = describe({ actor_name: 'wael', action: 'entitlement_grant', target_type: 'entitlement', target_id: 'ent-1', detail: `subject_id=sub-law user_id=${STUDENT} amount=0` }, 'academy', LOOKUPS);
  assert.equal(sentence(grant), 'wael منح مادة قانون مدني');
  assert.deepEqual(grant.extras[0].link, { tab: 'accounts', user: STUDENT });

  const bulk = describe({ actor_name: 'wael', action: 'request_bulk_accept', target_type: 'request_bulk', target_id: 'r1', detail: 'count=3 ids=r1,r2,r3' }, 'academy', LOOKUPS);
  assert.ok(sentence(bulk).startsWith('wael قبل '));
  assert.ok(bulk.target.text.includes('٣'), bulk.target.text);

  const settings = describe({ actor_name: 'wael', action: 'settings_updated', target_type: 'settings', target_id: 'app', detail: 'show_prices' }, 'academy', LOOKUPS);
  assert.equal(sentence(settings), 'wael عدّل الإعدادات');

  const level = describe({ actor_name: 'wael', action: 'level_update', target_type: 'level', target_id: 'diploma-x', detail: 'old name' }, 'academy', LOOKUPS);
  assert.equal(level.target.text, 'دبلومة');

  const suspend = describe({ actor_name: 'Wael', action: 'account_suspend', target_type: 'user', target_id: STUDENT, detail: 'spam' }, 'auth', LOOKUPS);
  assert.equal(sentence(suspend), `Wael أوقف حساب ${shortId(STUDENT)}`);
  assert.deepEqual(suspend.target.link, { tab: 'accounts', user: STUDENT });
});

test('without lookups a name falls back to the detail title, then to a short id', () => {
  setLang('ar');
  const subj = describe({ actor_name: 'wael', action: 'subject_update', target_type: 'subject', target_id: 'sub-unknown', detail: 'مادة جديدة' }, 'academy');
  assert.equal(subj.target.text, 'مادة جديدة');
  const pkg = describe({ actor_name: 'wael', action: 'iap_package_delete', target_type: 'iap_package', target_id: 'pkg_gone', detail: 'com.wael.app.pkg.year3' }, 'academy');
  assert.equal(pkg.target.text, 'com.wael.app.pkg.year3');
  const buy = describe({ actor_id: STUDENT, actor_name: STUDENT, action: 'iap_purchase', target_type: 'iap_package', target_id: 'pkg_aaaaaaaaaaaa', detail: 'product= env=Production tx=1 subjects=0' }, 'academy');
  assert.equal(buy.target.text, 'pkg_aaaa…');
  assert.equal(buy.target.isId, true);
});

test('an unknown action shows its raw code and the raw target type, with a neutral icon', () => {
  setLang('ar');
  const m = describe({ actor_name: 'wael', action: 'something_new', target_type: 'widget', target_id: 'w1', detail: 'x' }, 'academy', LOOKUPS);
  assert.equal(m.unknown, true);
  assert.equal(m.verb, 'something_new');
  assert.equal(m.target.text, 'widget w1');
  assert.equal(m.kind, 'neutral');
  assert.equal(m.icon, KIND_ICON.neutral);
});

test('student events read as "<student> <verb>" with device and the admin who acted', () => {
  setLang('ar');
  const login = describe({ user_id: STUDENT, student: 'a***@example.com', type: 'login', device_label: 'iPhone 15', actor_type: 'student' }, 'student');
  assert.equal(sentence(login), `a***@example.com ${ar('audit.verb.login')}`);
  assert.equal(login.extras[0].text, 'iPhone 15');
  const sus = describe({ user_id: STUDENT, student: 'm***@example.com', type: 'account_suspended', actor_type: 'admin', actor_name: 'Wael' }, 'student');
  assert.ok(sus.extras.some((x) => x.text === 'بواسطة المشرف Wael'));
  assert.equal(sus.kind, 'danger');
});

test('every action has a kind and icon: create/accept success, update info, delete/suspend/reject danger, purchase neutral grey, broadcast neutral', () => {
  const expect = {
    level_create: 'create', file_upload: 'create', entitlement_grant: 'create', subject_publish: 'create', iap_package_create: 'create',
    request_accept: 'accept', request_bulk_accept: 'accept', account_reactivate: 'accept', account_reactivated: 'accept', deletion_cancelled: 'accept',
    subject_update: 'update', iap_package_update: 'update', video_reorder: 'update', settings_updated: 'update', password_changed: 'update',
    iap_package_delete: 'danger', account_suspend: 'danger', account_suspended: 'danger', request_reject: 'danger', request_bulk_reject: 'danger',
    entitlement_revoke: 'danger', subject_unpublish: 'danger', account_purged: 'danger', deletion_requested: 'danger', session_ended_cap: 'danger',
    iap_purchase: 'neutral', broadcast_send: 'broadcast', broadcast_retry: 'broadcast', login: 'neutral', logout: 'neutral',
  };
  for (const [action, kind] of Object.entries(expect)) assert.equal(actionKind(action), kind, action);
  for (const actions of Object.values(SOURCE_ACTIONS)) {
    for (const a of actions) assert.ok(KIND_ICON[actionKind(a)], `${a} has an icon`);
  }
});

test('parseDetail reads key=value pairs and ignores free text', () => {
  assert.deepEqual(parseDetail('product=p env=Sandbox tx=12 subjects=3'), { product: 'p', env: 'Sandbox', tx: '12', subjects: '3' });
  assert.deepEqual(parseDetail('قانون مدني'), {});
  assert.deepEqual(parseDetail(undefined), {});
});

test('entries group by Cairo day under Today, Yesterday and the long date', () => {
  setLang('ar');
  const items = [
    { id: 1, created_at: '2026-11-14T09:00:00Z' },
    { id: 2, created_at: '2026-11-13T22:30:00Z' }, // 00:30 on the 14th in Cairo: still today
    { id: 3, created_at: '2026-11-13T08:00:00Z' },
    { id: 4, created_at: '2026-11-10T08:00:00Z' },
  ];
  const groups = groupByDay(items, NOW);
  assert.deepEqual(groups.map((g) => g.items.map((x) => x.id)), [[1, 2], [3], [4]]);
  assert.equal(groups[0].label, ar('audit.today'));
  assert.equal(groups[1].label, ar('audit.yesterday'));
  assert.ok(!groups[2].label.includes('2026-11-10'));
  assert.ok(groups[2].label.length > 6, groups[2].label);
  setLang('en');
  assert.equal(dayLabel('2026-11-14T09:00:00Z', NOW), en('audit.today'));
  const older = dayLabel('2026-11-10T08:00:00Z', NOW);
  assert.ok(older.includes('Tuesday') && older.includes('10 November 2026'), older);
  setLang('ar');
});

test('the text search matches actor and target names, emails and ids; the action filter is exact', () => {
  setLang('ar');
  const m = describe({ actor_id: STUDENT, actor_name: STUDENT, action: 'iap_purchase', target_type: 'iap_package', target_id: 'pkg_y2', detail: 'product=com.wael.app.pkg.year2 env=Sandbox tx=2000000123 subjects=3' }, 'academy', LOOKUPS);
  assert.ok(matchesFilter(m, { search: 'الفرقة 2' }));
  assert.ok(matchesFilter(m, { search: STUDENT.slice(0, 13) }), 'full id, not only the short one');
  assert.ok(matchesFilter(m, { search: '2000000123' }));
  assert.ok(matchesFilter(m, { search: 'PKG_Y2' }), 'case-insensitive');
  assert.ok(!matchesFilter(m, { search: 'nobody' }));
  assert.ok(matchesFilter(m, { action: 'iap_purchase' }));
  assert.ok(!matchesFilter(m, { action: 'iap_package_update' }));
  const s = describe({ user_id: STUDENT, student: 'ali@example.com', type: 'login', actor_type: 'student' }, 'student');
  assert.ok(matchesFilter(s, { search: 'ALI@example' }));
});

test('only the student source sends search and type to the server', () => {
  assert.deepEqual(auditQuery({ source: 'academy', page: 1, limit: 20, search: 'x', action: 'iap_purchase' }), { source: 'academy', page: 1, limit: 20 });
  assert.deepEqual(auditQuery({ source: 'student', page: 2, limit: 20, search: ' ali ', action: 'login' }), { source: 'student', page: 2, limit: 20, student: 'ali', type: 'login' });
});

// --- mounted tab --------------------------------------------------------------

function fakeApi(handler) {
  const calls = [];
  return {
    calls,
    async get(path, query = {}) {
      calls.push({ path, query });
      return handler(path, query);
    },
  };
}

function fakeMatchMedia(matches) {
  const listeners = [];
  const mq = { matches, addEventListener: (type, fn) => listeners.push(fn) };
  return { win: { matchMedia: () => mq }, set(v) { mq.matches = v; for (const fn of listeners) fn({ matches: v }); } };
}

const ACADEMY_ITEMS = [
  { id: 'c1', actor_id: 'adm-1', actor_name: 'wael', action: 'iap_package_update', target_type: 'iap_package', target_id: 'pkg_y4', detail: 'com.wael.app.pkg.year4', created_at: '2026-11-14T10:00:00Z' },
  { id: 'c2', actor_id: STUDENT, actor_name: STUDENT, action: 'iap_purchase', target_type: 'iap_package', target_id: 'pkg_y2', detail: 'product=com.wael.app.pkg.year2 env=Sandbox tx=2000000123 subjects=3', created_at: '2026-11-13T09:30:00Z' },
  { id: 'c3', actor_id: 'adm-1', actor_name: 'wael', action: 'subject_publish', target_type: 'subject', target_id: 'sub-law', detail: 'قانون مدني', created_at: '2026-11-01T09:30:00Z' },
];

function academyApi(items = ACADEMY_ITEMS) {
  return fakeApi((path) => {
    if (path === '/api/audit') return { ok: true, data: { items, total: items.length } };
    if (path === '/api/iap/packages') return { ok: true, data: { packages: [...LOOKUPS.packages.values()] } };
    if (path === '/api/levels') return { ok: true, data: { levels: [] } };
    if (path === '/api/subjects') return { ok: true, data: { items: [...LOOKUPS.subjects.values()], total: 1 } };
    return { ok: false, kind: 'not_found' };
  });
}

function freshDoc() {
  const d = new FakeDocument();
  globalThis.document = d;
  return d;
}

const listOf = (d) => d.getElementById('audit-list');
const rowsOf = (d) => listOf(d).findAll((c) => c.tagName === 'LI');
const daysOf = (d) => listOf(d).findAll((c) => c.tagName === 'H3').map((x) => x.textContent);

test('the tab opens on the academy source, groups by day and renders sentence rows with icon and time', async () => {
  setLang('ar');
  const d = freshDoc();
  const api = academyApi();
  const mm = fakeMatchMedia(false);
  const audit = mountAudit({ api, doc: d, win: mm.win, now: () => NOW });
  await audit.load();
  assert.equal(DEFAULT_SOURCE, 'academy');
  assert.equal(api.calls.find((c) => c.path === '/api/audit').query.source, 'academy');
  assert.equal(d.getElementById('audit-source-academy').getAttribute('aria-pressed'), 'true');
  assert.deepEqual(daysOf(d).slice(0, 2), [ar('audit.today'), ar('audit.yesterday')]);
  assert.equal(daysOf(d).length, 3);
  const rows = rowsOf(d);
  assert.equal(rows.length, 3);
  assert.equal(rows[0].className, 'audit-row kind-update');
  assert.equal(rows[1].className, 'audit-row kind-neutral');
  const icon = rows[1].children[0];
  assert.equal(icon.textContent, KIND_ICON.neutral);
  assert.equal(icon.getAttribute('aria-hidden'), 'true', 'the icon is decorative; the verb carries the meaning');
  assert.ok(rows[1].textContent.includes(ar('audit.verb.iap_purchase')));
  const time = rows[0].findAll((c) => c.tagName === 'TIME')[0];
  assert.equal(time.getAttribute('datetime'), '2026-11-14T10:00:00Z');
  assert.ok(time.textContent.length > 0);
  assert.ok(rows[2].textContent.includes('قانون مدني'));
});

test('names are isolated with <bdi>, details are LTR, collapsed by default, toggle and copy', async () => {
  setLang('ar');
  const d = freshDoc();
  const copied = [];
  Object.defineProperty(globalThis, 'navigator', { value: { clipboard: { writeText: async (s) => { copied.push(s); } } }, configurable: true });
  const audit = mountAudit({ api: academyApi(), doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
  await audit.load();
  const row = rowsOf(d)[1];
  const bdis = row.findAll((c) => c.tagName === 'BDI').map((b) => b.textContent);
  assert.ok(bdis.includes('الفرقة 2'));
  assert.ok(bdis.includes(shortId(STUDENT)));
  const details = row.findAll((c) => c.className === 'audit-details ltr-block')[0];
  assert.equal(details.getAttribute('dir'), 'ltr');
  assert.equal(details.hidden, true);
  assert.ok(details.textContent.includes('pkg_y2'));
  assert.ok(details.textContent.includes('tx=2000000123'));
  const toggle = row.findAll((c) => c.tagName === 'BUTTON' && c.className.includes('audit-toggle'))[0];
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
  assert.equal(toggle.getAttribute('aria-controls'), details.getAttribute('id'));
  toggle.click();
  assert.equal(details.hidden, false);
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');
  assert.equal(toggle.textContent, ar('audit.detailsHide'));
  audit.rerender();
  const again = rowsOf(d)[1].findAll((c) => c.className === 'audit-details ltr-block')[0];
  assert.equal(again.hidden, false, 'an open details block stays open across a re-render');
  const copy = again.findAll((c) => c.tagName === 'BUTTON')[0];
  copy.click();
  await flush();
  assert.equal(copied.length, 1);
  assert.ok(copied[0].includes(`actor_id: ${STUDENT}`));
  assert.ok(copied[0].includes('target_id: pkg_y2'));
  delete globalThis.navigator;
});

test('clicking a student or package name calls navigate with the right tab', async () => {
  const d = freshDoc();
  const went = [];
  const audit = mountAudit({ api: academyApi(), doc: d, win: fakeMatchMedia(false).win, navigate: (l) => went.push(l), now: () => NOW });
  await audit.load();
  const links = listOf(d).findAll((c) => c.tagName === 'BUTTON' && c.className.includes('name-link'));
  for (const l of links) l.click();
  assert.deepEqual(went, [
    { tab: 'iap', packageId: 'pkg_y4' },
    { tab: 'accounts', user: STUDENT },
    { tab: 'iap', packageId: 'pkg_y2' },
  ]);
});

test('the search and action filters work on the loaded page without a new request, and say so', async () => {
  setLang('ar');
  const d = freshDoc();
  const api = academyApi();
  const audit = mountAudit({ api, doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
  await audit.load();
  assert.equal(d.getElementById('audit-filter-note').textContent, ar('audit.filterNote.client'));
  const options = d.getElementById('audit-action').children.map((o) => o.getAttribute('value'));
  // Owner rule 2026-11-14: no Apple / in-app purchase option in the filter.
  assert.deepEqual(options, ['', ...SOURCE_ACTIONS.academy.filter((a) => a !== 'iap_purchase')]);
  assert.ok(!options.includes('iap_purchase'));
  const before = api.calls.length;

  d.getElementById('audit-search').value = 'الفرقة 2';
  d.getElementById('audit-filter').fire('submit');
  assert.equal(api.calls.length, before, 'no request for a client-side filter');
  assert.equal(rowsOf(d).length, 1);
  assert.equal(rowsOf(d)[0].dataset.action, 'iap_purchase');

  d.getElementById('audit-search').value = '';
  d.getElementById('audit-action').value = 'subject_publish';
  d.getElementById('audit-filter').fire('submit');
  assert.equal(rowsOf(d).length, 1);
  assert.equal(rowsOf(d)[0].dataset.action, 'subject_publish');

  d.getElementById('audit-search').value = 'nobody';
  d.getElementById('audit-action').value = '';
  d.getElementById('audit-filter').fire('submit');
  assert.ok(listOf(d).textContent.endsWith(ar('audit.noMatch')));

  d.getElementById('audit-filter-reset').click();
  assert.equal(rowsOf(d).length, 3);
  assert.equal(api.calls.length, before);
});

test('each source shows its own empty state explaining what it records', async () => {
  for (const lang of ['ar', 'en']) {
    setLang(lang);
    for (const source of ['auth', 'academy', 'notifications', 'student']) {
      const d = freshDoc();
      const api = fakeApi(() => ({ ok: true, data: { items: [], total: 0 } }));
      const audit = mountAudit({ api, doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
      if (source !== 'academy') d.getElementById(`audit-source-${source}`).click();
      else await audit.load();
      await flush();
      assert.ok(listOf(d).textContent.endsWith(MESSAGES[lang][`audit.empty.${source}`]), `${lang} ${source}`);
      assert.equal(listOf(d).findAll((c) => c.className === 'empty-state').length, 1, 'empty-state component');
    }
  }
  setLang('ar');
  assert.ok(ar('audit.empty.auth').includes('إيقاف/تفعيل/حذف حسابات الطلاب'));
});

test('below 720px the list switches to the card layout class, and back', async () => {
  const d = freshDoc();
  const mm = fakeMatchMedia(true);
  const audit = mountAudit({ api: academyApi(), doc: d, win: mm.win, now: () => NOW });
  await audit.load();
  assert.equal(listOf(d).className, 'audit-list is-narrow');
  mm.set(false);
  assert.equal(listOf(d).className, 'audit-list');
  mm.set(true);
  assert.equal(listOf(d).className, 'audit-list is-narrow');
  assert.equal(rowsOf(d).length, 3, 'the same rows render as cards');
});

test('without matchMedia the wide layout is used', async () => {
  const d = freshDoc();
  const audit = mountAudit({ api: academyApi(), doc: d, win: {}, now: () => NOW });
  await audit.load();
  assert.equal(listOf(d).className, 'audit-list');
});

test('English renders left-to-right sentences with the same structure', async () => {
  setLang('en');
  const d = freshDoc();
  const audit = mountAudit({ api: academyApi(), doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
  await audit.load();
  const s = rowsOf(d)[0].findAll((c) => c.className === 'audit-sentence')[0].textContent;
  assert.equal(s, 'wael updated package Year 4');
  assert.equal(daysOf(d)[0], 'Today');
  setLang('ar');
});

test('a failed name lookup keeps the log working with short ids', async () => {
  setLang('ar');
  const d = freshDoc();
  const api = fakeApi((path) => (path === '/api/audit'
    ? { ok: true, data: { items: [ACADEMY_ITEMS[1]], total: 1 } }
    : { ok: false, kind: 'unavailable' }));
  const audit = mountAudit({ api, doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
  await audit.load();
  assert.equal(d.getElementById('audit-banner').hidden, true);
  assert.ok(rowsOf(d)[0].textContent.includes('com.wael.app.pkg.year2'), 'falls back to the product id');
});

test('reset returns to the academy source and drops every row', async () => {
  const d = freshDoc();
  const audit = mountAudit({ api: academyApi(), doc: d, win: fakeMatchMedia(false).win, now: () => NOW });
  d.getElementById('audit-source-auth').click();
  await flush();
  audit.reset();
  assert.equal(d.getElementById('audit-source-academy').getAttribute('aria-pressed'), 'true');
  assert.equal(rowsOf(d).length, 0);
  assert.ok(!listOf(d).textContent.includes('wael'));
});
