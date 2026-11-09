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

import { AddToCartButton } from "./add-to-cart-button";
import styles from "./home-page.module.css";
import { ScrollRevealInit } from "./scroll-reveal";
import { SiteShell } from "./site-shell";

// Landing page (UI/UX brief step 2, docs/web/landing-plan.md, Part 3 engagement pass).
// Server component rendered at build time (static, refreshed with the catalog every
// 300 s). Plain <img>/<picture>, never next/image (its inline style breaks CSP).

// Phones (< 768 px) show a 1:1 crop of the art's top: they get square files
// (hero-sq-*) instead of the full 2:3 picture, so fewer bytes before LCP.
const HERO_WIDTHS = [480, 720, 1024] as const;
const HERO_SQ_WIDTHS = [480, 840, 1024] as const;
const HERO_SIZES = "(min-width: 1024px) 460px, 38vw";
const MOBILE = "(max-width: 767px)";
const heroSet = (ext: string) => HERO_WIDTHS.map((w) => `/brand/hero/hero-${w}.${ext} ${w}w`).join(", ");
// Google Play listing screenshots (owner decision 2026-11-07), per language.
const SCREEN_SIZES = "(min-width: 768px) 240px, 42vw";
const screenSet = (name: string, ext: string) => `/app/${name}-360.${ext} 360w, /app/${name}-540.${ext} 540w`;
interface ScreenItem {
  name: string;
  height540: number;
  alt: string;
}

const APP_SCREENS: Record<Lang, readonly [ScreenItem, ScreenItem]> = {
  ar: [
    { name: "play-ar-1", height540: 1080, alt: "الشاشة الرئيسية في تطبيق أكاديمية المتر" },
    { name: "play-ar-2", height540: 960, alt: "شاشة الدورات في التطبيق: المواد حسب الفرقة" },
  ],
  en: [
    { name: "play-en-1", height540: 960, alt: "The EL METR Academy app home screen" },
    { name: "play-en-2", height540: 960, alt: "The app's courses screen: subjects by year" },
  ],
} as const;
const heroSqSet = (ext: string) => HERO_SQ_WIDTHS.map((w) => `/brand/hero/hero-sq-${w}.${ext} ${w}w`).join(", ");

