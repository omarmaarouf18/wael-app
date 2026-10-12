# Release checklist — first deploy of `develop`

Order of operations for shipping the current `develop` content to production.
Procedures live in `infrastructure/deploy/SERVER-MANUAL.md` (manual) and
`infrastructure/deploy/RUNBOOK.md` (day-2 checklist); this file only orders
them and does not duplicate them.

## 1. Server prep (owner, on the server)

- [ ] Secrets in `$WAEL_HOME/.env.production` (mode `600`): `JWT_SECRET`,
  `GATEWAY_SECRET`, `INTERNAL_SERVICE_TOKEN` (and `BLOCKLIST_HMAC_KEY`) are at
  least 32 characters and carry no placeholder (`PASTE_`, `CHANGE_ME`,
  `devpassword123`); `APP_ENV` is absent (manual §5, preflight §8 item 3).
  Services refuse to start otherwise — this is a deploy blocker.
- [ ] `TERMS_URL` and `PRIVACY_URL` are set to existing `https://` pages
  (manual §5). The compose file refuses to render without them.
- [ ] *(Added 2026-10-11, for the 2026-10-09 release `b0de4d4` and later:)* the
  Phase 5 values exist per manual §5 "Files (Phase 5)": `STORAGE_DIR` (mode
  `700`), `DOCUMENT_ENCRYPTION_KEY` (64 hex, in the owner's password manager),
  `WAEL_UID`/`WAEL_GID` — preflight item 3b fails without them.
- [ ] Backup cron points at the repo script: deploybot `17 0 * * *` runs
  `<deploy-checkout>/scripts/backup.sh` (manual §10). The old
  `$WAEL_HOME/backup.sh` can go once the new cron has produced an archive.
- [ ] `BACKUP_PING_URL` is set in `.env.production` to a healthchecks.io URL
  (period 1 day, grace 6 hours), so a failed or missed nightly backup alerts
  (manual §10).

## 2. Preflight (owner, on the server)

- [ ] Run `./scripts/preflight.sh` by hand with a real `release.env` and read
  every line (manual §8). It touches nothing running. Fix every failure;
  never deploy past a red preflight.

## 3. Owner fast-forward of `main`

- [ ] `git fetch origin && git checkout main && git merge --ff-only
  origin/develop && git push origin main` (manual §8 Path 1). Agents never
  push `main`. CI Gate (including E2E compose and Prod Image Build) must be
  green first; publishing and deploy follow automatically.

## 4. Smoke checks (after Deploy goes green)

- [ ] `last-good.env` shows the deployed tag; `docker compose -p wael ps`
  shows 8 healthy containers (manual §8 verification).
- [ ] `curl -fsS https://api.<domain>/health` returns `{"status":"ok"}`.
- [ ] Admin host by hand: `curl -fsS https://admin.<domain>/healthz` prints
  `ok` (RUNBOOK "Admin console"; `deploy.sh` does not check it).
- [ ] Console sign-in with a fresh admin token; Accounts, Requests, Courses,
  Audit, Settings, Files and Notifications tabs load. *(Amended 2026-10-11: the Files tab was
  added with the 2026-10-09 release; it needs the Phase 5 server values above,
  and uploads stay internal until the owner flips `FEATURES_FILES`.)*
  *(Amended 2026-10-12, owner: the "Catalog" tab is now "Courses", and the
  Settings "center" labels are now "الأكاديمية" / "Academy".)*
  *(Amended 2026-10-12: the Notifications tab is SPEC Phase 7 broadcast,
  built on branch `feat/phase7-broadcast` and not on `main` yet — until it
  ships, expect six tabs; the audit log gains a "notifications" source with
  it. Delivery is in-app only, no phone push.)*
  *(Amended 2026-10-12, release PR: the broadcast work is on `develop` and in
  the 2026-10-12 release; after that fast-forward expect seven tabs — see
  "Release 2026-10-12 (app 1.0.1)" below.)*

## 5. Rollback (if the health gate fails)

- [ ] Automatic: `deploy.sh` runs `rollback.sh` to the last-good snapshot
  (manual §10). Recovery is always a new commit on `main` (fix forward);
  never re-run the rolled-back `release.env`.
- [ ] Data: rollback covers images only. Restoring the pre-deploy backup
  (`scripts/restore.sh`, the exact command is printed by the rollback output)
  discards every write since the backup — a human decision (RUNBOOK
  "Known gaps").

## 6. Post-deploy switches (owner)

- [ ] Subject prices: set `EXPOSE_PRICE_TO_STUDENTS=true` in the
  academy-service server env, set a price per subject in the console, and turn
  `show_prices` on in the console Settings tab. The app shows a price only
  when both are on; hiding needs no new APK (either switch off).
- [ ] Support WhatsApp: set the number in the console Settings tab (falls back
  to the `SUPPORT_WHATSAPP` server env when empty; no dummy fallback).
- [ ] Academy info: set name/address/hours and map URL in the console Settings
  tab (shown in Settings > Help; absent means nothing shown).
- [ ] Optional update lever: `MIN_VERSION` / `LATEST_VERSION` / `UPDATE_URL`
  (`UPDATE_URL` must be `https://` when set).

## 7. Owner gating tests (after deploy)

- [ ] Account deletion flow: request deletion, cancel by signing in during the
  grace period, password change ending the other sessions, 30-day per-field
  profile edit limit.
- [ ] New APK for the app changes: `frontend/.github/workflows/build-apk.yml`
  runs in wael-app-mobile (see `docs/mobile/RELEASE.md`).
- [ ] Dependabot hygiene: close the redis-8 PR that targets `main`; check the
  Dependabot page for configuration errors; dismiss the 16 stale `x/crypto`
  alerts only if the owner agrees. *(Not re-checked 2026-10-11: GitHub-side PR
  and alert state is not visible from the repo; the owner verifies on GitHub.
  The go1.26.9 toolchain release `d1a49f5` (2026-10-08) addressed the Go
  security advisories that failing govulncheck flagged on go1.26.6.)*

## Release 2026-10-12 (app 1.0.1)

*(Added 2026-10-12.)* Release PR `develop` → `main` from `b0de4d4`; contents
in `CHANGELOG.md` "Release 2026-10-12 (app 1.0.1)". This section only orders
the steps; the procedures are in §1–§7 above, SERVER-MANUAL, RUNBOOK,
`docs/mobile/RELEASE.md` and `docs/frontend/RELEASE-QA-1.0.1.md`.

### Server (before or with the fast-forward)

- [ ] No new secrets.
- [ ] Compose-only env, shipped in the compose file by the deploy (nothing to
  add to `.env.production`): notification-service `ADMIN_LISTEN_ADDR=":9003"`,
  `AUTH_ADMIN_URL`, `ACADEMY_ADMIN_URL`; admin-console
  `NOTIFICATION_ADMIN_URL` (SERVER-MANUAL §5). `:9003` is `expose` only, no
  new published port.
- [ ] Optional, academy-service (SERVER-MANUAL §5 rows, `docs/mobile/RELEASE.md`
  "Update gate values"): `UPDATE_URL_ANDROID`, `UPDATE_URL_IOS`
  (`https://apps.apple.com/...` only), `MIN_VERSION_IOS`, `LATEST_VERSION_IOS`.
  Keep the shared `UPDATE_URL` an Android link or empty while 1.0.0 iPhones
  exist (1.0.0 reads only the shared field).
- [ ] The Phase 5 files turn-on if not yet done: SERVER-MANUAL §5 "Files
  (Phase 5)" and §1 above (`FEATURES_FILES=false` first, test, then `true`).
- [ ] Preflight (§2), then the owner fast-forward (§3).

### Smoke checks (after Deploy goes green; §4 first)

- [ ] Console tabs load: Accounts, Requests, دوراتي / Courses, Audit (sources
  auth, academy and notifications), Settings with «بيانات الأكاديمية», Files,
  Notifications.
- [ ] Send a test announcement to the owner's test account (Notifications
  tab, check the preview count before confirming) and see it in the app.
