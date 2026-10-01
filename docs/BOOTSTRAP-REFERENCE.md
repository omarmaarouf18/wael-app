# Project Bootstrap Reference: Engineering Discipline, Security and CI/CD

> Derived from **saas-core** (branch `logic-exploitation`, 1,391 commits at review time)
> and audited against **wael-app** (branch `develop`, 53 commits at review time).
> Review date: 2026-09-30. Suggested location in the repo: `docs/BOOTSTRAP-REFERENCE.md`.
>
> Purpose: one document that (1) records how saas-core is built, secured, gated and
> shipped, independent of its business logic, (2) lists what not to copy from it, and
> (3) measures wael-app against that bar and turns the gaps into an ordered backlog.

---

## 0. How to read this document

### 0.1 Evidence basis (read this first)

| Item | Status |
|---|---|
| Source code, workflows, hooks, Dockerfiles, compose files, ADRs, docs of both repos | **Read** (static review) |
| Builds, tests, hooks, workflows, containers | **Not run.** No claim below says "passes" or "fails" |
| GitHub settings (branch protection, rulesets, secrets, environments, runner config) | **Not visible** from the repo. Marked `Unknown` |
| `saas-core-deploy`, `kyc-reviewer-console`, `quick-delivery-mobile`, production VM | **Not inspected.** Described only from what saas-core docs say |
| Numbers such as "82 endpoints" or "532 tests" | **Quoted from saas-core docs, not verified** |

Every finding cites a file so it can be re-checked. Where something is an inference it is
labelled `Observation`, not `Finding`.

### 0.2 Legend

- **Severity**: `P1` fix before any more feature work, `P2` fix before first deploy,
  `P3` hardening.
- **Status column values**: `Yes`, `Partial`, `No`, `Planned` (documented as future work),
  `Unknown` (not visible from the repo).
- Finding IDs: `S-xx` = weakness in saas-core (do not copy), `W-xx` = gap in wael-app.

### 0.3 One rule about hashes

Both repos treat any full 40-hex-character string in a `.md` file as a live commit citation
that must resolve. This document contains none. Keep it that way, or cite only verified
hashes.

---

## 1. The reference system (saas-core) on one page

### 1.1 Shape

- Go workspace monorepo (`go.work`, pinned `go 1.26` / `toolchain go1.26.6`) with five
  services: `api-gateway`, `auth-service`, `user-service`, `chat-service`,
  `notification-service`.
- `shared/infra` holds cross-cutting libraries: `jwtutil`, `ratelimit`, `handlerutil`
  (`max_bytes`, `safe_errors`, `security_logs`, `limiter`), `redact`, `resilience`,
  `tlsutil`, `storage`, `docgen`.
- `tools/` (`docgen`, `paritycheck`), `tests/contracts` (in-process inter-service schema
  tests), `tests/e2e` (critical user journeys, API contract suite, security regression).
- `frontend/` is a Flutter app. `infrastructure/` holds the dev compose, staging compose
  (Caddy edge, isolated Mongo/Redis, production images) and the reference production
  compose and deploy workflow.
- Every service has the same layout: `cmd/main.go` plus `internal/{config,handlers,models,store}`.
  Every service accepts `--check-env` (load config, exit 0/1, start nothing).

### 1.2 Multi-repo topology

| Repo | Role | Who writes to it |
|---|---|---|
| `saas-core` | Source of truth: services, Flutter app, tests, CI | Developers |
| `saas-core-deploy` | Deploy-only: compose + `deploy.yml`. The VM only ever sees this repo | CI (GitHub App token) |
| `quick-delivery-mobile` | Mirror of `frontend/` via `git subtree split`, builds and releases the APK | CI (GitHub App token) |
| `kyc-reviewer-console` | Separate ops console, contract-tested via `repository_dispatch` | Developers + CI trigger |

Reasoning recorded in ADR-0010: the production host, mobile CI and marketing site must never
need the monorepo source, a Go toolchain or a Flutter SDK.

### 1.3 Environments

| Dimension | Local dev | Staging | Production |
|---|---|---|---|
| Image target | `dev` (air hot reload) | `prod` | `prod` |
| Ingress | gateway port | Caddy | Caddy |
| Datastores | dev Mongo/Redis | isolated Mongo/Redis | production |
| Internal TLS | mTLS | enforced mTLS | enforced mTLS |
| Lifecycle | manual | `scripts/staging_up.sh` / `staging_down.sh` | continuous deploy |

Staging exists because of real incidents: SSE buffering behind a proxy, a console deployed
out of step with the gateway, and WebSocket upgrades broken by a wrapping middleware. All
three passed unit tests and failed only at the seams.

---

## 2. Security model to replicate

| Layer | Control | Where it lives in saas-core |
|---|---|---|
| Transport | mTLS between all services; internal services `expose` only; DB/Redis bound to `127.0.0.1` | `infrastructure/`, `shared/infra/tlsutil` |
| Edge | Gateway trusts `X-Forwarded-For` only from `TRUSTED_PROXY_IPS`; otherwise overwrites it with the socket address | `services/api-gateway/internal/iputil`, `proxy` |
| Service-to-service auth | `GATEWAY_SECRET` and `INTERNAL_SERVICE_TOKEN`, empty-secret guard, constant-time compare | service configs, handlers |
| Config | Required env vars checked at startup; service refuses to start if a secret is empty | each `internal/config/config.go` |
| Production guards | Production requires `DOCUMENT_ENCRYPTION_KEY`; production refuses the mock OTP dispatcher; refuses `APP_ENV=local` together with `CLOUDWATCH_LOG_GROUP` (so `dev_otp` cannot reach a production-observed instance) | `auth-service/internal/config`, `cmd/main.go` |
| Data at rest | AES-256-GCM for OTPs and uploaded documents; signed document URLs | `otpcrypto`, `shared/infra/storage` |
| Sessions | JWT with Redis-backed denylist and per-user revocation | `shared/infra/jwtutil` |
| Logging | Credential redaction in URIs, CR/LF stripping for log injection, no query strings or tokens in logs | `shared/infra/redact`, `handlerutil/security_logs` |
| Errors | Safe error writer, no internal detail in responses | `handlerutil/safe_errors` |
| Input limits | Request body size middleware | `handlerutil/max_bytes` |
| Attack surface | No admin HTTP endpoints. KYC approval, subscription activation, reviewer/agent onboarding are CLIs (`cmd/approve-kyc`, `cmd/onboard-*`) or documented manual runbooks | `services/*/cmd/`, README runbooks |
| Containers | Multi-stage build, static binary (`CGO_ENABLED=0`), non-root `appuser`, `HEALTHCHECK`, unprivileged ports, DB per service | `services/*/Dockerfile` |
| Security tests | JWT tampering, expiry, replay after logout, rate-limit bypass, CORS, broad RBAC matrix | `tests/e2e/security_regression_test.go`, `auth_matrix_test.go` |
| Static analysis | `gosec` and `govulncheck` on every module, locally and in CI | hook, `ci.yml` |

