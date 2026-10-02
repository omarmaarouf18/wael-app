# Docs audit 2026-10-02

Scope: every claim in `README.md`, `AI_CONTEXT.md`, `CLAUDE.md`, `docs/**`,
`infrastructure/**/*.md`, `services/*/README*` and `frontend/README.md` about
env vars, ports, commands, file paths, service lists, required checks, workflow
behaviour and feature status, checked against the code and config as it is now
(HEAD includes the pipeline-hardening commits; releases 557f367, 6bd6c12 and
b6a11fe are all ancestors of `origin/main`, verified with `git cat-file -e` and
`git merge-base --is-ancestor`).

Conventions: historical records (dated reviews, captured outputs, decision text
with amendments) were kept and annotated, not rewritten. Line numbers below are
post-fix.

## Fixes (file | line | was | now | evidence)

| File | Line | Was | Now | Evidence |
|---|---|---|---|---|
| `infrastructure/deploy/RUNBOOK.md` | 16 | Release flow ends with "success: record state/last-good.env" | Records `state/last-good/last-good.env` (plus legacy copy) and snapshots of `docker-compose.yml` + `Caddyfile`; failures append to `state/failed-releases` | `infrastructure/deploy/scripts/deploy.sh:37-44`, `scripts/lib.sh:10-17` |
| `infrastructure/deploy/RUNBOOK.md` | 120 | Prerequisites list "W-08 (strict probes in the Dockerfiles), W-10 (fixed UID)" as done | W-08 strictness lives in the production compose healthchecks (they override the Dockerfile fallbacks); W-10 still open, keys stay `644` | `infrastructure/deploy/docker-compose.yml:95-101` vs `services/api-gateway/Dockerfile:50-51` (same `-k`/HTTP fallback pattern in auth/notification/academy Dockerfiles); `adduser -S` without fixed UID in all 5 Dockerfiles |
| `infrastructure/deploy/RUNBOOK.md` | 157 | "`--ttl` takes a Go duration ... there is no `d` unit" | CLI accepts Go durations (`2160h`) AND the `Nd` day shorthand (`90d` is the default); bare numbers fail; max 365 days | `services/auth-service/cmd/onboard-admin/main.go:21-42,64-65,83-86` |
| `AI_CONTEXT.md` | 13 | "CI (publish disabled)" | "CI (publish live since 2026-10-02, ADR-0011)" | `gh api repos/omarmaarouf18/wael-app/actions/variables` shows `PUBLISH_ENABLED=true`; releases on `origin/main` |
| `AI_CONTEXT.md` | 94 | Phase 2.2: "levels with no published subjects are hidden" | Annotated superseded 2026-10-02: `GET /academy/levels` returns all levels | `docs/core-service/SPEC.md` Section 1 decision 2 amendment; `services/academy-service/internal/models/level.go:13` (`StudyTypeOrder`) |
| `AI_CONTEXT.md` | 146 | Open list: "RUNBOOK, DEPLOYMENT, changelog" | RUNBOOK exists at `infrastructure/deploy/RUNBOOK.md`; DEPLOYMENT/changelog still open | File exists in repo |
| `AI_CONTEXT.md` | 150 | onboard-admin example without `--name` | Example includes `--name "<name>"` (required flag) | `services/auth-service/cmd/onboard-admin/main.go:64,72-75` |
| `docs/REPOSITORY-SETTINGS.md` | 12 | "`main` ... require status check: `CI OK`" | Records the observed list: 17 per-job checks, no `CI OK`; missing `Prod Image Build`, `deploy-config-check`, `admin-console-web` | `gh api .../rulesets/24259214` (17 contexts enumerated) |
| `docs/REPOSITORY-SETTINGS.md` | 13 | "`develop` ... require status check: `CI OK`" | No required check configured on `develop` | `gh api .../rulesets/24259229` (only `deletion` + `non_fast_forward` rules) |
| `docs/REPOSITORY-SETTINGS.md` | 20 | Q2 "`CI OK` is the sole required status check on both `main` and `develop`" | Marked DECIDED, NOT YET APPLIED; owner action needed | Same two API calls; `ci.yml:414-456` (`ci-ok`, `name: CI OK`) proves the job exists but is not referenced by either ruleset |
| `docs/adr/0011-deploy-and-mobile-repositories.md` | 84 | "The first publish run creates three images" | Five images | `.github/workflows/build-and-publish.yml:37-42` (5-service matrix) |
| `docs/core-service/SPEC.md` | 67 | D12 "Bachelor years 1-4 only until the diploma/vocational lists are provided" | Dated amendment: resolved — seed holds bachelor 1-4 + single vocational level; diplomas admin-created | `docs/core-service/SPEC.md` Section 1 decision 2 (2026-09-30/2026-10-02 amendments) |
| `docs/core-service/SPEC.md` | 336 | 6.1 "held from `main` pending owner confirmation" | Owner confirmed; released to `main` as `b6a11fe` | `git merge-base --is-ancestor b6a11fe origin/main` |
| `docs/adr/0008-admin-identity.md` | 106 | "stays off `main` until the owner confirms" | Owner confirmed; released as `b6a11fe` | Same as above |
| `docs/BOOTSTRAP-REFERENCE.md` | 265 | Inventory lists 3 services | 5 services with current routes/phases | `infrastructure/deploy/docker-compose.yml:38-246`, publish matrix |
| `docs/BOOTSTRAP-REFERENCE.md` | 270 | "`build-and-publish.yml` disabled with `if: false`" | Gated by `vars.PUBLISH_ENABLED`, live since 2026-10-02 | `.github/workflows/build-and-publish.yml:26-29` |
| `docs/BOOTSTRAP-REFERENCE.md` | 271 | "8 ADRs" | 10 ADR files (0001-0009 plus 0011; 0010 reserved) | `docs/adr/` listing |
| `docs/BOOTSTRAP-REFERENCE.md` | 299 | "Not built yet" lists contract tests, academy service, console as unbuilt | Annotated: built since review (contracts in CI, academy Phases 2-3, console 6.1, CLI tooling 1.4); open items kept | `tests/contracts/`, phase entries in `AI_CONTEXT.md` |
| `docs/BOOTSTRAP-REFERENCE.md` | 306 | "`shared/infra/storage` (removed, scheduled to be restored in Phase 0.4)" | Restored in Phase 0.4, covered by contract tests | `shared/infra/storage/` exists |
| `docs/BOOTSTRAP-REFERENCE.md` | 377 | "(publishing is still disabled, so the last three are pre-work)" | Publishing live; scan/sign/SBOM still pre-work | Same evidence as CI row |
| `docs/BOOTSTRAP-REFERENCE.md` | 495 | Branch-ruleset comparison "Unknown / Unknown" | Points to `docs/REPOSITORY-SETTINGS.md` with the observed 2026-10-02 state | GitHub API as above |
| `docs/BOOTSTRAP-REFERENCE.md` | 516 | "Publish gated on CI and E2E: n/a (disabled)" | Yes — CI Gate runs `E2E (compose)` + `Prod Image Build` on `main`; publish waits via `workflow_run`; live since 557f367 | `.github/workflows/ci.yml:293-381`, `build-and-publish.yml:10-14` |
| `docs/BOOTSTRAP-REFERENCE.md` | 585 | Backlog item 13 "re-enable `build-and-publish.yml`" open | Done 2026-10-02, incl. last-good snapshot + failed-releases guard | `scripts/deploy.sh:37-44`, `scripts/rollback.sh:26-32` |
| `docs/BOOTSTRAP-REFERENCE.md` | 750 | Appendix E prescribes the old per-job check list | Annotated superseded by Q2 (`CI OK` sole check; not yet applied) | Same as REPOSITORY-SETTINGS |
| `docs/BOOTSTRAP-REFERENCE.md` | 850 | W-11 "Confirmed" (no logout, revocation never called) | Fixed by Phase 1.7: `POST /auth/logout`, `session_replaced`, newest-2 cap | `services/auth-service/internal/handlers/auth.go:593,626`, `admin.go` (non-test revocation callers) |
| `docs/BOOTSTRAP-REFERENCE.md` | 854 | W-15 "Unknown (not visible from the repo)" | Recorded with the API-observed state and the remaining Q2 action | Same as REPOSITORY-SETTINGS |
| `docs/frontend/BEHAVIOR.md` | 125,278 | Scenario-15 FAIL and Finding 2 state "no `POST /auth/logout`" as current | Dated 2026-10-02 supersede notes; captured 2026-09-30 output kept as-is | Phase 1.7 code paths above; gateway logout proxy `services/api-gateway/internal/proxy/proxy_test.go:187-199` |

