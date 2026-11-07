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
    legalNoticePrefix: string;
    termsLink: string;
    legalNoticeAnd: string;
    privacyLink: string;
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
    myRequestsSection: string;
    noRequests: string;
    requestDate: (date: string) => string;
    requestAmount: (amount: string) => string;
    requestStatusPending: string;
    requestStatusAccepted: string;
    requestStatusRejected: string;
    requestRejectReason: (reason: string) => string;
    requestRef: (ref: string) => string;
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

export interface CartText {
  navCart: (count: number) => string;
  title: string;
  empty: string;
  browseCatalog: string;
  itemPrice: (price: number) => string;
  total: (amount: number) => string;
  remove: string;
  clearCart: string;
  proceedToCheckout: string;
  addToCart: string;
  inCart: string;
  alreadyOwned: string;
  pendingRequest: string;
  closeCart: string;
}

export interface CheckoutText {
  title: string;
  loginRequiredPrompt: string;
  loginButton: string;
  signupButton: string;
  paymentMethodLabel: string;
  selectMethod: string;
  transferReferenceLabel: string;
  transferReferencePlaceholder: string;
  senderNumberLabel: string;
  senderNumberPlaceholder: string;
  amountPaidLabel: string;
  amountMismatchWarning: string;
  legalNoticePrefix: string;
  termsLink: string;
  legalNoticeAnd: string;
  privacyLink: string;
  submitOrder: string;
  submitting: string;
  successTitle: string;
  successLead: string;
  viewMyOrders: string;
  errors: {
    duplicate_reference: string;
    already_owned: (subj?: string) => string;
    pending_request_exists: (subj?: string) => string;
    subject_expired: (subj?: string) => string;
    subject_not_found: (subj?: string) => string;
    invalid_amount: string;
    invalid_reference: string;
    invalid_sender: string;
    invalid_subjects: string;
    duplicate_subject: string;
    too_many_requests: string;
    generic: string;
  };
}

export interface AccountHubText {
  tabs: {
    overview: string;
    profile: string;
    orders: string;
    devices: string;
    notifications: string;
    password: string;
    danger: string;
  };
  profile: {
    editBtn: string;
    editTitle: string;
    nameLabel: string;
    phoneLabel: string;
    phonePlaceholder: string;
    phoneHelper: string;
    currentPasswordLabel: string;
    save: string;
    saving: string;
    cancel: string;
    success: string;
    errorTooSoon: (days: number) => string;
    errorConflict: string;
    errorWrongPassword: string;
  };
  password: {
    title: string;
    lead: string;
    currentPassword: string;
    newPassword: string;
    confirmNewPassword: string;
    submit: string;
    submitting: string;
    success: string;
    mismatch: string;
    forgotLink: string;
  };
  devices: {
    title: string;
    lead: string;
    noSessions: string;
    currentBadge: string;
    webBadge: string;
    lastUsed: (date: string) => string;
    created: (date: string) => string;
    signOut: string;
    signingOut: string;
    signOutCurrentWarning: string;
  };
  notifications: {
    title: string;
    lead: string;
    noNotifications: string;
    unreadBadge: string;
    markRead: string;
    markingRead: string;
    allCaughtUp: string;
  };
  orders: {
    title: string;
    lead: string;
    noOrders: string;
    orderNumber: (id: string) => string;
    orderDate: (date: string) => string;
    amountPaid: (amount: number) => string;
    total: (amount: number) => string;
    transferRef: (ref: string) => string;
    senderNumber: (sender: string) => string;
    paymentMethod: (method: string) => string;
    itemsCount: (count: number) => string;
    statusPending: string;
    statusAccepted: string;
    statusRejected: string;
    rejectReason: (reason: string) => string;
    methodWallet: string;
    methodInstapay: string;
  };
  deleteAccount: {
    title: string;
    warning: string;
    confirmPrompt: string;
    confirmPlaceholder: string;
    currentPasswordLabel: string;
    submit: string;
    submitting: string;
    scheduledNotice: (date: string) => string;
  };
}

