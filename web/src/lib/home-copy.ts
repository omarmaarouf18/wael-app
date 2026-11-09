// Landing page wording (docs/web/landing-plan.md, owner answers 2026-11-07).
// Arabic first. The business is always called الأكاديمية / the academy
// (EL METR Academy), never «السنتر» / "the center". Everything here is built
// only on confirmed owner answers and system reality; nothing is invented.

import type { Lang } from "./api";

export interface FaqItem {
  q: string;
  a: string | null;
  todo?: string;
  link?: { href: string; text: string };
}

export interface HomeCopy {
  metaTitle: string;
  metaDescription: string;
  eyebrow: string;
  heroHeading: {
    prefix: string;
    accent: string;
    suffix: string;
  };
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
    portraitAlt: string;
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
    todoBadges: string;
  };
  academy: {
    eyebrow: string;
    title: string;
    lead: string;
    name: string;
    address: string;
    mapUrl: string;
    mapLinkText: string;
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
  metaDescription: "شرح مواد كلية الحقوق لكل الفرق — في الأكاديمية وعلى التطبيق.",
  eyebrow: "EL METR ACADEMY",
  heroHeading: {
    prefix: "شرح مواد كلية الحقوق لكل الفرق — في ",
    accent: "الأكاديمية",
    suffix: " وعلى التطبيق.",
  },
  heroTitle: "شرح مواد كلية الحقوق لكل الفرق — في الأكاديمية وعلى التطبيق.",
  heroAccent: "الأكاديمية",
  heroLead: "اختار مادتك، ادفع أونلاين أو في الأكاديمية، وذاكر من موبايلك.",
  heroArtAlt: "شخصية EL METR: رجل ببدلة سوداء وقبعة وكرافتة حمراء",
  ctaBrowse: "تصفّح المواد",
  ctaApp: "حمّل التطبيق",
  teacher: {
    eyebrow: "المحاضر",
    title: "عن الأستاذ وائل السعيد",
    lead: "كل مواد الأكاديمية يشرحها الأستاذ وائل السعيد.",
    portraitAlt: "الأستاذ وائل السعيد",
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
        body: "أونلاين بتحويل من الموقع، أو في الأكاديمية.",
      },
      { title: "المادة بتتفعّل على حسابك", body: "بعد ما الأكاديمية تراجع التحويل." },
      { title: "ذاكر من التطبيق", body: "الفيديوهات والملفات على تطبيق EL METR Academy بنفس الحساب." },
    ],
    todoActivation: "",
  },
  app: {
    eyebrow: "التطبيق",
    title: "كل محاضراتك في مكان واحد",
    lead: "فيديوهات ومذكرات مرتبة لكل مادة، وحسابك وأجهزتك تحت إيدك.",
    android: "Google Play",
    todoBadges: "",
  },
  academy: {
    eyebrow: "المقر",
    title: "مقر الأكاديمية",
    lead: "تقدر تدفع وتفعّل موادك وتحضر في مقر الأكاديمية.",
    name: "المتر اكاديمي",
    address: "61 - شارع 18 - توريل الجديدة - المنصورة",
    mapUrl: "https://maps.app.goo.gl/ASegpJDpQALSDf3D6",
    mapLinkText: "عرض على خرائط Google",
  },
  center: {
    title: "الأكاديمية",
    todo: "",
  },
  testimonials: {
    title: "آراء الطلاب",
    todo: "",
  },
  faq: {
    eyebrow: "أسئلة",
    title: "أسئلة شائعة",
    items: [
      {
        q: "إزاي أشتري مادة؟",
        a: "ضيف المادة للسلة، حوّل المبلغ على واحدة من طرق الدفع اللي هتظهرلك في صفحة الدفع، واكتب رقم العملية والرقم اللي حوّلت منه. الأكاديمية بتراجع التحويل وتفعّل المادة على حسابك.",
      },
      {
        q: "المادة بتتفعّل إمتى؟",
        a: "بعد ما الأكاديمية تراجع التحويل. تقدر تتابع حالة كل مادة من «طلباتي» في حسابك.",
      },
      {
        q: "ينفع أشتري كذا مادة مرة واحدة؟",
        a: "أيوه، ضيفهم كلهم للسلة وادفع بتحويل واحد.",
      },
      {
        q: "هذاكر فين؟",
        a: "على تطبيق EL METR Academy. الموقع للشراء وإدارة حسابك، والفيديوهات والملفات على التطبيق بس.",
      },
      {
        q: "أنا بحضر في الأكاديمية، أعمل إيه؟",
        a: "اعمل حساب على التطبيق، والأكاديمية بتفعّل موادك على حسابك.",
      },
      {
        q: "ينفع أستخدم حسابي على كام جهاز؟",
        a: "على جهازين في نفس الوقت. لو دخلت من جهاز تالت، أقدم جهاز بيخرج تلقائيًا.",
      },
      {
        q: "المادة متاحة لحد إمتى؟",
        a: "كل مادة مكتوب في صفحتها هي متاحة لحد إمتى.",
      },
      {
        q: "طلبي اترفض، أعمل إيه؟",
        a: "هتلاقي سبب الرفض في «طلباتي». لو عندك استفسار عن التحويل، تواصل مع الأكاديمية.",
      },
      {
        q: "فيه استرداد؟",
        a: "مفيش استرداد من خلال الموقع. لأي طلب استرداد تواصل مع الأكاديمية مباشرة.",
        link: { href: "/refund", text: "تفاصيل سياسة الاسترداد" },
      },
      {
        q: "نسيت كلمة السر؟",
        a: "استخدم «نسيت كلمة السر» في صفحة الدخول.",
      },
      {
        q: "أغيّر رقم تليفوني إزاي؟",
        a: "من «حسابي»، ومسموح بتغييره مرة كل 30 يوم.",
      },
    ],
  },
  closing: { title: "جاهز تبدأ؟", lead: "اختار مادتك وابدأ المذاكرة من التطبيق." },
};

