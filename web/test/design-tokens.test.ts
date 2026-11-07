// Design-token guard (web/DESIGN.md "Rules"): raw design values live only in
// src/app/tokens.css. Fails on a colour literal or a physical left/right
// property in any other stylesheet, and on an inline style= in any component
// or page (the CSP blocks server-rendered style attributes).
// Run: npm test (node --test).

import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { test } from "node:test";

const SRC = join(import.meta.dirname, "..", "src");
const TOKENS = join(SRC, "app", "tokens.css");

function files(dir: string, ext: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return files(p, ext);
    return e.name.endsWith(ext) ? [p] : [];
  });
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

function hits(paths: string[], re: RegExp, prepare: (s: string) => string = (s) => s): string[] {
  const out: string[] = [];
  for (const p of paths) {
    prepare(readFileSync(p, "utf8"))
      .split("\n")
      .forEach((line, i) => {
        if (re.test(line)) out.push(`${relative(SRC, p)}:${i + 1}: ${line.trim()}`);
      });
  }
  return out;
}

const stylesheets = files(SRC, ".css").filter((p) => p !== TOKENS);

test("no colour literal outside tokens.css", () => {
  const found = hits(stylesheets, /#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(/, stripComments);
  assert.deepEqual(found, []);
});

test("no physical left/right properties (logical properties only)", () => {
  const found = hits(
    stylesheets,
    /(^|[\s;{])(margin|padding|border)-(left|right)\b|(^|[\s;{])(left|right)\s*:|text-align:\s*(left|right)/,
    stripComments,
  );
  assert.deepEqual(found, []);
});

test("no inline style attributes in components or pages", () => {
  const found = hits(files(SRC, ".tsx"), /\bstyle=\{/);
  assert.deepEqual(found, []);
});

test("tokens.css defines every custom property the stylesheets use", () => {
  const defined = new Set(
    [...readFileSync(TOKENS, "utf8").matchAll(/(--[a-z0-9-]+)\s*:/g)].map((m) => m[1]),
  );
  // Set by next/font (src/lib/fonts.ts), not by tokens.css.
  for (const v of ["--font-cairo", "--font-syne", "--font-plus-jakarta-sans"]) defined.add(v);
  const used = new Set<string>();
  for (const p of [...stylesheets, TOKENS]) {
    for (const m of stripComments(readFileSync(p, "utf8")).matchAll(/var\((--[a-z0-9-]+)/g)) used.add(m[1]!);
  }
  const missing = [...used].filter((v) => !defined.has(v)).sort();
  assert.deepEqual(missing, []);
});
