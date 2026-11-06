import type { Metadata } from "next";

import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description: "Privacy policy of El Metr Academy",
};

export default function Page() {
  return (
    <SiteShell lang="en" path="/privacy">
      <h1>Privacy Policy</h1>
      <p style={{ marginTop: "1rem" }}>
        The official privacy policy of the academy is available at:
      </p>
      <p style={{ marginTop: "0.5rem" }}>
        <a
          href="https://legal.elmetracademy.app/privacy#en"
          target="_blank"
          rel="noopener noreferrer"
          style={{ textDecoration: "underline" }}
        >
          https://legal.elmetracademy.app/privacy#en ↗
        </a>
      </p>
    </SiteShell>
  );
}
