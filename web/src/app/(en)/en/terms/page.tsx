import type { Metadata } from "next";

import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "Terms & Conditions",
  description: "Terms and conditions of El Metr Academy",
};

export default function Page() {
  return (
    <SiteShell lang="en" path="/terms">
      <h1>Terms &amp; Conditions</h1>
      <p style={{ marginTop: "1rem" }}>
        The official terms and conditions of the academy are available at:
      </p>
      <p style={{ marginTop: "0.5rem" }}>
        <a
          href="https://legal.elmetracademy.app/terms#en"
          target="_blank"
          rel="noopener noreferrer"
          style={{ textDecoration: "underline" }}
        >
          https://legal.elmetracademy.app/terms#en ↗
        </a>
      </p>
    </SiteShell>
  );
}
