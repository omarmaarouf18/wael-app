// Public catalog data (ADR-0014 W2a/W2b): the shape of
// GET /api/v1/academy/public/catalog (SPEC Section 6, amendment 2026-11-04)
// and a strict parser for it. Pure module: no Next.js or React imports, so
// test/catalog-data.test.ts runs it with `node --test`.
//
// The parser keeps only the fields the site renders. Anything else the API
// might send is dropped, so no content field can reach a page even if the
// API ever returned one.

export interface Text {
  ar: string;
  en: string;
}

export interface CatalogSubject {
  id: string;
  levelKey: string;
  term: "first" | "second" | "";
  title: Text;
  description: Text;
  price: number;
  currency: "EGP";
  instructor: Text;
  counts: { videos: number; books: number; notes: number };
  /** ISO date-time, or "" when the API sent none. */
  accessExpiresAt: string;
}

export interface CatalogLevel {
  key: string;
  studyType: string;
  title: Text;
  subjects: CatalogSubject[];
}

export interface CatalogStudyType {
  key: string;
  title: Text;
  levels: CatalogLevel[];
}

export interface TeacherProfile {
  name: Text;
  titles: Text;
  bio: Text;
}

export interface Catalog {
  studyTypes: CatalogStudyType[];
  teacherProfile?: TeacherProfile;
}

/** Ids and keys go into URLs: letters, digits, "-" and "_" only. */
const ID_RE = /^[A-Za-z0-9_-]{1,64}$/;
const MAX_TEXT = 5000;

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function str(v: unknown): string | null {
  return typeof v === "string" && v.length <= MAX_TEXT ? v : null;
}

function id(v: unknown): string | null {
  return typeof v === "string" && ID_RE.test(v) ? v : null;
}

function count(v: unknown): number | null {
  return typeof v === "number" && Number.isSafeInteger(v) && v >= 0 ? v : null;
}

function text(v: unknown): Text | null {
  if (!isObj(v)) return null;
  const ar = str(v.ar ?? "");
  const en = str(v.en ?? "");
  return ar === null || en === null ? null : { ar, en };
}

function parseSubject(v: unknown): CatalogSubject | null {
  if (!isObj(v)) return null;
  const subjectId = id(v.id);
  const levelKey = id(v.level_key);
  const term = v.term;
  const title = text(v.title);
  const description = text(v.description);
  const instructor = text(v.instructor);
  const price = count(v.price);
  const counts = isObj(v.counts) ? v.counts : null;
  const videos = count(counts?.videos);
  const books = count(counts?.books);
  const notes = count(counts?.notes);
  const expires = v.access_expires_at === undefined ? "" : str(v.access_expires_at);
  if (
    subjectId === null ||
    levelKey === null ||
    (term !== "first" && term !== "second" && term !== "") ||
    title === null ||
    title.ar === "" ||
    description === null ||
    instructor === null ||
    price === null ||
    v.currency !== "EGP" ||
    videos === null ||
    books === null ||
    notes === null ||
    expires === null
  ) {
    return null;
  }
  return {
    id: subjectId,
    levelKey,
    term,
    title,
    description,
    price,
    currency: "EGP",
    instructor,
    counts: { videos, books, notes },
    accessExpiresAt: expires,
  };
}

function parseLevel(v: unknown): CatalogLevel | null {
  if (!isObj(v) || !Array.isArray(v.subjects)) return null;
  const key = id(v.key);
  const studyType = id(v.study_type);
  const title = text(v.title);
  if (key === null || studyType === null || title === null) return null;
  const subjects: CatalogSubject[] = [];
  for (const raw of v.subjects) {
    const s = parseSubject(raw);
    if (s !== null && s.levelKey === key) subjects.push(s);
  }
  return { key, studyType, title, subjects };
}

function parseTeacherProfile(v: unknown): TeacherProfile | undefined {
  if (!isObj(v)) return undefined;
  const nameAr = str(v.name_ar) ?? "";
  const nameEn = str(v.name_en) ?? "";
  const titlesAr = str(v.titles_ar) ?? "";
  const titlesEn = str(v.titles_en) ?? "";
  const bioAr = str(v.bio_ar) ?? "";
  const bioEn = str(v.bio_en) ?? "";
  return {
    name: { ar: nameAr, en: nameEn },
    titles: { ar: titlesAr, en: titlesEn },
    bio: { ar: bioAr, en: bioEn },
  };
}

/**
 * Parses the catalog response. Returns null when the top-level shape is
 * wrong; a malformed level or subject is dropped on its own. A subject id
 * seen twice keeps its first occurrence.
 */
export function parseCatalog(raw: unknown): Catalog | null {
  if (!isObj(raw) || !Array.isArray(raw.study_types)) return null;
  const seen = new Set<string>();
  const studyTypes: CatalogStudyType[] = [];
  for (const st of raw.study_types) {
    if (!isObj(st) || !Array.isArray(st.levels)) continue;
    const key = id(st.key);
    const title = text(st.title);
    if (key === null || title === null) continue;
    const levels: CatalogLevel[] = [];
    for (const rawLevel of st.levels) {
      const level = parseLevel(rawLevel);
      if (level === null) continue;
      level.subjects = level.subjects.filter((s) => {
        if (seen.has(s.id)) return false;
        seen.add(s.id);
        return true;
      });
      levels.push(level);
    }
    studyTypes.push({ key, title, levels });
  }
  const teacherProfile = parseTeacherProfile(raw.teacher_profile);
  return { studyTypes, ...(teacherProfile ? { teacherProfile } : {}) };
}

/** Every subject in catalog order. */
export function allSubjects(catalog: Catalog): CatalogSubject[] {
  return catalog.studyTypes.flatMap((st) => st.levels.flatMap((l) => l.subjects));
}

/** A subject with its level and study type, or null when it is not listed. */
export function findSubject(
  catalog: Catalog,
  subjectId: string,
): { subject: CatalogSubject; level: CatalogLevel; studyType: CatalogStudyType } | null {
  for (const studyType of catalog.studyTypes) {
    for (const level of studyType.levels) {
      const subject = level.subjects.find((s) => s.id === subjectId);
      if (subject) return { subject, level, studyType };
    }
  }
  return null;
}

/** Text in the page language, Arabic when the English text is empty. */
export function pick(t: Text, lang: "ar" | "en"): string {
  return lang === "en" && t.en.trim() !== "" ? t.en : t.ar;
}
