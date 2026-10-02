# Content gaps: what the app shows, what is real, what is missing

Written 2026-10-02 (branch `fe/remove-mocks`). This is the owner's "what exists
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

## 1. Removed or emptied (was fake, now gone or hidden)

| # | File(s) | What it showed | Action taken | Needed to make it real |
|---|---|---|---|---|
| 1 | `providers/home_provider.dart` (old), `models/instructor_profile.dart` | Director card: "Dean Wael El Metr", "Ph.D. in Law, Certified International Arbitrator, Academy Founder", a bio claiming 15+ years, four credential chips (Arabic and English) | Deleted. Content moved to `lib/content/director_profile.dart`, every field empty. The card is hidden while there is no name | **Owner content**: name, title, badge, bio, credentials (ar and en), optional portrait asset. Fill only `lib/content/director_profile.dart` |
| 2 | `widgets/instructor_dossier_card.dart` | A hardcoded "FOUNDER" tag | Now the profile's own `badge` field; not drawn when empty | **Owner content** (the badge text, if any) |
| 3 | `l10n` `directorTagline`; `screens/course_detail/course_detail_body.dart` | Strip on every subject: "Counselor, International Arbitrator, Arab Lawyers Union" | Key deleted. The strip uses the profile's name and title, hidden when empty | **Owner content** (same file as 1) |
| 4 | `widgets/catalog_subject_card.dart`, `screens/courses_screen.dart` | The director's name under every subject title | Row hidden when the profile is empty | **Owner content** (same file as 1) |
| 5 | `providers/ebook_provider.dart` (old) | Six sample books with fake titles, authors, page counts, file sizes and `example.com` PDF links; sample study notes; an exam checklist; a "pinned maxim" quote | Deleted with `EBook`, `StudyNote`, `ChecklistItem`, `AcademicMaterial` | **Backend phase**: study notes and a library (SPEC Phase 5 files and downloads, ADR-0005; ADR-0010 app content is reserved but not written) |
| 6 | `screens/ebook_screen.dart` | The whole fake library UI (search, filters, reader sheet, note editor) | Replaced by an empty state: "Study materials and notes will appear here." Route `/ebooks` and the tab kept | **Backend phase** (as 5) |
| 7 | `screens/course_detail/course_detail_body.dart` | "Add to Study Notes": created a note with invented text ("Observations on ...", "Key observations recorded...") | Card and its strings deleted | **Owner decision** + **backend phase**: whether students keep personal notes (ADR-0005) |
| 8 | `screens/settings_screen.dart`, `providers/settings_provider.dart` (old) | Biometric sign-in switch (default on), reminders switch, curriculum-updates switch: in-memory flags that persisted nowhere | Tiles and provider deleted | **Backend phase**: notification preferences (not in the SPEC; Phase 7 covers sending notifications); biometric sign-in is an **owner decision** (not planned) |
| 9 | same | "Password & Two-Factor Auth" tile with the message "Two-Factor Authentication is active." No two-factor exists | Deleted | **Owner decision**: 2FA is not in the SPEC. Change password exists as the reset flow |
| 10 | same | "Email & Communications" tile: "Dispatches sent to primary email line." | Deleted | **Nothing** (no such setting exists) |
| 11 | same | Edit-profile sheet; saving only changed a local object | Sheet and `AuthProvider.updateProfile` deleted | **Backend phase**: a profile update endpoint (not in the SPEC yet) |
| 12 | `models/user_profile.dart`, settings header | Invented student record: "Senior Scholar, Top 3% Class Standing" (Arabic: "scholar, 2024 cohort"), GPA 3.98, 3 enrolled courses, 42 hours, 18 notes, "1080p Full HD" | Fields deleted. `UserProfile` is id, name, email, phone | **Backend phase** only if progress or standing is ever wanted (SPEC removed server-side progress, decision 6) |
| 13 | `assets/images/profile_alexander_vane.jpg` | A stock person's photo as every student's avatar | Deleted. Avatar is a neutral person icon; the status dot is removed from it | **Owner decision**: whether students can have a picture |
| 14 | `screens/settings_screen.dart` | Footer "EL METR ACADEMY iOS . Build 1.0.0" (hardcoded, and the app is Android first) | Deleted | **Nothing** (a real version line needs `package_info_plus`, a dependency change) |
| 15 | `screens/settings_screen.dart`, `l10n.privacyVerified` | "Privacy Protocol verified offline." snackbar on the Privacy tile | Tile and text deleted | **Owner content**: privacy policy text or URL. Needed before release |
| 16 | `screens/settings_screen.dart`, `l10n.honorCodeBody` | Honor code and terms sheet. Its text is not owner-approved and names "Counselor Wael El Saeed" (a different surname from the academy's) | Tile and text deleted (text is in git history) | **Owner content**: approved honor code and terms. Needed before release (see 33) |
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
| 24 | `widgets/home_hero_banner.dart`, `screens/login_screen.dart`, `assets/branding/el_metr_character_art.png`, `assets/images/home_hero.png`, `assets/images/login_portrait.jpg` | Art of a person on Home and on the login screen | Who it shows and the right to publish it are UNCONFIRMED in `docs/asset-provenance.md` | **Owner content**: confirm source and license (blocks the release review, SPEC Phase 8) |
| 25 | `l10n.heroHeadline`, `heroSubheadline` | "Your next level starts here" and "Learn. Understand. Apply. Because knowledge is power." | Marketing copy nobody approved | **Owner decision** |
| 26 | `screens/signup_screen.dart`, `l10n.agreeToTerms` | Checkbox "I agree to the Academy Honor Code & Terms", **pre-checked** (`_agreeToTerms = true`) | Consent is pre-ticked, and after item 16 the terms are shown nowhere in the app | **Owner decision** (legal): approved terms text, and whether the box starts unchecked. Changes sign-up behaviour, so not done here |
| 27 | `screens/signup_screen.dart`, `l10n.admissionsNote` | "Admissions are strictly merit-based." | A policy claim | **Owner decision** |
| 28 | `screens/settings_screen.dart`, `l10n.allRightsReserved` | "All rights reserved (c) 2026" | A legal line nobody approved | **Owner decision** |
| 29 | `widgets/director_strip.dart` | A crimson dot next to the director's name | It reads as "online"; it is decoration | **Owner decision** (kept, hidden with the strip while empty) |
| 30 | `screens/course_detail/course_detail_body.dart`, `assets/images/catalog_composure.jpg` | The same banner image on every subject | Not subject-specific; subject images are not in the API | **Owner content** or **backend phase** (a per-subject image field) |
| 31 | `l10n.navNotes` (tab label "My Notes") | The tab is labelled "My Notes" but is an empty library | Label and content disagree until item 5 exists | **Owner decision**: relabel to "Library" or keep |
| 32 | `screens/notifications_screen.dart` | Each notification's time is the raw `created_at` string from the API | Real data, unformatted | **Nothing from the owner**: a formatting follow-up |

## 3. Correct as it is (real data or honest UI)

| File(s) | What it shows | Source |
|---|---|---|
| `screens/home_screen.dart`, `courses_screen.dart`, `course_detail_screen.dart` | Levels, subjects, counts, owned flags, videos, files, expiry date | `GET /academy/levels`, `/subjects`, `/subjects/{id}` |
| `screens/course_detail/course_detail_body.dart` | Request sending, pending or error state; support link after a request | `POST /academy/subjects/{id}/access-request` |
| `screens/video_player_screen.dart` | Protected player with watermark | `POST /academy/videos/{id}/play`, `GET /auth/me` |
| `screens/settings_screen.dart` | Name, email, phone; language switch; sign out | `GET /auth/me`, local language |
| `screens/notifications_screen.dart` | Notification list, unread state | notification-service list and SSE stream |
| `screens/login_screen.dart`, `signup_screen.dart`, `forgot_password_screen.dart` | Input hints "name@example.com" and "Jane Doe" | Format hints, not data |
| `lib/debug/*` (debug builds only) | Component library with sample widgets | Excluded from release (`debug_routes_test.dart`) |

## 4. Left over, not mock data

| What | Detail |
|---|---|
| Unused generic strings | 10 l10n keys with no caller: `eduLisence`, `eduDiploma`, `eduVocational`, `levelYear1` to `levelYear4`, `save`, `videoLocked`, `academyMotto`. Also unused: the constants `academyMotto`, `academyWordmark`, `academySubWordmark`. Four more keys are used only by tests that assert the removed mock sections stay gone: `tabClasses`, `continueLearning`, `upcoming`, `downloadSyllabus` |
| Unused asset | `assets/branding/el_metr_landscape.jpg` (listed in the provenance doc) |
| Orphaned shared widgets | `widgets/file_details_sheet.dart` and `widgets/pill_filter_bar.dart` have no caller. `confirm_action_dialog.dart` is used only by the debug library. Not deleted: they are not mock data and the task did not name them |
| Not done (out of scope) | `device_id` and logout API (needs the Phase 1.7 backend), `url_launcher` (the support link can only be copied), backend, admin, player |
