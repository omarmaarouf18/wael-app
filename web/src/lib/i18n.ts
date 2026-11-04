// Website wording, Arabic first (same style as the app's
// frontend/lib/l10n/app_localizations.dart). The website may sell
// (ADR-0014): the app's store-safety word list does not apply here.
//
// Arabic lives at the root paths ("/about"), English under "/en"
// ("/en/about").

import type { Lang } from "./api";

export type { Lang };

export type PageKey =
  | "home"
  | "about"
  | "subjects"
  | "subject"
  | "login"
  | "signup"
  | "terms"
  | "privacy"
  | "refund";

/** Site path for a page in a language: "/about" -> "/en/about" in English. */
export function localePath(lang: Lang, path: string): string {
  if (lang === "ar") return path;
  return path === "/" ? "/en" : `/en${path}`;
}

interface PageText {
  title: string;
  lead: string;
  /** Placeholder note, shown until the page gets real content. */
  todo: string;
}

interface Dictionary {
  brand: string;
  brandEyebrow: string;
  switchLanguage: string;
  switchLanguageLabel: string;
  nav: { home: string; about: string; subjects: string; login: string; signup: string };
  footer: { terms: string; privacy: string; refund: string; rights: string; brandLine: string };
  todoBadge: string;
  pages: Record<PageKey, PageText>;
  catalog: CatalogText;
}

interface CatalogText {
  /** Shown when the catalog could not be read from the API. */
  unavailable: string;
  emptyLevel: string;
  empty: string;
  price: (amount: string) => string;
  instructor: string;
  term: { first: string; second: string };
  counts: { videos: string; books: string; notes: string };
  accessUntil: (date: string) => string;
  details: string;
  backToSubjects: string;
  studyInApp: string;
  buyTodo: string;
  notFoundTitle: string;
  notFoundLead: string;
}

/** Number formatting locale per language (Arabic digits on the Arabic site). */
export const numberLocale: Record<Lang, string> = { ar: "ar-EG", en: "en-EG" };

const ar: Dictionary = {
  brand: "أكاديمية المتر",
  brandEyebrow: "EL METR ACADEMY",
  switchLanguage: "English",
  switchLanguageLabel: "Switch to English",
  nav: {
    home: "الرئيسية",
    about: "عن الأكاديمية",
    subjects: "المواد الدراسية",
    login: "تسجيل الدخول",
    signup: "إنشاء حساب جديد",
  },
  footer: {
    terms: "الشروط والأحكام",
    privacy: "سياسة الخصوصية",
    refund: "سياسة الاسترداد",
    rights: "© ٢٠٢٦ أكاديمية المتر",
    brandLine: "EL METR علامة مملوكة للأستاذ وائل السعيد.",
  },
  todoBadge: "قيد الإعداد",
  pages: {
    home: {
      title: "أكاديمية المتر",
      lead: "اشترِ المادة من هنا، وذاكرها على تطبيق أكاديمية المتر.",
      todo: "TODO: محتوى الصفحة الرئيسية (W5).",
    },
    about: {
      title: "عن الأكاديمية",
      lead: "تعرّف على الأكاديمية والأستاذ وائل السعيد.",
      todo: "TODO: نص صفحة عن الأكاديمية (W5).",
    },
    subjects: {
      title: "المواد الدراسية",
      lead: "كل المواد المتاحة مع أسعارها.",
      todo: "",
    },
    subject: {
      title: "المادة الدراسية",
      lead: "تفاصيل المادة وطريقة الشراء.",
      todo: "",
    },
    login: {
      title: "تسجيل الدخول",
      lead: "بنفس حساب التطبيق.",
      todo: "TODO: تسجيل الدخول (W3).",
    },
    signup: {
      title: "إنشاء حساب جديد",
      lead: "حساب واحد للموقع والتطبيق.",
      todo: "TODO: إنشاء الحساب (W3).",
    },
    terms: {
      title: "الشروط والأحكام",
      lead: "شروط استخدام الموقع والشراء.",
      todo: "TODO: نص الشروط من المالك.",
    },
    privacy: {
      title: "سياسة الخصوصية",
      lead: "كيف نتعامل مع بياناتك.",
      todo: "TODO: نص سياسة الخصوصية من المالك.",
    },
    refund: {
      title: "سياسة الاسترداد",
      lead: "متى وكيف يمكن استرداد المبلغ.",
      todo: "TODO: نص سياسة الاسترداد من المالك.",
    },
  },
  catalog: {
    unavailable: "تعذّر عرض المواد الآن. حاول مرة أخرى بعد قليل.",
    emptyLevel: "لا توجد مواد في هذه الفرقة بعد.",
    empty: "لا توجد مواد متاحة بعد.",
    price: (amount) => `${amount} جنيه`,
    instructor: "المحاضر",
    term: { first: "الترم الأول", second: "الترم الثاني" },
    counts: { videos: "محاضرات الفيديو", books: "الكتب", notes: "المذكرات" },
    accessUntil: (date) => `متاحة حتى ${date}`,
    details: "التفاصيل",
    backToSubjects: "كل المواد",
    studyInApp: "بعد التفعيل تذاكر المادة على تطبيق أكاديمية المتر.",
    buyTodo: "TODO: طلب الشراء بالتحويل (W4).",
    notFoundTitle: "الصفحة غير موجودة",
    notFoundLead: "المادة غير متاحة أو الرابط غير صحيح.",
  },
};

