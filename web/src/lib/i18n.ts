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
  | "forgotPassword"
  | "account"
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

export interface AuthText {
  login: {
    title: string;
    lead: string;
    emailLabel: string;
    emailPlaceholder: string;
    passwordLabel: string;
    passwordPlaceholder: string;
    submit: string;
    submitting: string;
    forgotPasswordLink: string;
    noAccountPrompt: string;
    signupLink: string;
  };
  signup: {
    title: string;
    lead: string;
    fullNameLabel: string;
    fullNamePlaceholder: string;
    emailLabel: string;
    emailPlaceholder: string;
    phoneLabel: string;
    phonePlaceholder: string;
    passwordLabel: string;
    passwordPlaceholder: string;
    submit: string;
    submitting: string;
    hasAccountPrompt: string;
    loginLink: string;
    otpTitle: string;
    otpLead: (email: string) => string;
    otpCodeLabel: string;
    otpSubmit: string;
    otpSubmitting: string;
    resendOtp: string;
    resendSuccess: string;
  };
  forgotPassword: {
    step1Title: string;
    step1Lead: string;
    sendCodeSubmit: string;
    sendingCode: string;
    step2Title: string;
    step2Lead: (email: string) => string;
    verifyCodeSubmit: string;
    step3Title: string;
    step3Lead: string;
    newPasswordLabel: string;
    confirmPasswordLabel: string;
    passwordMismatch: string;
    resetSubmit: string;
    resetSuccess: string;
    backToLogin: string;
  };
  account: {
    title: string;
    lead: string;
    profileSection: string;
    fullName: string;
    email: string;
    phone: string;
    noPhone: string;
    role: string;
    studentRole: string;
    ownedSubjectsSection: string;
    noOwnedSubjects: string;
    browseSubjects: string;
    studyBannerTitle: string;
    studyBannerLead: string;
    downloadAndroid: string;
    downloadIos: string;
    logout: string;
    loggingOut: string;
    loading: string;
  };
  errors: {
    invalid_credentials: string;
    account_locked: string;
    conflict: string;
    too_many_attempts: string;
    invalid_token: string;
    password_too_long: string;
    generic: string;
  };
}

