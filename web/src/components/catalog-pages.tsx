import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { findSubject, pick, type CatalogSubject } from "@/lib/catalog-data";
import { getCatalog } from "@/lib/catalog";
import { dictionary, localePath, numberLocale, type Lang } from "@/lib/i18n";

import styles from "./catalog-pages.module.css";
import { SiteShell } from "./site-shell";
import { SubjectBuySection } from "./subject-buy-view";

// Catalog pages (ADR-0014 W2b): plain, functional markup; design comes in a
// later brief. Server components only: the catalog is read on the server by
// src/lib/catalog.ts, the browser never calls the API from these pages.

function formatPrice(lang: Lang, price: number): string {
  return dictionary(lang).catalog.price(new Intl.NumberFormat(numberLocale[lang]).format(price));
}

function formatDate(lang: Lang, iso: string): string | null {
  const d = new Date(iso);
  if (iso === "" || Number.isNaN(d.getTime())) return null;
  return new Intl.DateTimeFormat(numberLocale[lang], { dateStyle: "long", timeZone: "Africa/Cairo" }).format(d);
}

function Counts({ lang, subject }: { lang: Lang; subject: CatalogSubject }) {
  const t = dictionary(lang).catalog;
  const n = new Intl.NumberFormat(numberLocale[lang]);
  return (
    <ul className={styles.counts}>
      <li>
        {t.counts.videos}: {n.format(subject.counts.videos)}
      </li>
      <li>
        {t.counts.notes}: {n.format(subject.counts.notes)}
      </li>
      <li>
        {t.counts.books}: {n.format(subject.counts.books)}
      </li>
    </ul>
  );
}