function formatPrice(lang: Lang, price: number): string {
  return dictionary(lang).catalog.price(new Intl.NumberFormat(numberLocale[lang]).format(price));
}

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
  const d = dictionary(lang);
  const t = d.catalog;
  const to = (p: string) => localePath(lang, p);
  const groups = studyTypes
    .map((st) => ({
      st,
      levels: st.levels.filter((l) => l.subjects.length > 0),
    }))
    .filter((g) => g.levels.length > 0);

  return (
    <div className={styles.subjectsContainer}>
      {groups.map(({ st, levels }) => (
        <div key={st.key} className={styles.studyGroup}>
          {groups.length > 1 ? (
            <h3 className={styles.studyTitle} lang={lang === "en" && st.title.en.trim() === "" ? "ar" : undefined}>
              {pick(st.title, lang)}
            </h3>
          ) : null}
          {levels.map((l) => (
            <div key={l.key} className={styles.levelBlock}>
              <div className={styles.levelHead}>
                <h3 className={styles.levelTitle} lang={lang === "en" && l.title.en.trim() === "" ? "ar" : undefined}>
                  {pick(l.title, lang)}
                </h3>
                <span className={styles.levelCountBadge}>
                  {l.subjects.length} {lang === "ar" ? "مواد" : "subjects"}
                </span>
              </div>
              <ul className={styles.subjectSnapScroll}>
                {l.subjects.map((s) => (
                  <li key={s.id} className={styles.subjectCard}>
                    <div className={styles.subjectCardTop}>
                      {s.term !== "" ? (
                        <span className={styles.termBadge}>{t.term[s.term]}</span>
                      ) : (
                        <span className={styles.termPlaceholder} />
                      )}
                      <span className={styles.subjectPrice}>{formatPrice(lang, s.price)}</span>
                    </div>
                    <h4 className={styles.subjectTitle}>
                      <Link href={to(`/subjects/${s.id}`)} className={styles.subjectLink}>
                        {pick(s.title, lang)}
                      </Link>
                    </h4>
                    <p className={styles.subjectInstructor}>{pick(s.instructor, lang)}</p>
                    <div className={styles.subjectCardFoot}>
                      <AddToCartButton
                        id={s.id}
                        titleAr={s.title.ar}
                        titleEn={s.title.en}
                        price={s.price}
                        lang={lang}
                      />
                      <Link href={to(`/subjects/${s.id}`)} className={styles.subjectDetailsBtn}>
                        {t.details}
                      </Link>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

export async function HomePage({ lang }: { lang: Lang }) {
  const c = homeCopy(lang);
  const to = (p: string) => localePath(lang, p);
  const catalog = await getCatalog();
  const n = new Intl.NumberFormat(numberLocale[lang]);
  const otherLang: Lang = lang === "ar" ? "en" : "ar";

  const legalTermsUrl =
    lang === "en" ? "https://legal.elmetracademy.app/terms#en" : "https://legal.elmetracademy.app/terms";
  const legalPrivacyUrl =
    lang === "en" ? "https://legal.elmetracademy.app/privacy#en" : "https://legal.elmetracademy.app/privacy";

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
              {c.heroHeading.prefix}
              <span className={styles.heroAccent}>{c.heroHeading.accent}</span>
              {c.heroHeading.suffix}
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
      {(() => {
        const profile = catalog.ok ? catalog.catalog.teacherProfile : undefined;
        const nameVal = profile?.name ? pick(profile.name, lang).trim() : "";
        const teacherName = nameVal || (lang === "en" ? "Ustaz Wael El Saeed" : "الأستاذ وائل السعيد");
        const heading = nameVal ? (lang === "en" ? `About ${nameVal}` : `عن ${nameVal}`) : c.teacher.title;
        const titles = profile?.titles ? pick(profile.titles, lang).trim() : "";
        const bio = profile?.bio ? pick(profile.bio, lang).trim() : "";

        return (
          <section id="teacher" className={styles.section} data-reveal aria-labelledby="teacher-title">
            <div className={`${styles.container} ${styles.teacher}`}>
              {/* The app's director portrait (v3, owner decision 2026-11-07). */}
              <picture className={styles.portrait}>
                <source type="image/avif" srcSet="/brand/teacher/wael-192.avif 192w, /brand/teacher/wael-384.avif 384w" sizes="(min-width: 768px) 12rem, 9rem" />
                <source type="image/webp" srcSet="/brand/teacher/wael-192.webp 192w, /brand/teacher/wael-384.webp 384w" sizes="(min-width: 768px) 12rem, 9rem" />
                <img
                  src="/brand/teacher/wael-384.jpg"
                  srcSet="/brand/teacher/wael-192.jpg 192w, /brand/teacher/wael-384.jpg 384w"
                  sizes="(min-width: 768px) 12rem, 9rem"
                  alt={teacherName}
                  width={384}
                  height={384}
                  loading="lazy"
                  decoding="async"
                  className={styles.portraitImg}
                />
              </picture>
              <div>
                <Eyebrow>{c.teacher.eyebrow}</Eyebrow>
                <h2 id="teacher-title" className={styles.sectionTitle}>
                  {heading}
                </h2>
                {titles ? <p className={styles.teacherTitles}>{titles}</p> : null}
                {bio ? <p className={styles.teacherBio}>{bio}</p> : null}
              </div>
            </div>
          </section>
        );
      })()}

      {/* 3. Subjects by year (live catalog) */}
      <section id="subjects" className={`${styles.section} ${styles.sectionAlt}`} data-reveal aria-labelledby="subjects-title">
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

          {/* Trust indicator & legal links near buy actions */}
          <div className={styles.trustBanner}>
            <div className={styles.trustItem}>
              <svg className={styles.trustIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                <polyline points="9 12 11 14 15 10" />
              </svg>
              <span>
                {lang === "ar"
                  ? "تحويلات الدفع تتم مراجعتها وتفعيلها من قِبل إدارة الأكاديمية"
                  : "Payments are reviewed and activated directly by the academy"}
              </span>
            </div>
            <div className={styles.trustLinks}>
              <a href={legalTermsUrl} target="_blank" rel="noopener noreferrer">
                {dictionary(lang).footer.terms}
              </a>
              <span aria-hidden="true">·</span>
              <a href={legalPrivacyUrl} target="_blank" rel="noopener noreferrer">
                {dictionary(lang).footer.privacy}
              </a>
              <span aria-hidden="true">·</span>
              <Link href={localePath(lang, "/refund")}>
                {dictionary(lang).footer.refund}
              </Link>
            </div>
          </div>
        </div>
      </section>

      {/* 4. How to start */}
      <section id="how" className={styles.section} data-reveal aria-labelledby="how-title">
        <div className={styles.container}>
          <Eyebrow>{c.steps.eyebrow}</Eyebrow>
          <h2 id="how-title" className={styles.sectionTitle}>
            {c.steps.title}
          </h2>
          <div className={styles.stepsSequence}>
            <div className={styles.stepsLine} aria-hidden="true" />
            <ol className={styles.stepsList}>
              {c.steps.items.map((s, i) => (
                <li key={s.title} className={styles.stepItem}>
                  <div className={styles.stepBadgeRow}>
                    <span className={styles.stepNumberBadge} aria-hidden="true">
                      {n.format(i + 1)}
                    </span>
                    <div className={styles.stepIconWrap} aria-hidden="true">
                      {i === 0 ? (
                        <svg className={styles.stepIcon} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <rect x="2" y="5" width="20" height="14" rx="2" />
                          <line x1="2" y1="10" x2="22" y2="10" />
                        </svg>
                      ) : i === 1 ? (
                        <svg className={styles.stepIcon} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                          <polyline points="9 12 11 14 15 10" />
                        </svg>
                      ) : (
                        <svg className={styles.stepIcon} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <rect x="5" y="2" width="14" height="20" rx="2" ry="2" />
                          <line x1="12" y1="18" x2="12.01" y2="18" />
                          <polygon points="10 8 15 11 10 14 10 8" />
                        </svg>
                      )}
                    </div>
                  </div>
                  <h3 className={styles.stepTitle}>{s.title}</h3>
                  <p className={styles.stepBody}>{s.body}</p>
                </li>
              ))}
            </ol>
          </div>
          <Todo lang={lang}>{c.steps.todoActivation}</Todo>
        </div>
      </section>

      {/* 5. The app */}
      <section id="app" className={`${styles.section} ${styles.sectionAlt}`} data-reveal aria-labelledby="app-title">
        <div className={`${styles.container} ${styles.appLayout}`}>
          <div className={styles.appInfo}>
            <Eyebrow>{c.app.eyebrow}</Eyebrow>
            <h2 id="app-title" className={styles.sectionTitle}>
              {c.app.title}
            </h2>
            <p className={styles.sectionLead}>{c.app.lead}</p>
            <div className={styles.storeLinks}>
              <a
                href={STORE_URLS.android}
                className={styles.playBadgeLink}
                rel="noopener noreferrer"
                target="_blank"
                aria-label={lang === "ar" ? "احصل عليه من Google Play" : "Get it on Google Play"}
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={lang === "ar" ? "/brand/play-badge-ar.png" : "/brand/play-badge-en.png"}
                  alt={lang === "ar" ? "احصل عليه من Google Play" : "Get it on Google Play"}
                  width={162}
                  height={48}
                  loading="lazy"
                  decoding="async"
                  className={styles.playBadgeImg}
                />
              </a>
            </div>
          </div>
          <div className={styles.appVisual}>
            <div className={`${styles.phoneFrame} ${styles.phoneTilt1}`}>
              <div className={styles.phoneSpeaker} aria-hidden="true" />
              <picture>
                <source type="image/avif" srcSet={screenSet(APP_SCREENS[lang][0].name, "avif")} sizes={SCREEN_SIZES} />
                <source type="image/webp" srcSet={screenSet(APP_SCREENS[lang][0].name, "webp")} sizes={SCREEN_SIZES} />
                <img
                  src={`/app/${APP_SCREENS[lang][0].name}-540.jpg`}
                  srcSet={screenSet(APP_SCREENS[lang][0].name, "jpg")}
                  sizes={SCREEN_SIZES}
                  alt={APP_SCREENS[lang][0].alt}
                  width={540}
                  height={APP_SCREENS[lang][0].height540}
                  loading="lazy"
                  decoding="async"
                  className={styles.phoneScreenImg}
                />
              </picture>
            </div>
            <div className={`${styles.phoneFrame} ${styles.phoneTilt2}`}>
              <div className={styles.phoneSpeaker} aria-hidden="true" />
              <picture>
                <source type="image/avif" srcSet={screenSet(APP_SCREENS[lang][1].name, "avif")} sizes={SCREEN_SIZES} />
                <source type="image/webp" srcSet={screenSet(APP_SCREENS[lang][1].name, "webp")} sizes={SCREEN_SIZES} />
                <img
                  src={`/app/${APP_SCREENS[lang][1].name}-540.jpg`}
                  srcSet={screenSet(APP_SCREENS[lang][1].name, "jpg")}
                  sizes={SCREEN_SIZES}
                  alt={APP_SCREENS[lang][1].alt}
                  width={540}
                  height={APP_SCREENS[lang][1].height540}
                  loading="lazy"
                  decoding="async"
                  className={styles.phoneScreenImg}
                />
              </picture>
            </div>
          </div>
        </div>
      </section>

      {/* 6. Academy address & map (item 9) */}
      <section id="academy" className={styles.section} data-reveal aria-labelledby="academy-title">
        <div className={styles.container}>
          <Eyebrow>{c.academy.eyebrow}</Eyebrow>
          <h2 id="academy-title" className={styles.sectionTitle}>
            {c.academy.title}
          </h2>
          <p className={styles.sectionLead}>{c.academy.lead}</p>
          <div className={styles.academyCard}>
            <h3 className={styles.academyName}>{c.academy.name}</h3>
            <p className={styles.academyAddress}>{c.academy.address}</p>
            <p className={styles.academyActions}>
              <a
                href={c.academy.mapUrl}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.mapLink}
              >
                {c.academy.mapLinkText}
              </a>
            </p>
          </div>
        </div>
      </section>

      {/* 7. FAQ (item 12) */}
      <section id="faq" className={`${styles.section} ${styles.sectionAlt}`} data-reveal aria-labelledby="faq-title">
        <div className={`${styles.container} ${styles.narrow}`}>
          <Eyebrow>{c.faq.eyebrow}</Eyebrow>
          <h2 id="faq-title" className={styles.sectionTitle}>
            {c.faq.title}
          </h2>
          <div className={styles.faqList}>
            {c.faq.items.map((item) => (
              <details key={item.q} className={styles.faqItem}>
                <summary className={styles.faqQuestion}>{item.q}</summary>
                <p className={styles.faqAnswer}>
                  {item.a}
                  {item.link ? (
                    <>
                      {" "}
                      <Link href={localePath(lang, item.link.href)} className={styles.textLink}>
                        {item.link.text}
                      </Link>
                    </>
                  ) : null}
                </p>
              </details>
            ))}
          </div>
        </div>
      </section>

      {/* 8. Closing call to action */}
      <section className={styles.closing} data-reveal aria-labelledby="closing-title">
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

      {/* Progressive enhancement scroll observer */}
      <ScrollRevealInit />
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