Two habits worth copying as habits, not as code: security decisions are recorded as ADRs the
day they are made, and every security fix goes into a categorized changelog with a verified
commit citation.

---

## 3. Engineering discipline (the part that transfers to any project)

### 3.1 Agent and human contract

- `CLAUDE.md` is the single instruction file. It tells any agent to read `AI_CONTEXT.md`
  first and to update it in the same commit as any change that alters project state.
- Other tool-specific files (`GEMINI.md`, `AGENTS.md`) are one-line pointers: "Follow
  CLAUDE.md verbatim". Never copies, to avoid drift.
- Rules that matter most:
  - One logical change per commit, specific messages.
  - Never `--amend` a commit whose hash is cited anywhere. Never force-push.
  - Write a commit hash into a document only after the commit exists, and verify it with
    `git rev-parse HEAD`, `git cat-file -e <sha>^{commit}` and `git show --stat <sha>`.
  - Never paste "illustrative" command output. Paste captured output or say it was not
    captured.
  - Report `make push` output verbatim (`PUSH_VERIFIED: <hash>`) instead of retyping hashes.
  - Call out incidental changes to shared UI widgets separately in commit and report.
  - Open questions and locked decisions are not resolved by the implementer: stop and report.

### 3.2 Local gate (`make setup` then `.githooks/pre-push`)

Runs, in order: `gofmt` -> `dart format` -> frontend composition gate -> Markdown SHA
validity and reachability -> Go version drift guard (go.work, every go.mod, ci.yml, every
Dockerfile) -> per module `go build/vet/test` -> `govulncheck` -> `gosec` -> contract tests
-> `flutter analyze` and `flutter test`. The CI job "Flutter Lint & Test" uses the same
frontend order: format, composition gate, gate self-test, analyze, test.

Makefile targets worth keeping: `ensure-hooks`, `setup`, `ci`, `commit`, `push` (verifies
local HEAD equals remote HEAD and writes `PUSH_VERIFIED`), `report-hash`,
`since-last-report`, `docs-check`, `contract-test`, `backend-frontend-parity-check`.

Known limitation: the hook is inactive on a fresh clone until any `make` target runs
`ensure-hooks`. CI is the real gate; the hook exists to fail early.

### 3.3 Documentation as code

- `tools/docgen` regenerates the endpoint table in `docs/APPLICATION_MAP.md` from
  `RegisterRoutes` declarations (AST), and `make docs-check` fails on drift, including
  every Dart file under `frontend/lib/` being mentioned in `docs/frontend/STATUS.md`.
- `tools/paritycheck` cross-references backend routes with client call sites and flags
  unconsumed or rogue routes.
- ADRs (`docs/adr/NNNN-title.md`) use a fixed template: Status, Date, Related Commit SHA,
  Related finding, Context, Decision, Consequences, Alternatives Considered.
- Changelog is split by kind (security, features, infrastructure, bug fixes, documentation,
  localization, refactoring). Each entry cites a verified commit.

### 3.4 Branching and release

- All work on the development branch; `main` receives fast-forward merges only, after full
  tests and CI.
- Emergency CD hotfix may go straight to `main` and must be fast-forwarded back to the
  development branch as the very next action.
- Changes to suspension, gating and admin authorization stay off `main` until the owner
  confirms (ADR-0022 "Deployment Gate").

### 3.5 Frontend composition gate

`scripts/frontend_composition_gate.sh` scans `frontend/lib/screens/` only (widgets are the
shared layer and are not scanned). Screens compose shared widgets and design tokens; the
gate fails on nine text-matched rules:

| Rule label | Pattern it counts |
|---|---|
| `scaffold` | `Scaffold(` |
| `appbar` | `AppBar(` |
| `box_decoration` | `BoxDecoration(` |
| `text_style` | `TextStyle(` |
| `font_size` | `fontSize:` |
| `color_literal` | `Color(0x` followed by eight hex digits, `Color.fromARGB(`, `Color.fromRGBO(` |
| `material_color` | `Colors.<name>` (`Colors.transparent` is allowed) |
| `to_upper_case` | `.toUpperCase()` |
| `non_directional_insets` | `EdgeInsets.only(` and `EdgeInsets.fromLTRB(` (Arabic is RTL) |

Matching is textual, so a pattern inside a comment counts too.

Ratchet: wael-app started with existing violations, so the gate compares per-file, per-rule
counts against `scripts/frontend_gate_baseline.txt` (`file|rule|count`):

- a count above the baseline, or a violation in a file with no baseline entry, fails;
- a count below the baseline also fails, with a note to lower the baseline in the same
  commit (`scripts/frontend_composition_gate.sh --update`), so the baseline can only go down;
- an empty baseline makes the gate strict;
- if `frontend/lib/screens` does not exist, or grep fails on a file, the gate prints
  `GATE ERROR` and exits 2 (fail closed).

`scripts/frontend_gate_test.sh` self-tests the gate on throwaway layouts (an injected
violation per rule, ratchet up and down, missing directory, unreadable file) and runs in
`.githooks/pre-push` and in CI right after the gate.

It exists because styling drifted twice through per-screen overrides.

---

## 4. CI/CD pipeline (reference)

```
developer -> make push (pre-push hook)
   -> push/PR to main or logic-exploitation: ci.yml
        lint-formatting | build-test (matrix, Mongo+Redis containers) | flutter-test
   -> fast-forward merge to main
        build-and-publish.yml : 5 images -> ghcr.io/<owner>/saas-core-<svc>:<sha>, :latest
                                -> sync compose + deploy.yml into saas-core-deploy
        release-gate-e2e.yml  : staging stack, CUJ + API contract + security suites
        sync-mobile-frontend  : subtree split -> quick-delivery-mobile -> APK + release
        trigger-reviewer-console-sync : repository_dispatch contract run
   -> saas-core-deploy push -> deploy.yml on self-hosted runner [self-hosted, saas-vm]
        pre-flight (--check-env in the new image) -> pull -> up -d -> health per service
        -> automatic rollback on health failure
```