## Checked, no change (still accurate)

- `omar-maarouf.me`: zero occurrences anywhere (`grep` over `*.md,*.yml,*.sh,Caddyfile,compose`) — nothing to remove. Production domain is `elmetracademy.app` (`API_DOMAIN`/`ADMIN_DOMAIN` in `env.production.example:12-15`, Caddyfile, RUNBOOK).
- Pipeline LIVE: 557f367, 6bd6c12, b6a11fe all resolve (`git cat-file -e`) and are ancestors of `origin/main`. `PUBLISH_ENABLED=true`, `MOBILE_SYNC_ENABLED=true` (wael-app vars API), `DEPLOY_ENABLED=true` (wael-app-deploy vars API).
- Stack shape: 5 app services + mongo + redis + caddy = 8 containers (`infrastructure/deploy/docker-compose.yml`); only Caddy publishes ports (`80/443 TCP, 443/UDP`); `:9001`/`:9002` never published.
- Caddy healthcheck uses `http://127.0.0.1:2019/config/` (`docker-compose.yml:57`); `localhost` would resolve to `::1`.
- `deploy.yml` (41 lines) does carry the `allow_failed_release` input quoted by RUNBOOK (`deploy.yml:10-15,40`).
- `onboard-admin` default/max TTL (`90d`/365d), `--id` on `revoke-admin`, no list-admins CLI (only the two CLIs exist under `services/auth-service/cmd/`; listing is via the console `GET /api/accounts` proxy).
- `--ttl 90` (bare) fails with "missing unit" (falls through to `time.ParseDuration`); `--ttl 90d` and `--ttl 2160h` both work — the troubleshooting rows for the bare-number failure are accurate.
- `generate-certs.sh` flags (`--sign-only <svc> --ca-dir <dir> --out-dir <dir> [--force]`), 5-service list, 825-day validity, `644` key install (needed because containers run as `USER appuser` with images built without a fixed UID, reading host-mounted `:ro` files owned by `deploybot`).
- `frontend/README.md` ports table (8080 default with 18080 custom-port examples) matches `infrastructure/docker-compose.yml:68` (`${GATEWAY_HOST_PORT:-8080}`) and the `../scripts/` gate path; `services/admin-console/README.md` 6-route allowlist and config table match `config.Load()`; `docs/REPOSITORY-SETTINGS.md` wael-app-deploy/wael-app-mobile rows match ADR-0011 mirror design.
- `CLAUDE.md` merge policy ("owner-approved fast-forward of a `develop` green on `CI OK`") is process intent; enforcement lags (see Unverified/owner actions). Left untouched as governance.
- `docs/frontend/VIDEO_PLAYER.md` "not verified against the real service yet" still stands (backend play endpoint has unit/handler/leak tests, no live-service verification recorded).

