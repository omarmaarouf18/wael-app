import type { Metadata } from "next";
import Link from "next/link";
import type { ReactNode } from "react";
import { preload } from "react-dom";

import { pick, type CatalogStudyType } from "@/lib/catalog-data";
import { getCatalog } from "@/lib/catalog";
import { homeCopy } from "@/lib/home-copy";
import { dictionary, localePath, numberLocale, type Lang } from "@/lib/i18n";
import { showTodoBlocks } from "@/lib/site-env";
import { STORE_URLS } from "@/lib/store-links";

import styles from "./home-page.module.css";
import { SiteShell } from "./site-shell";

// Landing page (UI/UX brief step 2, docs/web/landing-plan.md). A server
// component rendered at build time (static, refreshed with the catalog every
// 300 s): it adds no client JavaScript and no API call beyond the shared
// server-side catalog read. Plain <img>/<picture>, never next/image (its
// inline style breaks the CSP).

// Phones (< 768 px) show a 1:1 crop of the art's top: they get square files
// (hero-sq-*) instead of the full 2:3 picture, so fewer bytes before LCP.
const HERO_WIDTHS = [480, 720, 1024] as const;
const HERO_SQ_WIDTHS = [480, 840, 1024] as const;
const HERO_SIZES = "(min-width: 1024px) 460px, 38vw";
const MOBILE = "(max-width: 767px)";
const heroSet = (ext: string) => HERO_WIDTHS.map((w) => `/brand/hero/hero-${w}.${ext} ${w}w`).join(", ");
const heroSqSet = (ext: string) => HERO_SQ_WIDTHS.map((w) => `/brand/hero/hero-sq-${w}.${ext} ${w}w`).join(", ");

/** A visible note for copy or a photo the owner has not given (non-production builds only). */
function Todo({ lang, children }: { lang: Lang; children: ReactNode }) {
  if (!showTodoBlocks()) return null;
  return (
    <p className={styles.todo}>
      <span className={styles.todoBadge}>TODO · {dictionary(lang).todoBadge}</span>
      <span>{children}</span>
    </p>
  );
}

function Eyebrow({ children, lang }: { children: string; lang?: Lang }) {
  return (
    <p className={styles.eyebrow} lang={lang}>
      <span className={styles.eyebrowRule} aria-hidden="true" />
      {children}
    </p>
  );
}

