# Content gaps: what the app shows, what is real, what is missing

Written 2026-10-02 (branch `fe/remove-mocks`); rows 1-4 and 24 resolved the same day on `fe/login-v2` (owner-verified director content, owner-approved art). Refreshed 2026-10-06 against `origin/develop`: rows 6, 15, 16, 26 and 31 and the last table carry dated amendments; owner-content rows stay open. This is the owner's "what exists
vs. what does not" list for the Flutter app.

**Rule.** Anything shown to a student as real (people, bios, credentials,
courses, books, files, numbers, statistics, ratings, prices, dates, statuses,
notifications, toggles that claim a setting is on) must come from the API or
not be shown. Generic UI strings (labels, buttons, empty-state text) stay.
`frontend/test/no_mock_content_test.dart` fails if a removed mock class, sample
person, sample link or unreferenced asset comes back into `lib/`.

**What is real today:** sign up, sign in, OTP, password reset, session; the
catalog (levels, subjects, subject detail with videos and files); the access
request and its "pending" state; the protected video player; the notification
list and live stream; the profile header (name, email, phone from
`GET /auth/me`).

**"Needed" column.** *Owner content* = text or images only the owner can supply.
*Owner decision* = a product or legal choice, not code. *Backend phase* = needs
an API that does not exist yet (SPEC section 11). *Nothing* = already correct.

## 1. Removed, emptied or resolved (was fake, now gone, hidden or owner-verified)

