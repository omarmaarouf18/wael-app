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
    'login.idleLocked': 'تم قفل الجلسة لعدم النشاط. سجّل الدخول من جديد.',

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

    'unsaved.title': 'تغييرات غير محفوظة',
    'unsaved.note': 'عندك تغييرات لم تُحفظ بعد. لو غادرت الآن ستضيع.',
    'unsaved.stay': 'البقاء هنا',
    'unsaved.discard': 'تجاهل التغييرات',

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
    'err.invalid_request_id': 'معرّف الطلب مش صالح.',
    'err.request_not_found': 'الطلب مش موجود.',
    'err.request_not_pending': 'الطلب تم البت فيه بالفعل.',
    'err.invalid_reason': 'السبب مش صالح (من حرف واحد لحد ١٠٠٠ حرف).',
    'err.invalid_status': 'قيمة فلتر الحالة مش صالحة.',
    'err.invalid_user_id': 'معرّف الطالب مش صالح.',
    'err.invalid_entitlement_id': 'معرّف الاشتراك مش صالح.',
    'err.entitlement_not_found': 'الاشتراك مش موجود.',
    'err.already_owned': 'الطالب معاه اشتراك نشط في المادة دي بالفعل.',
    'err.subject_expired': 'تاريخ انتهاء المادة فات، عدّله من الكتالوج الأول.',

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
    'status.pending_deletion': 'بانتظار الحذف',
    'action.suspend': 'إيقاف',
    'action.reactivate': 'إعادة تفعيل',
    'action.delete': 'حذف',
    'action.subjects': 'المواد',

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
    'audit.target.request': 'طلب',
    'audit.target.entitlement': 'اشتراك',
    'audit.action.request_accept': 'قبول طلب اشتراك',
    'audit.action.request_reject': 'رفض طلب اشتراك',
    'audit.action.entitlement_grant': 'منح مادة',
    'audit.action.entitlement_revoke': 'سحب مادة',

    'requests.title': 'طلبات الاشتراك',
    'requests.filterStatus': 'الحالة',
    'requests.filterSubject': 'المادة',
    'requests.filterAllSubjects': 'كل المواد',
    'requests.statusAll': 'كل الحالات',
    'requests.col.student': 'الطالب',
    'requests.col.phone': 'الهاتف',
    'requests.col.email': 'البريد',
    'requests.col.subject': 'المادة والمستوى',
    'requests.col.price': 'السعر',
    'requests.col.requested': 'تاريخ الطلب',
    'requests.col.status': 'الحالة',
    'requests.col.actions': 'إجراءات',
    'requests.action.accept': 'قبول',
    'requests.action.reject': 'رفض',
    'requests.status.pending': 'قيد الانتظار',
    'requests.status.accepted': 'مقبول',
    'requests.status.rejected': 'مرفوض',
    'requests.student_fetch_failed': 'تعذر تحميل بيانات الطالب',
    'requests.toast.accepted': 'تم قبول الطلب وتفعيل المادة.',
    'requests.toast.rejected': 'تم رفض الطلب.',

    'dialog.accept.title': 'قبول الطلب وتفعيل المادة',
    'dialog.accept.confirm': 'تأكيد القبول',
    'dialog.accept.note': 'هيتسجل دفع {price} ج.م وهيتفتح للطالب لحد {expiry}.',
    'dialog.reject.title': 'رفض الطلب',
    'dialog.reject.confirm': 'تأكيد الرفض',
    'dialog.reject.note': 'سيصل السبب للطالب في إشعار (تجنب كلمات الدفع أو الأسعار لحماية المتجر). يمكن للطالب تقديم طلب جديد لاحقًا.',

    'entitlements.title': 'مواد الطالب: {name}',
    'entitlements.grantBtn': 'منح مادة',
    'entitlements.empty': 'لا توجد مواد لهذا الطالب.',
    'entitlements.col.subject': 'المادة والمستوى',
    'entitlements.col.status': 'الحالة',
    'entitlements.col.source': 'المصدر',
    'entitlements.col.granted': 'تاريخ المنح',
    'entitlements.col.expires': 'تاريخ الانتهاء',
    'entitlements.col.actions': 'إجراءات',
    'entitlements.status.active': 'نشط',
    'entitlements.status.expired': 'منتهي',
    'entitlements.status.revoked': 'مسحوب',
    'entitlements.source.request': 'طلب شراء',
    'entitlements.source.admin_grant': 'منح إداري',
    'entitlements.action.revoke': 'سحب المادة',
    'entitlements.revocationDetail': 'سُحبت في {date} بواسطة {by}، السبب: {reason}',
    'entitlements.toast.granted': 'تم منح المادة للطالب.',
    'entitlements.toast.revoked': 'تم سحب المادة من الطالب.',

    'dialog.grant.title': 'منح مادة للطالب',
    'dialog.grant.levelLabel': 'المستوى أو الدبلومة',
    'dialog.grant.subjectLabel': 'المادة',
    'dialog.grant.selectLevel': 'اختر المستوى أولًا',
    'dialog.grant.selectSubject': 'اختر المادة',
    'dialog.grant.noSubjects': 'لا توجد مواد منشورة في هذا المستوى.',
    'dialog.grant.confirm': 'تأكيد المنح',
    'dialog.grant.note': 'هيتسجل دفع {price} ج.م وهيتفتح للطالب لحد {expiry}.',
    'dialog.revoke.title': 'سحب المادة من الطالب',
    'dialog.revoke.confirm': 'تأكيد السحب',
    'dialog.revoke.note': 'سيتم إيقاف وصول الطالب فورًا وإشعاره. سيبقى سجل الدفع محفوظًا.',

    'idle.warnTitle': 'تنبيه انتهاء الجلسة',
    'idle.warnBody': 'سيتم قفل الجلسة بعد {seconds} ثانية بسبب عدم النشاط.',
    'idle.staySignedIn': 'متابعة الجلسة',

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
    'login.idleLocked': 'Session locked due to inactivity. Sign in again.',

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

    'unsaved.title': 'Unsaved changes',
    'unsaved.note': 'You have changes that are not saved yet. They are lost if you leave now.',
    'unsaved.stay': 'Stay here',
    'unsaved.discard': 'Discard changes',

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
    'err.invalid_request_id': 'The request ID is not valid.',
    'err.request_not_found': 'The request was not found.',
    'err.request_not_pending': 'The request is no longer pending.',
    'err.invalid_reason': 'The reason is not valid (1 to 1000 characters).',
    'err.invalid_status': 'The status filter value is not valid.',
    'err.invalid_user_id': 'The student ID is not valid.',
    'err.invalid_entitlement_id': 'The entitlement ID is not valid.',
    'err.entitlement_not_found': 'The entitlement was not found.',
    'err.already_owned': 'The student already holds an active entitlement for this subject.',
    'err.subject_expired': 'Subject access date has expired; update it in the catalog first.',

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
    'status.pending_deletion': 'Pending deletion',
    'action.suspend': 'Suspend',
    'action.reactivate': 'Reactivate',
    'action.delete': 'Delete',
    'action.subjects': 'Subjects',

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
    'audit.target.request': 'Request',
    'audit.target.entitlement': 'Entitlement',
    'audit.action.request_accept': 'Request accepted',
    'audit.action.request_reject': 'Request rejected',
    'audit.action.entitlement_grant': 'Subject granted',
    'audit.action.entitlement_revoke': 'Subject revoked',

    'requests.title': 'Subscription requests',
    'requests.filterStatus': 'Status',
    'requests.filterSubject': 'Subject',
    'requests.filterAllSubjects': 'All subjects',
    'requests.statusAll': 'All statuses',
    'requests.col.student': 'Student',
    'requests.col.phone': 'Phone',
    'requests.col.email': 'Email',
    'requests.col.subject': 'Subject & level',
    'requests.col.price': 'Price',
    'requests.col.requested': 'Requested at (Cairo time)',
    'requests.col.status': 'Status',
    'requests.col.actions': 'Actions',
    'requests.action.accept': 'Accept',
    'requests.action.reject': 'Reject',
    'requests.status.pending': 'Pending',
    'requests.status.accepted': 'Accepted',
    'requests.status.rejected': 'Rejected',
    'requests.student_fetch_failed': 'Failed to load student details',
    'requests.toast.accepted': 'Request accepted and subject activated.',
    'requests.toast.rejected': 'Request rejected.',

    'dialog.accept.title': 'Accept request & activate subject',
    'dialog.accept.confirm': 'Confirm acceptance',
    'dialog.accept.note': 'A payment of {price} EGP will be recorded and access granted until {expiry}.',
    'dialog.reject.title': 'Reject request',
    'dialog.reject.confirm': 'Confirm rejection',
    'dialog.reject.note': 'The student will be notified of the reason (avoid payment or price wording for store safety) and can submit a new request later.',

    'entitlements.title': 'Student subjects: {name}',
    'entitlements.grantBtn': 'Grant subject',
    'entitlements.empty': 'No subjects for this student.',
    'entitlements.col.subject': 'Subject & level',
    'entitlements.col.status': 'Status',
    'entitlements.col.source': 'Source',
    'entitlements.col.granted': 'Granted at',
    'entitlements.col.expires': 'Expires at',
    'entitlements.col.actions': 'Actions',
    'entitlements.status.active': 'Active',
    'entitlements.status.expired': 'Expired',
    'entitlements.status.revoked': 'Revoked',
    'entitlements.source.request': 'Purchase request',
    'entitlements.source.admin_grant': 'Admin grant',
    'entitlements.action.revoke': 'Revoke subject',
    'entitlements.revocationDetail': 'Revoked on {date} by {by}, reason: {reason}',
    'entitlements.toast.granted': 'Subject granted to student.',
    'entitlements.toast.revoked': 'Subject revoked from student.',

    'dialog.grant.title': 'Grant subject to student',
    'dialog.grant.levelLabel': 'Level or diploma',
    'dialog.grant.subjectLabel': 'Subject',
    'dialog.grant.selectLevel': 'Select level first',
    'dialog.grant.selectSubject': 'Select subject',
    'dialog.grant.noSubjects': 'No published subjects in this level.',
    'dialog.grant.confirm': 'Confirm grant',
    'dialog.grant.note': 'A payment of {price} EGP will be recorded and access granted until {expiry}.',
    'dialog.revoke.title': 'Revoke subject from student',
    'dialog.revoke.confirm': 'Confirm revoke',
    'dialog.revoke.note': 'Student access will end immediately and they will be notified. The payment record is kept.',

    'idle.warnTitle': 'Session timeout warning',
    'idle.warnBody': 'Session will lock in {seconds} seconds due to inactivity.',
    'idle.staySignedIn': 'Stay signed in',

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

export function formatCairoDateTime(value) {
  return formatWith(value, {
    timeZone: 'Africa/Cairo',
    dateStyle: 'medium',
    timeStyle: 'short',
  });
}
