// Content-Security-Policy (docs/web/README.md "Security headers"). Two
// policies since W2b (ADR-0014 decision 7, options A + C):
// - buildCsp: per request with a fresh nonce, set by src/proxy.ts on the
//   pages with forms (log in, sign up; buy in W4). Next.js renders inline
//   bootstrap scripts, and a nonce allows them without 'unsafe-inline'.
// - buildStaticCsp: one fixed policy for the prerendered static pages, set
//   by next.config.ts. A static page has no per-request nonce, so it needs
//   'unsafe-inline' for Next.js's inline scripts. Styles need no inline
//   allowance: no static page has a <style> tag or style= attribute, and a
//   browser check (2026-11-04) saw no violation with style-src 'self'.
//   Tradeoff recorded in ADR-0014: those pages have no forms, no user input
//   and no login state.

/** Directives shared by both policies, after script-src and style-src. */
function commonDirectives(apiOrigin: string, dev: boolean): string[] {
  const directives = [
    "img-src 'self'",
    "font-src 'self'",
    // 'self' is Next.js's own navigation requests; the API origin is the only
    // other host the site may call.
    `connect-src 'self' ${apiOrigin}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    "frame-src 'none'",
    "manifest-src 'self'",
    "worker-src 'self'",
  ];
  if (!dev) {
    directives.push("upgrade-insecure-requests");
  }
  return directives;
}

/** Fixed CSP for the static pages (no nonce). */
export function buildStaticCsp(apiOrigin: string, dev: boolean): string {
  return [
    "default-src 'self'",
    `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ""}`,
    "style-src 'self'",
    ...commonDirectives(apiOrigin, dev),
  ].join("; ");
}

export function buildCsp(nonce: string, apiOrigin: string, dev: boolean): string {
  const directives = [
    "default-src 'self'",
    // 'strict-dynamic': scripts loaded by a nonced script are trusted too;
    // 'unsafe-eval' only under `next dev` (React's dev stack traces).
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? " 'unsafe-eval'" : ""}`,
    `style-src 'self' 'nonce-${nonce}'`,
    ...commonDirectives(apiOrigin, dev),
  ];
  return directives.join("; ");
}
