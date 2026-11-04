import "../globals.css";

import type { Metadata } from "next";
import { connection } from "next/server";
import type { ReactNode } from "react";

import { fontVariables } from "@/lib/fonts";

export const metadata: Metadata = {
  title: { default: "أكاديمية المتر", template: "%s | أكاديمية المتر" },
  icons: { icon: "/brand/logo-512.png" },
  // Placeholder site (foundations): kept out of search engines until W5
  // puts real pages up.
  robots: { index: false, follow: false },
};

export default async function RootLayout({ children }: { children: ReactNode }) {
  // Render per request: Next.js applies the CSP nonce from src/proxy.ts
  // only to dynamically rendered pages.
  await connection();
  return (
    <html lang="ar" dir="rtl" className={fontVariables}>
      <body>{children}</body>
    </html>
  );
}