const en: Dictionary = {
  brand: "EL METR ACADEMY",
  brandEyebrow: "أكاديمية المتر",
  switchLanguage: "العربية",
  switchLanguageLabel: "التبديل إلى العربية",
  nav: {
    home: "Home",
    about: "About the academy",
    subjects: "Subjects",
    login: "Sign In",
    signup: "Create Account",
  },
  footer: {
    terms: "Terms & Conditions",
    privacy: "Privacy Policy",
    refund: "Refund Policy",
    rights: "© 2026 EL METR ACADEMY",
    brandLine: "EL METR is a brand of Ustaz Wael Al-Saeed.",
  },
  todoBadge: "In preparation",
  pages: {
    home: {
      title: "EL METR ACADEMY",
      lead: "Buy a subject here, and study it in the EL METR ACADEMY app.",
      todo: "TODO: home page content (W5).",
    },
    about: {
      title: "About the academy",
      lead: "Meet the academy and Ustaz Wael Al-Saeed.",
      todo: "TODO: about page text (W5).",
    },
    subjects: {
      title: "Subjects",
      lead: "Every available subject with its price.",
      todo: "",
    },
    subject: {
      title: "Subject",
      lead: "Subject details and how to buy it.",
      todo: "",
    },
    login: {
      title: "Sign In",
      lead: "With the same account as the app.",
      todo: "TODO: sign in (W3).",
    },
    signup: {
      title: "Create Account",
      lead: "One account for the website and the app.",
      todo: "TODO: account creation (W3).",
    },
    terms: {
      title: "Terms & Conditions",
      lead: "Terms for using the website and buying.",
      todo: "TODO: terms text from the owner.",
    },
    privacy: {
      title: "Privacy Policy",
      lead: "How we handle your data.",
      todo: "TODO: privacy policy text from the owner.",
    },
    refund: {
      title: "Refund Policy",
      lead: "When and how a payment can be refunded.",
      todo: "TODO: refund policy text from the owner.",
    },
  },
  catalog: {
    unavailable: "The subjects cannot be shown right now. Please try again shortly.",
    emptyLevel: "No subjects in this level yet.",
    empty: "No subjects available yet.",
    price: (amount) => `EGP ${amount}`,
    instructor: "Instructor",
    term: { first: "First term", second: "Second term" },
    counts: { videos: "Video lectures", books: "Books", notes: "Notes" },
    accessUntil: (date) => `Available until ${date}`,
    details: "Details",
    backToSubjects: "All subjects",
    studyInApp: "Once activated, you study the subject in the EL METR ACADEMY app.",
    buyTodo: "TODO: transfer purchase request (W4).",
    notFoundTitle: "Page not found",
    notFoundLead: "This subject is not available, or the link is wrong.",
  },
};

const dictionaries: Record<Lang, Dictionary> = { ar, en };

export function dictionary(lang: Lang): Dictionary {
  return dictionaries[lang];
}
