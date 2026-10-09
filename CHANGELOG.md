# Changelog

One entry per production release, reconstructable from git
(`git log <previous>..<sha> --oneline`). Hashes are 7-char short refs.
`origin/main` and `origin/develop` are both at `620da53` (2026-10-08).

## Unreleased (`develop` = `d1a49f5`)

120 commits since `32d185e` (2026-10-03..2026-10-09): iOS store-ready + CI
(`d549dbd`..`e962f81`), Go security release (`d1a49f5`), P1 gateway/JWT
hardening, verified pre-deploy backup (`93bb0e7`), console settings with
`show_prices` + center, legal links + signup summary, 10-07 review fixes,
10-08 frontend hardening. See "Pending release" in `AI_CONTEXT.md` and
`git log 32d185e..origin/develop --oneline`.

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
