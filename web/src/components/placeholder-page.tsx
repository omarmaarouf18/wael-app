import type { Metadata } from "next";

import { dictionary, type Lang, type PageKey } from "@/lib/i18n";

import styles from "./placeholder-page.module.css";
import { SiteShell } from "./site-shell";

interface PlaceholderPageProps {
  lang: Lang;
  page: PageKey;
  /** Path of this page without the language prefix ("/about"). */
  path: string;
}

/**
 * Static placeholder for a page that has no real content yet. Every one
 * shows a visible TODO; no prices or subjects are made up (ADR-0014,
 * foundations brief).
 */
export function PlaceholderPage({ lang, page, path }: PlaceholderPageProps) {
  const t = dictionary(lang);
  const text = t.pages[page];
  return (
    <SiteShell lang={lang} path={path}>
      {page === "home" ? (
        <div className={styles.hero}>
          {/* Plain <img> for the strict CSP; see site-shell.tsx. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/brand/banner.jpg" alt={t.brand} width={1242} height={649} className={styles.heroImage} />
        </div>
      ) : null}
      <h1 className={styles.title}>{text.title}</h1>
      <p className={styles.lead}>{text.lead}</p>
      <p className={styles.todo}>
        <span className={styles.badge}>{t.todoBadge}</span>
        <span>{text.todo}</span>
      </p>
    </SiteShell>
  );
}

/** Page title for a placeholder page; the layout adds the site name. */
export function placeholderMetadata(lang: Lang, page: PageKey): Metadata {
  const text = dictionary(lang).pages[page];
  return page === "home" ? { description: text.lead } : { title: text.title, description: text.lead };
}
