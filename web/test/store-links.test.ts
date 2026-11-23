// Store links (src/lib/store-links.ts): the Google Play listing is
// com.wael.app (owner decision 2026-11-07); the iOS app is live on the App
// Store (owner decision 2026-11-23), and every App Store link on the site is
// STORE_URLS.ios, never a hard-coded URL.
// Run: npm test (node --test).

import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { test } from "node:test";

import { STORE_URLS } from "../src/lib/store-links.ts";

const SRC = join(import.meta.dirname, "..", "src");

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    return e.isDirectory() ? files(p) : /\.(tsx?|css)$/.test(e.name) ? [p] : [];
  });
}

test("Google Play link is the app's listing (com.wael.app)", () => {
  assert.equal(STORE_URLS.android, "https://play.google.com/store/apps/details?id=com.wael.app");
});

test("App Store link is the app's listing (id6820829162)", () => {
  assert.equal(STORE_URLS.ios, "https://apps.apple.com/eg/app/el-metr-academy/id6820829162");
});

test("App Store URLs appear only in store-links.ts", () => {
  const hits: string[] = [];
  for (const p of files(SRC)) {
    if (p.endsWith(join("lib", "store-links.ts"))) continue;
    const text = readFileSync(p, "utf8");
    if (/apps\.apple\.com|itunes\.apple\.com/.test(text)) hits.push(relative(SRC, p));
  }
  assert.deepEqual(hits, []);
});

test("every Play badge has an App Store badge beside it", () => {
  for (const name of ["home-page.tsx", "site-shell.tsx"]) {
    const text = readFileSync(join(SRC, "components", name), "utf8");
    assert.match(text, /href=\{STORE_URLS\.android\}/, name);
    assert.match(text, /href=\{STORE_URLS\.ios\}/, name);
    assert.match(text, /\/brand\/appstore-badge-ar\.svg/, name);
    assert.match(text, /\/brand\/appstore-badge-en\.svg/, name);
  }
});
