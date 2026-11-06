import type { Metadata } from "next";

import { SiteShell } from "@/components/site-shell";

export const metadata: Metadata = {
  title: "سياسة الخصوصية",
  description: "سياسة الخصوصية الخاصة بأكاديمية المتر",
};

export default function Page() {
  return (
    <SiteShell lang="ar" path="/privacy">
      <h1>سياسة الخصوصية</h1>
      <p style={{ marginTop: "1rem" }}>
        تتوفر سياسة الخصوصية الرسمية للأكاديمية عبر الرابط التالي:
      </p>
      <p style={{ marginTop: "0.5rem" }}>
        <a
          href="https://legal.elmetracademy.app/privacy"
          target="_blank"
          rel="noopener noreferrer"
          style={{ textDecoration: "underline" }}
        >
          https://legal.elmetracademy.app/privacy ↗
        </a>
      </p>
    </SiteShell>
  );
}