| # | File(s) | What it showed | Action taken | Needed to make it real |
|---|---|---|---|---|
| 1 | `providers/home_provider.dart` (old), `models/instructor_profile.dart` | Director card: "Dean Wael El Metr", "Ph.D. in Law, Certified International Arbitrator, Academy Founder", a bio claiming 15+ years, four credential chips (Arabic and English) | **Resolved 2026-10-02.** Content is the owner's business card, verified by the owner: Wael El Saeed / وائل السعيد and four titles (no bio), portrait = the character art. Lives only in `lib/content/director_profile.dart` | Nothing (resolved) |
| 2 | `widgets/instructor_dossier_card.dart` | A hardcoded "FOUNDER" tag | **Resolved 2026-10-02.** The tag is gone; the card shows one chip per title | Nothing (resolved) |
| 3 | `l10n` `directorTagline`; `screens/course_detail/course_detail_body.dart` | Strip on every subject: "Counselor, International Arbitrator, Arab Lawyers Union" | **Resolved 2026-10-02.** Key deleted; the strip shows the portrait, the name and the four titles on one line | Nothing (resolved) |
| 4 | `widgets/catalog_subject_card.dart`, `screens/courses_screen.dart` | The director's name under every subject title | **Resolved 2026-10-02.** The row shows the verified name (hidden if the profile had no name) | Nothing (resolved) |
| 5 | `providers/ebook_provider.dart` (old) | Six sample books with fake titles, authors, page counts, file sizes and `example.com` PDF links; sample study notes; an exam checklist; a "pinned maxim" quote | Deleted with `EBook`, `StudyNote`, `ChecklistItem`, `AcademicMaterial` | **Backend phase**: study notes and a library (SPEC Phase 5 files and downloads, ADR-0005; ADR-0010 app content is reserved but not written) |
| 6 | `screens/ebook_screen.dart` | The whole fake library UI (search, filters, reader sheet, note editor) | Replaced by an empty state: "Study materials and notes will appear here." Route `/ebooks` and the tab kept. *(Amended 2026-10-06: since `9676fbf` the tab shows a "coming soon" badge and an explanation, no payment wording; the tab label and the screen title are the same string, `navNotes`.)* | **Backend phase** (as 5) |
| 7 | `screens/course_detail/course_detail_body.dart` | "Add to Study Notes": created a note with invented text ("Observations on ...", "Key observations recorded...") | Card and its strings deleted | **Owner decision** + **backend phase**: whether students keep personal notes (ADR-0005) |
| 8 | `screens/settings_screen.dart`, `providers/settings_provider.dart` (old) | Biometric sign-in switch (default on), reminders switch, curriculum-updates switch: in-memory flags that persisted nowhere | Tiles and provider deleted | **Backend phase**: notification preferences (not in the SPEC; Phase 7 covers sending notifications); biometric sign-in is an **owner decision** (not planned) |
| 9 | same | "Password & Two-Factor Auth" tile with the message "Two-Factor Authentication is active." No two-factor exists | Deleted | **Owner decision**: 2FA is not in the SPEC. Change password exists as the reset flow |
| 10 | same | "Email & Communications" tile: "Dispatches sent to primary email line." | Deleted | **Nothing** (no such setting exists) |
| 11 | same | Edit-profile sheet; saving only changed a local object | Sheet and `AuthProvider.updateProfile` deleted | **Backend phase**: a profile update endpoint (not in the SPEC yet) |
| 12 | `models/user_profile.dart`, settings header | Invented student record: "Senior Scholar, Top 3% Class Standing" (Arabic: "scholar, 2024 cohort"), GPA 3.98, 3 enrolled courses, 42 hours, 18 notes, "1080p Full HD" | Fields deleted. `UserProfile` is id, name, email, phone | **Backend phase** only if progress or standing is ever wanted (SPEC removed server-side progress, decision 6) |
| 13 | `assets/images/profile_alexander_vane.jpg` | A stock person's photo as every student's avatar | Deleted. Avatar is a neutral person icon; the status dot is removed from it | **Owner decision**: whether students can have a picture |
| 14 | `screens/settings_screen.dart` | Footer "EL METR ACADEMY iOS . Build 1.0.0" (hardcoded, and the app is Android first) | Deleted | **Resolved 2026-10-06** (F-UX2 Part B): `package_info_plus` (BSD-3-Clause) reads the installed version via `AppConfigProvider.currentVersion()`, shown in Settings About section. |
| 15 | `screens/settings_screen.dart`, `l10n.privacyVerified` | "Privacy Protocol verified offline." snackbar on the Privacy tile | Tile and text deleted | **Link wiring resolved 2026-10-06** (F-UX2 Part B): Settings About section reads `privacy_url` from `GET /academy/app-config` and opens it in the browser (`LaunchMode.externalApplication`). *(Amended 2026-10-08: the tile always shows now; an empty/malformed config falls back to `https://legal.elmetracademy.app/privacy` (`lib/core/legal_links.dart`).* **Owner content**: legal text review remains the owner's (placeholders filled at https://legal.elmetracademy.app/privacy). |
| 16 | `screens/settings_screen.dart`, `l10n.honorCodeBody` | Honor code and terms sheet. Its text is not owner-approved and names "Counselor Wael El Saeed" (a different surname from the academy's) | Tile and text deleted (text is in git history) | **Link wiring resolved 2026-10-06** (F-UX2 Part B): Settings About section reads `terms_url` from `GET /academy/app-config` and opens it in the browser (`LaunchMode.externalApplication`). *(Amended 2026-10-08: the tile always shows now; an empty/malformed config falls back to `https://legal.elmetracademy.app/terms` (`lib/core/legal_links.dart`).* **Owner content**: legal text review remains the owner's (placeholders filled at https://legal.elmetracademy.app/terms). |
| 17 | `repositories/notification_repository.dart` (old) | Two bundled notifications in debug builds: "Payment Verified & Enrolment Active" and "Crisis Communication Seminar, today 20:00 GMT" | `MockNotificationRepository` deleted. A failed load shows the error state, an empty one the empty state, in debug and release | **Nothing** (the real list and stream already exist). Sending notifications is **backend phase** (SPEC Phase 7) |
| 18 | `services/push_notification_service.dart` | "Initialized in offline demo mode": an in-process emitter of fake notifications | Deleted, with its call in `main()` | **Backend phase**: real push (not in the SPEC; the live SSE stream covers the foreground only) |
| 19 | `screens/main_shell.dart` | Header bell showed its unread dot permanently | Dot follows the real unread count | **Nothing** |
| 20 | `core/constants.dart` | Payment placeholders: Vodafone Cash "0100 000 0000", InstaPay "academy@instapay", an IBAN of zeros | Deleted | **Owner decision**: SPEC question 7 (payment flow). The support WhatsApp link comes from the API |
| 21 | `models/payment_request.dart`, `widgets/status_badge.dart` | `PaymentStatus` and a badge with "PENDING VERIFICATION", "APPROVED & VERIFIED" | Deleted (see the shared-widget note in the commit) | **Nothing** |
| 22 | l10n (52 keys) | Mock-era labels: a fake seminar and its time, category names (Leadership, Rhetoric ...), "CONTINUE LESSON 07", "Completed", "Master Class", notes and e-book labels, "Lessons", "Hours" | Deleted | **Nothing** |
| 23 | 10 files in `assets/` | Mock book covers, mock catalog and course images, a poster and two card images | Deleted; provenance rows removed (`docs/asset-provenance.md`) | **Nothing** |

## 2. Kept, but not owner-approved or not real: needs an owner decision

I did not decide these. Each is shown to students today.

| # | File(s) | What it shows | Why it is listed | Needed |
|---|---|---|---|---|
| 24 | `widgets/home_hero_banner.dart`, `screens/login_screen.dart`, `assets/branding/el_metr_character_art.png`, `assets/branding/el_metr_poster.jpg`, `assets/images/home_hero.png` | Art of a person on Home; the EL METR poster on the login screen | **Resolved 2026-10-02.** The owner supplied and approved the character art (Home hero, director portrait, app icon) and the poster (login only); provenance rows updated. `login_portrait.jpg` deleted (same picture as `home_hero.png`) | Nothing (resolved). The poster's own text includes a phone number line, as supplied |
| 25 | `l10n.heroHeadline`, `heroSubheadline` | "Your next level starts here" and "Learn. Understand. Apply. Because knowledge is power." | Marketing copy nobody approved | **Owner decision 2026-10-08**: hero copy approved as is; nothing to change. |
| 26 | `screens/signup_screen.dart`, `l10n.agreeToTermsPrefix` (+ link labels) | Checkbox "I agree to the Academy Honor Code & Terms", unticked by default; signup stays disabled until it is ticked | **Resolved 2026-10-03** (owner brief): the box starts unchecked (`_agreeToTerms = false`) and the submit CTA is disabled until ticked. **Link wiring resolved 2026-10-06** (F-UX2 Part B): signup reads `terms_url` from `GET /academy/app-config` and opens externally. *(Amended 2026-10-08, owner decision: the consent label is now "I agree to the Terms and Privacy Policy" with tappable Terms/Privacy names opening an in-app summary sheet (short bullets matching legal v2.0, full-page links, an agree button that ticks the box); every legal link always works via `lib/core/legal_links.dart` fallbacks, even with empty app-config or offline. The old `agreeToTerms`/`readTerms` keys are deleted.)* **Owner content**: legal text review remains the owner's (placeholders filled at https://legal.elmetracademy.app/terms). | Approved terms text (owner content; link wiring complete). |
| 27 | `screens/signup_screen.dart` (was `l10n.admissionsNote`) | "Admissions are strictly merit-based." | A policy claim | **Owner decision 2026-10-08 (#27)**: removed from signup; `l10n.admissionsNote` and its widget are deleted. |
| 28 | `screens/settings_screen.dart`, `l10n.brandLine`, `l10n.appCreditLine` | "All rights reserved (c) 2026" | A legal line nobody approved | **Owner decision 2026-10-08 (#28)**: replaced with two short lines — "EL METR علامة مملوكة للأستاذ وائل السعيد." / "EL METR is a brand of Ustaz Wael Al-Saeed." and "التطبيق © ٢٠٢٦ عمر معروف." / "App © 2026 Omar Maarouf." |
| 29 | `widgets/director_strip.dart` | A crimson dot next to the director's name | It reads as "online"; it is decoration | **Owner decision** (kept, hidden with the strip while empty) |
| 30 | `screens/course_detail/course_detail_body.dart`, `assets/images/catalog_composure.jpg` | The same banner image on every subject | Not subject-specific; subject images are not in the API. **Owner decision 2026-10-08 (#30)**: the banner image stays as is; no per-subject image planned, item stays open. | **Owner content** or **backend phase** (a per-subject image field) |
| 31 | `l10n.navNotes` (tab label "My Notes") | The tab is labelled "My Notes" but is an empty library | Label and content disagree until item 5 exists | **Owner decision**: relabel to "Library" or keep. *(Amended 2026-10-06: unchanged in code, `navNotes` is still "My Notes" / "المذكرات"; the tab now shows a coming-soon state. The decision is still open.)* |
| 32 | `screens/notifications_screen.dart`, `lib/core/notification_time.dart` | ~~Each notification's time is the raw `created_at` string from the API~~ | Real data, formatted 2026-10-08 | **Resolved 2026-10-08 (#32, owner decision)**: times render relative when recent ("الآن", "منذ ٥ دقائق", "منذ ساعتين", "أمس") and as a date otherwise ("٨ أكتوبر ٢٠٢٦، ٣:٤٠ م" / "8 Oct 2026, 3:40 PM"), in Africa/Cairo time, ar and en; unparsable strings fall back to raw, never crash. |

## 3. Correct as it is (real data or honest UI)

| File(s) | What it shows | Source |
|---|---|---|
| `screens/home_screen.dart`, `courses_screen.dart`, `course_detail_screen.dart` | Levels, subjects, counts, owned flags, videos, files, expiry date | `GET /academy/levels`, `/subjects`, `/subjects/{id}` |
| `screens/course_detail/course_detail_body.dart` | Request sending, pending or error state; support link after a request | `POST /academy/subjects/{id}/access-request` |
| `screens/video_player_screen.dart` | Protected player with watermark | `POST /academy/videos/{id}/play`, `GET /auth/me` |
| `screens/settings_screen.dart` | Name, email, phone; language switch; sign out | `GET /auth/me`, local language |
| `screens/notifications_screen.dart` | Notification list with formatted times (relative when recent, date otherwise), unread state | notification-service list and SSE stream; `lib/core/notification_time.dart` (Africa/Cairo, owner decision 2026-10-08 #32) |
| `screens/login_screen.dart`, `signup_screen.dart`, `forgot_password_screen.dart` | Input hints "name@example.com" and "Jane Doe" | Format hints, not data |
| `lib/debug/*` (debug builds only) | Component library with sample widgets | Excluded from release (`debug_routes_test.dart`) |

## 4. Left over, not mock data

| What | Detail |
|---|---|
| Unused generic strings | 7 l10n keys with no caller: `levelYear1` to `levelYear4`, `save`, `videoLocked`, `academyMotto` (`eduLisence`, `eduDiploma` and `eduVocational` were replaced by `studyTypeLabel` on 2026-10-02). Also unused: the constants `academyMotto`, `academyWordmark`, `academySubWordmark`. Four more keys are used only by tests that assert the removed mock sections stay gone: `tabClasses`, `continueLearning`, `upcoming`, `downloadSyllabus` |
| Orphaned shared widgets | `widgets/pill_filter_bar.dart` and `widgets/hero_backdrop.dart` have no screen caller (`HeroBackdrop` lost its last one when the login screen switched to the poster). `confirm_action_dialog.dart` is used only by the debug library. Not deleted: they are not mock data, and `HeroBackdrop` still has its own tests. *(Corrected 2026-10-02: an earlier version of this row also listed `file_details_sheet.dart`, which the subject screen does use.)* |
| Not done (out of scope) | `url_launcher` (the support link can only be copied), backend, admin, player *(Amended 2026-10-06: `url_launcher` 6.3.2 is now in use: `lib/core/external_links.dart` opens only `https://wa.me` support links from a pending request (`e5ff0eb`), with copy as the fallback. The other items remain out of scope of this file.)* |
