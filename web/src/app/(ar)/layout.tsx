import "../globals.css";

import type { Metadata } from "next";
import type { ReactNode } from "react";

import { AuthProvider } from "@/components/auth-provider";
import { CartProvider } from "@/components/cart-provider";
import { fontVariables } from "@/lib/fonts";

export const metadata: Metadata = {
  title: { default: "أكاديمية المتر", template: "%s | أكاديمية المتر" },
  // Absolute URLs for Open Graph images and canonical links.
  metadataBase: new URL("https://elmetracademy.app"),
  icons: {
    icon: [
      { url: "/favicon.ico", sizes: "48x48" },
      { url: "/brand/favicon-32.png", sizes: "32x32", type: "image/png" },
      { url: "/brand/icon-192.png", sizes: "192x192", type: "image/png" },
    ],
    apple: [{ url: "/brand/apple-touch-icon.png", sizes: "180x180" }],
  },
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
      <body>
        <AuthProvider>
          <CartProvider>{children}</CartProvider>
        </AuthProvider>
      </body>
    </html>
  );
}
