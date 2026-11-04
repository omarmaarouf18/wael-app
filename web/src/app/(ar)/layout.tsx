import "../globals.css";

import type { Metadata } from "next";
import type { ReactNode } from "react";

import { fontVariables } from "@/lib/fonts";

export const metadata: Metadata = {
  title: { default: "أكاديمية المتر", template: "%s | أكاديمية المتر" },
  icons: { icon: "/brand/logo-512.png" },
  // Placeholder site (foundations): kept out of search engines until W5
  // puts real pages up.
  robots: { index: false, follow: false },
};

// No connection() here: the fixed pages prerender static with the static CSP
// from next.config.ts; only the form pages call connection() themselves to
// get the per-request nonce (ADR-0014 decision 7, options A + C).
export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="ar" dir="rtl" className={fontVariables}>
      <body>{children}</body>
    </html>
  );
}
