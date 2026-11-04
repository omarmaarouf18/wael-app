// Content-Security-Policy for every page (docs/web/README.md "Security
// headers"). Built per request by src/proxy.ts with a fresh nonce: Next.js
// renders inline bootstrap scripts, and a nonce is the only way to allow
// them without 'unsafe-inline'.

export function buildCsp(nonce: string, apiOrigin: string, dev: boolean): string {
  const directives = [
    "default-src 'self'",
    // 'strict-dynamic': scripts loaded by a nonced script are trusted too;
    // 'unsafe-eval' only under `next dev` (React's dev stack traces).
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? " 'unsafe-eval'" : ""}`,
    `style-src 'self' 'nonce-${nonce}'`,
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
  return directives.join("; ");
}