- [ ] `GET /academy/app-config` shows `update_url_android`, `update_url_ios`,
  `min_version_ios`, `latest_version_ios` when they are set.

### Production data reset (after the smoke checks, before store review)

*(Order changed by the owner 2026-10-12: the reset runs BEFORE store review,
not after it.)*

- [ ] Run `reset-data.sh` — `--dry-run` first and read the counts, then the
  real reset (SERVER-MANUAL §10 "Resetting production data (one-time, before
  launch)", including its after-steps: new admin token, health). Run it before
  entering any real data; `BLOCKLIST_HMAC_KEY` stays unchanged.
- [ ] Then enter the real data (subjects, videos, files, academy settings,
  the reviewer demo account) before store review.

### Mobile

- [ ] wael-app-mobile: "Build Android" (signed) and "Build iOS" (upload to
  TestFlight) on the released commit (`docs/mobile/RELEASE.md`,
  `docs/frontend/IOS-RELEASE.md`).
- [ ] `docs/frontend/RELEASE-QA-1.0.1.md` on one real Android phone and one
  iPhone.

### Store

- [ ] App Store Connect App Privacy adds "Device ID" (matches the app's
  `PrivacyInfo.xcprivacy`).
- [ ] Review notes: students enrol and pay at the academy; the address is in
  Settings → Help; include the reviewer demo account.
- [ ] Prices are shown as in production (§6 switches) during review.
- [ ] Google Play: closed testing (12 testers for 14 days) first if the
  developer account is personal.

### Rollback

- [ ] As in §5 above.
