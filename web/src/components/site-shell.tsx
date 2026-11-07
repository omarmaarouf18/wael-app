import Link from "next/link";
import type { ReactNode } from "react";

import { dictionary, localePath, type Lang } from "@/lib/i18n";
import { STORE_URLS } from "@/lib/store-links";
import { HeaderAuthNav } from "./auth-provider";
import { CartDrawer } from "./cart-drawer";
import { HeaderCartButton } from "./cart-provider";
import { HeaderMenu } from "./header-menu";

import styles from "./site-shell.module.css";

interface SiteShellProps {
  lang: Lang;
  /** Path of the current page without the language prefix ("/about"). */
  path: string;
  children: ReactNode;
  /** Full-width main with no padding (the landing page's banded sections). */
  fullBleed?: boolean;
}

/** Header, main and footer shared by every page (web/DESIGN.md "Header", "Footer"). */
export function SiteShell({ lang, path, children, fullBleed = false }: SiteShellProps) {
  const t = dictionary(lang);
  const other: Lang = lang === "ar" ? "en" : "ar";
  const to = (p: string) => localePath(lang, p);

  const legalTermsUrl =
    lang === "en" ? "https://legal.elmetracademy.app/terms#en" : "https://legal.elmetracademy.app/terms";
  const legalPrivacyUrl =
    lang === "en" ? "https://legal.elmetracademy.app/privacy#en" : "https://legal.elmetracademy.app/privacy";

  // The language switch keeps the page: a full load (two root layouts).
  const langSwitch = (
    <a
      href={localePath(other, path)}
      className={styles.langSwitch}
      hrefLang={other}
      lang={other}
      aria-label={t.switchLanguageLabel}
    >
      {t.switchLanguage}
    </a>
  );
  const pageLinks = (
    <>
      <Link href={to("/subjects")} className={styles.navLink} aria-current={path === "/subjects" ? "page" : undefined}>
        {t.nav.subjects}
      </Link>
      <Link href={to("/about")} className={styles.navLink} aria-current={path === "/about" ? "page" : undefined}>
        {t.nav.about}
      </Link>
    </>
  );

  return (
    <>
      <a href="#main" className={styles.skipLink}>
        {t.skipToContent}
      </a>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Link href={to("/")} className={styles.brand}>
            {/* A plain <img>: next/image adds an inline style attribute,
                which the strict CSP (style-src without 'unsafe-inline')
                blocks. The logo is a local, already-sized file. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/brand/logo-72.png" alt="" width={36} height={36} className={styles.logo} />
            <span className={styles.brandText}>
              <span className={styles.brandName}>{t.brand}</span>
              <span className={styles.brandEyebrow} lang={other}>
                {t.brandEyebrow}
              </span>
            </span>
          </Link>

          {/* 1024 px and up: inline navigation. */}
          <nav className={styles.navDesktop} aria-label={t.brand}>
            {pageLinks}
          </nav>
          <div className={styles.actions}>
            <HeaderCartButton lang={lang} />
            <div className={styles.desktopOnly}>
              <HeaderAuthNav lang={lang} />
              {langSwitch}
            </div>
            {/* Under 1024 px: the same links in a menu. */}
            <div className={styles.mobileOnly}>
              <HeaderMenu label={t.common.menu}>
                <nav className={styles.menuNav} aria-label={t.brand}>
                  {pageLinks}
                  <HeaderAuthNav lang={lang} />
                  {langSwitch}
                </nav>
              </HeaderMenu>
            </div>
          </div>
        </div>
      </header>
      <main id="main" tabIndex={-1} className={fullBleed ? styles.mainBleed : styles.main}>
        {children}
      </main>
      <CartDrawer lang={lang} />
      <footer className={styles.footer}>
        <div className={styles.footerInner}>
          <div className={styles.footerBrand}>
            <span className={styles.footerName}>{t.brand}</span>
            <span className={styles.footerEyebrow} lang={other}>
              {t.brandEyebrow}
            </span>
            <p className={styles.footerLine}>{t.footer.brandLine}</p>
          </div>
          <nav className={styles.footerCol} aria-label={t.footer.explore}>
            <h2 className={styles.footerTitle}>{t.footer.explore}</h2>
            <Link href={to("/subjects")}>{t.nav.subjects}</Link>
            <Link href={to("/about")}>{t.nav.about}</Link>
            <a href={to("/account")}>{t.nav.account}</a>
          </nav>
          <nav className={styles.footerCol} aria-label={t.footer.legal}>
            <h2 className={styles.footerTitle}>{t.footer.legal}</h2>
            <a href={legalTermsUrl} target="_blank" rel="noopener noreferrer">
              {t.footer.terms}
            </a>
            <a href={legalPrivacyUrl} target="_blank" rel="noopener noreferrer">
              {t.footer.privacy}
            </a>
            <Link href={to("/refund")}>{t.footer.refund}</Link>
          </nav>
          <div className={styles.footerCol}>
            <h2 className={styles.footerTitle}>{t.footer.app}</h2>
            <div className={styles.footerStores}>
              <a href={STORE_URLS.android} target="_blank" rel="noopener noreferrer" className={styles.storeLink} lang="en">
                Google Play
              </a>
            </div>
            <h2 className={styles.footerTitle}>{t.footer.language}</h2>
            {langSwitch}
          </div>
        </div>
        <div className={styles.footerBottom}>
          <span>{t.footer.rights}</span>
        </div>
      </footer>
    </>
  );
}
