import assert from "node:assert/strict";
import { test } from "node:test";
import { ESLint } from "eslint";

test("ESLint rules block dangerous HTML and dynamic code execution (CSP compensating controls)", async () => {
  const eslint = new ESLint();

  const cases = [
    { name: "dangerouslySetInnerHTML in JSX", code: 'export const Component = () => <div dangerouslySetInnerHTML={{ __html: "x" }} />;\n', file: "src/comp.tsx" },
    { name: "dangerouslySetInnerHTML property access", code: 'export const fn = (obj: any) => obj.dangerouslySetInnerHTML;\n', file: "src/fn.ts" },
    { name: "innerHTML assignment", code: 'export const fn = (el: HTMLElement) => { el.innerHTML = "x"; };\n', file: "src/fn.ts" },
    { name: "outerHTML assignment", code: 'export const fn = (el: HTMLElement) => { el.outerHTML = "x"; };\n', file: "src/fn.ts" },
    { name: "insertAdjacentHTML call", code: 'export const fn = (el: HTMLElement) => { el.insertAdjacentHTML("afterbegin", "x"); };\n', file: "src/fn.ts" },
    { name: "document.write call", code: 'export const fn = () => { document.write("x"); };\n', file: "src/fn.ts" },
    { name: "eval call", code: 'export const fn = () => { eval("1+1"); };\n', file: "src/fn.ts" },
    { name: "new Function constructor", code: 'export const fn = () => { new Function("return 1"); };\n', file: "src/fn.ts" },
    { name: "Function call without new", code: 'export const fn = () => { Function("return 1"); };\n', file: "src/fn.ts" },
  ];

  for (const tc of cases) {
    const results = await eslint.lintText(tc.code, { filePath: tc.file });
    const errors = results.flatMap((r) => r.messages.filter((m) => m.severity === 2));
    assert(
      errors.length > 0,
      `Expected ESLint error for ${tc.name}, but got none. Code: ${tc.code}`
    );
  }
});
