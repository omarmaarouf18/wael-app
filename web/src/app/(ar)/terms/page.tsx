import type { Metadata } from "next";

import styles from "@/components/placeholder-page.module.css";
import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "الشروط والأحكام",
  description: "الشروط والأحكام الخاصة بأكاديمية المتر",
};

export default function Page() {
  return (
    <SiteShell lang="ar" path="/terms">
      <h1>الشروط والأحكام</h1>
      <p className={styles.legalPara}>
        تتوفر الشروط والأحكام الرسمية للأكاديمية عبر الرابط التالي:
      </p>
      <p className={styles.legalLinkRow}>
        <a
          href="https://legal.elmetracademy.app/terms"
          target="_blank"
          rel="noopener noreferrer"
          className={styles.legalLink}
        >
          https://legal.elmetracademy.app/terms ↗
        </a>
      </p>
    </SiteShell>
  );
}
