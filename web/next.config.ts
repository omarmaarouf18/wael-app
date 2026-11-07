import type { NextConfig } from "next";

import { parseApiOrigin, serverApiOrigin } from "./src/lib/config";
import { buildStaticCsp } from "./src/lib/csp";

// Fail the build (locally, in CI and on Vercel) on a bad
// NEXT_PUBLIC_API_BASE_URL or API_BASE_URL instead of serving 500s at
// request time.
const apiOrigin = parseApiOrigin(process.env.NEXT_PUBLIC_API_BASE_URL);
const catalogOrigin = serverApiOrigin();
// On Vercel the catalog pages need the API (ADR-0014 W2b); off Vercel (CI,
// local) an unset API_BASE_URL builds them as "unavailable".
if (process.env.VERCEL === "1" && catalogOrigin === null) {
  throw new Error("API_BASE_URL is required on Vercel (server-side API origin for the catalog pages)");
}

// NODE_ENV is "development" only under `next dev`.
const dev = process.env.NODE_ENV === "development";

// Every page except the form and dynamic user pages (login, signup, forgot-password, account, in both languages):
// they get the per-request nonce CSP from src/proxy.ts instead.
const STATIC_PAGE_SOURCE = "/((?!(?:en/)?(?:login|signup|forgot-password|account)(?:/|$)).*)";

// Static security headers on every response (docs/web/README.md "Security
// headers"). The Content-Security-Policy is split (ADR-0014 decision 7): a
// fixed policy for the static pages here, a per-request nonce policy on the
// form pages from src/proxy.ts.
const securityHeaders = [
  // Same value as the API and admin sites (owner decision 2026-10-05, S3).
  { key: "Strict-Transport-Security", value: "max-age=31536000; includeSubDomains" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  // Legacy twin of CSP frame-ancestors 'none' for old browsers.
  { key: "X-Frame-Options", value: "DENY" },
  {
    key: "Permissions-Policy",
    value:
      "accelerometer=(), autoplay=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), " +
      "magnetometer=(), microphone=(), midi=(), payment=(), publickey-credentials-get=(), " +
      "screen-wake-lock=(), serial=(), usb=(), xr-spatial-tracking=(), browsing-topics=()",
  },
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
];

const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  // No remote images: logo and art are local files under public/brand/.
  images: { unoptimized: true },
  // One 404 page for URLs that match no route (src/app/global-not-found.tsx).
  // The site has two root layouts ((ar) and (en)), so there is no single
  // layout for Next.js to put its built-in 404 in; that built-in page also
  // used inline styles the static CSP blocks. Documented for exactly this
  // case (multiple root layouts); experimental in Next.js 16.4.
  experimental: { globalNotFound: true },
  async headers() {
    return [
      { source: "/:path*", headers: securityHeaders },
      { source: STATIC_PAGE_SOURCE, headers: [{ key: "Content-Security-Policy", value: buildStaticCsp(apiOrigin, dev) }] },
    ];
  },
  async redirects() {
    return [
      {
        source: "/terms",
        destination: "https://legal.elmetracademy.app/terms",
        permanent: true,
      },
      {
        source: "/en/terms",
        destination: "https://legal.elmetracademy.app/terms#en",
        permanent: true,
      },
      {
        source: "/privacy",
        destination: "https://legal.elmetracademy.app/privacy",
        permanent: true,
      },
      {
        source: "/en/privacy",
        destination: "https://legal.elmetracademy.app/privacy#en",
        permanent: true,
      },
    ];
  },
};

export default nextConfig;
