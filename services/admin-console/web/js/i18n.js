// Arabic first, English on request. The language lives in memory and in the
// URL hash (#en / #ar) so a reload keeps it; nothing is written to browser
// storage (ADR-0008).

export const LANGS = Object.freeze(['ar', 'en']);
export const DEFAULT_LANG = 'ar';
const DIRS = Object.freeze({ ar: 'rtl', en: 'ltr' });

export const MESSAGES = Object.freeze({
  ar: {
    'app.title': 'لوحة إدارة EL METR',
    'app.brand': 'EL METR',
    'app.subtitle': 'لوحة الإدارة',

    'login.title': 'تسجيل دخول المشرف',
    'login.subtitle': 'أدخل رمز المشرف الخاص بك للمتابعة.',
    'login.token': 'رمز المشرف',
    'login.submit': 'دخول',
    'login.verifying': 'جارٍ التحقق…',
    'login.hint': 'يبقى الرمز في ذاكرة هذه الصفحة فقط. عند إعادة تحميل الصفحة ستحتاج إلى إدخاله من جديد.',
    'login.empty': 'أدخل رمز المشرف.',
    'login.expired': 'انتهت الجلسة أو أن الرمز غير صالح. سجّل الدخول من جديد.',

    'top.signedInAs': 'مسجّل الدخول باسم',
    'top.signOut': 'تسجيل الخروج',
    'top.language': 'English',

    'tabs.label': 'أقسام اللوحة',
    'tabs.accounts': 'الحسابات',
    'tabs.audit': 'سجل العمليات',
    'tabs.requests': 'الطلبات',
    'tabs.catalog': 'الكتالوج',
    'tabs.files': 'الملفات',

    'common.retry': 'إعادة المحاولة',
    'common.refresh': 'تحديث',
    'common.cancel': 'إلغاء',
    'common.loading': 'جارٍ التحميل…',
    'common.empty': 'لا توجد نتائج.',
    'common.prev': 'السابق',
    'common.next': 'التالي',
    'common.pageOf': 'صفحة {page} من {pages}',
    'common.total': '{total} نتيجة',
    'common.done': 'تم تنفيذ الإجراء.',

    'err.unauthorized': 'انتهت الجلسة. سجّل الدخول من جديد.',
    'err.forbidden': 'ليست لديك صلاحية لتنفيذ هذا الإجراء.',
    'err.not_found': 'العنصر غير موجود. ربما تم حذفه.',
    'err.conflict': 'تعذّر تنفيذ الإجراء لأن حالة الحساب تغيّرت. حدّث القائمة ثم أعد المحاولة.',
    'err.rate_limited': 'محاولات كثيرة. انتظر قليلًا ثم أعد المحاولة.',
    'err.bad_request': 'البيانات المُدخلة غير صالحة. راجعها ثم أعد المحاولة.',
    'err.unavailable': 'الخدمة غير متاحة الآن. أعد المحاولة بعد قليل.',

    'accounts.title': 'حسابات الطلاب',
    'accounts.search': 'ابحث بالاسم أو البريد أو الهاتف أو المعرّف',
    'accounts.searchLabel': 'بحث',
    'accounts.statusLabel': 'الحالة',
    'accounts.statusAll': 'كل الحالات',
    'accounts.filter': 'تصفية',
    'accounts.reset': 'إعادة ضبط',
    'accounts.col.name': 'الاسم',
    'accounts.col.email': 'البريد الإلكتروني',
    'accounts.col.phone': 'الهاتف',
    'accounts.col.status': 'الحالة',
    'accounts.col.created': 'تاريخ الإنشاء',
    'accounts.col.actions': 'إجراءات',
    'status.active': 'نشط',
    'status.suspended': 'موقوف',
    'status.deleted': 'محذوف',
    'action.suspend': 'إيقاف',
    'action.reactivate': 'إعادة تفعيل',
    'action.delete': 'حذف',

    'dialog.reason': 'السبب (إلزامي)',
    'dialog.reasonHint': 'لا يزيد عن {max} حرف. يُسجَّل السبب في سجل العمليات.',
    'dialog.reasonCount': '{n} / {max}',
    'dialog.reasonRequired': 'اكتب سببًا من حرف واحد على الأقل.',
    'dialog.suspend.title': 'إيقاف الحساب',
    'dialog.suspend.note': 'ستنتهي جلسات الطالب فورًا ولن يتمكن من تسجيل الدخول حتى تعيد تفعيل حسابه.',
    'dialog.suspend.confirm': 'تأكيد الإيقاف',
    'dialog.reactivate.title': 'إعادة تفعيل الحساب',
    'dialog.reactivate.note': 'سيتمكن الطالب من تسجيل الدخول مجددًا.',
    'dialog.reactivate.confirm': 'تأكيد إعادة التفعيل',
    'dialog.delete.title': 'حذف الحساب',
    'dialog.delete.note': 'ستنتهي جلسات الطالب ويُمنع بريده الإلكتروني ورقم هاتفه من إنشاء حساب جديد. لا يمكن التراجع عن هذا الإجراء.',
    'dialog.delete.confirm': 'تأكيد الحذف',
    'dialog.working': 'جارٍ التنفيذ…',

    'audit.title': 'سجل العمليات',
    'audit.hint': 'الأحدث أولًا.',
    'audit.col.time': 'الوقت',
    'audit.col.actor': 'المشرف',
    'audit.col.action': 'الإجراء',
    'audit.col.target': 'الهدف',
    'audit.col.detail': 'التفاصيل',
    'audit.action.account_suspend': 'إيقاف حساب',
    'audit.action.account_reactivate': 'إعادة تفعيل حساب',
    'audit.action.account_delete': 'حذف حساب',
    'audit.target.user': 'طالب',

    'soon.title': 'قريبًا',
    'soon.body': 'هذا القسم غير متاح بعد.',
  },
  en: {
    'app.title': 'EL METR Admin Console',
    'app.brand': 'EL METR',
    'app.subtitle': 'Admin console',

    'login.title': 'Admin sign-in',
    'login.subtitle': 'Enter your admin token to continue.',
    'login.token': 'Admin token',
    'login.submit': 'Sign in',
    'login.verifying': 'Verifying…',
    'login.hint': 'The token stays in this page’s memory only. Reloading the page means signing in again.',
    'login.empty': 'Enter your admin token.',
    'login.expired': 'The session ended or the token is not valid. Sign in again.',

    'top.signedInAs': 'Signed in as',
    'top.signOut': 'Sign out',
    'top.language': 'العربية',

    'tabs.label': 'Console sections',
    'tabs.accounts': 'Accounts',
    'tabs.audit': 'Audit log',
    'tabs.requests': 'Requests',
    'tabs.catalog': 'Catalog',
    'tabs.files': 'Files',

    'common.retry': 'Retry',
    'common.refresh': 'Refresh',
    'common.cancel': 'Cancel',
    'common.loading': 'Loading…',
    'common.empty': 'No results.',
    'common.prev': 'Previous',
    'common.next': 'Next',
    'common.pageOf': 'Page {page} of {pages}',
    'common.total': '{total} results',
    'common.done': 'Done.',

    'err.unauthorized': 'Your session ended. Sign in again.',
    'err.forbidden': 'You are not allowed to do this.',
    'err.not_found': 'That item was not found. It may have been deleted.',
    'err.conflict': 'The action could not be applied because the account changed. Refresh the list and try again.',
    'err.rate_limited': 'Too many attempts. Wait a moment, then try again.',
    'err.bad_request': 'Some of the input is not valid. Check it and try again.',
    'err.unavailable': 'The service is not available right now. Try again shortly.',

    'accounts.title': 'Student accounts',
    'accounts.search': 'Search by name, email, phone or ID',
    'accounts.searchLabel': 'Search',
    'accounts.statusLabel': 'Status',
    'accounts.statusAll': 'All statuses',
    'accounts.filter': 'Filter',
    'accounts.reset': 'Reset',
    'accounts.col.name': 'Name',
    'accounts.col.email': 'Email',
    'accounts.col.phone': 'Phone',
    'accounts.col.status': 'Status',
    'accounts.col.created': 'Created',
    'accounts.col.actions': 'Actions',
    'status.active': 'Active',
    'status.suspended': 'Suspended',
    'status.deleted': 'Deleted',
    'action.suspend': 'Suspend',
    'action.reactivate': 'Reactivate',
    'action.delete': 'Delete',

    'dialog.reason': 'Reason (required)',
    'dialog.reasonHint': 'At most {max} characters. The reason is recorded in the audit log.',
    'dialog.reasonCount': '{n} / {max}',
    'dialog.reasonRequired': 'Write a reason of at least one character.',
    'dialog.suspend.title': 'Suspend account',
    'dialog.suspend.note': 'The student’s sessions end immediately and they cannot sign in until you reactivate the account.',
    'dialog.suspend.confirm': 'Confirm suspension',
    'dialog.reactivate.title': 'Reactivate account',
    'dialog.reactivate.note': 'The student will be able to sign in again.',
    'dialog.reactivate.confirm': 'Confirm reactivation',
    'dialog.delete.title': 'Delete account',
    'dialog.delete.note': 'The student’s sessions end and their email and phone number are blocked from creating a new account. This cannot be undone.',
    'dialog.delete.confirm': 'Confirm deletion',
    'dialog.working': 'Working…',

    'audit.title': 'Audit log',
    'audit.hint': 'Newest first.',
    'audit.col.time': 'Time',
    'audit.col.actor': 'Admin',
    'audit.col.action': 'Action',
    'audit.col.target': 'Target',
    'audit.col.detail': 'Details',
    'audit.action.account_suspend': 'Account suspended',
    'audit.action.account_reactivate': 'Account reactivated',
    'audit.action.account_delete': 'Account deleted',
    'audit.target.user': 'Student',

    'soon.title': 'Coming soon',
    'soon.body': 'This section is not available yet.',
  },
});

