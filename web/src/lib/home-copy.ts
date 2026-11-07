// Landing page wording (docs/web/landing-plan.md, approved for step 2 on
// 2026-11-07). Arabic first. Nothing here is a fact the owner has not given
// or the system does not already define: activation time, the teacher's bio
// and titles, the center and testimonials are TODO blocks (lib/site-env.ts),
// never invented text.

import type { Lang } from "./api";

export interface FaqItem {
  q: string;
  /** null: the owner has not answered yet; shown only as a TODO block. */
  a: string | null;
  todo?: string;
}

export interface HomeCopy {
  metaTitle: string;
  metaDescription: string;
  eyebrow: string;
  heroTitle: string;
  heroAccent: string;
  heroLead: string;
  heroArtAlt: string;
  ctaBrowse: string;
  ctaApp: string;
  teacher: {
    eyebrow: string;
    title: string;
    lead: string;
    logoAlt: string;
    todoPortrait: string;
    todoBio: string;
  };
  subjects: {
    eyebrow: string;
    title: string;
    lead: string;
    all: string;
    count: (n: number) => string;
    unavailable: string;
  };
  steps: {
    eyebrow: string;
    title: string;
    items: { title: string; body: string }[];
    todoActivation: string;
  };
  app: {
    eyebrow: string;
    title: string;
    lead: string;
    android: string;
    ios: string;
    todoScreens: string;
    todoBadges: string;
  };
  center: { title: string; todo: string };
  testimonials: { title: string; todo: string };
  faq: { eyebrow: string; title: string; items: FaqItem[] };
  closing: { title: string; lead: string };
}

/** Arabic plural for "subject" (مادة): Intl plural categories. */
function arSubjects(n: number): string {
  const num = new Intl.NumberFormat("ar-EG").format(n);
  switch (new Intl.PluralRules("ar").select(n)) {
    case "one":
      return "مادة واحدة";
    case "two":
      return "مادتان";
    case "few":
      return `${num} مواد`;
    default:
      return `${num} مادة`;
  }
}

const ar: HomeCopy = {
  metaTitle: "أكاديمية المتر — شرح مواد كلية الحقوق",
  metaDescription: "فيديوهات ومذكرات لكل فرقة ودبلومة، تذاكرها على موبايلك في أي وقت.",
  eyebrow: "EL METR ACADEMY",
  heroTitle: "شرح مواد كلية الحقوق…",
  heroAccent: "مع EL\u00a0METR Academy",
  heroLead: "فيديوهات ومذكرات لكل فرقة ودبلومة، تذاكرها على موبايلك في أي وقت.",
  heroArtAlt: "شخصية EL METR: رجل ببدلة سوداء وقبعة وكرافتة حمراء",
  ctaBrowse: "تصفّح المواد",
  ctaApp: "حمّل التطبيق",
  teacher: {
    eyebrow: "المحاضر",
    title: "عن الأستاذ وائل السعيد",
    lead: "كل مواد الأكاديمية يشرحها الأستاذ وائل السعيد.",
    logoAlt: "شعار أكاديمية المتر",
    todoPortrait:
      "صورة الأستاذ: موافقة على استخدام صورة التطبيق في الموقع (الأستاذ والمصوّر) — بند ٣ في خطة الصفحة.",
    todoBio: "نبذة قصيرة (٢–٤ جمل) والألقاب التي تظهر — بند ٤ في خطة الصفحة.",
  },
  subjects: {
    eyebrow: "المواد",
    title: "المواد حسب الفرقة",
    lead: "اختر فرقتك وشوف المواد المتاحة.",
    all: "كل المواد",
    count: arSubjects,
    unavailable: "قائمة المواد غير متاحة الآن.",
  },
  steps: {
    eyebrow: "خطوات",
    title: "تبدأ إزاي؟",
    items: [
      {
        title: "اختار المادة وادفع",
        body: "أونلاين بتحويل (محفظة أو InstaPay) من الموقع، أو في السنتر.",
      },
      { title: "المادة بتتفعّل على حسابك", body: "بعد ما نراجع التحويل." },
      { title: "ذاكر من التطبيق", body: "الفيديوهات والمذكرات على التطبيق بنفس الحساب." },
    ],
    todoActivation: "مدة التفعيل المعتادة (مثلًا «خلال ٢٤ ساعة») — بند ٥ في خطة الصفحة.",
  },
  app: {
    eyebrow: "التطبيق",
    title: "كل محاضراتك في مكان واحد",
    lead: "فيديوهات ومذكرات مرتبة لكل مادة، وحسابك وأجهزتك تحت إيدك.",
    android: "Google Play",
    ios: "App Store",
    todoScreens: "صور التطبيق: موافقة على استخدام صور المتجر في الموقع — بند ٦.",
    todoBadges: "شارات المتجرين الرسمية وتأكيد أن الرابطين يعملان — بندا ٧ و٨.",
  },
  center: {
    title: "السنتر",
    todo: "عنوان السنتر ومواعيده ورابط الخريطة ورقم التواصل وصور — بندا ٩ و١٠. القسم لا يظهر في الموقع الحقيقي بدونها.",
  },
  testimonials: {
    title: "آراء الطلاب",
    todo: "آراء حقيقية بموافقة مكتوبة من أصحابها — بند ١١. القسم لا يظهر بدونها.",
  },
  faq: {
    eyebrow: "أسئلة",
    title: "أسئلة شائعة",
    items: [
      {
        q: "الدفع بيتم إزاي؟",
        a: "ضيف المواد للسلة، وحوّل المبلغ لمحفظة أو InstaPay من اللي بتظهر لك وقت الدفع، وبعدين ابعت رقم التحويل والرقم اللي حوّلت منه والمبلغ. وتقدر كمان تدفع في السنتر.",
      },
      { q: "التفعيل بياخد قد إيه؟", a: null, todo: "إجابة المالك — بند ١٢." },
      {
        q: "أقدر أذاكر على كام جهاز؟",
        a: "على جهازين بحد أقصى في نفس الوقت بنفس الحساب؛ لو دخلت من جهاز تالت، أقدم جهاز بيخرج. استخدام الموقع مش محسوب.",
      },
      { q: "أنا بذاكر في السنتر، أعمل إيه؟", a: null, todo: "إجابة المالك — بند ١٢." },
      { q: "لو التحويل اتعمل غلط؟", a: null, todo: "إجابة المالك وسياسة الاسترداد — بندا ١٢ و١٣." },
    ],
  },
  closing: { title: "جاهز تبدأ؟", lead: "اختار مادتك وابدأ المذاكرة من التطبيق." },
};