Design points worth copying:

- **GitHub App installation tokens** (`actions/create-github-app-token`) instead of PATs;
  the old PATs are documented as revoked.
- **Pull-only CD:** the runner polls GitHub outbound; no inbound port, no SSH key in GitHub.
- **Pre-flight before touching containers:** each new image is run with `--check-env`
  against the production `.env`.
- **Health-gated rollback** that re-verifies health after rolling back.
- **Single source of truth for production env** (ADR-0012): one file, symlinked into the
  runner workspace.
- **Publishing only from `main`, no `workflow_dispatch` on the publish workflow.** ADR
  records the incident that produced this rule.

---

## 5. Weaknesses in saas-core: do not copy

| ID | Finding | Evidence | Why it matters |
|---|---|---|---|
| S-01 | Publish and E2E gate run in parallel on push to `main`; the image build does not depend on CI or E2E | `build-and-publish.yml`, `release-gate-e2e.yml` | `docs/RELEASE-GATE.md` calls the gates blocking; the workflows do not enforce it. Only branch protection could |
| S-02 | The "pre-flight fails before touching running containers" promise is broken by an earlier step that may `docker compose down` and `rm -rf certs` | `infrastructure/deploy/deploy.yml`, step "Ensure Real mTLS Certificate Files" | A cert-state problem can take production down before validation |
| S-03 | Rollback restores `docker-compose.yml` from `HEAD~1`; saved image tags are only printed | `deploy.yml`, "Rollback on Health Failure" | Assumes the previous commit was good; no DB schema rollback story |
| S-04 | Actions pinned by tag, `gosec@latest`, no `permissions:` block in `ci.yml`, no Dependabot, no CODEOWNERS, no image scanning, signing or SBOM | `.github/`, `ci.yml` | Supply-chain exposure |
| S-05 | Private keys at mode `644` as a documented workaround for UID mismatch; 10-year certs; no rotation; cert generation logic duplicated in docs, `generate-certs.sh` and `deploy.yml` | `docs/DEPLOYMENT.md` 5.3, `deploy.yml` | Weak key hygiene, drift between copies |
| S-06 | `HEALTHCHECK` and deploy health probe fall back to plain HTTP with `-k` | `services/*/Dockerfile`, `deploy.yml` | A broken mTLS setup still looks healthy. The docs admit this |
| S-07 | `deploybot` is in the `docker` group (root-equivalent); Caddy proxies to the gateway with `tls_insecure_skip_verify` | `docs/CI_CD_AND_HOOKS.md`, `docs/DEPLOYMENT.md` 8.1 | Accepted trade-offs, but they should be conscious choices in a new project, not inherited defaults |
| S-08 | No migration framework; migrations are hand-run `mongosh` snippets; backups are a manual `mongodump`; no restore drill | `docs/DEPLOYMENT.md` 9, 11 | Rollback cannot cover data changes |
| S-09 | Production secrets live in one file on the VM (`/home/deploybot/.env`), no secret manager | ADR-0012 | Single point of loss and exposure; drift risk between copies |
| S-10 | `AI_CONTEXT.md` is about 437 KB | repo root | A "read this first" file that large defeats its purpose. History belongs in the changelog |
| S-11 | Documentation drift: `docs/CI_CD_AND_HOOKS.md` still shows PAT-based flow diagram; several links are local absolute paths (`/mnt/windows_data/...`); `REPOSITORY_MAP` mentions `rsync -a` that the workflow does not run | those docs | The drift gate covers endpoints and counts, not diagrams or workflow descriptions |
| S-12 | The local hook filters `govulncheck` output (ignores stdlib findings, matches one module prefix) while CI uses the full action | `.githooks/pre-push`, `ci.yml` | Breaks the "local equals CI" principle |

---

## 6. wael-app: current state

### 6.1 Inventory (from the repo at `develop`)

| Area | What exists |
|---|---|
| Services | `api-gateway` (routes `/api/v1/auth/`, `/api/v1/notifications/`), `auth-service` (signup, verify-otp, login, refresh, reset request/verify/confirm, me; single role `user`), `notification-service` (SSE, list, mark-read, internal push) |
| Shared | `shared/infra`: `jwtutil`, `ratelimit`, `handlerutil`, `redact`, `resilience`, `tlsutil` (10 test files) |
| Tests | Go unit tests per module (gateway 4, auth 5, notification 3 test files); `tests/contracts` is a skeleton (`doc.go` only); `tests/e2e/chain_test.go` runs only when `E2E_GATEWAY_URL` and `E2E_CA_CERT` are set; Flutter: 11 test files |
| Frontend | Flutter app with real gateway auth, notification repository, debug diagnostics screen (excluded in release) |
| Infra | Local compose (`mongo:7`, `redis:7-alpine`, mTLS), `generate-certs.sh`, `.env.example` |
| CI | `ci.yml` (lint-formatting, build-test matrix, security, gitleaks, flutter-test); `build-and-publish.yml` disabled with `if: false` |
| Governance | `CLAUDE.md`, `AGENTS.md` pointer, `AI_CONTEXT.md`, 8 ADRs, `docs/core-service/SPEC.md`, `docs/frontend/BEHAVIOR.md` (24 executed scenarios with captured output), `docs/asset-provenance.md` |

### 6.2 Done well (keep, and copy into project 3)

- **Public-repo hygiene from the start:** gitleaks over full history with a fingerprint
  allowlist that requires a stated reason per entry; top-level `permissions: contents: read`
  in `ci.yml`; extended `.gitignore` for keys, keystores, service-account files, dumps.
- **`gosec` is pinned** and a drift check ties the hook pin to the CI pin.
- **The security requirements are written before the code:** SPEC section 8 forbids copying
  `tls_insecure_skip_verify`, requires `--check-env`, empty-secret guards, fail-closed on
  Redis or auth outage, PDF magic-byte checks, redacted logging.
- **Gateway strips a client-supplied `X-Internal-Token` and injects none**
  (`services/api-gateway/internal/proxy/proxy.go`); SPEC asserts a test for it.
- **Refresh tokens are consumed atomically** (single redemption); signup ignores any client
  role.
- **Behavior evidence over assertions:** `docs/frontend/BEHAVIOR.md` records real container
  fault injection (Redis down, Mongo down) with captured output, and states honestly which
  items were not verified.
