import type { Metadata } from "next";

import styles from "@/components/placeholder-page.module.css";
import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "سياسة الخصوصية",
  description: "سياسة الخصوصية الخاصة بأكاديمية المتر",
};

export default function Page() {
  return (
    <SiteShell lang="ar" path="/privacy">
      <h1>سياسة الخصوصية</h1>
      <p className={styles.legalPara}>
        تتوفر سياسة الخصوصية الرسمية للأكاديمية عبر الرابط التالي:
      </p>
      <p className={styles.legalLinkRow}>
        <a
          href="https://legal.elmetracademy.app/privacy"
          target="_blank"
          rel="noopener noreferrer"
          className={styles.legalLink}
        >
          https://legal.elmetracademy.app/privacy ↗
        </a>
      </p>
    </SiteShell>
  );
}