interface Dictionary {
  brand: string;
  brandEyebrow: string;
  switchLanguage: string;
  switchLanguageLabel: string;
  skipToContent: string;
  nav: {
    home: string;
    about: string;
    subjects: string;
    login: string;
    signup: string;
    account: string;
    logout: string;
    cart: (count: number) => string;
  };
  footer: { terms: string; privacy: string; refund: string; rights: string; brandLine: string };
  todoBadge: string;
  pages: Record<PageKey, PageText>;
  catalog: CatalogText;
  auth: AuthText;
  cart: CartText;
  checkout: CheckoutText;
  accountHub: AccountHubText;
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
  ownedSubjectBannerTitle: string;
  ownedSubjectBannerLead: string;
  pendingRequestBannerTitle: string;
  pendingRequestBannerLead: string;
  loginToBuyPrompt: string;
  loginButton: string;
  signupButton: string;
  buyFormTitle: string;
  paymentMethodLabel: string;
  selectPaymentMethod: string;
  paymentMethodWallet: string;
  paymentMethodInstapay: string;
  noPaymentMethodsConfigured: string;
  transferReferenceLabel: string;
  transferReferencePlaceholder: string;
  senderNumberLabel: string;
  senderNumberPlaceholder: string;
  amountLabel: string;
  submitPurchaseRequest: string;
  submittingPurchaseRequest: string;
  purchaseRequestSuccess: string;
  addToCart: string;
  inCart: string;
  alreadyOwned: string;
  pendingRequest: string;
  buyErrors: {
    duplicate_reference: string;
    already_owned: string;
    pending_request_exists: string;
    subject_expired: string;
    invalid_amount: string;
    invalid_reference: string;
    invalid_sender: string;
    generic: string;
  };
}

/** Number formatting locale per language (Arabic digits on the Arabic site). */
export const numberLocale: Record<Lang, string> = { ar: "ar-EG", en: "en-EG" };

