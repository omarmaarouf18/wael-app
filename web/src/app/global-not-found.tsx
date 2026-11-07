import "./globals.css";

import type { Metadata } from "next";
import Link from "next/link";

import { fontVariables } from "@/lib/fonts";

import styles from "./global-not-found.module.css";

// 404 for URLs that match no route (next.config.ts experimental.globalNotFound).
// It renders outside both root layouts, so it cannot know the language from
// a layout: Arabic first (the site's default), English beside it.
export const metadata: Metadata = {
  title: "الصفحة غير موجودة · Page not found | أكاديمية المتر",
  robots: { index: false, follow: false },
};

export default function GlobalNotFound() {
  return (
    <html lang="ar" dir="rtl" className={fontVariables}>
      <body>
        <main className={styles.page}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/brand/logo-72.png" alt="" width={56} height={56} className={styles.logo} />
          <p className={styles.code} aria-hidden="true">
            404
          </p>
          <h1 className={styles.title}>الصفحة غير موجودة</h1>
          <p className={styles.lead}>الرابط ده مش موجود أو اتنقل. ارجع للرئيسية أو تصفّح المواد.</p>
          <div className={styles.actions}>
            <Link href="/" className={styles.primary}>
              الصفحة الرئيسية
            </Link>
            <Link href="/subjects" className={styles.secondary}>
              تصفّح المواد
            </Link>
          </div>
          <div className={styles.en} lang="en" dir="ltr">
            <p className={styles.enTitle}>Page not found</p>
            <p className={styles.lead}>This link does not exist or has moved.</p>
            <div className={styles.actions}>
              <Link href="/en" className={styles.secondary}>
                English home
              </Link>
              <Link href="/en/subjects" className={styles.secondary}>
                Browse subjects
              </Link>
            </div>
          </div>
        </main>
      </body>
    </html>
  );
}
