# Changelog

One entry per production release, reconstructable from git
(`git log <previous>..<sha> --oneline`). Hashes are 7-char short refs.
`origin/main` was at `b0de4d4` (2026-10-09) when the 2026-10-12 release
pull request (`develop` → `main`) was opened; the entry below is that release.

## Release 2026-10-12 (app 1.0.1) — release PR open, `main` still `b0de4d4`

*(Amended 2026-10-13: released. The owner fast-forwarded `main` to
`dcc4fca`, then to `17d1c0d`, which adds one fix found by the first "Build
iOS" run of this release: `fix(ios)` wraps the iOS 17 trait-registration
calls in `CaptureStreamHandler` in `MainActor.assumeIsolated`, because Xcode
26.6 rejected them as main-actor calls from a nonisolated context; no
behaviour change. The released range is `b0de4d4..17d1c0d`. "Build iOS" run
38353982883 uploaded it to App Store Connect as 1.0.1 build 4.)*

Range `b0de4d4..<develop HEAD>`: the 19 commits `7f9397a`..`3b6df3e` plus the
release-prep docs commit that adds this entry (a commit cannot cite its own
hash). `main` moves only by the owner's fast-forward; after it, the `main` tip
is that same `develop` HEAD. `reset-data.sh` is in it (owner decision
2026-10-12). Not in it: the locked-subject card change (owner 2026-10-12:
deferred).
Reconstruct: `git log b0de4d4..<release tip> --oneline`.

### App 1.0.1

- app: per-platform update link and version (owner decisions 2026-10-12) —
  the platform's own fields first, the shared ones as fallback; iOS only ever
  opens an `https://apps.apple.com/...` link; a required update with no usable
  link shows the support WhatsApp button; the Settings "Update available" tile
  follows the same rule. Version `1.0.1+2`. Tests:
  `frontend/test/update_per_platform_test.dart`. (`975c8db`)
- app: notes & books freshness — a file the admin adds shows without a
  reload. Causes found: the shell mounts every tab at once and the notes tab's
  `ensureLoaded()` returned while Home's load was running, so it saw no owned
  subjects and loaded no files; it loaded only in `initState`; subject details
  cached earlier were never refetched; subjects with cached zero file counts
  were skipped; app-config (`features.files`) was read once per cold start.
  One shared mechanism (`RefreshWhenVisible` + `TabVisibility`) refreshes
  Home, Courses, Notes & books and Notifications in the background on tab
  visible / screen open / app resume (≥30 s apart), cached first; concurrent
  catalog loads share one request. Tests: `frontend/test/refresh_freshness_test.dart`. (`048889d`)
- app: iOS screen-capture cover for the player (owner decision 2026-10-12:
  recording protection covers iOS too) — native channel + event stream in
  `AppDelegate.swift` (`sceneCaptureState` on iOS 17+, `isCaptured` +
  `capturedDidChangeNotification` before); the player checks before the first
  play, on every change and on resume, pauses and covers the video while the
  screen is recorded or mirrored, blocks play, never auto-resumes, and fails
  closed when the channel is missing. Screenshots are a documented iOS limit.
  Android `FLAG_SECURE` unchanged. Tests: `frontend/test/ios_capture_cover_test.dart`. (`6e0933e`)
- app: store items and UX pass — iOS `CFBundleDisplayName` "EL METR ACADEMY"
  (owner decision 2026-10-12, same as Android), `CFBundleLocalizations` ar/en,
  an app-level `PrivacyInfo.xcprivacy` (no tracking; name, email, phone, user
  ID, device ID for app functionality) in the Runner resources; the Home hero
  art is bundled as a 190 KB JPEG instead of the 2 MB PNG (the PNG stays the
  icon source); autofill hints, keyboard types and next/done actions in an
  `AutofillGroup` on the password-change, email-change, delete-account and
  profile-edit forms; system back (and the iOS swipe) in full-screen video
  leaves full screen first; the subject screen's WhatsApp button is 48 dp.
  Tests: `frontend/test/autofill_forms_test.dart`, a new full-screen back test. (`b107703`)
- tests and QA: an SSE reconnect test (the last function of the pass's test
  list without a named test) and `docs/frontend/RELEASE-QA-1.0.1.md`, the
  device checklist for one Android phone and one iPhone (TestFlight), one row
  per function with its automated test, plus the earlier unverified iOS fixes
  (iOS 15 target, YouTube frame, PDF Quick Look / share). (`d6a21cc`, `3fe80af`)
- app: new director portrait (owner decision 2026-10-12) — a 720 x 720
  head-and-shoulders crop of the owner's studio photo (Hazem El layub
  Photography), metadata stripped, same asset path; provenance updated. (`01b5d03`)
