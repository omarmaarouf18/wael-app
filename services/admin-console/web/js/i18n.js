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

    // Server error codes relayed by the console (academy admin API plus the
    // auth codes above). Every code the handlers can return has a message.
    'err.locked_out': 'محاولات كتير، استنى شوية وجرّب تاني.',
    'err.method_not_allowed': 'طريقة الطلب مش مدعومة.',
    'err.service_unavailable': 'الخدمة مش متاحة دلوقتي، جرّب بعد شوية.',
    'err.invalid_json': 'البيانات المُرسلة مش مقروءة. حدّث الصفحة وجرّب تاني.',
    'err.invalid_level_id': 'المستوى مش صالح.',
    'err.invalid_study_type': 'نوع الدراسة مش صالح.',
    'err.invalid_name': 'الاسم مش صالح (من حرف واحد لحد ٢٠٠ حرف).',
    'err.level_not_found': 'المستوى أو الدبلومة مش موجودة.',
    'err.level_not_deletable': 'المستويات الأساسية ما ينفعش تتمسح.',
    'err.level_has_subjects': 'الدبلومة فيها مواد، انقل المواد لمستوى تاني الأول.',
    'err.invalid_subject_id': 'المادة مش صالحة.',
    'err.subject_not_found': 'المادة مش موجودة.',
    'err.invalid_title': 'العنوان مطلوب أو طويل جدًا.',
    'err.invalid_description': 'الوصف طويل جدًا.',
    'err.invalid_term': 'الترم لازم يكون أول أو تاني.',
    'err.invalid_price': 'السعر لازم يكون رقم صحيح بالموجب.',
    'err.invalid_expires_at': 'تاريخ انتهاء الاشتراك لازم يكون في المستقبل.',
    'err.invalid_published': 'قيمة فلتر النشر مش صالحة.',
    'err.subject_has_no_videos': 'ضيف فيديو واحد على الأقل قبل النشر.',
    'err.invalid_video_id': 'الفيديو مش صالح.',
    'err.video_not_found': 'الفيديو مش موجود.',
    'err.invalid_youtube_id': 'رابط يوتيوب أو الـ ID مش صحيح.',
    'err.invalid_duration_seconds': 'المدة مش صحيحة.',
    'err.invalid_video_order': 'الترتيب اتغير من مكان تاني، حدّث الصفحة وجرّب تاني.',
    'err.last_video_of_published_subject': 'ده آخر فيديو في مادة منشورة. الحذف هيلغي نشر المادة.',

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
    'audit.source.auth': 'الحسابات',
    'audit.source.academy': 'المحتوى',
    'audit.col.time': 'الوقت',
    'audit.col.actor': 'المشرف',
    'audit.col.action': 'الإجراء',
    'audit.col.target': 'الهدف',
    'audit.col.detail': 'التفاصيل',
    'audit.action.account_suspend': 'إيقاف حساب',
    'audit.action.account_reactivate': 'إعادة تفعيل حساب',
    'audit.action.account_delete': 'حذف حساب',
    'audit.action.level_create': 'إنشاء مستوى',
    'audit.action.level_update': 'تعديل مستوى',
    'audit.action.level_delete': 'حذف مستوى',
    'audit.action.subject_create': 'إنشاء مادة',
    'audit.action.subject_update': 'تعديل مادة',
    'audit.action.subject_publish': 'نشر مادة',
    'audit.action.subject_unpublish': 'إلغاء نشر مادة',
    'audit.action.video_create': 'إضافة فيديو',
    'audit.action.video_update': 'تعديل فيديو',
    'audit.action.video_delete': 'حذف فيديو',
    'audit.action.video_reorder': 'إعادة ترتيب فيديوهات',
    'audit.target.user': 'طالب',
    'audit.target.level': 'مستوى',
    'audit.target.subject': 'مادة',
    'audit.target.video': 'فيديو',

    'catalog.title': 'الكتالوج',
    'catalog.back': 'رجوع',
    'catalog.open': 'فتح',
    'catalog.edit': 'تعديل',
    'catalog.save': 'حفظ',
    'catalog.create': 'إضافة',
    'catalog.delete': 'حذف',
    'catalog.publish': 'نشر',
    'catalog.unpublish': 'إلغاء النشر',
    'catalog.published': 'منشورة',
    'catalog.draft': 'مسودة',
    'catalog.counts': '{total} مادة · {published} منشورة',
    'catalog.study.bachelor': 'بكالوريوس',
    'catalog.study.diploma': 'دبلومات',
    'catalog.study.vocational': 'تعليم مهني',
    'catalog.addDiploma': 'إضافة دبلومة',
    'catalog.addSubject': 'إضافة مادة',
    'catalog.addVideo': 'إضافة فيديو',
    'catalog.filter.all': 'الكل',
    'catalog.filter.label': 'الحالة',
    'catalog.filter.published': 'منشورة',
    'catalog.filter.draft': 'مسودة',
    'catalog.publishDisabledHint': 'ضيف فيديو واحد على الأقل قبل النشر.',
    'catalog.unpublishNote': 'الطلبة اللي معاهم اشتراك هيفضلوا شايفينها لحد ما اشتراكهم يخلص.',
    'catalog.deleteLevelTitle': 'حذف الدبلومة',
    'catalog.deleteLevelNote': 'الدبلومة هتتمسح نهائيًا. الدبلومة اللي فيها مواد ما ينفعش تتمسح.',
    'catalog.deleteVideoTitle': 'حذف الفيديو',
    'catalog.deleteVideoNote': 'الفيديو هيختفي من عند الطلبة فورًا.',
    'catalog.forceDeleteNote': 'ده آخر فيديو، المادة هتتلغى من النشر.',
    'catalog.confirmDelete': 'تأكيد الحذف',
    'catalog.confirmUnpublish': 'تأكيد إلغاء النشر',
    'catalog.levelCreateTitle': 'إضافة دبلومة',
    'catalog.levelEditTitle': 'تعديل المستوى',
    'catalog.subjectCreateTitle': 'إضافة مادة',
    'catalog.subjectEditTitle': 'تعديل المادة',
    'catalog.videoCreateTitle': 'إضافة فيديو',
    'catalog.videoEditTitle': 'تعديل الفيديو',
    'catalog.field.nameAr': 'الاسم بالعربية',
    'catalog.field.nameEn': 'الاسم بالإنجليزية (اختياري)',
    'catalog.field.titleAr': 'العنوان بالعربية',
    'catalog.field.titleEn': 'العنوان بالإنجليزية (اختياري)',
    'catalog.field.descAr': 'الوصف بالعربية (اختياري)',
    'catalog.field.descEn': 'الوصف بالإنجليزية (اختياري)',
    'catalog.field.level': 'المستوى',
    'catalog.field.order': 'الترتيب',
    'catalog.field.term': 'الترم',
    'catalog.term.first': 'الأول',
    'catalog.term.second': 'التاني',
    'catalog.field.price': 'السعر بالجنيه',
    'catalog.field.expires': 'نهاية الاشتراك',
    'catalog.field.youtube': 'رابط يوتيوب أو الـ ID',
    'catalog.field.duration': 'المدة (دقائق:ثواني، اختياري)',
    'catalog.required': 'الحقل ده مطلوب.',
    'catalog.tooLong': 'النص أطول من المسموح.',
    'catalog.badPrice': 'اكتب السعر رقم صحيح بالموجب.',
    'catalog.badDate': 'اختار تاريخ صحيح في المستقبل.',
    'catalog.badDuration': 'اكتب المدة بالشكل دقائق:ثواني (مثال 12:30).',
    'catalog.reorder.save': 'حفظ الترتيب',
    'catalog.reorder.dirty': 'في ترتيب متغير ولسه ما اتحفظش.',
    'catalog.reorder.up': 'لفوق',
    'catalog.reorder.down': 'لتحت',
    'catalog.price': '{n} جنيه',
    'catalog.col.videos': 'الفيديوهات',
    'catalog.col.actions': 'إجراءات',
    'catalog.expires': 'تنتهي في {date}',
    'catalog.videos': '{n} فيديو',
    'catalog.watch': 'مشاهدة على يوتيوب',
    'catalog.toast.created': 'تمت الإضافة.',
    'catalog.toast.updated': 'تم الحفظ.',
    'catalog.toast.deleted': 'تم الحذف.',
    'catalog.toast.published': 'تم النشر.',
    'catalog.toast.unpublished': 'تم إلغاء النشر.',
    'catalog.toast.reordered': 'تم حفظ الترتيب.',

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

    // Server error codes relayed by the console (academy admin API plus the
    // auth codes above). Every code the handlers can return has a message.
    'err.locked_out': 'Too many attempts. Wait a while, then try again.',
    'err.method_not_allowed': 'That request method is not supported.',
    'err.service_unavailable': 'The service is unavailable right now. Try again in a bit.',
    'err.invalid_json': 'The sent data could not be read. Refresh the page and try again.',
    'err.invalid_level_id': 'The level is not valid.',
    'err.invalid_study_type': 'The study type is not valid.',
    'err.invalid_name': 'The name is not valid (1 to 200 characters).',
    'err.level_not_found': 'The level or diploma was not found.',
    'err.level_not_deletable': 'Seeded levels cannot be deleted.',
    'err.level_has_subjects': 'The diploma still has subjects. Move them to another level first.',
    'err.invalid_subject_id': 'The subject is not valid.',
    'err.subject_not_found': 'The subject was not found.',
    'err.invalid_title': 'The title is required or too long.',
    'err.invalid_description': 'The description is too long.',
    'err.invalid_term': 'The term must be first or second.',
    'err.invalid_price': 'The price must be a non-negative integer.',
    'err.invalid_expires_at': 'The subscription end date must be in the future.',
    'err.invalid_published': 'The publish filter value is not valid.',
    'err.subject_has_no_videos': 'Add at least one video before publishing.',
    'err.invalid_video_id': 'The video is not valid.',
    'err.video_not_found': 'The video was not found.',
    'err.invalid_youtube_id': 'The YouTube link or ID is not correct.',
    'err.invalid_duration_seconds': 'The duration is not correct.',
    'err.invalid_video_order': 'The order changed elsewhere. Refresh the page and try again.',
    'err.last_video_of_published_subject': 'That is the last video of a published subject. Deleting it unpublishes the subject.',

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
    'audit.source.auth': 'Accounts',
    'audit.source.academy': 'Content',
    'audit.col.time': 'Time',
    'audit.col.actor': 'Admin',
    'audit.col.action': 'Action',
    'audit.col.target': 'Target',
    'audit.col.detail': 'Details',
    'audit.action.account_suspend': 'Account suspended',
    'audit.action.account_reactivate': 'Account reactivated',
    'audit.action.account_delete': 'Account deleted',
    'audit.action.level_create': 'Level created',
    'audit.action.level_update': 'Level updated',
    'audit.action.level_delete': 'Level deleted',
    'audit.action.subject_create': 'Subject created',
    'audit.action.subject_update': 'Subject updated',
    'audit.action.subject_publish': 'Subject published',
    'audit.action.subject_unpublish': 'Subject unpublished',
    'audit.action.video_create': 'Video added',
    'audit.action.video_update': 'Video updated',
    'audit.action.video_delete': 'Video deleted',
    'audit.action.video_reorder': 'Videos reordered',
    'audit.target.user': 'Student',
    'audit.target.level': 'Level',
    'audit.target.subject': 'Subject',
    'audit.target.video': 'Video',

    'catalog.title': 'Catalog',
    'catalog.back': 'Back',
    'catalog.open': 'Open',
    'catalog.edit': 'Edit',
    'catalog.save': 'Save',
    'catalog.create': 'Add',
    'catalog.delete': 'Delete',
    'catalog.publish': 'Publish',
    'catalog.unpublish': 'Unpublish',
    'catalog.published': 'Published',
    'catalog.draft': 'Draft',
    'catalog.counts': '{total} subjects · {published} published',
    'catalog.study.bachelor': 'Bachelor',
    'catalog.study.diploma': 'Diplomas',
    'catalog.study.vocational': 'Vocational training',
    'catalog.addDiploma': 'Add diploma',
    'catalog.addSubject': 'Add subject',
    'catalog.addVideo': 'Add video',
    'catalog.filter.all': 'All',
    'catalog.filter.label': 'Status',
    'catalog.filter.published': 'Published',
    'catalog.filter.draft': 'Draft',
    'catalog.publishDisabledHint': 'Add at least one video before publishing.',
    'catalog.unpublishNote': 'Students with a subscription keep seeing it until their subscription ends.',
    'catalog.deleteLevelTitle': 'Delete diploma',
    'catalog.deleteLevelNote': 'The diploma is deleted permanently. A diploma with subjects cannot be deleted.',
    'catalog.deleteVideoTitle': 'Delete video',
    'catalog.deleteVideoNote': 'The video disappears for students immediately.',
    'catalog.forceDeleteNote': 'That is the last video; the subject will be unpublished.',
    'catalog.confirmDelete': 'Confirm deletion',
    'catalog.confirmUnpublish': 'Confirm unpublishing',
    'catalog.levelCreateTitle': 'Add diploma',
    'catalog.levelEditTitle': 'Edit level',
    'catalog.subjectCreateTitle': 'Add subject',
    'catalog.subjectEditTitle': 'Edit subject',
    'catalog.videoCreateTitle': 'Add video',
    'catalog.videoEditTitle': 'Edit video',
    'catalog.field.nameAr': 'Name in Arabic',
    'catalog.field.nameEn': 'Name in English (optional)',
    'catalog.field.titleAr': 'Title in Arabic',
    'catalog.field.titleEn': 'Title in English (optional)',
    'catalog.field.descAr': 'Description in Arabic (optional)',
    'catalog.field.descEn': 'Description in English (optional)',
    'catalog.field.level': 'Level',
    'catalog.field.order': 'Order',
    'catalog.field.term': 'Term',
    'catalog.term.first': 'First',
    'catalog.term.second': 'Second',
    'catalog.field.price': 'Price in EGP',
    'catalog.field.expires': 'Subscription end',
    'catalog.field.youtube': 'YouTube link or ID',
    'catalog.field.duration': 'Duration (minutes:seconds, optional)',
    'catalog.required': 'This field is required.',
    'catalog.tooLong': 'The text is longer than allowed.',
    'catalog.badPrice': 'Write the price as a non-negative integer.',
    'catalog.badDate': 'Choose a valid date in the future.',
    'catalog.badDuration': 'Write the duration as minutes:seconds (e.g. 12:30).',
    'catalog.reorder.save': 'Save order',
    'catalog.reorder.dirty': 'There are unsaved order changes.',
    'catalog.reorder.up': 'Up',
    'catalog.reorder.down': 'Down',
    'catalog.price': '{n} EGP',
    'catalog.col.videos': 'Videos',
    'catalog.col.actions': 'Actions',
    'catalog.expires': 'Ends {date}',
    'catalog.videos': '{n} videos',
    'catalog.watch': 'Watch on YouTube',
    'catalog.toast.created': 'Added.',
    'catalog.toast.updated': 'Saved.',
    'catalog.toast.deleted': 'Deleted.',
    'catalog.toast.published': 'Published.',
    'catalog.toast.unpublished': 'Unpublished.',
    'catalog.toast.reordered': 'Order saved.',

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
 * Text for a failed request. A known server error code wins over the status
 * kind; anything unknown falls back to the kind. Server text is never shown.
 */
export function errorText(code, kind) {
  const key = typeof code === 'string' && code ? `err.${code}` : null;
  if (key && (key in MESSAGES.ar || key in MESSAGES.en)) return t(key);
  return t(`err.${kind}`);
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
