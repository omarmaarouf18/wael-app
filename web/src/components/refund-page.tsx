import type { Metadata } from "next";
import Link from "next/link";

import { localePath, type Lang } from "@/lib/i18n";
import { SiteShell } from "./site-shell";
import styles from "./refund-page.module.css";

const REFUND_COPY = {
  ar: {
    title: "سياسة الاسترداد",
    notice:
      "مفيش استرداد من خلال الموقع. لأي طلب استرداد، أو استفسار عن تحويل أو طلب مرفوض، تواصل مع الأكاديمية مباشرة.",
    contactTitle: "بيانات التواصل ومقر الأكاديمية",
    whatsappLabel: "واتساب الأكاديمية",
    whatsappNumber: "+20 122 270 0727",
    whatsappUrl: "https://wa.me/201222700727",
    addressLabel: "العنوان",
    address: "61 - شارع 18 - توريل الجديدة - المنصورة",
    mapLabel: "الموقع على الخريطة",
    mapLinkText: "عرض المقر على خرائط Google",
    mapUrl: "https://maps.app.goo.gl/ASegpJDpQALSDf3D6",
    backHome: "العودة للرئيسية",
  },
  en: {
    title: "Refund Policy",
    notice:
      "There are no refunds through the website. For any refund request, or a question about a transfer or a rejected order, contact the academy directly.",
    contactTitle: "Contact Details and Academy Address",
    whatsappLabel: "Academy WhatsApp",
    whatsappNumber: "+20 122 270 0727",
    whatsappUrl: "https://wa.me/201222700727",
    addressLabel: "Address",
    address: "61 -18St - new toril - Mansoura",
    mapLabel: "Map Location",
    mapLinkText: "View academy on Google Maps",
    mapUrl: "https://maps.app.goo.gl/ASegpJDpQALSDf3D6",
    backHome: "Back to Home",
  },
} as const;

export function RefundPage({ lang }: { lang: Lang }) {
  const c = REFUND_COPY[lang];

  return (
    <SiteShell lang={lang} path="/refund">
      <div className={styles.container}>
        <h1 className={styles.title}>{c.title}</h1>

        <div className={styles.noticeCard}>
          <p className={styles.noticeText}>{c.notice}</p>
        </div>

        <section className={styles.contactSection} aria-labelledby="refund-contact-heading">
          <h2 id="refund-contact-heading" className={styles.contactTitle}>
            {c.contactTitle}
          </h2>

          <ul className={styles.contactList}>
            <li className={styles.contactItem}>
              <span className={styles.label}>{c.whatsappLabel}</span>
              <a href={c.whatsappUrl} target="_blank" rel="noopener noreferrer" className={styles.link} dir="ltr">
                {c.whatsappNumber}
              </a>
            </li>

            <li className={styles.contactItem}>
              <span className={styles.label}>{c.addressLabel}</span>
              <span className={styles.value}>{c.address}</span>
            </li>

            <li className={styles.contactItem}>
              <span className={styles.label}>{c.mapLabel}</span>
              <a href={c.mapUrl} target="_blank" rel="noopener noreferrer" className={styles.link}>
                {c.mapLinkText}
              </a>
            </li>
          </ul>
        </section>

        <p className={styles.backRow}>
          <Link href={localePath(lang, "/")} className={styles.link}>
            {c.backHome}
          </Link>
        </p>
      </div>
    </SiteShell>
  );
}

export function refundMetadata(lang: Lang): Metadata {
  const c = REFUND_COPY[lang];
  return {
    title: c.title,
    description: c.notice,
    alternates: { canonical: localePath(lang, "/refund"), languages: { ar: "/refund", en: "/en/refund" } },
  };
}
