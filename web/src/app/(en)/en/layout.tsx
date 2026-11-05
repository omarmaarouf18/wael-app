import "../../globals.css";

import type { Metadata } from "next";
import type { ReactNode } from "react";

import { AuthProvider } from "@/components/auth-provider";
import { fontVariables } from "@/lib/fonts";

export const metadata: Metadata = {
  title: { default: "EL METR ACADEMY", template: "%s | EL METR ACADEMY" },
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
    <html lang="en" dir="ltr" className={fontVariables}>
      <body>
        <AuthProvider>{children}</AuthProvider>
      </body>
    </html>
  );
}
