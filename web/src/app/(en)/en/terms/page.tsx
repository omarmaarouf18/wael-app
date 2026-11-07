import type { Metadata } from "next";

import styles from "@/components/placeholder-page.module.css";
import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "Terms & Conditions",
  description: "Terms and conditions of El Metr Academy",
};

export default function Page() {
  return (
    <SiteShell lang="en" path="/terms">
      <h1>Terms &amp; Conditions</h1>
      <p className={styles.legalPara}>
        The official terms and conditions of the academy are available at:
      </p>
      <p className={styles.legalLinkRow}>
        <a
          href="https://legal.elmetracademy.app/terms#en"
          target="_blank"
          rel="noopener noreferrer"
          className={styles.legalLink}
        >
          https://legal.elmetracademy.app/terms#en ↗
        </a>
      </p>
    </SiteShell>
  );
}
