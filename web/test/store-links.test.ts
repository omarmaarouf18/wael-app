// Store links (owner decisions 2026-11-07, src/lib/store-links.ts): the
// Google Play listing is com.wael.app, and the iOS app is not live yet, so the
// site must not link to the App Store anywhere. Remove the second test when the
// owner says the iOS app is live.
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

test("no App Store link anywhere until the iOS app is live", () => {
  const hits: string[] = [];
  for (const p of files(SRC)) {
    const text = readFileSync(p, "utf8");
    // Comments may explain the rule; links and visible labels may not.
    // "//" after a colon is a URL ("https://"), not a comment.
    const code = text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
    if (/apps\.apple\.com|itunes\.apple\.com|App Store|آب ستور/.test(code)) hits.push(relative(SRC, p));
  }
  assert.deepEqual(hits, []);
});
