import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  globalIgnores([".next/**", "out/**", "next-env.d.ts"]),
  {
    rules: {
      "no-eval": "error",
      "no-new-func": "error",
      "no-restricted-properties": [
        "error",
        {
          "property": "dangerouslySetInnerHTML",
          "message": "dangerouslySetInnerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "property": "innerHTML",
          "message": "innerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "property": "outerHTML",
          "message": "outerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "property": "insertAdjacentHTML",
          "message": "insertAdjacentHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "object": "document",
          "property": "write",
          "message": "document.write is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "object": "document",
          "property": "writeln",
          "message": "document.writeln is forbidden as a CSP compensating control (H1 / 5.3).",
        },
      ],
      "no-restricted-syntax": [
        "error",
        {
          "selector": "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          "message": "dangerouslySetInnerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "CallExpression[callee.name='eval']",
          "message": "eval is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "CallExpression[callee.name='Function']",
          "message": "Function is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "NewExpression[callee.name='Function']",
          "message": "new Function is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "MemberExpression[property.name='innerHTML']",
          "message": "innerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "MemberExpression[property.name='outerHTML']",
          "message": "outerHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "CallExpression[callee.property.name='insertAdjacentHTML']",
          "message": "insertAdjacentHTML is forbidden as a CSP compensating control (H1 / 5.3).",
        },
        {
          "selector": "CallExpression[callee.object.name='document'][callee.property.name='write']",
          "message": "document.write is forbidden as a CSP compensating control (H1 / 5.3).",
        },
      ],
    },
  },
]);