/** /subjects: every published subject, grouped by study type and level. */
export async function SubjectsPage({ lang }: { lang: Lang }) {
  const d = dictionary(lang);
  const t = d.catalog;
  const result = await getCatalog();
  const to = (p: string) => localePath(lang, p);

  return (
    <SiteShell lang={lang} path="/subjects">
      <header className={styles.pageHead}>
        <h1 className={styles.title}>{d.pages.subjects.title}</h1>
        <p className={styles.lead}>{d.pages.subjects.lead}</p>
      </header>
      {!result.ok ? (
        <p className={styles.notice} role="status">
          {t.unavailable}
        </p>
      ) : result.catalog.studyTypes.every((st) => st.levels.every((l) => l.subjects.length === 0)) ? (
        <p className={styles.notice}>{t.empty}</p>
      ) : (
        result.catalog.studyTypes.map((st) =>
          st.levels.some((l) => l.subjects.length > 0) ? (
            <section key={st.key} className={styles.studyType} aria-labelledby={`st-${st.key}`}>
              <h2 id={`st-${st.key}`} className={styles.studyTypeTitle}>
                {pick(st.title, lang)}
              </h2>
              {st.levels.map((level) => (
                <section key={level.key} className={styles.level} aria-labelledby={`lv-${level.key}`}>
                  <h3 id={`lv-${level.key}`} className={styles.levelTitle}>
                    {pick(level.title, lang)}
                  </h3>
                  {level.subjects.length === 0 ? (
                    <p className={styles.muted}>{t.emptyLevel}</p>
                  ) : (
                    <ul className={styles.subjectGrid}>
                      {level.subjects.map((s) => (
                        <li key={s.id}>
                          <Link href={to(`/subjects/${s.id}`)} className={styles.subjectCard}>
                            {s.term !== "" ? <span className={styles.termBadge}>{t.term[s.term]}</span> : null}
                            <span className={styles.subjectTitle}>{pick(s.title, lang)}</span>
                            <span className={styles.cardFoot}>
                              <span className={styles.price}>{formatPrice(lang, s.price)}</span>
                              <span className={styles.cardCta}>{t.details}</span>
                            </span>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </section>
              ))}
            </section>
          ) : null,
        )
      )}
    </SiteShell>
  );
}

/** /subjects/[id]: one subject's details and price. Unknown id: 404. */
export async function SubjectPage({ lang, id }: { lang: Lang; id: string }) {
  const d = dictionary(lang);
  const t = d.catalog;
  const result = await getCatalog();
  const path = `/subjects/${encodeURIComponent(id)}`;

  if (!result.ok) {
    return (
      <SiteShell lang={lang} path={path}>
        <h1 className={styles.title}>{d.pages.subject.title}</h1>
        <p className={styles.notice} role="status">
          {t.unavailable}
        </p>
        <p>
          <Link href={localePath(lang, "/subjects")} className={styles.textLink}>
            {t.backToSubjects}
          </Link>
        </p>
      </SiteShell>
    );
  }
  const found = findSubject(result.catalog, id);
  if (found === null) notFound();
  const { subject, level, studyType } = found;
  const until = formatDate(lang, subject.accessExpiresAt);
  const description = pick(subject.description, lang);

  return (
    <SiteShell lang={lang} path={path}>
      <nav className={styles.breadcrumb} aria-label={d.pages.subjects.title}>
        <Link href={localePath(lang, "/subjects")}>{d.pages.subjects.title}</Link>
        <span aria-hidden="true">/</span>
        <span>{pick(studyType.title, lang)}</span>
        <span aria-hidden="true">/</span>
        <Link href={localePath(lang, `/subjects#lv-${level.key}`)}>{pick(level.title, lang)}</Link>
      </nav>
      <div className={styles.subjectLayout}>
        <div>
          {subject.term !== "" ? <span className={styles.termBadge}>{t.term[subject.term]}</span> : null}
          <h1 className={styles.title}>{pick(subject.title, lang)}</h1>
          <p className={styles.instructor}>
            {t.instructor}: <strong>{pick(subject.instructor, lang)}</strong>
          </p>
          {description !== "" ? <p className={styles.description}>{description}</p> : null}
          <Counts lang={lang} subject={subject} />
        </div>
        <aside className={styles.pricePanel} aria-label={formatPrice(lang, subject.price)}>
          <p className={styles.priceLarge}>{formatPrice(lang, subject.price)}</p>
          {until !== null ? <p className={styles.muted}>{t.accessUntil(until)}</p> : null}
          <p className={styles.studyNote}>{t.studyInApp}</p>
        </aside>
      </div>
      <SubjectBuySection
        lang={lang}
        subjectId={subject.id}
        price={subject.price}
        titleAr={subject.title.ar}
        titleEn={subject.title.en}
      />
      <p>
        <Link href={localePath(lang, "/subjects")} className={styles.textLink}>
          {t.backToSubjects}
        </Link>
      </p>
    </SiteShell>
  );
}

/** Subject ids for generateStaticParams, from the same one catalog call. */
export async function subjectStaticParams(): Promise<{ id: string }[]> {
  const result = await getCatalog();
  if (!result.ok) return [];
  return result.catalog.studyTypes.flatMap((st) => st.levels.flatMap((l) => l.subjects.map((s) => ({ id: s.id }))));
}

/** Title for a subject page: the subject's own title when it is listed. */
export async function subjectMetadata(lang: Lang, id: string): Promise<Metadata> {
  const d = dictionary(lang);
  const result = await getCatalog();
  const found = result.ok ? findSubject(result.catalog, id) : null;
  if (found === null) return { title: d.pages.subject.title };
  const description = pick(found.subject.description, lang);
  return { title: pick(found.subject.title, lang), description: description !== "" ? description.slice(0, 160) : undefined };
}

/** Localized 404 for the catalog pages (notFound() above). */
export function CatalogNotFound({ lang }: { lang: Lang }) {
  const d = dictionary(lang);
  const t = d.catalog;
  return (
    <SiteShell lang={lang} path="/subjects">
      <div className={styles.notFound}>
        <p className={styles.notFoundCode} aria-hidden="true">
          {d.common.notFoundCode}
        </p>
        <h1 className={styles.title}>{t.notFoundTitle}</h1>
        <p className={styles.lead}>{t.notFoundLead}</p>
        <div className={styles.actions}>
          <Link href={localePath(lang, "/subjects")} className={styles.primaryButton}>
            {t.backToSubjects}
          </Link>
          <Link href={localePath(lang, "/")} className={styles.secondaryButton}>
            {d.common.goHome}
          </Link>
        </div>
      </div>
    </SiteShell>
  );
}
