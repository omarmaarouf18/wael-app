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
- [ ] Console sign-in with a fresh admin token; Accounts, Requests, Catalog,
  Audit and Settings tabs load.

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
- [ ] Center info: set name/address/hours and map URL in the console Settings
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
  alerts only if the owner agrees.