- **Smaller, cleaner governance files:** `AI_CONTEXT.md` is about 5 KB, `CLAUDE.md` about
  3 KB, and the e2e test refuses `InsecureSkipVerify`.

### 6.3 Not built yet (documented as open, so not defects)

Deploy repo, CD workflow, staging stack, release-gate workflow, RUNBOOK, DEPLOYMENT,
changelog, real `tools/docgen`, real contract tests, academy service (SPEC phases 0 to 7),
admin console and CLI identity tooling (ADR-0008 accepted, not implemented),
`shared/infra/storage` (removed, scheduled to be restored in Phase 0.4).

---

## 7. wael-app gap report

### 7.1 Findings

**W-01 `P1` Production configuration fails open.**
`services/auth-service/internal/config/config.go` documents that Mongo, Redis and TLS are
optional, and `cmd/main.go` (lines around 37 to 60) silently falls back to
`store.NewMemoryStore()`, in-process OTP/lockout stores and plain HTTP with only a log line.
`APP_ENV` defaults to `production`, so a missing `MONGO_URI` in production produces a
"healthy" service that loses all users on restart and shares no lockout state across
replicas. The gateway has the same optional-TLS design (`MTLSClientEnabled`), and the
notification-service config header describes the same fallback (its `main.go` was not read).
saas-core required these values and failed fast.
*Fix:* validate `APP_ENV` against an allow-list; outside `local` and `test`, `Load()` must
require Mongo, Redis and the TLS triple, and services must refuse plain HTTP.
*Acceptance:* a table test per service, one case per missing variable in each non-local
`APP_ENV`, expecting an error naming the variable.

**W-02 `P1` One-time codes are written to logs unless a real mail sender is configured, in any environment.**
`cmd/main.go` defaults `sender` to `mailer.LogSender{}`, which does
`log.Printf("... code=%s", ...)` (`internal/mailer/mailer.go`). Nothing ties this to
`APP_ENV`. `dev_otp` is returned in API responses when `APP_ENV` is `local` or `test`
(`handlers/auth.go`, around lines 81, 152, 312). saas-core refused to start in production
with a mock dispatcher and refused `APP_ENV=local` next to production telemetry; neither
guard was carried over.
*Fix:* refuse to start when `APP_ENV` is not `local`/`test` and no real sender is
configured; carry over the local-with-production-telemetry guard (adapt the signal to
whatever telemetry variable this project uses).
*Acceptance:* unit tests for both refusals; startup log states the active dispatcher.

**W-03 `P1` CI does not verify Markdown commit citations; the hook does.**
`.githooks/pre-push` scans all `.md` files for 40-hex strings and checks existence and
ancestry, but `ci.yml` `lint-formatting` has only `gofmt` and the version drift guard. The
hook is inactive on a fresh clone. saas-core added the CI step after an incident where a
fabricated SHA passed because the hook was not activated.
*Fix:* add the same step to `ci.yml` (snippet in Appendix D).
*Acceptance:* a PR that cites a non-existent hash fails CI.

**W-04 `P1` No `--check-env` in any service.**
`grep` finds no `--check-env` in `services/*/cmd/main.go`, while SPEC section 8 item 10
makes it mandatory for new services and the whole pre-flight design depends on it.
*Fix:* add it to gateway, auth and notification; make it call the same `Load()` as
startup, including the W-01 rules; test that it exits 1 on a missing required variable.

**W-05 `P1` No Mongo-backed test runs anywhere.**
No `_test.go` under `services/` or `shared/` references `MONGO_URI`, and `ci.yml` declares
no service containers. SPEC section 10 requires every `Store` behavior to be tested against
both `MemoryStore` and `MongoStore`. The Mongo stores, atomic refresh redemption and the
compare-and-set access rules planned in SPEC R4 are therefore unproven against a real
database.
*Fix:* add `mongo:7` and `redis:7-alpine` service containers with health checks to
`build-test`; export `MONGO_URI` and `REDIS_URI`; make the tests skip only when a
`REQUIRE_DB` variable is unset, and set it in CI so a skip becomes a failure.

**W-06 `P2` CI reports green for suites that execute nothing.**
`tests/contracts` contains only `doc.go`. `tests/e2e/chain_test.go` calls `t.Skip` when
`E2E_GATEWAY_URL` is unset, and the CI matrix runs `go test` on it. The local
`make contract-test` and the hook step "running contract tests" therefore run zero tests.
*Fix:* real contract tests (start with auth<->notification and gateway<->auth request and
response shapes); an E2E job that boots the compose stack; `E2E_REQUIRED=1` converts a skip
into a failure (Appendix D).

**W-07 `P2` Supply chain is unpinned.**
Actions use tags (`actions/checkout@v4`, `setup-go@v5`, `golang/govulncheck-action@v1`,
`subosito/flutter-action@v2`, `docker/*`). The hook installs `govulncheck@latest`.
Dockerfile bases use tags (`golang:1.26.6-alpine`, `alpine:3.20`, `mongo:7`,
`redis:7-alpine`), not digests. No Dependabot, no CODEOWNERS, no image scan, signing or
SBOM (publishing is still disabled, so the last three are pre-work).
*Fix:* pin actions by commit SHA with a version comment, pin `govulncheck`, add
`.github/dependabot.yml` (gomod per module, github-actions, docker, pub), add CODEOWNERS
for `.github/`, `infrastructure/`, `shared/infra/`, `docs/adr/`, `services/auth-service/`.

**W-08 `P2` Container health probes still fall back to plain HTTP.**
The prod `HEALTHCHECK` in `services/auth-service/Dockerfile` ends with
`curl -f -s -k https://... || curl -f -s http://...`; the gateway compose healthcheck falls
back to `wget http://`. With W-01 fixed, a broken mTLS state should make the container
unhealthy, not pass through a fallback.
*Fix:* one strict probe per service (with client cert and `--cacert`). The dev compose
already does this for auth and notification; align the Dockerfiles and the gateway.

**W-09 `P2` Redis password is passed on the command line.**
`infrastructure/docker-compose.yml`: `command: redis-server --appendonly yes --requirepass ${REDIS_PASSWORD}`
exposes it through `docker inspect` and the process list. Also, dev compose publishes Mongo
and Redis on `127.0.0.1`; that must not carry into a production compose.
*Fix:* mount a config file or pass the password via a secret file; keep DB ports unpublished
in every non-dev compose.

