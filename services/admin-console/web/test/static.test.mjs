// Static guards over the files that ship to the browser. They run on source
// text, so they hold even for code paths the other tests never execute.

import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { ACTIONS } from '../js/account-dialog.js';
import { MESSAGES } from '../js/i18n.js';
import { TABS } from '../js/tabs.js';

const webDir = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (rel) => readFileSync(join(webDir, rel), 'utf8');
const html = read('index.html');
const css = read('style.css');
const jsFiles = readdirSync(join(webDir, 'js')).filter((f) => f.endsWith('.js'));
const jsSources = Object.fromEntries(jsFiles.map((f) => [f, read(`js/${f}`)]));

/** Removes comments so guards look at code only (a comment may mention localStorage). */
function stripJsComments(src) {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[\s;{}()])\/\/.*$/gm, '$1');
}
const jsCode = Object.fromEntries(Object.entries(jsSources).map(([f, s]) => [f, stripJsComments(s)]));

test('the expected modules are present', () => {
  for (const name of ['api', 'auth', 'i18n', 'accounts', 'audit', 'app', 'account-dialog', 'tabs', 'ui', 'dom', 'catalog', 'levels', 'subjects', 'subject-dialog', 'videos', 'video-dialog', 'confirm', 'requests', 'idle', 'entitlements-dialog', 'unsaved', 'settings', 'files']) {
    assert.ok(jsFiles.includes(`${name}.js`), `${name}.js`);
  }
});

