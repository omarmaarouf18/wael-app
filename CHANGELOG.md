# Changelog

One entry per production release, reconstructable from git
(`git log <previous>..<sha> --oneline`). Hashes are 7-char short refs.
`origin/main` and `origin/develop` were both at `b0de4d4` (2026-10-09); the
2026-10-12 stable-release pass lands new commits on `develop` only.

## develop — 2026-10-12 stable-release pass (staging for the 1.0.1 store build)

- academy-service: per-platform update metadata (owner decision 2026-10-12) —
  env `UPDATE_URL_ANDROID`, `UPDATE_URL_IOS` (https, must be an
  `https://apps.apple.com/...` App Store link), `MIN_VERSION_IOS`,
  `LATEST_VERSION_IOS`; app-config serves them as `update_url_android`,
  `update_url_ios`, `min_version_ios`, `latest_version_ios` with the shared
  `update_url`/`min_version`/`latest_version` unchanged so installed 1.0.0
  apps keep working; a bad value fails startup and `--check-env`. Contract
  tests, `.env` examples and SPEC §6/§9 amended; SERVER-MANUAL §5 rows added.
- app: per-platform update link and version (owner decisions 2026-10-12) —
  the platform's own fields first, the shared ones as fallback; iOS only ever
  opens an `https://apps.apple.com/...` link; a required update with no usable
  link shows the support WhatsApp button; the Settings "Update available" tile
  follows the same rule. Version `1.0.1+2`. Tests:
  `frontend/test/update_per_platform_test.dart`.
- app: notes & books freshness — a file the admin adds shows without a
  reload. Causes found: the shell mounts every tab at once and the notes tab's
  `ensureLoaded()` returned while Home's load was running, so it saw no owned
  subjects and loaded no files; it loaded only in `initState`; subject details
  cached earlier were never refetched; subjects with cached zero file counts
  were skipped; app-config (`features.files`) was read once per cold start.
  One shared mechanism (`RefreshWhenVisible` + `TabVisibility`) refreshes
  Home, Courses, Notes & books and Notifications in the background on tab
  visible / screen open / app resume (≥30 s apart), cached first; concurrent
  catalog loads share one request. Tests: `frontend/test/refresh_freshness_test.dart`.
- app: iOS screen-capture cover for the player (owner decision 2026-10-12:
  recording protection covers iOS too) — native channel + event stream in
  `AppDelegate.swift` (`sceneCaptureState` on iOS 17+, `isCaptured` +
  `capturedDidChangeNotification` before); the player checks before the first
  play, on every change and on resume, pauses and covers the video while the
  screen is recorded or mirrored, blocks play, never auto-resumes, and fails
  closed when the channel is missing. Screenshots are a documented iOS limit.
  Android `FLAG_SECURE` unchanged. Tests: `frontend/test/ios_capture_cover_test.dart`.
- app: store items and UX pass — iOS `CFBundleDisplayName` "EL METR ACADEMY"
  (owner decision 2026-10-12, same as Android), `CFBundleLocalizations` ar/en,
  an app-level `PrivacyInfo.xcprivacy` (no tracking; name, email, phone, user
  ID, device ID for app functionality) in the Runner resources; the Home hero
  art is bundled as a 190 KB JPEG instead of the 2 MB PNG (the PNG stays the
  icon source); autofill hints, keyboard types and next/done actions in an
  `AutofillGroup` on the password-change, email-change, delete-account and
  profile-edit forms; system back (and the iOS swipe) in full-screen video
  leaves full screen first; the subject screen's WhatsApp button is 48 dp.
  Tests: `frontend/test/autofill_forms_test.dart`, a new full-screen back test.
- tests and QA: an SSE reconnect test (the last function of the pass's test
  list without a named test) and `docs/frontend/RELEASE-QA-1.0.1.md`, the
  device checklist for one Android phone and one iPhone (TestFlight), one row
  per function with its automated test, plus the earlier unverified iOS fixes
  (iOS 15 target, YouTube frame, PDF Quick Look / share).