**W-10 `P2` Certificate handling inherits saas-core's compromises.**
`generate-certs.sh` sets `chmod 644` on all keys (the saas-core UID-mismatch workaround),
issues 825-day certificates, and has no rotation or expiry check. Cert generation will need
to be reused by dev, staging and deploy; keep exactly one script.
*Fix:* run containers under a fixed UID/GID that owns the key files (or use
`group_add`), keep keys at `640`, shorten lifetimes, add an expiry check to the health or
preflight step.

**W-11 `P2` Session lifecycle.**
Access tokens live 24 hours (`shared/infra/jwtutil/jwt.go`), no logout endpoint exists in
`auth-service` routes, and `RevokeToken` / `RevokeAllUserTokens` have no non-test caller
under `services/` (checked by search). This matches Findings 2 in
`docs/frontend/BEHAVIOR.md`. SPEC rule R7 (suspension) depends on `RevokeAllUserTokens`
working end to end.
*Fix:* 15-minute access tokens with the existing rotating refresh, `POST /auth/logout`
calling `RevokeToken`, and a test that a revoked token is refused.

**W-12 `P2` Error semantics hide outages.**
From `BEHAVIOR.md` findings 3 and 6: Redis outage is reported as HTTP 429 with a malformed
`Retry-After: 30ns`; a Mongo failure during signup is reported as `409 email already
registered`; auth-service sets no timeout on database contexts.
*Fix:* 503 for dependency failures, integer `Retry-After`, distinguish duplicate-key from
topology errors, 3 to 5 second query timeouts. This also matters for alerting: a database
outage must not look like client behavior.

**W-13 `P2` No documentation drift gate; `tools/docgen` is a placeholder.**
`tools/docgen/main.go` prints "docgen skeleton (pending)". There are no `docs-check`,
`docs` or parity targets, no endpoint map, and no changelog directory. `AI_CONTEXT.md` is
one dense paragraph of history rather than a structured state file.
*Fix:* before the academy service lands, implement the endpoint-table generator and
`make docs-check`; add `docs/changelog/`; restructure `AI_CONTEXT.md` into fixed sections
with a size budget (Appendix H).