let lang = DEFAULT_LANG;
const listeners = new Set();

export function getLang() {
  return lang;
}

export function dirOf(l = lang) {
  return DIRS[l] ?? DIRS[DEFAULT_LANG];
}

/** Returns the language named by a location hash ("#en"), else the default. */
export function langFromHash(hash) {
  const name = String(hash ?? '').replace(/^#/, '');
  return LANGS.includes(name) ? name : DEFAULT_LANG;
}

export function setLang(next) {
  if (!LANGS.includes(next)) return false;
  lang = next;
  for (const cb of listeners) cb(next);
  return true;
}

export function otherLang(l = lang) {
  return l === 'ar' ? 'en' : 'ar';
}

/** Subscribes to language changes; returns an unsubscribe function. */
export function onLangChange(cb) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

/** Looks a key up in the current language, then English, then returns the key. */
export function t(key, params) {
  const raw = MESSAGES[lang]?.[key] ?? MESSAGES.en[key] ?? key;
  if (!params) return raw;
  return raw.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m));
}

/**
 * Translates every data-i18n* element under root and sets lang/dir on the
 * document element. Supported attributes: data-i18n (text),
 * data-i18n-placeholder, data-i18n-aria-label.
 */
export function applyTranslations(root) {
  const doc = root.ownerDocument ?? root;
  doc.documentElement.lang = lang;
  doc.documentElement.dir = dirOf(lang);
  doc.title = t('app.title');
  for (const el of root.querySelectorAll('[data-i18n]')) {
    el.textContent = t(el.dataset.i18n);
  }
  for (const el of root.querySelectorAll('[data-i18n-placeholder]')) {
    el.setAttribute('placeholder', t(el.dataset.i18nPlaceholder));
  }
  for (const el of root.querySelectorAll('[data-i18n-aria-label]')) {
    el.setAttribute('aria-label', t(el.dataset.i18nAriaLabel));
  }
}

function formatWith(value, options) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  return new Intl.DateTimeFormat(lang === 'ar' ? 'ar-EG' : 'en-GB', options).format(d);
}

export function formatDate(value) {
  return formatWith(value, { dateStyle: 'medium' });
}

export function formatTime(value) {
  return formatWith(value, { timeStyle: 'short' });
}

export function formatNumber(n) {
  return new Intl.NumberFormat(lang === 'ar' ? 'ar-EG' : 'en-GB').format(n);
}