- app: "السنتر" / "center" → "الأكاديمية" / "academy" in the legal summary
  bullet and the locked-subject activation line (owner decision 2026-10-12,
  as in the admin console). The legal terms page needs the same edit. (`3b319b1`)
- app: one shared `LaunchGuard` (`lib/core/external_links.dart`) for every
  external launch — WhatsApp, update and legal links: a second tap on the
  same link while it launches or within 1 s is ignored, so a double tap
  opens it once. Tests: `frontend/test/launch_guard_test.dart`. (`fd40b24`)

### Admin console

- Catalog tab renamed "دوراتي" / "Courses", Settings "center" renamed
  "الأكاديمية" / "Academy" (owner 2026-10-12); labels only — i18n keys, ids,
  API paths and JSON fields unchanged (`584f77a`).
- Notifications tab (SPEC Phase 7, ADR-0013): compose a broadcast to all
  students or one subject's owners, dry-run preview with the recipient count
  before confirm, history and retry; the Audit tab gains a third source,
  "notifications" (`ab7f350`).

### Server

- academy-service: per-platform update metadata (owner decision 2026-10-12) —
  env `UPDATE_URL_ANDROID`, `UPDATE_URL_IOS` (https, must be an
  `https://apps.apple.com/...` App Store link), `MIN_VERSION_IOS`,
  `LATEST_VERSION_IOS`; app-config serves them as `update_url_android`,
  `update_url_ios`, `min_version_ios`, `latest_version_ios` with the shared
  `update_url`/`min_version`/`latest_version` unchanged so installed 1.0.0
  apps keep working; a bad value fails startup and `--check-env`. Contract
  tests, `.env` examples and SPEC §6/§9 amended; SERVER-MANUAL §5 rows added. (`38b655e`)
- notification-service: admin listener `:9003` (mTLS, internal only) with
  `/internal/admin/broadcasts` create / preview / history / get / retry;
  idempotent fan-out that resumes on retry, its own audit log, 10 sends per
  hour per admin; in-app only, no phone push, no app change needed. New env
  `ADMIN_LISTEN_ADDR`, `AUTH_ADMIN_URL`, `ACADEMY_ADMIN_URL` (required outside
  local/test). academy-service: `GET /internal/admin/subjects/{id}/owner-ids`
  (paged audience for a subject). `shared/infra/storesafety`: the single
  store-safety word rule (no payment wording in admin-composed, student-visible
  text), used by the console settings and broadcast checks (`ab7f350`).

### Infra & ops

- Compose wiring for the broadcast admin URLs in both compose files
  (notification-service `AUTH_ADMIN_URL`, `ACADEMY_ADMIN_URL`,
  `ADMIN_LISTEN_ADDR ":9003"` on `expose` only; admin-console
  `NOTIFICATION_ADMIN_URL`); static values, no new secrets, no new published
  port. `scripts/deploy_scripts_test.sh` Test 22 asserts every required
  `*_ADMIN_URL` inside its own service block, with a negative case; E2E
  `TestBroadcastChain` (env-gated, skips in CI without admin credentials)
  (`a1a5378`).
- `reset-data.sh` (owner decision 2026-10-09 part B, approved for this
  release 2026-10-12): the one-time scripted production data reset. Refuses
  without the exact `--confirm` phrase or with the stack down; takes and
  verifies a `pre-reset` backup first; stops the five app services, drops
  collections one by one (never databases or users; keeps the seeded academy
  levels and `auth_db.blocklist` — `BLOCKLIST_HMAC_KEY` must stay unchanged),
  empties `STORAGE_DIR`, `FLUSHALL` on redis, restarts and runs the deploy
  health gate; on failure prints the exact `restore.sh` command and never
  auto-restores; `--dry-run` only counts. `scripts/deploy_scripts_test.sh`
  Test 23; `lib.sh` gains a shell-only `WAEL_CURL_CA_FILE` override for local
  rehearsals; SERVER-MANUAL §10 "Resetting production data (one-time, before
  launch)" and a RUNBOOK pointer. Rehearsed end to end on a local compose
  stack with a restore drill; not yet run on the real server (`547e5ce`,
  `37b3c1e`, `9e3897d`, `3b6df3e`).

### Docs

- Audit of all tracked Markdown against the `b0de4d4` release state
  (`7f9397a`).
- Broadcast notes in SERVER-MANUAL, RUNBOOK and the release checklist
  (`41641ac`).
- Release prep: this entry, the AI_CONTEXT "Release state" amendment and
  `docs/RELEASE-CHECKLIST.md` "Release 2026-10-12 (app 1.0.1)" (the commit
  that adds this text).

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