const en: HomeCopy = {
  metaTitle: "EL METR Academy — Law school subjects, explained",
  metaDescription: "Videos and notes for every year and diploma, on your phone, any time.",
  eyebrow: "EL METR ACADEMY",
  heroTitle: "Law school subjects, explained",
  heroAccent: "with EL\u00a0METR Academy",
  heroLead: "Videos and notes for every year and diploma, on your phone, any time.",
  heroArtAlt: "The EL METR character: a man in a black suit, hat and red tie",
  ctaBrowse: "Browse subjects",
  ctaApp: "Get the app",
  teacher: {
    eyebrow: "The teacher",
    title: "About Ustaz Wael El Saeed",
    lead: "Every subject at the academy is taught by Ustaz Wael El Saeed.",
    logoAlt: "EL METR Academy logo",
    todoPortrait:
      "Teacher photo: consent to use the app portrait on the website (teacher and photographer) — plan item 3.",
    todoBio: "A short bio (2–4 sentences) and the titles to show — plan item 4.",
  },
  subjects: {
    eyebrow: "Subjects",
    title: "Subjects by year",
    lead: "Pick your year and see what is available.",
    all: "All subjects",
    count: (n) => `${n} ${n === 1 ? "subject" : "subjects"}`,
    unavailable: "The subject list is not available right now.",
  },
  steps: {
    eyebrow: "Steps",
    title: "How to start",
    items: [
      { title: "Choose and pay", body: "Online by transfer (wallet or InstaPay) on this site, or at the center." },
      { title: "The subject is activated on your account", body: "After we check the transfer." },
      { title: "Study in the app", body: "Videos and notes, same account." },
    ],
    todoActivation: 'Typical activation time (e.g. "within 24 hours") — plan item 5.',
  },
  app: {
    eyebrow: "The app",
    title: "All your lectures in one place",
    lead: "Videos and notes organised by subject, and your account in your hands.",
    android: "Google Play",
    ios: "App Store",
    todoScreens: "App screenshots: consent to use the store screenshots on the website — item 6.",
    todoBadges: "The official store badges and confirmation that both links are live — items 7 and 8.",
  },
  center: {
    title: "The center",
    todo: "Center address, hours, map link, contact number and photos — items 9 and 10. The section is not shown on the live site without them.",
  },
  testimonials: {
    title: "What students say",
    todo: "Real quotes with their authors' written consent — item 11. The section is not shown without them.",
  },
  faq: {
    eyebrow: "Questions",
    title: "FAQ",
    items: [
      {
        q: "How does payment work?",
        a: "Add subjects to your cart, transfer the amount to one of the wallets or the InstaPay address shown at checkout, then send the transfer reference, the number you paid from and the amount. You can also pay at the center.",
      },
      { q: "How long does activation take?", a: null, todo: "Owner's answer — item 12." },
      {
        q: "On how many devices can I study?",
        a: "Up to two devices at a time on one account; signing in on a third signs out the oldest. Using this website does not count.",
      },
      { q: "I study at the center — what do I do?", a: null, todo: "Owner's answer — item 12." },
      { q: "What if my transfer was wrong?", a: null, todo: "Owner's answer and the refund policy — items 12 and 13." },
    ],
  },
  closing: { title: "Ready to start?", lead: "Pick your subject and start studying in the app." },
};

export function homeCopy(lang: Lang): HomeCopy {
  return lang === "ar" ? ar : en;
}