test('dialogs are balanced, unnested, and directly under body when opened by script', () => {
  const dialogTags = [...html.matchAll(/<\/?dialog\b[^>]*>/gi)].map((m) => m[0]);
  assert.equal(dialogTags.filter((tag) => /^<dialog\b/i.test(tag)).length,
    dialogTags.filter((tag) => /^<\/dialog\s*>$/i.test(tag)).length,
    'opening and closing dialog tag counts match');

  const voidElements = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr']);
  const stack = [];
  const dialogDepths = new Map();
  const tagPattern = /<!--[\s\S]*?-->|<![^>]*>|<\/?[a-z][\w:-]*\b[^>]*>/gi;
  for (const match of html.matchAll(tagPattern)) {
    const tag = match[0];
    if (tag.startsWith('<!--') || tag.startsWith('<!')) continue;
    const closing = /^<\//.test(tag);
    const name = tag.match(/^<\/?([a-z][\w:-]*)\b/i)[1].toLowerCase();
    if (closing) {
      assert.equal(stack.at(-1), name, `unexpected closing </${name}>`);
      stack.pop();
      continue;
    }
    if (name === 'dialog') {
      dialogDepths.set(match.index, { depth: stack.filter((entry) => entry === 'dialog').length + 1, parent: stack.at(-1) });
      assert.ok(!stack.includes('dialog'), 'a dialog opens before the previous dialog closes');
    }
    if (!voidElements.has(name) && !/\/\s*>$/.test(tag)) stack.push(name);
  }
  assert.equal(stack.length, 0, `unclosed HTML elements: ${stack.join(', ')}`);

  const openedDialogIds = new Set();
  for (const source of Object.values(jsCode)) {
    for (const declaration of source.matchAll(/(?:const|let)\s+([\w$]+)\s*=\s*doc\.getElementById\(\s*['"]([\w-]+)['"]\s*\)/g)) {
      if (new RegExp(`\\b${declaration[1]}\\.showModal\\s*\\(`).test(source)) openedDialogIds.add(declaration[2]);
    }
  }

  for (const id of openedDialogIds) {
    const tag = new RegExp(`<dialog\\b(?=[^>]*\\bid="${id}")[^>]*>`, 'i').exec(html);
    assert.ok(tag, `showModal() target #${id} is a dialog in the HTML`);
    const context = dialogDepths.get(tag.index);
    assert.ok(context, `dialog #${id} was included in the structural scan`);
    assert.equal(context.depth, 1, `dialog #${id} is not nested in another dialog`);
    assert.equal(context.parent, 'body', `dialog #${id} is a direct child of body`);
  }
});

test('no inline script: every script is an external module under /js/', () => {
  const scripts = [...html.matchAll(/<script\b[^>]*>/gi)].map((m) => m[0]);
  assert.ok(scripts.length >= 1);
  for (const tag of scripts) {
    assert.match(tag, /\btype="module"/, tag);
    assert.match(tag, /\bsrc="\/js\/[\w-]+\.js"/, tag);
  }
  assert.ok(!/<script\b[^>]*>\s*\S/.test(html.replace(/<script\b[^>]*><\/script>/gi, '')), 'script with a body');
  assert.ok(!/<script\b(?![^>]*\bsrc=)/i.test(html));
});

test('no inline style: no <style>, no style="" attribute, no inline event handlers', () => {
  assert.ok(!/<style\b/i.test(html), '<style> element');
  assert.ok(!/\sstyle\s*=/i.test(html), 'style attribute');
  assert.ok(!/\son[a-z]+\s*=/i.test(html), 'on* handler attribute');
  assert.ok(!/javascript:/i.test(html), 'javascript: URL');
  for (const [name, src] of Object.entries(jsCode)) {
    assert.ok(!/setAttribute\(\s*['"]style['"]/.test(src), `${name} sets a style attribute`);
    assert.ok(!/\.style\.cssText/.test(src), `${name} writes cssText`);
  }
});

test('no storage, cookies, or other persistence anywhere in the scripts (token is memory only)', () => {
  const forbidden = [
    /\blocalStorage\b/, /\bsessionStorage\b/, /\bindexedDB\b/, /\bopenDatabase\b/, /\bcaches\b\s*\./,
    /\bdocument\s*\.\s*cookie\b/, /\.cookie\b/, /cookieStore/, /\bwindow\s*\.\s*name\b/, /\blocation\s*\.\s*search\b/,
    /serviceWorker/, /\bnavigator\s*\.\s*storage\b/,
  ];
  for (const [name, src] of Object.entries(jsCode)) {
    for (const re of forbidden) assert.ok(!re.test(src), `${name} matches ${re}`);
  }
  assert.ok(!/<form[^>]*\baction=/i.test(html));
});

test('the token module variable is not exported for writing and never reaches the URL', () => {
  const auth = jsCode['auth.js'];
  assert.match(auth, /^let token = '';/m, 'token is a module-level variable');
  assert.ok(!/export\s+(let|var)\s/.test(auth), 'no exported mutable binding');
  for (const [name, src] of Object.entries(jsCode)) {
    if (name === 'api.js' || name === 'auth.js') continue;
    assert.ok(!/getToken\(/.test(src), `${name} reads the token directly`);
  }
  assert.ok(!/location\s*\.\s*(hash|href|search)\s*=\s*[^=].*[Tt]oken/.test(Object.values(jsCode).join('\n')));
});

test('no HTML injection or dynamic code in the scripts', () => {
  const forbidden = [
    /\binnerHTML\b/, /\bouterHTML\b/, /\binsertAdjacentHTML\b/, /\bdocument\s*\.\s*write/, /\beval\s*\(/, /\bnew\s+Function\b/,
    /\bsetTimeout\s*\(\s*['"`]/, /\bsetInterval\s*\(\s*['"`]/, /\bimport\s*\(/, /\bXMLHttpRequest\b/, /\bWebSocket\b/,
    /\bEventSource\b/, /\bsendBeacon\b/, /\bpostMessage\b/, /\bwindow\s*\.\s*open\b/, /\bcreateContextualFragment\b/, /\bDOMParser\b/,
  ];
  for (const [name, src] of Object.entries(jsCode)) {
    for (const re of forbidden) assert.ok(!re.test(src), `${name} matches ${re}`);
  }
});

test('only api.js talks to the network, and only to the console itself', () => {
  for (const [name, src] of Object.entries(jsCode)) {
    if (name === 'api.js') continue;
    assert.ok(!/\bfetch\b/.test(src), `${name} uses fetch`);
  }
  const apiPaths = [...new Set([...Object.values(jsCode).join('\n').matchAll(/['"`](\/api\/[\w/]*)['"`]/g)].map((m) => m[1]))].sort();
  assert.deepEqual(apiPaths, [
    '/api/accounts', '/api/accounts/delete', '/api/accounts/reactivate', '/api/accounts/suspend', '/api/audit',
    '/api/entitlements', '/api/entitlements/grant', '/api/entitlements/revoke',
    '/api/files', '/api/files/delete', '/api/files/upload',
    '/api/levels', '/api/levels/create', '/api/levels/delete', '/api/levels/update',
    '/api/requests', '/api/requests/accept', '/api/requests/reject',
    '/api/settings', '/api/settings/update',
    '/api/subjects', '/api/subjects/create', '/api/subjects/publish', '/api/subjects/unpublish', '/api/subjects/update',
    '/api/videos', '/api/videos/create', '/api/videos/delete', '/api/videos/reorder', '/api/videos/update',
    '/api/whoami',
  ]);
});

// The watch link is the single allowed third-party URL: a plain anchor the
// page never fetches, embeds or previews. Everything else stays first-party.
const YOUTUBE_WATCH = 'https://www.youtube.com/watch?v=';

test('the YouTube watch link is one constant, used only for an anchor href after id validation', () => {
  const users = Object.entries(jsCode).filter(([, src]) => src.includes(YOUTUBE_WATCH));
  assert.deepEqual(users.map(([name]) => name), ['videos.js']);
  const videos = jsCode['videos.js'];
  assert.ok(videos.includes('A-Za-z0-9_-]{11}'), 'id shape is validated before the link is built');
  assert.ok(/attrs:\s*\{[^}]*href/.test(videos), 'the link is an anchor href, not fetched content');
  assert.ok(videos.includes("rel: 'noopener noreferrer'") || videos.includes('rel: "noopener noreferrer"'), 'the link is noopener');
  assert.ok(videos.includes("target: '_blank'") || videos.includes('target: "_blank"'), 'the link opens in a new tab');
});

test('no third-party request: no absolute URL in HTML, CSS or scripts; imports are relative', () => {
  assert.ok(!/https?:\/\//i.test(html), 'absolute URL in index.html');
  assert.ok(!/https?:\/\//i.test(css), 'absolute URL in style.css');
  for (const [name, src] of Object.entries(jsCode)) {
    const stripped = src.split(YOUTUBE_WATCH).join('');
    assert.ok(!/https?:\/\//i.test(stripped), `${name} has an absolute URL other than the watch link`);
    assert.ok(!/(^|[^\w.])\/\/[a-z0-9.-]+\.[a-z]{2,}/i.test(stripped), `${name} has a protocol-relative URL`);
    for (const m of src.matchAll(/^\s*import\b[^'"]*['"]([^'"]+)['"]/gm)) {
      assert.match(m[1], /^\.\/[\w-]+\.js$/, `${name} imports ${m[1]}`);
    }
  }
  const links = [...html.matchAll(/<link\b[^>]*>/gi)].map((m) => m[0]);
  for (const tag of links) assert.match(tag, /href="(\/style\.css|data:,)"/, tag);
  assert.ok(!/@import/i.test(css));
  for (const m of css.matchAll(/url\(([^)]*)\)/gi)) assert.match(m[1].trim().replace(/^['"]/, ''), /^data:/, `css url(${m[1]})`);
  assert.ok(!/<(iframe|object|embed|base|img|video|audio|source|frame)\b/i.test(html));
});

test('the page is Arabic first and right-to-left, with a password field that does not autofill', () => {
  assert.match(html, /<html\b[^>]*\blang="ar"/);
  assert.match(html, /<html\b[^>]*\bdir="rtl"/);
  assert.match(html, /<meta name="viewport"/);
  const field = html.match(/<input\b[^>]*id="login-token"[^>]*>/)?.[0] ?? '';
  assert.match(field, /type="password"/);
  assert.match(field, /autocomplete="off"/);
  assert.match(field, /spellcheck="false"/);
  assert.ok(!/name=/.test(field), 'a name attribute invites password-manager saving');
});

test('the CSS uses logical properties so one stylesheet serves both directions', () => {
  assert.ok(!/(^|[\s{;])(margin|padding)-(left|right)\s*:/m.test(css), 'physical margin/padding');
  assert.ok(!/(^|[\s{;])(left|right)\s*:/m.test(css), 'physical offsets');
  assert.ok(!/text-align\s*:\s*(left|right)/.test(css), 'physical text-align');
  assert.match(css, /prefers-reduced-motion/);
});

function jsIdsAsked() {
  const ids = new Set();
  for (const src of Object.values(jsCode)) {
    for (const m of src.matchAll(/getElementById\(\s*'([\w-]+)'\s*\)/g)) ids.add(m[1]);
    for (const m of src.matchAll(/querySelector\(\s*'#([\w-]+)[ '\]]/g)) ids.add(m[1]);
  }
  // [ 'lang-toggle', 'login-lang' ] style arrays in app.js
  for (const m of jsCode['app.js'].matchAll(/for \(const id of \[([^\]]+)\]/g)) {
    for (const s of m[1].matchAll(/'([\w-]+)'/g)) ids.add(s[1]);
  }
  return ids;
}

test('every element id the scripts ask for exists in index.html', () => {
  const present = new Set([...html.matchAll(/\bid="([\w-]+)"/g)].map((m) => m[1]));
  const asked = jsIdsAsked();
  assert.ok(asked.size > 30, `expected many ids, found ${asked.size}`);
  for (const id of asked) assert.ok(present.has(id), `#${id} is missing from index.html`);
  for (const tab of TABS) {
    assert.ok(present.has(`tab-${tab.id}`), `tab-${tab.id}`);
    assert.ok(present.has(`panel-${tab.id}`), `panel-${tab.id}`);
  }
  assert.ok(present.has('badge-requests'));
  const ids = [...html.matchAll(/\bid="([\w-]+)"/g)].map((m) => m[1]);
  assert.equal(new Set(ids).size, ids.length, 'duplicate ids');
});

test('tabs for APIs that do not exist yet are hidden in the markup and disabled in code', () => {
  for (const tab of TABS.filter((x) => !x.enabled)) {
    assert.match(html, new RegExp(`<button[^>]*id="tab-${tab.id}"[^>]*\\bhidden\\b`), `tab-${tab.id} button`);
    assert.match(html, new RegExp(`<section[^>]*id="panel-${tab.id}"[^>]*\\bhidden\\b`), `panel-${tab.id}`);
  }
  assert.match(html, /id="badge-requests"[^>]*\bhidden\b/);
});

test('every translation key used by the page or the scripts is defined in both languages', () => {
  const have = (key) => key in MESSAGES.ar && key in MESSAGES.en;
  for (const m of html.matchAll(/data-i18n(?:-placeholder|-aria-label)?="([\w.]+)"/g)) assert.ok(have(m[1]), `html uses ${m[1]}`);
  for (const [name, src] of Object.entries(jsCode)) {
    for (const m of src.matchAll(/\bt\(\s*'([\w.]+)'/g)) assert.ok(have(m[1]), `${name} uses ${m[1]}`);
  }
  for (const kind of ['unauthorized', 'forbidden', 'not_found', 'conflict', 'rate_limited', 'bad_request', 'unavailable']) assert.ok(have(`err.${kind}`));
  for (const s of ['active', 'suspended', 'deleted']) assert.ok(have(`status.${s}`));
  for (const action of Object.keys(ACTIONS)) {
    assert.ok(have(`action.${action}`));
    for (const part of ['title', 'note', 'confirm']) assert.ok(have(`dialog.${action}.${part}`), `dialog.${action}.${part}`);
  }
  for (const a of ['account_suspend', 'account_reactivate', 'account_delete']) assert.ok(have(`audit.action.${a}`));
  for (const tab of TABS) assert.ok(have(`tabs.${tab.id}`));
});

// --- contrast (WCAG 2.x) ---------------------------------------------------

function tokens() {
  const root = css.match(/:root\s*{([^}]*)}/)[1];
  return Object.fromEntries([...root.matchAll(/--([\w-]+)\s*:\s*(#[0-9a-fA-F]{6})\s*;/g)].map((m) => [m[1], m[2]]));
}
function luminance(hex) {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

test('text and control colours meet WCAG AA contrast on the dark surfaces', () => {
  const c = tokens();
  const surfaces = ['void', 'surface-1', 'surface-2', 'surface-3'];
  for (const s of surfaces) {
    for (const fg of ['text', 'text-2', 'text-3', 'danger-text', 'approved', 'pending']) {
      assert.ok(contrast(c[fg], c[s]) >= 4.5, `${fg} on ${s}: ${contrast(c[fg], c[s]).toFixed(2)}`);
    }
  }
  for (const bg of ['crimson', 'crimson-hover']) {
    assert.ok(contrast(c.text, c[bg]) >= 4.5, `white on ${bg}: ${contrast(c.text, c[bg]).toFixed(2)}`);
  }
  assert.ok(contrast(c.focus, c.void) >= 3, 'focus ring');
  assert.ok(contrast(c.focus, c['surface-3']) >= 3, 'focus ring on surface');
  // The crimson accent is for fills and borders; as small text it would fail, so it must not be used that way.
  assert.ok(contrast(c.crimson, c['surface-1']) < 4.5);
  assert.ok(!/(^|[\s{;])color\s*:\s*var\(--crimson\)/m.test(css), 'crimson used as text colour');
});

test('the dark and crimson tokens match the Flutter design system', () => {
  const c = tokens();
  assert.equal(c.void.toLowerCase(), '#080808');
  assert.equal(c['surface-1'].toLowerCase(), '#111111');
  assert.equal(c['surface-2'].toLowerCase(), '#181818');
  assert.equal(c.crimson.toLowerCase(), '#c1121f');
  assert.equal(c['crimson-deep'].toLowerCase(), '#7f0d15');
  assert.equal(c['crimson-hover'].toLowerCase(), '#a30f1a');
  assert.equal(c.border.toLowerCase(), '#303030');
  assert.equal(c.hairline.toLowerCase(), '#262626');
});
