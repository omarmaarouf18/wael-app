import Link from "next/link";
import type { ReactNode } from "react";

import { dictionary, localePath, type Lang } from "@/lib/i18n";
import { HeaderAuthNav } from "./auth-provider";
import { CartDrawer } from "./cart-drawer";
import { HeaderCartButton } from "./cart-provider";

import styles from "./site-shell.module.css";

interface SiteShellProps {
  lang: Lang;
  /** Path of the current page without the language prefix ("/about"). */
  path: string;
  children: ReactNode;
}

/** Header, main and footer shared by every page. */
export function SiteShell({ lang, path, children }: SiteShellProps) {
  const t = dictionary(lang);
  const other: Lang = lang === "ar" ? "en" : "ar";
  const to = (p: string) => localePath(lang, p);

  const legalTermsUrl =
    lang === "en" ? "https://legal.elmetracademy.app/terms#en" : "https://legal.elmetracademy.app/terms";
  const legalPrivacyUrl =
    lang === "en" ? "https://legal.elmetracademy.app/privacy#en" : "https://legal.elmetracademy.app/privacy";

  return (
    <>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Link href={to("/")} className={styles.brand}>
            {/* A plain <img>: next/image adds an inline style attribute,
                which the strict CSP (style-src without 'unsafe-inline')
                blocks. The logo is a local, already-sized file. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/brand/logo-512.png" alt="" width={36} height={36} className={styles.logo} />
            <span className={styles.brandText}>
              <span className={styles.brandName}>{t.brand}</span>
              <span className={styles.brandEyebrow} lang={other}>
                {t.brandEyebrow}
              </span>
            </span>
          </Link>
          <nav className={styles.nav} aria-label={t.brand}>
            <Link href={to("/subjects")} className={styles.navLink}>
              {t.nav.subjects}
            </Link>
            <Link href={to("/about")} className={styles.navLink}>
              {t.nav.about}
            </Link>
            <HeaderCartButton lang={lang} />
            <HeaderAuthNav lang={lang} />
            <Link
              href={localePath(other, path)}
              className={styles.langSwitch}
              hrefLang={other}
              lang={other}
              aria-label={t.switchLanguageLabel}
            >
              {t.switchLanguage}
            </Link>
          </nav>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
      <CartDrawer lang={lang} />
      <footer className={styles.footer}>
        <div className={styles.footerInner}>
          <nav className={styles.footerLinks} aria-label={t.footer.terms}>
            <a href={legalTermsUrl} target="_blank" rel="noopener noreferrer">
              {t.footer.terms}
            </a>
            <a href={legalPrivacyUrl} target="_blank" rel="noopener noreferrer">
              {t.footer.privacy}
            </a>
            <Link href={to("/refund")}>{t.footer.refund}</Link>
          </nav>
          <span>{t.footer.brandLine}</span>
          <span>{t.footer.rights}</span>
        </div>
      </footer>
    </>
  );
}
