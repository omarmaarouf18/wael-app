import type { NextConfig } from "next";

import { parseApiOrigin } from "./src/lib/config";

// Fail the build (locally, in CI and on Vercel) on a bad
// NEXT_PUBLIC_API_BASE_URL instead of serving 500s at request time.
parseApiOrigin(process.env.NEXT_PUBLIC_API_BASE_URL);

// Static security headers on every response (docs/web/README.md "Security
// headers"). The Content-Security-Policy needs a per-request nonce, so it is
// set by src/proxy.ts instead.
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
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

export default nextConfig;
