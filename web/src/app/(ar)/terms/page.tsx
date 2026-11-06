import type { Metadata } from "next";

import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "الشروط والأحكام",
  description: "الشروط والأحكام الخاصة بأكاديمية المتر",
};

export default function Page() {
  return (
    <SiteShell lang="ar" path="/terms">
      <h1>الشروط والأحكام</h1>
      <p style={{ marginTop: "1rem" }}>
        تتوفر الشروط والأحكام الرسمية للأكاديمية عبر الرابط التالي:
      </p>
      <p style={{ marginTop: "0.5rem" }}>
        <a
          href="https://legal.elmetracademy.app/terms"
          target="_blank"
          rel="noopener noreferrer"
          style={{ textDecoration: "underline" }}
        >
          https://legal.elmetracademy.app/terms ↗
        </a>
      </p>
    </SiteShell>
  );
}
