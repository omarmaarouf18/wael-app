"use client";

import Link from "next/link";
import { useCart } from "./cart-provider";
import type { Lang } from "@/lib/i18n";
import { localePath } from "@/lib/i18n";
import styles from "./sticky-mobile-action.module.css";

interface StickyMobileActionProps {
  lang: Lang;
  path: string;
}

export function StickyMobileAction({ lang, path }: StickyMobileActionProps) {
  const { itemCount, openCart } = useCart();

  // Only show on home and catalog pages; hide everywhere else (forms, account, legal, etc.)
  if (path !== "/" && path !== "/subjects") {
    return null;
  }

  const to = (p: string) => localePath(lang, p);

  if (itemCount > 0) {
    return (
      <div className={styles.bar} role="region" aria-label={lang === "ar" ? "إجراء سريع" : "Quick action"}>
        <button type="button" onClick={openCart} className={styles.actionButton}>
          <svg className={styles.icon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M3 4h2l2.4 11.2a1 1 0 0 0 1 .8h8.9a1 1 0 0 0 1-.8L20 8H6.2" />
            <circle cx="9.5" cy="19.5" r="1.3" />
            <circle cx="17" cy="19.5" r="1.3" />
          </svg>
          <span className={styles.label}>
            {lang === "ar" ? `السلة (${itemCount})` : `Cart (${itemCount})`}
          </span>
          <span className={styles.actionPrompt}>
            {lang === "ar" ? "إتمام الشراء ←" : "Checkout →"}
          </span>
        </button>
      </div>
    );
  }

  return (
    <div className={styles.bar} role="region" aria-label={lang === "ar" ? "إجراء سريع" : "Quick action"}>
      {path === "/" ? (
        <a href="#subjects" className={styles.actionButton}>
          <svg className={styles.icon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
            <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
          </svg>
          <span className={styles.label}>
            {lang === "ar" ? "تصفّح المواد" : "Browse subjects"}
          </span>
          <span className={styles.actionPrompt} aria-hidden="true">
            ↓
          </span>
        </a>
      ) : (
        <Link href={to("/subjects")} className={styles.actionButton}>
          <svg className={styles.icon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
            <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
          </svg>
          <span className={styles.label}>
            {lang === "ar" ? "تصفّح المواد" : "Browse subjects"}
          </span>
          <span className={styles.actionPrompt} aria-hidden="true">
            ←
          </span>
        </Link>
      )}
    </div>
  );
}
