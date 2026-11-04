import { NextResponse, type NextRequest } from "next/server";

import { API_ORIGIN } from "./lib/config";
import { buildCsp } from "./lib/csp";

// Per-request CSP nonce (Next.js "Content Security Policy" guide). Next.js
// reads the nonce from the request's CSP header and puts it on its own
// scripts and styles; pages are therefore rendered per request.
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

export const config = {
  matcher: [
    {
      // Pages only: static files carry the other headers from next.config.ts.
      source: "/((?!_next/static|_next/image|brand/|favicon.ico|robots.txt).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