function SubjectsByYear({ lang, studyTypes }: { lang: Lang; studyTypes: CatalogStudyType[] }) {
  const c = homeCopy(lang).subjects;
  const to = (p: string) => localePath(lang, p);
  const groups = studyTypes
    .map((st) => ({ st, levels: st.levels.filter((l) => l.subjects.length > 0) }))
    .filter((g) => g.levels.length > 0);
  return (
    <>
      {groups.map(({ st, levels }) => (
        <div key={st.key} className={styles.studyGroup}>
          <h3 className={styles.studyTitle} lang={lang === "en" && st.title.en.trim() === "" ? "ar" : undefined}>
            {pick(st.title, lang)}
          </h3>
          <ul className={styles.levelGrid}>
            {levels.map((l) => (
              <li key={l.key}>
                <Link href={to(`/subjects#lv-${l.key}`)} className={styles.levelCard}>
                  <span
                    className={styles.levelTitle}
                    lang={lang === "en" && l.title.en.trim() === "" ? "ar" : undefined}
                  >
                    {pick(l.title, lang)}
                  </span>
                  <span className={styles.levelCount}>{c.count(l.subjects.length)}</span>
                  <span className={styles.levelArrow} aria-hidden="true">
                    ←
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </>
  );
}

export async function HomePage({ lang }: { lang: Lang }) {
  const c = homeCopy(lang);
  const to = (p: string) => localePath(lang, p);
  const catalog = await getCatalog();
  const n = new Intl.NumberFormat(numberLocale[lang]);
  const otherLang: Lang = lang === "ar" ? "en" : "ar";

  // The hero art is the LCP element: preload the AVIF set (browsers without
  // AVIF skip a typed preload and load the WebP/JPEG source instead).
  preload(`/brand/hero/hero-sq-1024.avif`, {
    as: "image",
    type: "image/avif",
    media: MOBILE,
    imageSrcSet: heroSqSet("avif"),
    imageSizes: "100vw",
    fetchPriority: "high",
  });
  preload(`/brand/hero/hero-1024.avif`, {
    as: "image",
    type: "image/avif",
    media: "(min-width: 768px)",
    imageSrcSet: heroSet("avif"),
    imageSizes: HERO_SIZES,
    fetchPriority: "high",
  });

  return (
    <SiteShell lang={lang} path="/" fullBleed>
      {/* 1. Hero */}
      <section className={styles.hero} aria-labelledby="hero-title">
        <div className={styles.heroInner}>
          <div className={styles.heroText}>
            <Eyebrow lang={lang === "ar" ? otherLang : undefined}>{c.eyebrow}</Eyebrow>
            <h1 id="hero-title" className={styles.heroTitle}>
              {c.heroTitle} <span className={styles.heroAccent}>{c.heroAccent}</span>
            </h1>
            <p className={styles.heroLead}>{c.heroLead}</p>
            <div className={styles.actions}>
              <Link href={to("/subjects")} className={styles.primaryButton}>
                {c.ctaBrowse}
              </Link>
              <a href="#app" className={styles.secondaryButton}>
                {c.ctaApp}
              </a>
            </div>
          </div>
          <picture className={styles.heroArt}>
            <source media={MOBILE} type="image/avif" srcSet={heroSqSet("avif")} sizes="100vw" width={1024} height={1024} />
            <source media={MOBILE} type="image/webp" srcSet={heroSqSet("webp")} sizes="100vw" width={1024} height={1024} />
            <source type="image/avif" srcSet={heroSet("avif")} sizes={HERO_SIZES} />
            <source type="image/webp" srcSet={heroSet("webp")} sizes={HERO_SIZES} />
            <img
              src="/brand/hero/hero-1024.jpg"
              srcSet={heroSet("jpg")}
              sizes={HERO_SIZES}
              width={1024}
              height={1536}
              alt={c.heroArtAlt}
              fetchPriority="high"
              decoding="async"
              className={styles.heroImg}
            />
          </picture>
        </div>
      </section>

      {/* 2. About the teacher */}
      <section id="teacher" className={styles.section} aria-labelledby="teacher-title">
        <div className={`${styles.container} ${styles.teacher}`}>
          <div className={styles.portrait}>
            {/* Brand logo until the owner confirms website use of the portrait. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/brand/icon-192.png"
              alt={c.teacher.logoAlt}
              width={192}
              height={192}
              loading="lazy"
              decoding="async"
              className={styles.portraitImg}
            />
          </div>
          <div>
            <Eyebrow>{c.teacher.eyebrow}</Eyebrow>
            <h2 id="teacher-title" className={styles.sectionTitle}>
              {c.teacher.title}
            </h2>
            <p className={styles.sectionLead}>{c.teacher.lead}</p>
            <Todo lang={lang}>{c.teacher.todoPortrait}</Todo>
            <Todo lang={lang}>{c.teacher.todoBio}</Todo>
          </div>
        </div>
      </section>

      {/* 3. Subjects by year (live catalog) */}
      <section id="subjects" className={`${styles.section} ${styles.sectionAlt}`} aria-labelledby="subjects-title">
        <div className={styles.container}>
          <div className={styles.sectionHead}>
            <div>
              <Eyebrow>{c.subjects.eyebrow}</Eyebrow>
              <h2 id="subjects-title" className={styles.sectionTitle}>
                {c.subjects.title}
              </h2>
              <p className={styles.sectionLead}>{c.subjects.lead}</p>
            </div>
            <Link href={to("/subjects")} className={styles.textLink}>
              {c.subjects.all}
            </Link>
          </div>
          {catalog.ok ? (
            <SubjectsByYear lang={lang} studyTypes={catalog.catalog.studyTypes} />
          ) : (
            <p className={styles.notice} role="status">
              {c.subjects.unavailable}
            </p>
          )}
        </div>
      </section>

      {/* 4. How to start */}
      <section id="how" className={styles.section} aria-labelledby="how-title">
        <div className={styles.container}>
          <Eyebrow>{c.steps.eyebrow}</Eyebrow>
          <h2 id="how-title" className={styles.sectionTitle}>
            {c.steps.title}
          </h2>
          <ol className={styles.steps}>
            {c.steps.items.map((s, i) => (
              <li key={s.title} className={styles.step}>
                <span className={styles.stepNumber} aria-hidden="true">
                  {n.format(i + 1)}
                </span>
                <h3 className={styles.stepTitle}>{s.title}</h3>
                <p className={styles.stepBody}>{s.body}</p>
              </li>
            ))}
          </ol>
          <Todo lang={lang}>{c.steps.todoActivation}</Todo>
        </div>
      </section>

      {/* 5. The app */}
      <section id="app" className={`${styles.section} ${styles.sectionAlt}`} aria-labelledby="app-title">
        <div className={styles.container}>
          <Eyebrow>{c.app.eyebrow}</Eyebrow>
          <h2 id="app-title" className={styles.sectionTitle}>
            {c.app.title}
          </h2>
          <p className={styles.sectionLead}>{c.app.lead}</p>
          <div className={styles.storeLinks}>
            <a href={STORE_URLS.android} className={styles.storeLink} rel="noopener noreferrer" target="_blank" lang="en">
              {c.app.android}
            </a>
            <a href={STORE_URLS.ios} className={styles.storeLink} rel="noopener noreferrer" target="_blank" lang="en">
              {c.app.ios}
            </a>
          </div>
          <Todo lang={lang}>{c.app.todoScreens}</Todo>
          <Todo lang={lang}>{c.app.todoBadges}</Todo>
        </div>
      </section>

      {/* 6. The center and 7. Testimonials: omitted on the live site until the owner gives the facts. */}
      {showTodoBlocks() ? (
        <section id="center" className={styles.section} aria-labelledby="center-title">
          <div className={styles.container}>
            <h2 id="center-title" className={styles.sectionTitle}>
              {c.center.title}
            </h2>
            <Todo lang={lang}>{c.center.todo}</Todo>
            <h2 className={`${styles.sectionTitle} ${styles.spacedTitle}`}>{c.testimonials.title}</h2>
            <Todo lang={lang}>{c.testimonials.todo}</Todo>
          </div>
        </section>
      ) : null}

      {/* 8. FAQ: only answered questions on the live site */}
      <section id="faq" className={`${styles.section} ${styles.sectionAlt}`} aria-labelledby="faq-title">
        <div className={`${styles.container} ${styles.narrow}`}>
          <Eyebrow>{c.faq.eyebrow}</Eyebrow>
          <h2 id="faq-title" className={styles.sectionTitle}>
            {c.faq.title}
          </h2>
          <div className={styles.faqList}>
            {c.faq.items.map((item) =>
              item.a !== null ? (
                <details key={item.q} className={styles.faqItem}>
                  <summary className={styles.faqQuestion}>{item.q}</summary>
                  <p className={styles.faqAnswer}>{item.a}</p>
                </details>
              ) : showTodoBlocks() ? (
                <details key={item.q} className={styles.faqItem}>
                  <summary className={styles.faqQuestion}>{item.q}</summary>
                  <Todo lang={lang}>{item.todo}</Todo>
                </details>
              ) : null,
            )}
          </div>
        </div>
      </section>

      {/* 9. Closing call to action */}
      <section className={styles.closing} aria-labelledby="closing-title">
        <div className={`${styles.container} ${styles.closingInner}`}>
          <h2 id="closing-title" className={styles.sectionTitle}>
            {c.closing.title}
          </h2>
          <p className={styles.sectionLead}>{c.closing.lead}</p>
          <div className={`${styles.actions} ${styles.actionsCentered}`}>
            <Link href={to("/subjects")} className={styles.primaryButton}>
              {c.ctaBrowse}
            </Link>
            <a href="#app" className={styles.textLink}>
              {c.ctaApp}
            </a>
          </div>
        </div>
      </section>
    </SiteShell>
  );
}

export function homeMetadata(lang: Lang): Metadata {
  const c = homeCopy(lang);
  return {
    title: { absolute: c.metaTitle },
    description: c.metaDescription,
    alternates: { canonical: localePath(lang, "/"), languages: { ar: "/", en: "/en" } },
    openGraph: {
      type: "website",
      siteName: lang === "ar" ? "أكاديمية المتر" : "EL METR Academy",
      locale: lang === "ar" ? "ar_EG" : "en_US",
      title: c.metaTitle,
      description: c.metaDescription,
      url: localePath(lang, "/"),
      images: [{ url: `/brand/og-${lang}.jpg`, width: 1200, height: 630, alt: c.metaTitle }],
    },
    twitter: { card: "summary_large_image", title: c.metaTitle, description: c.metaDescription, images: [`/brand/og-${lang}.jpg`] },
  };
}