const en: HomeCopy = {
  metaTitle: "EL METR Academy — Law school subjects, explained",
  metaDescription: "Law school subjects explained, for every year — at the academy and in the app.",
  eyebrow: "EL METR ACADEMY",
  heroHeading: {
    prefix: "Law school subjects explained, for every year — at the ",
    accent: "academy",
    suffix: " and in the app.",
  },
  heroTitle: "Law school subjects explained, for every year — at the academy and in the app.",
  heroAccent: "academy",
  heroLead: "Pick your subject, pay online or at the academy, and study on your phone.",
  heroArtAlt: "The EL METR character: a man in a black suit, hat and red tie",
  ctaBrowse: "Browse subjects",
  ctaApp: "Get the app",
  teacher: {
    eyebrow: "The teacher",
    title: "About Ustaz Wael El Saeed",
    lead: "Every subject at the academy is taught by Ustaz Wael El Saeed.",
    portraitAlt: "Ustaz Wael El Saeed",
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
      { title: "Choose and pay", body: "Online by transfer on this site, or at the academy." },
      { title: "The subject is activated on your account", body: "After the academy checks the transfer." },
      { title: "Study in the app", body: "Videos and files in the EL METR Academy app, same account." },
    ],
    todoActivation: "",
  },
  app: {
    eyebrow: "The app",
    title: "All your lectures in one place",
    lead: "Videos and notes organised by subject, and your account in your hands.",
    android: "Google Play",
    todoBadges: "",
  },
  academy: {
    eyebrow: "Location",
    title: "Academy Location",
    lead: "You can pay, activate subjects and attend classes in person at the academy.",
    name: "El Metr Academy",
    address: "61 -18St - new toril - Mansoura",
    mapUrl: "https://maps.app.goo.gl/ASegpJDpQALSDf3D6",
    mapLinkText: "View on Google Maps",
  },
  center: {
    title: "The academy",
    todo: "",
  },
  testimonials: {
    title: "What students say",
    todo: "",
  },
  faq: {
    eyebrow: "Questions",
    title: "FAQ",
    items: [
      {
        q: "How do I buy a subject?",
        a: "Add it to your cart, transfer the amount using one of the payment methods shown at checkout, then enter the transfer reference and the number you paid from. The academy checks the transfer and activates the subject on your account.",
      },
      {
        q: "When is my subject activated?",
        a: "As soon as the academy has checked your transfer. Follow each subject's status under \"My orders\" in your account.",
      },
      {
        q: "Can I buy several subjects at once?",
        a: "Yes. Add them all to the cart and pay with one transfer.",
      },
      {
        q: "Where do I study?",
        a: "In the EL METR Academy app. The website is for buying and managing your account; videos and files are in the app only.",
      },
      {
        q: "I attend classes at the academy. What do I do?",
        a: "Create an account in the app; the academy activates your subjects on it.",
      },
      {
        q: "On how many devices can I use my account?",
        a: "Two at the same time. Signing in on a third device signs out the oldest one.",
      },
      {
        q: "How long can I access a subject?",
        a: "Each subject page shows the date it is available until.",
      },
      {
        q: "My order was rejected. What now?",
        a: "The reason is shown under \"My orders\". For questions about your transfer, contact the academy.",
      },
      {
        q: "Can I get a refund?",
        a: "There are no refunds through the website. Contact the academy directly for any refund request.",
        link: { href: "/en/refund", text: "Refund policy details" },
      },
      {
        q: "Forgot your password?",
        a: "Use \"Forgot password\" on the sign-in page.",
      },
      {
        q: "How do I change my phone number?",
        a: "From \"My account\"; it can be changed once every 30 days.",
      },
    ],
  },
  closing: { title: "Ready to start?", lead: "Pick your subject and start studying in the app." },
};

export function homeCopy(lang: Lang): HomeCopy {
  return lang === "ar" ? ar : en;
}