const ar: Dictionary = {
  brand: "أكاديمية المتر",
  brandEyebrow: "EL METR ACADEMY",
  switchLanguage: "English",
  switchLanguageLabel: "Switch to English",
  skipToContent: "تخطَّ إلى المحتوى",
  nav: {
    home: "الرئيسية",
    about: "عن الأكاديمية",
    subjects: "المواد الدراسية",
    login: "دخول",
    signup: "تسجيل",
    account: "حسابي",
    logout: "خروج",
    cart: (count: number) => `السلة (${count})`,
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
    ownedSubjectBannerTitle: "أنت مشترك في هذه المادة",
    ownedSubjectBannerLead: "يمكنك متابعة المادة ومشاهدة الفيديوهات عبر تطبيق أكاديمية المتر على هاتفك.",
    pendingRequestBannerTitle: "طلب الاشتراك قيد المراجعة",
    pendingRequestBannerLead: "تم استلام بيانات التحويل وسيتم مراجعتها وتفعيل المادة على حسابك قريباً.",
    loginToBuyPrompt: "سجّل الدخول أو أنشئ حساباً لتقديم طلب اشتراك في هذه المادة.",
    loginButton: "تسجيل الدخول",
    signupButton: "إنشاء حساب",
    buyFormTitle: "طلب اشتراك عبر التحويل",
    paymentMethodLabel: "طريقة التحويل المالي",
    selectPaymentMethod: "اختر طريقة التحويل...",
    paymentMethodWallet: "محفظة إلكترونية (كاش)",
    paymentMethodInstapay: "انستاباي (InstaPay)",
    noPaymentMethodsConfigured: "بيانات التحويل غير متاحة حالياً، يرجى التواصل مع الدعم.",
    transferReferenceLabel: "رقم العملية / مرجع التحويل",
    transferReferencePlaceholder: "الرقم المرجعي من رسالة التحويل",
    senderNumberLabel: "رقم الهاتف أو الحساب المحول منه",
    senderNumberPlaceholder: "010xxxxxxxx أو username@instapay",
    amountLabel: "المبلغ المحول (جنيه)",
    submitPurchaseRequest: "إرسال طلب الاشتراك",
    submittingPurchaseRequest: "جارٍ إرسال الطلب...",
    purchaseRequestSuccess: "تم إرسال طلب الاشتراك بنجاح! سيتم مراجعته وتفعيل المادة.",
    addToCart: "أضف إلى السلة",
    inCart: "في السلة",
    alreadyOwned: "مشترك بالفعل",
    pendingRequest: "قيد المراجعة",
    buyErrors: {
      duplicate_reference: "رقم العملية مسجل من قبل في طلب آخر. يرجى التأكد من الرقم وكتابته بدقة.",
      already_owned: "أنت مشترك في هذه المادة بالفعل.",
      pending_request_exists: "لديك طلب اشتراك مفتوح لهذه المادة بالفعل وهو قيد المراجعة.",
      subject_expired: "انتهت فترة التسجيل في هذه المادة.",
      invalid_amount: "المبلغ المدخل غير صالح.",
      invalid_reference: "رقم المرجع يجب أن يكون بين 3 و 100 حرف.",
      invalid_sender: "رقم أو حساب المحول يجب أن يكون بين 3 و 50 حرفاً.",
      generic: "تعذر إرسال الطلب. يرجى مراجعة البيانات والمحاولة مرة أخرى.",
    },
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
      legalNoticePrefix: "بالمتابعة فإنك توافق على ",
      termsLink: "الشروط والأحكام",
      legalNoticeAnd: " و ",
      privacyLink: "سياسة الخصوصية",
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
      myRequestsSection: "طلبات الاشتراك",
      noRequests: "لا توجد طلبات اشتراك سابقة.",
      requestDate: (date: string) => `تاريخ الطلب: ${date}`,
      requestAmount: (amount: string) => `المبلغ: ${amount} جنيه`,
      requestStatusPending: "قيد المراجعة",
      requestStatusAccepted: "تم القبول",
      requestStatusRejected: "مرفوض",
      requestRejectReason: (reason: string) => `سبب الرفض: ${reason}`,
      requestRef: (ref: string) => `رقم العملية: ${ref}`,
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
  cart: {
    navCart: (count: number) => `السلة (${count})`,
    title: "سلة المشتريات",
    empty: "السلة فارغة حالياً.",
    browseCatalog: "تصفح المواد الدراسية",
    itemPrice: (price: number) => `${price} جنيه`,
    total: (amount: number) => `الإجمالي: ${amount} جنيه`,
    remove: "إزالة",
    clearCart: "إفراغ السلة",
    proceedToCheckout: "متابعة لإتمام الشراء",
    addToCart: "أضف إلى السلة",
    inCart: "في السلة",
    alreadyOwned: "مشترك بالفعل",
    pendingRequest: "قيد المراجعة",
    closeCart: "إغلاق",
  },
  checkout: {
    title: "إتمام الشراء والتحويل",
    loginRequiredPrompt: "يرجى تسجيل الدخول أو إنشاء حساب جديد لإتمام الشراء.",
    loginButton: "تسجيل الدخول",
    signupButton: "إنشاء حساب",
    paymentMethodLabel: "طريقة التحويل المالي",
    selectMethod: "اختر وسيلة التحويل...",
    transferReferenceLabel: "رقم العملية / مرجع التحويل",
    transferReferencePlaceholder: "الرقم المرجعي من رسالة أو إيصال التحويل",
    senderNumberLabel: "رقم الهاتف أو الحساب المحول منه",
    senderNumberPlaceholder: "010xxxxxxxx أو username@instapay",
    amountPaidLabel: "المبلغ المحول بالجنيه",
    amountMismatchWarning: "تنبيه: المبلغ المدخل يختلف عن إجمالي سعر المواد بالسلة.",
    legalNoticePrefix: "بالمتابعة فإنك توافق على ",
    termsLink: "الشروط والأحكام",
    legalNoticeAnd: " و ",
    privacyLink: "سياسة الخصوصية",
    submitOrder: "تأكيد وإرسال طلب الشراء",
    submitting: "جارٍ إرسال الطلب...",
    successTitle: "تم استلام طلب الشراء بنجاح!",
    successLead: "تم حفظ بيانات التحويل وسيتم مراجعتها وتفعيل المواد على حسابك قريباً.",
    viewMyOrders: "عرض ومتابعة طلباتي",
    errors: {
      duplicate_reference: "رقم العملية أو المرجع مستخدم بالفعل في طلب سابق.",
      already_owned: (subj?: string) =>
        subj ? `أنت مشترك بالفعل في مادة «${subj}».` : "أنت مشترك بالفعل في إحدى المواد المحددة.",
      pending_request_exists: (subj?: string) =>
        subj ? `لديك طلب معلق لمادة «${subj}» قيد المراجعة.` : "لديك طلب معلق لإحدى المواد المحددة.",
      subject_expired: (subj?: string) =>
        subj ? `انتهت فترة التسجيل لمادة «${subj}».` : "انتهت فترة التسجيل لإحدى المواد المحددة.",
      subject_not_found: (subj?: string) =>
        subj ? `المادة «${subj}» غير متاحة أو تم إلغاؤها.` : "إحدى المواد غير متاحة.",
      invalid_amount: "المبلغ المدخل غير صالح (يجب أن يكون أكبر من صفر).",
      invalid_reference: "رقم المرجع يجب أن يكون بين 3 و 100 حرف.",
      invalid_sender: "رقم الهاتف أو الحساب المحول منه يجب أن يكون بين 3 و 50 حرفاً.",
      invalid_subjects: "يرجى اختيار مادة واحدة على الأقل للشراء (الحد الأقصى 50).",
      duplicate_subject: "لا يمكن تكرار نفس المادة في نفس الطلب.",
      too_many_requests: "تم تجاوز حد المحاولات المسموح به. يرجى الانتظار والمحاولة لاحقاً.",
      generic: "حدث خطأ أثناء إرسال الطلب. يرجى مراجعة البيانات والمحاولة مرة أخرى.",
    },
  },
  accountHub: {
    tabs: {
      overview: "نظرة عامة",
      profile: "البيانات الشخصية",
      orders: "طلباتي",
      devices: "الأجهزة المسجلة",
      notifications: "الإشعارات",
      password: "كلمة المرور",
      danger: "حذف الحساب",
    },
    profile: {
      editBtn: "تعديل البيانات",
      editTitle: "تعديل بيانات الحساب",
      nameLabel: "الاسم بالكامل",
      phoneLabel: "رقم الهاتف",
      phonePlaceholder: "010xxxxxxxx",
      phoneHelper: "إضافة أول رقم هاتف متاحة في أي وقت، والتعديل اللاحق متاح مرة كل 30 يوماً.",
      currentPasswordLabel: "كلمة المرور الحالية (مطلوبة لتأكيد التعديل)",
      save: "حفظ التعديلات",
      saving: "جارٍ الحفظ...",
      cancel: "إلغاء",
      success: "تم تحديث البيانات بنجاح.",
      errorTooSoon: (days: number) => `لا يمكن تغيير رقم الهاتف إلا بعد مرور 30 يوماً (المتبقي: ${days} يوم).`,
      errorConflict: "رقم الهاتف مسجل بالفعل بحساب آخر.",
      errorWrongPassword: "كلمة المرور الحالية غير صحيحة.",
    },
    password: {
      title: "تغيير كلمة المرور",
      lead: "قم بتحديث كلمة المرور الخاصة بحسابك (بين 8 و 72 حرفاً).",
      currentPassword: "كلمة المرور الحالية",
      newPassword: "كلمة المرور الجديدة",
      confirmNewPassword: "تأكيد كلمة المرور الجديدة",
      submit: "تحديث كلمة المرور",
      submitting: "جارٍ التحديث...",
      success: "تم تحديث كلمة المرور بنجاح.",
      mismatch: "كلمتا المرور غير متطابقتين.",
      forgotLink: "نسيت كلمة المرور؟",
    },
    devices: {
      title: "الأجهزة والجلسات المسجلة",
      lead: "الأجهزة المسجلة بحسابك. جلسات الويب تظهر بعلامة «ويب» ولا تُحتسب ضمن الحد الأقصى للأجهزة (جهازين).",
      noSessions: "لا توجد جلسات أخرى مسجلة.",
      currentBadge: "الجلسة الحالية",
      webBadge: "ويب",
      lastUsed: (date: string) => `آخر نشاط: ${date}`,
      created: (date: string) => `تاريخ الدخول: ${date}`,
      signOut: "تسجيل الخروج من هذا الجهاز",
      signingOut: "جارٍ الخروج...",
      signOutCurrentWarning: "تسجيل الخروج من الجلسة الحالية سينهي تسجيل دخولك في هذا المتصفح.",
    },
    notifications: {
      title: "صندوق الإشعارات",
      lead: "التنبيهات والرسائل الإدارية والإشعارات الخاصة بحسابك.",
      noNotifications: "لا توجد إشعارات لديك حالياً.",
      unreadBadge: "جديد",
      markRead: "تحديد كمقروء",
      markingRead: "جارٍ التحديث...",
      allCaughtUp: "لقد اطلعت على كافة الإشعارات.",
    },
    orders: {
      title: "سجل طلباتي",
      lead: "متابعة حالة طلبات الاشتراك والتحويلات المالية لكل مادة.",
      noOrders: "لا توجد طلبات شراء سابقة حتى الآن.",
      orderNumber: (id: string) => `طلب رقم #${id.slice(-8)}`,
      orderDate: (date: string) => `تاريخ الطلب: ${date}`,
      amountPaid: (amount: number) => `المبلغ المحول: ${amount} جنيه`,
      total: (amount: number) => `الإجمالي: ${amount} جنيه`,
      transferRef: (ref: string) => `رقم المرجع: ${ref}`,
      senderNumber: (sender: string) => `المحول منه: ${sender}`,
      paymentMethod: (method: string) => `طريقة الدفع: ${method}`,
      itemsCount: (count: number) => `عدد المواد: ${count}`,
      statusPending: "قيد المراجعة",
      statusAccepted: "تم القبول والتفعيل",
      statusRejected: "مرفوض",
      rejectReason: (reason: string) => `سبب الرفض: ${reason}`,
      methodWallet: "محفظة إلكترونية",
      methodInstapay: "انستاباي (InstaPay)",
    },
    deleteAccount: {
      title: "حذف الحساب نهائياً",
      warning:
        "تحذير هام: سيتم وضع حسابك في فترة سماح لمدة ٣٠ يوماً، وسيتم تسجيل الخروج من كافة الأجهزة فوراً. في حال رغبتك بإلغاء الحذف يمكنك تسجيل الدخول قبل انقضاء الـ ٣٠ يوماً.",
      confirmPrompt: 'لتأكيد الحذف، يرجى كتابة كلمة "حذف" في الحقل أدناه وإدخال كلمة المرور الحالية:',
      confirmPlaceholder: "حذف",
      currentPasswordLabel: "كلمة المرور الحالية",
      submit: "تأكيد طلب حذف الحساب",
      submitting: "جارٍ تقديم طلب الحذف...",
      scheduledNotice: (date: string) => `تم تحديد موعد حذف الحساب نهائياً في ${date}. تم تسجيل خروجك بنجاح.`,
    },
  },
};

const en: Dictionary = {
  brand: "EL METR ACADEMY",
  brandEyebrow: "أكاديمية المتر",
  switchLanguage: "العربية",
  switchLanguageLabel: "التبديل إلى العربية",
  skipToContent: "Skip to content",
  nav: {
    home: "Home",
    about: "About the academy",
    subjects: "Subjects",
    login: "Log in",
    signup: "Sign up",
    account: "My Account",
    logout: "Log out",
    cart: (count: number) => `Cart (${count})`,
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
    ownedSubjectBannerTitle: "You own this subject",
    ownedSubjectBannerLead: "You can study this subject and watch videos in the EL METR ACADEMY mobile app.",
    pendingRequestBannerTitle: "Subscription request pending review",
    pendingRequestBannerLead: "Your transfer details have been received and are being reviewed. Access will be activated shortly.",
    loginToBuyPrompt: "Sign in or create an account to purchase this subject.",
    loginButton: "Sign In",
    signupButton: "Create Account",
    buyFormTitle: "Purchase via Transfer",
    paymentMethodLabel: "Payment Method",
    selectPaymentMethod: "Select payment method...",
    paymentMethodWallet: "Mobile Wallet (Cash)",
    paymentMethodInstapay: "InstaPay",
    noPaymentMethodsConfigured: "Payment methods are currently unavailable. Please contact support.",
    transferReferenceLabel: "Transfer Reference / Transaction ID",
    transferReferencePlaceholder: "Reference from transfer receipt",
    senderNumberLabel: "Sender Phone or Account Address",
    senderNumberPlaceholder: "010xxxxxxxx or username@instapay",
    amountLabel: "Transferred Amount (EGP)",
    submitPurchaseRequest: "Submit Purchase Request",
    submittingPurchaseRequest: "Submitting...",
    purchaseRequestSuccess: "Purchase request submitted successfully! It will be reviewed and activated.",
    addToCart: "Add to Cart",
    inCart: "In Cart",
    alreadyOwned: "Already Owned",
    pendingRequest: "Pending Review",
    buyErrors: {
      duplicate_reference: "This transfer reference is already registered in another request. Please verify.",
      already_owned: "You already own this subject.",
      pending_request_exists: "You already have an open request pending review for this subject.",
      subject_expired: "Registration period for this subject has expired.",
      invalid_amount: "The entered amount is invalid.",
      invalid_reference: "Reference must be between 3 and 100 characters.",
      invalid_sender: "Sender account must be between 3 and 50 characters.",
      generic: "Failed to submit request. Please verify your details and try again.",
    },
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
      legalNoticePrefix: "By continuing you agree to the ",
      termsLink: "Terms & Conditions",
      legalNoticeAnd: " and ",
      privacyLink: "Privacy Policy",
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
      myRequestsSection: "Subscription Requests",
      noRequests: "No subscription requests submitted yet.",
      requestDate: (date: string) => `Requested: ${date}`,
      requestAmount: (amount: string) => `Amount: EGP ${amount}`,
      requestStatusPending: "Pending Review",
      requestStatusAccepted: "Accepted",
      requestStatusRejected: "Rejected",
      requestRejectReason: (reason: string) => `Reason: ${reason}`,
      requestRef: (ref: string) => `Ref: ${ref}`,
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
  cart: {
    navCart: (count: number) => `Cart (${count})`,
    title: "Shopping Cart",
    empty: "Your cart is currently empty.",
    browseCatalog: "Browse Subjects",
    itemPrice: (price: number) => `EGP ${price}`,
    total: (amount: number) => `Total: EGP ${amount}`,
    remove: "Remove",
    clearCart: "Clear Cart",
    proceedToCheckout: "Proceed to Checkout",
    addToCart: "Add to Cart",
    inCart: "In Cart",
    alreadyOwned: "Already Owned",
    pendingRequest: "Pending Review",
    closeCart: "Close",
  },
  checkout: {
    title: "Checkout & Transfer Payment",
    loginRequiredPrompt: "Please sign in or create an account to proceed with your order.",
    loginButton: "Sign In",
    signupButton: "Create Account",
    paymentMethodLabel: "Payment Method",
    selectMethod: "Select transfer method...",
    transferReferenceLabel: "Transfer Reference / Transaction ID",
    transferReferencePlaceholder: "Reference from transfer receipt message",
    senderNumberLabel: "Sender Phone or Account Address",
    senderNumberPlaceholder: "010xxxxxxxx or username@instapay",
    amountPaidLabel: "Amount Paid (EGP)",
    amountMismatchWarning: "Note: The amount entered differs from the cart total.",
    legalNoticePrefix: "By continuing you agree to the ",
    termsLink: "Terms & Conditions",
    legalNoticeAnd: " and ",
    privacyLink: "Privacy Policy",
    submitOrder: "Confirm and Submit Order",
    submitting: "Submitting order...",
    successTitle: "Order Submitted Successfully!",
    successLead: "Your transfer details have been received and are pending admin review. Subjects will be activated shortly.",
    viewMyOrders: "View My Orders",
    errors: {
      duplicate_reference: "This transfer reference is already registered in another request. Please verify.",
      already_owned: (subj?: string) =>
        subj ? `You already own "${subj}".` : "You already own one of the selected subjects.",
      pending_request_exists: (subj?: string) =>
        subj ? `You already have an open request pending review for "${subj}".` : "You already have an open request for one of the subjects.",
      subject_expired: (subj?: string) =>
        subj ? `Registration period for "${subj}" has expired.` : "Registration period for one of the subjects has expired.",
      subject_not_found: (subj?: string) =>
        subj ? `Subject "${subj}" is unavailable.` : "One of the selected subjects is not available.",
      invalid_amount: "The entered amount is invalid (must be greater than 0).",
      invalid_reference: "Reference must be between 3 and 100 characters.",
      invalid_sender: "Sender account must be between 3 and 50 characters.",
      invalid_subjects: "Please select at least one subject (maximum 50).",
      duplicate_subject: "A subject cannot be added twice to the same order.",
      too_many_requests: "Too many attempts. Please try again later.",
      generic: "Failed to submit order. Please verify your details and try again.",
    },
  },
  accountHub: {
    tabs: {
      overview: "Overview",
      profile: "Profile",
      orders: "My Orders",
      devices: "Devices",
      notifications: "Notifications",
      password: "Password",
      danger: "Delete Account",
    },
    profile: {
      editBtn: "Edit Profile",
      editTitle: "Edit Account Profile",
      nameLabel: "Full Name",
      phoneLabel: "Phone Number",
      phonePlaceholder: "010xxxxxxxx",
      phoneHelper: "Adding a first phone is allowed anytime; subsequent changes follow the 30-day rule.",
      currentPasswordLabel: "Current Password (required to save changes)",
      save: "Save Changes",
      saving: "Saving...",
      cancel: "Cancel",
      success: "Profile updated successfully.",
      errorTooSoon: (days: number) => `Phone number cannot be changed within 30 days of last change (${days} days remaining).`,
      errorConflict: "This phone number is already registered to another account.",
      errorWrongPassword: "The current password entered is incorrect.",
    },
    password: {
      title: "Change Password",
      lead: "Update your account password (between 8 and 72 characters).",
      currentPassword: "Current Password",
      newPassword: "New Password",
      confirmNewPassword: "Confirm New Password",
      submit: "Update Password",
      submitting: "Updating...",
      success: "Password updated successfully.",
      mismatch: "Passwords do not match.",
      forgotLink: "Forgot password?",
    },
    devices: {
      title: "Signed-in Devices & Sessions",
      lead: "Active sessions for this account. Web sessions are labeled as web and do not count toward the 2-device mobile limit.",
      noSessions: "No other active sessions found.",
      currentBadge: "Current Session",
      webBadge: "Web",
      lastUsed: (date: string) => `Last active: ${date}`,
      created: (date: string) => `Signed in: ${date}`,
      signOut: "Sign Out",
      signingOut: "Signing out...",
      signOutCurrentWarning: "Signing out of the current session will log you out of this browser.",
    },
    notifications: {
      title: "Notifications Inbox",
      lead: "System alerts, course updates, and account notifications.",
      noNotifications: "You have no notifications.",
      unreadBadge: "New",
      markRead: "Mark as read",
      markingRead: "Updating...",
      allCaughtUp: "You're all caught up!",
    },
    orders: {
      title: "My Orders",
      lead: "History of course orders, transfer verification details, and subject statuses.",
      noOrders: "No previous orders found.",
      orderNumber: (id: string) => `Order #${id.slice(-8)}`,
      orderDate: (date: string) => `Date: ${date}`,
      amountPaid: (amount: number) => `Paid: EGP ${amount}`,
      total: (amount: number) => `Total: EGP ${amount}`,
      transferRef: (ref: string) => `Ref: ${ref}`,
      senderNumber: (sender: string) => `From: ${sender}`,
      paymentMethod: (method: string) => `Method: ${method}`,
      itemsCount: (count: number) => `Subjects: ${count}`,
      statusPending: "Pending Review",
      statusAccepted: "Accepted & Active",
      statusRejected: "Rejected",
      rejectReason: (reason: string) => `Reason: ${reason}`,
      methodWallet: "Mobile Wallet",
      methodInstapay: "InstaPay",
    },
    deleteAccount: {
      title: "Delete Account",
      warning:
        "Important Warning: Your account will enter a 30-day grace period before permanent deletion, and you will be signed out from all devices immediately. You may cancel deletion by signing back in before the 30 days end.",
      confirmPrompt: 'To confirm deletion, type "حذف" in the field below and enter your current password:',
      confirmPlaceholder: "حذف",
      currentPasswordLabel: "Current Password",
      submit: "Confirm Account Deletion",
      submitting: "Processing deletion...",
      scheduledNotice: (date: string) => `Account deletion scheduled for ${date}. You have been signed out.`,
    },
  },
};

const dictionaries: Record<Lang, Dictionary> = { ar, en };

export function dictionary(lang: Lang): Dictionary {
  return dictionaries[lang];
}
