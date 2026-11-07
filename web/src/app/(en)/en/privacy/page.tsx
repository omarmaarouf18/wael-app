import type { Metadata } from "next";

import styles from "@/components/placeholder-page.module.css";
import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description: "Privacy policy of El Metr Academy",
};

export default function Page() {
  return (
    <SiteShell lang="en" path="/privacy">
      <h1>Privacy Policy</h1>
      <p className={styles.legalPara}>
        The official privacy policy of the academy is available at:
      </p>
      <p className={styles.legalLinkRow}>
        <a
          href="https://legal.elmetracademy.app/privacy#en"
          target="_blank"
          rel="noopener noreferrer"
          className={styles.legalLink}
        >
          https://legal.elmetracademy.app/privacy#en ↗
        </a>
      </p>
    </SiteShell>
  );
}
