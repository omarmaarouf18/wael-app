import { NextResponse, type NextRequest } from "next/server";

import { API_ORIGIN } from "./lib/config";
import { buildCsp } from "./lib/csp";

// Per-request CSP nonce (Next.js "Content Security Policy" guide) for the
// pages with forms only (log in, sign up; buy in W4). Next.js reads the nonce
// from the request's CSP header and puts it on its own scripts and styles;
// those pages call connection() so they render per request. Every other page
// is static and gets the fixed CSP from next.config.ts (ADR-0014 decision 7).
// Keep this matcher, STATIC_PAGE_SOURCE in next.config.ts and
// DYNAMIC_ROUTES in scripts/web_gate.sh in step; the gate checks the built
// routes.
export function proxy(request: NextRequest) {
  const nonce = btoa(crypto.randomUUID());
  // NODE_ENV is "development" only under `next dev`; `next build` and
  // `next start` always run as production.
  const csp = buildCsp(nonce, API_ORIGIN, process.env.NODE_ENV === "development");

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  return response;
}

// Exact paths, one per form page, as literals (Next.js reads this config
// statically). A pattern with an optional "en/" group does not match
// "/login" (path-to-regexp makes the leading "/" optional with it).
export const config = {
  matcher: [
    {
      source: "/login",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
    {
      source: "/signup",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
    {
      source: "/en/login",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
    {
      source: "/en/signup",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