- app: new director portrait (owner decision 2026-10-12) — a 720 x 720
  head-and-shoulders crop of the owner's studio photo (Hazem El layub
  Photography), metadata stripped, same asset path; provenance updated.
- app: "السنتر" / "center" → "الأكاديمية" / "academy" in the legal summary
  bullet and the locked-subject activation line (owner decision 2026-10-12,
  as in the admin console). The legal terms page needs the same edit.
- app: one shared `LaunchGuard` (`lib/core/external_links.dart`) for every
  external launch — WhatsApp, update and legal links: a second tap on the
  same link while it launches or within 1 s is ignored, so a double tap
  opens it once. Tests: `frontend/test/launch_guard_test.dart`.

## `b0de4d4` — 2026-10-09

`main` fast-forwarded `620da53` → `b0de4d4` (`620da53` itself had
fast-forwarded `32d185e` on 2026-10-08 with the P1 gateway/JWT, pre-deploy
backup, console settings, legal links, review and hardening wave recorded in
`docs/HISTORY.md`). The 39 commits: SPEC Phase 5 files server side — storage
config, admin PDF upload/list/delete, student download with per-call ownership
check, app-config `features.files` — with the console Files tab and file proxy
routes; download limits (one download per student, `MAX_CONCURRENT_DOWNLOADS`
default 3, `DOWNLOAD_STALL_TIMEOUT` default 30s); infra I1/I2 (backups archive
`STORAGE_DIR` as one set; restore validates first, never restarts a failed
restore healthy); faster CI (parallel E2E, per-job Go cache, Flutter cache);
iOS store-ready + TestFlight fixes (AppIcon, `build-ios.yml`, iOS 15.0
deployment target after ITMS-90068, YouTube frame on iOS, iOS PDF
open/share), plus the go1.26.9 security release. Whether the server runs
`b0de4d4` is owner-confirmed on the host.
Reconstruct: `git log 620da53..b0de4d4 --oneline`.

## `32d185e` — 2026-10-03

Request review and manual grant/revoke (SPEC 4.5/4.6) with the console
Requests tab, student entitlements modal and idle lock; `NOTIFICATION_SERVICE_URL`
in the deploy manual. Reconstruct: `git log 70ec845..32d185e --oneline`.

## `70ec845` — 2026-10-03

Console Catalog part 1 (SPEC 6.3 part 1): fourteen catalog proxy routes,
per-source audit switch, Cairo dates, video force-delete flow. Live since this
release. Reconstruct: `git log 9e0c8b2..70ec845 --oneline`.

## `9e0c8b2` — 2026-10-02

Academy admin API Phases 4.1–4.4: admin listener with verify client and audit
log; diploma/subject/video CRUD with publish rules and YouTube-id validation;
owners keep unpublished subjects until entitlement expiry. Live 2026-10-03.
Reconstruct: `git log b6a11fe..9e0c8b2 --oneline`.

## `b6a11fe` — 2026-10-02

Admin-console skeleton (SPEC 6.1, ADR-0008) released to `main` on owner
confirmation: thin proxy + static Accounts/Audit pages. Third successful
pipeline release. Reconstruct: `git log 6bd6c12..b6a11fe --oneline`.

## `6bd6c12` — 2026-10-02

Session token propagation, cross-service sid revocation, and the logout retry
gap (Phase 1.7 follow-ups). Second successful pipeline release. Reconstruct:
`git log 557f367..6bd6c12 --oneline`.

## `557f367` — 2026-10-02

Production goes live: publishing (`PUBLISH_ENABLED=true`) and deploys
(`DEPLOY_ENABLED=true`) on; Caddy healthcheck uses `127.0.0.1`. First
successful pipeline release. Reconstruct: `git log --oneline 557f367 -15`.