## Unverified (not checked, do not treat as fact)

1. Anything on the server itself: `$WAEL_HOME` layout/contents, cert files and expiry, `.env.production` values, whether the backup cron exists, swap/zram state, runner online status. None of this is visible from the repo.
2. DNS and mail records (`api`/`admin` A records, Resend DKIM/SPF/DMARC) — no authority access from here.
3. GHCR package visibility (API needs `read:packages` scope this session lacks). The manual covers both public and private cases.
4. Whether any `wael-app-mobile` sync or APK build has run (var `MOBILE_SYNC_ENABLED=true` is set; no run is observable from this repo).
5. Whether each of the three releases deployed green on the host (they are on `main` and publish triggers on CI success; deploy outcomes are not visible here).
6. GitHub App key validity and accepted permissions (values not readable by design).
7. `BEHAVIOR.md` Findings 1, 3, 4, 5, 6 against current code (only Finding 2 / scenario 15 were re-checked for this audit).
8. The 1 GB memory profile and the manual-trial (no-GHCR) deploy path (RUNBOOK already marks both untried; the new manual marks restore/migration "not yet rehearsed").
9. Azure size figures in the manual (B2ats_v2, 887 MB, 320-400 MB stack) are owner-measured and repeated as given, not re-measured here.

## Owner actions (code/settings, not docs — listed only)

- Apply the Q2 ruleset change: replace the `main` per-job list with the single `CI OK` check and add `CI OK` to `develop` (`docs/REPOSITORY-SETTINGS.md` records the current state).
- Dockerfile `HEALTHCHECK` fallbacks (`-k`, plain HTTP) in api-gateway, auth-service, notification-service and academy-service still fail open if ever run without the production compose overrides; admin-console's probe is already strict.
- No backup automation exists in the repo (RUNBOOK "Known gaps"); the new `SERVER-MANUAL.md` §10 gives the setup but it is not yet rehearsed.