**W-14 `P2` Frontend gates were thinner than the reference. Fixed in 06f0612 (composition gate).**
The composition-layer gate (saas-core's `frontend_composition_gate.sh`) is ported as
`scripts/frontend_composition_gate.sh` with a ratchet baseline
(`scripts/frontend_gate_baseline.txt`), and runs in `.githooks/pre-push` and in the CI job
"Flutter Lint & Test". CI also pins Flutter (3.44.6) and runs format, analyze and test.
Goldens are not used, so the golden regeneration workflow is not needed yet.
*Still open:* the backend to frontend route parity check; port it when the academy routes
arrive. The baseline still holds existing violations; it is lowered as screens migrate to
the shared widget layer.

**W-15 `P3` Branch protection and repository security settings are unknown.**
`main` must be fast-forward only after CI, per `CLAUDE.md`, but only a repository ruleset
can enforce that. The repo is public.
*Fix:* apply the ruleset in Appendix E; enable secret scanning with push protection,
Dependabot alerts and code scanning.

**W-16 `P3` Observation: OTP hashing.**
`otp.HashToken` is an unsalted SHA-256. For a short numeric code that is trivially
reversible if the store is read. saas-core encrypted OTPs with AES-GCM. Consider an HMAC
keyed with a server secret, and confirm that verify attempts are rate limited
(login lockout exists; OTP-verify attempt limiting was not checked).

**W-17 `P3` Observation: `staging` needs an OTP strategy.**
saas-core ran staging with `APP_ENV=local` so tests could read `dev_otp`. Once W-02 is
fixed, staging E2E needs either a dedicated `test` environment value with its own guard or
a captured-mail sink. Decide this in an ADR before building staging.

### 7.2 Summary matrix

| # | Finding | Sev | Blocks |
|---|---|---|---|
| W-01 (done) | Fail-open production config | P1 | Any deploy |
| W-02 (done) | OTP codes in logs without real sender | P1 | Any deploy |
| W-03 (done) | No SHA citation check in CI | P1 | Trust in docs |
| W-04 (done) | No `--check-env` | P1 | CD pre-flight |
| W-05 (done) | No real-DB tests | P1 | Academy Phase 1 |
| W-06 (done) | Empty contract/E2E suites report green | P2 | Release gate |
| W-07 | Unpinned supply chain | P2 | First publish |
| W-08 | Probe fallback to HTTP | P2 | First deploy |
| W-09 | Redis password on command line | P2 | First deploy |
| W-10 | Cert permissions and lifetime | P2 | First deploy |
| W-11 | 24h tokens, no logout, revocation unused | P2 | Suspension (R7) |
| W-12 | Error semantics hide outages | P2 | Ops |
| W-13 | No docs gate, placeholder docgen | P2 | Academy Phase 1 |
| W-14 (gate done) | Thin frontend gates (route parity check still open) | P2 | Frontend growth |
| W-15 | Ruleset/security settings unknown | P3 | Merge discipline |
| W-16 | OTP hash choice | P3 | Hardening |
| W-17 | Staging OTP strategy | P3 | Staging |

---

## 8. Control checklist (reusable for any new project)

Use this as the acceptance sheet for a new repo. The two status columns are the review
result; the last column is the target.

| Control | saas-core | wael-app | Target |
|---|---|---|---|
| **Governance** | | | |
| Single agent contract file plus pointer files | Yes | Yes | Yes |
| Small structured current-state file with size budget | Partial (437 KB) | Partial (5 KB, unstructured) | Yes |
| ADR template and index | Yes (27) | Yes (8) | Yes |
| Categorized changelog with verified SHAs | Yes | No | Yes |
| SHA citation check locally | Yes | Yes | Yes |
| SHA citation check in CI | Yes | Yes | Yes |
| Branch ruleset (required checks, linear history, no force push) | Unknown | Unknown | Yes |
| CODEOWNERS | No | No | Yes |
| Dependabot or Renovate | No | No | Yes |
| Secret scanning in CI (full history) | No | Yes | Yes |
| **Local gate** | | | |
| Tracked hooks, `make setup` | Yes | Yes | Yes |
| `make push` with `PUSH_VERIFIED` | Yes | Yes | Yes |
| Tool versions pinned and drift-guarded | Partial | Partial (gosec yes, govulncheck no) | Yes |
| Hook and CI share one script | No | No | Yes |
| Docs drift gate | Yes | No | Yes |
| Route parity gate | Yes | No | Yes |
| Frontend composition gate | Yes | Yes | Yes |
| **CI** | | | |
| Least-privilege `permissions:` | No | Yes | Yes |
| Actions pinned by SHA | No | No | Yes |
| Per-module matrix build/vet/test | Yes | Yes | Yes |
| Mongo and Redis service containers | Yes | Yes | Yes |
| `govulncheck` and `gosec` | Yes | Yes | Yes |
| Real contract tests | Yes | **No (skeleton)** | Yes |
| E2E on a production-image stack | Yes | No | Yes |
| Skips fail in CI | No | Yes for e2e | Yes |
| Publish gated on CI and E2E | No | n/a (disabled) | Yes |
| **Build** | | | |
| Multi-stage, non-root, static binary | Yes | Yes | Yes |
| Base images pinned by digest | No | No | Yes |
| Image scan, signing, SBOM | No | No | Yes |
| Immutable commit-SHA tags, no `latest` in deploy | Partial | Planned | Yes |
| No secrets in image or build context | Yes | Yes (`.dockerignore`) | Yes |
| **Runtime** | | | |
| mTLS between services | Yes | Partial (optional) | Yes, required outside local |
| Fail-fast config, refuse mocks in production | Yes | Yes | Yes |
| `--check-env` | Yes | Yes | Yes |
| Gateway header hygiene and trusted proxies | Yes | Yes | Yes |
| Rate limiting fails closed | Yes | Yes (wrong status code) | Yes |
| Token revocation wired to real flows | Partial | Partial | Yes |
| Log redaction and CR/LF sanitization | Yes | Yes | Yes |
| Encryption at rest for sensitive files | Yes | Planned | Yes |
| Strict health probes (no HTTP fallback) | No | No | Yes |
| Ops actions as CLI, no admin HTTP surface | Yes | Planned (ADR-0008) | Yes |
| **Deploy and operate** | | | |
| Separate deploy repo, pull-only runner | Yes | Planned | Yes |
| Pre-flight env validation before any change | Partial (S-02) | No | Yes |
| Health-gated rollback | Yes (S-03 caveats) | No | Yes |
| Staging parity plus release gate | Yes | No | Yes |
| Migration framework tied to rollback | No | No | Yes |
| Scheduled backups plus restore drill | No | No | Yes |
| Secret manager | No | No | Yes |
| Certificate rotation and expiry alert | No | No | Yes |
| Runbook and deployment guide | Yes | No | Yes |

---

## 9. Backlog for wael-app (ordered)

Each item is one commit or one small PR. Do not start academy Phase 1 before Phase A is
done, because the academy service will copy whatever the existing services do.

### Phase A: foundations (before more feature code)

1. **W-03** (done) Add the Markdown SHA step to `ci.yml`. *Done when:* a deliberately bad hash on a
   throwaway branch fails the job.
2. **W-01** (done) Strict `Load()` for gateway, auth, notification with a shared helper for the
   `APP_ENV` allow-list. *Done when:* table tests cover every missing variable per
   non-local environment.
3. **W-02** (done) Production refusal for the log-only sender and the local-with-telemetry guard.
   *Done when:* both refusals have tests.
4. **W-04** (done) `--check-env` in all three services (and in every new service by template).
   *Done when:* each exits 1 on a missing required variable and 0 on a valid environment.
5. **W-05** (done) Service containers in CI, `REQUIRE_DB=1`, first Mongo store tests for auth
   (refresh single-redemption under `-race`). *Done when:* CI fails if Mongo is
   unreachable.
6. **W-06** (done) Real contract tests and `E2E_REQUIRED=1` in CI. *Done when:* CI shows a
   non-zero executed test count for `tests/contracts` and `tests/e2e`.
7. **W-15** Ruleset, secret scanning push protection, Dependabot alerts (settings, not
   code; record the applied settings in `docs/adr/` or `docs/REPOSITORY-SETTINGS.md`).

### Phase B: before the first deployment

8. **W-07** Pin actions, `govulncheck`, base image digests; Dependabot; CODEOWNERS.
9. **W-08, W-09, W-10** Strict probes, Redis config file, cert script with 640 keys,
   fixed UID, expiry check.
10. **W-11, W-12** Short access tokens plus logout; correct error semantics and timeouts.
11. **W-13** Real `docgen` (endpoint table from `RegisterRoutes`), `make docs-check`,
    `docs/changelog/`, restructured `AI_CONTEXT.md`.
12. Staging stack on production images with a Caddy edge that verifies the local CA
    (SPEC 8.5), and its OTP strategy ADR (W-17).
13. Deploy repo, pull-only runner, pre-flight ordering fixed (S-02), rollback that uses
    recorded image tags (S-03), then re-enable `build-and-publish.yml` gated on CI and E2E
    (S-01).
14. RUNBOOK and DEPLOYMENT, including backup, restore drill and secret handling.

### Phase C: hardening

15. **W-14** Frontend composition gate and parity check as the widget layer and academy
    routes appear.
16. **W-16** HMAC for stored OTP hashes; confirm verify-attempt limiting.
17. Image scan, cosign signing, SBOM; secret manager; certificate rotation job; migration
    framework.

---

## 10. Starting a third project from zero

| When | Deliver |
|---|---|
| Day 0 | Repo, `go.work` with pinned versions, `Makefile`, `.githooks/pre-push`, `CLAUDE.md` plus pointer files, `AI_CONTEXT.md` skeleton, ADR template, `ci.yml` with drift guard, SHA check, `permissions: contents: read`, pinned actions, gitleaks; ruleset applied |
| Week 1 | `shared/infra` (strict config helper, jwt, ratelimit, redact, tlsutil, handlerutil), Dockerfile template (non-root, strict probe, digest-pinned), local mTLS compose, `--check-env` in the service template |
| Week 2 | Real contract tests, staging stack, E2E with `E2E_REQUIRED`, security regression suite, `docgen` and `docs-check` |
| Week 3 | Deploy repo, runner, pre-flight, rollback, backups with restore drill, RUNBOOK, DEPLOYMENT |
| Always | An ADR the day a significant decision is made; a changelog entry with a verified hash for every security fix |

---

## Appendix A: repository skeleton

```
.githooks/pre-push            # thin wrapper: exec scripts/gate.sh
.github/
  workflows/ci.yml
  workflows/build-and-publish.yml      # main only, needs: ci + e2e
  workflows/release-gate-e2e.yml
  dependabot.yml
  CODEOWNERS
CLAUDE.md  AGENTS.md  AI_CONTEXT.md  README.md  Makefile  go.work
docs/{adr,changelog,architecture,frontend}/  RUNBOOK.md  DEPLOYMENT.md  RELEASE-GATE.md
infrastructure/{docker-compose.yml,staging/,deploy/,certs/generate-certs.sh,.env.example}
scripts/{gate.sh,staging_up.sh,staging_down.sh,frontend_composition_gate.sh}
services/<svc>/{Dockerfile,.dockerignore,cmd/main.go,internal/{config,handlers,models,store}}
shared/infra/{jwtutil,ratelimit,handlerutil,redact,resilience,tlsutil,storage,docgen,config}
tests/{contracts,e2e}
tools/{docgen,paritycheck}
```

## Appendix B: strict configuration pattern

```go
func Load() (*Config, error) {
    env := strings.ToLower(os.Getenv("APP_ENV"))
    switch env {
    case "local", "test", "staging", "production":
    default:
        return nil, fmt.Errorf("config: APP_ENV %q is not one of local|test|staging|production", env)
    }
    dev := env == "local" || env == "test"

    must := func(k string) (string, error) {
        v := os.Getenv(k)
        if v == "" {
            return "", fmt.Errorf("config: required env var %s is empty", k)
        }
        return v, nil
    }
    // Always required
    // JWT_SECRET, GATEWAY_SECRET, INTERNAL_SERVICE_TOKEN via must(...)

    // Required outside dev: MONGO_URI, REDIS_URI, TLS_CERT_PATH, TLS_KEY_PATH, TLS_CA_PATH
    // and a real mail sender when the service sends codes.
    if !dev {
        // for each key: must(k)
    }
    // Never default APP_ENV to production and then make production behavior optional.
    // ...
}
```

Rules: no silent fallback to memory stores or plain HTTP outside `local`/`test`;
`--check-env` calls this exact function; every secret comparison rejects an empty
configured secret and uses a constant-time compare.

## Appendix C: hardened production Dockerfile stage

```dockerfile
FROM alpine:3.20@sha256:<pin-the-digest> AS prod
RUN apk --no-cache add ca-certificates curl \
 && addgroup -S -g 10001 app && adduser -S -u 10001 -G app app
COPY --from=build /bin/service /bin/service
USER 10001:10001
EXPOSE 3002
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS --cacert /app/certs/ca.crt \
      --cert /app/certs/auth-service.crt --key /app/certs/auth-service.key \
      https://localhost:3002/health || exit 1
ENTRYPOINT ["/bin/service"]
```

Notes: no HTTP or `-k` fallback; a fixed UID/GID lets key files stay at `640` owned by the
same group; create and chown any writable data directory in the image.

## Appendix D: CI snippets

Top of `ci.yml`:

```yaml
permissions:
  contents: read
```

Pin actions by commit SHA (look up the real SHA when applying; do not guess):

```yaml
- uses: actions/checkout@<full-commit-sha>   # v4.x.y
```

Markdown citation check (same logic as the hook), inside `lint-formatting`:

```yaml
- name: Verify Markdown Commit SHAs
  run: |
    FAILED=0
    for file in $(find . -type f -name "*.md" -not -path "*/node_modules/*" -not -path "*/.git/*"); do
      for sha in $(grep -oE "[0-9a-f]{40}" "$file" || true); do
        git cat-file -e "$sha^{commit}" 2>/dev/null || { echo "BLOCKED: unknown SHA $sha in $file"; FAILED=1; continue; }
        git merge-base --is-ancestor "$sha" HEAD 2>/dev/null || { echo "BLOCKED: unreachable SHA $sha in $file"; FAILED=1; }
      done
    done
    exit $FAILED
```

Service containers for `build-test`:

```yaml
services:
  mongo:
    image: mongo:7
    ports: ["27017:27017"]
    options: >-
      --health-cmd "mongosh --eval 'db.runCommand({ping: 1})'"
      --health-interval 10s --health-timeout 5s --health-retries 5
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    options: >-
      --health-cmd "redis-cli ping" --health-interval 10s --health-timeout 5s --health-retries 5
```

with `env: { MONGO_URI: mongodb://localhost:27017, REDIS_URI: redis://localhost:6379, REQUIRE_DB: "1" }`.

Turning skips into failures (Go test helper):

```go
func requireOrSkip(t *testing.T, reason string) {
    t.Helper()
    if os.Getenv("E2E_REQUIRED") == "1" {
        t.Fatalf("required suite cannot run: %s", reason)
    }
    t.Skip(reason)
}
```

Gate ordering for publishing: the image build job must `needs:` the CI and E2E jobs (use
one workflow, or `workflow_run` with a success condition), never run beside them.

## Appendix E: repository ruleset (settings to apply)

- Target `main`: require pull request or fast-forward-only flow, require status checks
  (`Lint & Formatting`, every `Build & Test` matrix entry, `Security Scan`,
  `Secret Scan`, `Flutter Lint & Test`, the E2E job once it exists), require linear
  history, block force pushes, block deletion, require branches to be up to date.
- Target the development branch: block force pushes and deletion; require the same checks.
- Restrict who can edit `.github/workflows/` (CODEOWNERS plus required review).
- Enable secret scanning with push protection, Dependabot alerts and security updates,
  code scanning.
- Environments: a `production` environment with required reviewers for any workflow that
  touches the deploy repo.

## Appendix F: templates

ADR (as used in both repos):

```markdown
# ADR-NNNN: <Title>
- **Status**: Proposed | Accepted | Deprecated | Superseded
- **Date**: YYYY-MM-DD
- **Related Commit SHA**: <verified sha or "none (decision only)">
- **Related finding**: <id or n/a>

## Context
## Decision
## Consequences
## Alternatives Considered
```

Changelog entry (file chosen by kind):

```markdown
### <Short title>
- **Date**: YYYY-MM-DD
- **Commit SHA**: <written only after the commit exists, verified with git cat-file>
- **Summary**: <what changed and why>
- **Verification**: <commands run, and what was not verified>
```

## Appendix G: one gate, two callers

Put the checks in `scripts/gate.sh` and call it from `.githooks/pre-push` and from CI job
steps. Today the hook and `ci.yml` re-implement the same drift and SHA logic twice, which
is where the S-12 style divergences come from. Keep tool versions in one file
(`scripts/versions.env`) that the script, the workflow and the Dockerfile linter all read.

## Appendix H: `AI_CONTEXT.md` shape and budget

Fixed sections: **Current state** (10 lines), **Done** (bulleted, dated, one line each),
**Open** (owner decisions pending), **Decisions** (ADR index), **Next task** (one task).
History goes to `docs/changelog/`, not here. Add a CI check that fails when the file
exceeds an agreed size (for example 16 KB).

## Appendix I: pull-request definition of done

- Gates for every touched module ran, with `-race` for concurrency, tokens or stores.
- `gosec` and `govulncheck` clean for touched modules.
- New or changed endpoint: docs regenerated (`make docs`) and `make docs-check` clean.
- New secret or environment variable: `.env.example`, `--check-env`, compose files and the
  secrets inventory updated in the same change.
- New security-relevant decision: ADR added. Security fix: changelog entry with a verified
  hash written after the commit.
- `AI_CONTEXT.md` updated if project state changed.
- Report lists changed files, gates run, deviations and anything not verified.

## Appendix J: secrets inventory template

| Secret | Purpose | Generated by | Stored in | Rotated | Consumers |
|---|---|---|---|---|---|
| `JWT_SECRET` | access token signing | `openssl rand -hex 32` | secret manager | every N days | auth, notification |
| `GATEWAY_SECRET` | gateway to service trust | `openssl rand -hex 32` | secret manager | every N days | gateway, auth, notification |
| `INTERNAL_SERVICE_TOKEN` | service to service | `openssl rand -hex 32` | secret manager | every N days | auth, notification |
| `MONGO_INITDB_ROOT_PASSWORD` | DB root | `openssl rand -hex 24` | secret manager | on staff change | mongo |
| `REDIS_PASSWORD` | Redis auth | `openssl rand -hex 24` | secret manager | on staff change | redis |
| `RESEND_API_KEY` | mail delivery | provider dashboard | secret manager | on staff change | auth |
| GitHub App private key | cross-repo pushes | GitHub | Actions secret | yearly | workflows |

The dev-only default `devpassword123` in `infrastructure/.env.example` must never appear in
any staging or production file; enforce it with a `--check-env` rule that rejects that
value when `APP_ENV` is not `local`.

## Appendix K: Verification against develop

Examined commit: `46c7997...`

| ID | Status | Evidence | Correction |
|---|---|---|---|
| W-01 | Fixed in 0f6c02b + e1e8479 | `services/*/internal/config/config.go`, `services/*/cmd/main.go` | Outside local/test, Load() requires Mongo, Redis and the TLS triple (auth also Resend); main.go refuses memory stores, LogSender, plain HTTP and TLS without a client CA. e1e8479 restores REDIS_URI as required in every APP_ENV for the gateway (0f6c02b had made it optional in local/test). |
| W-02 | Fixed in 0f6c02b | `services/auth-service/internal/config/config.go`, `services/auth-service/cmd/main.go` | RESEND_API_KEY and RESEND_FROM_EMAIL are required outside local/test; LogSender only in local/test. The local-with-telemetry guard is not applicable: the repo has no telemetry variable. |
| W-03 | Fixed in 36033d7 | .github/workflows/ci.yml:37-54 | none |
| W-04 | Fixed | `services/*/cmd/main.go`, `services/*/cmd/checkenv_test.go` | `--check-env` flag added to gateway, auth, and notification services; validates config via Load() and exits 0/1 without starting resources. |
| W-05 | Fixed | `.github/workflows/ci.yml:105-121`, `services/auth-service/internal/{store,otp}/*_test.go`, `services/notification-service/internal/store/*_test.go` | Mongo and Redis service containers added to build-test job with health checks and `REQUIRE_DB=1`; dual-implementation store test suites run against MemoryStore and MongoStore/RedisStore. |
| W-06 | Fixed (dev stack) | `tests/contracts/contracts_test.go:20-159`, `tests/e2e/chain_test.go`, `.github/workflows/ci.yml` | Contract tests active in build-test matrix; tests/e2e split into sequential t.Run stages with E2E_REQUIRED=1 fail-closed mode and executed against live compose stack in CI job "E2E (compose)". |
| W-07 | Confirmed | `.github/workflows/ci.yml:19,24,146,189`, `.githooks/pre-push:125`, `services/*/Dockerfile:11,28,41` | none |
| W-08 | Confirmed | `services/*/Dockerfile:50-51,45-46`, `infrastructure/docker-compose.yml:98` | none |
| W-09 | Confirmed | `infrastructure/docker-compose.yml:24,45,50` | none |
| W-10 | Confirmed | `infrastructure/certs/generate-certs.sh:11,31,37,39,40` | none |
| W-11 | Confirmed | `shared/infra/jwtutil/jwt.go:142`, `services/auth-service/cmd/main.go:74-81`, `grep -rn "RevokeToken\|RevokeAllUserTokens" services/` (0 matches) | none |
| W-12 | Confirmed | `services/auth-service/internal/handlers/auth.go:136-139`, `services/api-gateway/internal/middleware/limiter.go:47` | none |
| W-13 | Confirmed | `tools/docgen/main.go:8`, `Makefile:1-51` (no docs target), absence of `docs/changelog/` | none |
| W-14 | Fixed in 06f0612 (gate); route parity check still open | `scripts/frontend_composition_gate.sh`, `scripts/frontend_gate_baseline.txt`, `.githooks/pre-push:26-30`, `.github/workflows/ci.yml` (step "Frontend Composition Gate") | Composition gate ported with a ratchet baseline and run in the pre-push hook and in CI job "Flutter Lint & Test". The route parity script does not exist yet. |
| W-15 | Unknown (not visible from the repo) | GitHub repository rulesets and branch protection settings cannot be inspected from local git clone | none |
| W-16 | Confirmed | `services/auth-service/internal/otp/otp.go:31-34`, `services/auth-service/internal/handlers/auth.go:174-178,337-341` | none |
| W-17 | Confirmed | `services/auth-service/internal/config/config.go:64-66` (staging not allowed; no staging OTP strategy exists) | none |

### Not verified
- W-15: GitHub repository rulesets, branch protection rules, and remote security settings (not visible from local repo).
- Real database container execution for MongoDB and Redis (unverified locally without live compose stack; `tests/e2e` skipped).
- Remote GitHub Actions execution environment behaviors (workflow runs in GitHub Actions runners; validated locally via pre-push gate).