interface Dictionary {
  brand: string;
  brandEyebrow: string;
  switchLanguage: string;
  switchLanguageLabel: string;
  nav: { home: string; about: string; subjects: string; login: string; signup: string; account: string };
  footer: { terms: string; privacy: string; refund: string; rights: string; brandLine: string };
  todoBadge: string;
  pages: Record<PageKey, PageText>;
  catalog: CatalogText;
  auth: AuthText;
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
    account: "حسابي",
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
      todo: "",
    },
    signup: {
      title: "إنشاء حساب جديد",
      lead: "حساب واحد للموقع والتطبيق.",
      todo: "",
    },
    forgotPassword: {
      title: "استعادة كلمة المرور",
      lead: "أدخل بريدك الإلكتروني لاستعادة كلمة المرور.",
      todo: "",
    },
    account: {
      title: "حسابي",
      lead: "بيانات حسابك والمواد المشترك بها.",
      todo: "",
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
  auth: {
    login: {
      title: "تسجيل الدخول",
      lead: "بنفس حساب التطبيق.",
      emailLabel: "البريد الإلكتروني",
      emailPlaceholder: "name@example.com",
      passwordLabel: "كلمة المرور",
      passwordPlaceholder: "••••••••",
      submit: "تسجيل الدخول",
      submitting: "جارٍ تسجيل الدخول...",
      forgotPasswordLink: "نسيت كلمة المرور؟",
      noAccountPrompt: "ليس لديك حساب؟",
      signupLink: "إنشاء حساب جديد",
    },
    signup: {
      title: "إنشاء حساب جديد",
      lead: "حساب واحد للموقع والتطبيق.",
      fullNameLabel: "الاسم بالكامل",
      fullNamePlaceholder: "الاسم كما هو مدون في البطاقة",
      emailLabel: "البريد الإلكتروني",
      emailPlaceholder: "name@example.com",
      phoneLabel: "رقم الهاتف (اختياري)",
      phonePlaceholder: "010xxxxxxxx",
      passwordLabel: "كلمة المرور",
      passwordPlaceholder: "٨ أحرف على الأقل",
      submit: "متابعة وإنشاء الحساب",
      submitting: "جارٍ إنشاء الحساب...",
      hasAccountPrompt: "لديك حساب بالفعل؟",
      loginLink: "تسجيل الدخول",
      otpTitle: "تأكيد البريد الإلكتروني",
      otpLead: (email: string) => `أدخل رمز التحقق المكون من 6 أرقام المرسل إلى ${email}`,
      otpCodeLabel: "رمز التحقق (OTP)",
      otpSubmit: "تأكيد الحساب ومتابعة",
      otpSubmitting: "جارٍ التحقق...",
      resendOtp: "إعادة إرسال الرمز",
      resendSuccess: "تم إرسال رمز جديد إلى بريدك الإلكتروني",
    },
    forgotPassword: {
      step1Title: "استعادة كلمة المرور",
      step1Lead: "أدخل بريدك الإلكتروني المسجل لإرسال رمز التحقق.",
      sendCodeSubmit: "إرسال رمز التحقق",
      sendingCode: "جارٍ الإرسال...",
      step2Title: "رمز التحقق",
      step2Lead: (email: string) => `أدخل الرمز المكون من 6 أرقام المرسل إلى ${email}`,
      verifyCodeSubmit: "تأكيد الرمز",
      step3Title: "تعيين كلمة المرور الجديدة",
      step3Lead: "أدخل كلمة المرور الجديدة لحسابك.",
      newPasswordLabel: "كلمة المرور الجديدة",
      confirmPasswordLabel: "تأكيد كلمة المرور",
      passwordMismatch: "كلمتا المرور غير متطابقتين",
      resetSubmit: "حفظ وتغيير كلمة المرور",
      resetSuccess: "تم تغيير كلمة المرور بنجاح. يمكنك الآن تسجيل الدخول.",
      backToLogin: "العودة لتسجيل الدخول",
    },
    account: {
      title: "حسابي",
      lead: "بيانات الحساب والمواد المشترك بها.",
      profileSection: "بيانات الطالب",
      fullName: "الاسم:",
      email: "البريد الإلكتروني:",
      phone: "رقم الهاتف:",
      noPhone: "غير مسجل",
      role: "نوع الحساب:",
      studentRole: "طالب",
      ownedSubjectsSection: "المواد المشترك بها",
      noOwnedSubjects: "لست مشتركاً في أي مادة حالياً.",
      browseSubjects: "تصفح قائمة المواد والاشتراك",
      studyBannerTitle: "ذاكر على تطبيق أكاديمية المتر",
      studyBannerLead: "مشاهدة الفيديوهات وتحميل المذكرات والكتب متاحة حصرياً عبر تطبيق الهاتف.",
      downloadAndroid: "تحميل لأجهزة Android (Google Play)",
      downloadIos: "تحميل لأجهزة iPhone (App Store)",
      logout: "تسجيل الخروج",
      loggingOut: "جارٍ الخروج...",
      loading: "جارٍ تحميل البيانات...",
    },
    errors: {
      invalid_credentials: "البريد الإلكتروني أو كلمة المرور غير صحيحة.",
      account_locked: "تم قفل الحساب مؤقتاً لكثرة المحاولات الخاطئة. يرجى المحاولة لاحقاً.",
      conflict: "البريد الإلكتروني مستخدم بالفعل بحساب مسجل.",
      too_many_attempts: "محاولات كثيرة جداً. يرجى الانتظار والمحاولة لاحقاً.",
      invalid_token: "رمز التحقق غير صحيح أو انتهت صلاحيته.",
      password_too_long: "كلمة المرور طويلة جداً (الحد الأقصى 72 حرفاً).",
      generic: "حدث خطأ غير متوقع. يرجى المحاولة لاحقاً.",
    },
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
    account: "My Account",
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
      todo: "",
    },
    signup: {
      title: "Create Account",
      lead: "One account for the website and the app.",
      todo: "",
    },
    forgotPassword: {
      title: "Forgot Password",
      lead: "Enter your email to reset your password.",
      todo: "",
    },
    account: {
      title: "My Account",
      lead: "Your account details and subscribed subjects.",
      todo: "",
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
  auth: {
    login: {
      title: "Sign In",
      lead: "With the same account as the app.",
      emailLabel: "Email Address",
      emailPlaceholder: "name@example.com",
      passwordLabel: "Password",
      passwordPlaceholder: "••••••••",
      submit: "Sign In",
      submitting: "Signing in...",
      forgotPasswordLink: "Forgot password?",
      noAccountPrompt: "Don't have an account?",
      signupLink: "Create Account",
    },
    signup: {
      title: "Create Account",
      lead: "One account for the website and the app.",
      fullNameLabel: "Full Name",
      fullNamePlaceholder: "Full Name as on ID",
      emailLabel: "Email Address",
      emailPlaceholder: "name@example.com",
      phoneLabel: "Phone Number (optional)",
      phonePlaceholder: "010xxxxxxxx",
      passwordLabel: "Password",
      passwordPlaceholder: "At least 8 characters",
      submit: "Continue & Create Account",
      submitting: "Creating account...",
      hasAccountPrompt: "Already have an account?",
      loginLink: "Sign In",
      otpTitle: "Verify Email",
      otpLead: (email: string) => `Enter the 6-digit verification code sent to ${email}`,
      otpCodeLabel: "Verification Code (OTP)",
      otpSubmit: "Verify & Continue",
      otpSubmitting: "Verifying...",
      resendOtp: "Resend Code",
      resendSuccess: "A new code has been sent to your email",
    },
    forgotPassword: {
      step1Title: "Forgot Password",
      step1Lead: "Enter your registered email to receive a verification code.",
      sendCodeSubmit: "Send Verification Code",
      sendingCode: "Sending...",
      step2Title: "Verification Code",
      step2Lead: (email: string) => `Enter the 6-digit code sent to ${email}`,
      verifyCodeSubmit: "Verify Code",
      step3Title: "Set New Password",
      step3Lead: "Enter a new password for your account.",
      newPasswordLabel: "New Password",
      confirmPasswordLabel: "Confirm New Password",
      passwordMismatch: "Passwords do not match",
      resetSubmit: "Save New Password",
      resetSuccess: "Password changed successfully. You can now sign in.",
      backToLogin: "Back to Sign In",
    },
    account: {
      title: "My Account",
      lead: "Account details and owned subjects.",
      profileSection: "Student Profile",
      fullName: "Name:",
      email: "Email:",
      phone: "Phone:",
      noPhone: "Not provided",
      role: "Role:",
      studentRole: "Student",
      ownedSubjectsSection: "Owned Subjects",
      noOwnedSubjects: "You do not own any subjects yet.",
      browseSubjects: "Browse available subjects",
      studyBannerTitle: "Study in EL METR ACADEMY app",
      studyBannerLead: "Videos, notes, and books are available exclusively via the mobile app.",
      downloadAndroid: "Download for Android (Google Play)",
      downloadIos: "Download for iPhone (App Store)",
      logout: "Sign Out",
      loggingOut: "Signing out...",
      loading: "Loading...",
    },
    errors: {
      invalid_credentials: "Invalid email or password.",
      account_locked: "Account is temporarily locked due to too many failed attempts.",
      conflict: "Email address is already registered.",
      too_many_attempts: "Too many attempts. Please try again later.",
      invalid_token: "Invalid or expired verification code.",
      password_too_long: "Password is too long (maximum 72 characters).",
      generic: "An unexpected error occurred. Please try again.",
    },
  },
};

const dictionaries: Record<Lang, Dictionary> = { ar, en };

export function dictionary(lang: Lang): Dictionary {
  return dictionaries[lang];
}
