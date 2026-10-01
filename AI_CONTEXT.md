# AI_CONTEXT — wael-app

## Current state
Monorepo (`github.com/omarmaarouf18/wael-app`, public): api-gateway,
auth-service (single role `user`, no tenants), notification-service (SSE,
list, mark-read, internal push), academy-service (skeleton), shared/infra,
Flutter app with real gateway auth, local compose (mongo:7, redis:7, mTLS),
pre-push hook, CI. Branch: `develop` (work), `main` (fast-forward merges after CI).

## Done
Skeleton, shared/infra, gateway+auth, notifications, Flutter wiring+rename,
compose+certs, hooks+Makefile, CI (publish disabled). Refresh tokens consume
atomically (single redemption); signup takes no client role (always user).
Gateway chain covered by env-gated tests/e2e. gosec pinned v2.29.0 in
hook and CI with drift check. README matches the repo. ADR-0002/0003/0004/0005
amended (2026-09-29) per ADR-0007. No domain logic beyond
auth/notifications/academy mocks. Core Phase 0.1: gateway strips client
`X-Internal-Token` and injects none; it no longer requires or receives
`INTERNAL_SERVICE_TOKEN`. Core Phase 0.2: corrected ADR-0007 per
`docs/core-service/SPEC.md` Section 14. Core Phase 0.3: ADR-0008 (admin
identity and console boundaries) written and accepted. Core Phase 0.0:
public-repo hygiene — gitleaks CI job (pinned v8.30.1, full history) with
`.gitleaksignore` for three known test-fixture findings, top-level
`permissions: contents: read` in ci.yml, extended `.gitignore` (signing
keys/keystores, credential JSONs, logs/databases, Flutter coverage), and
`docs/asset-provenance.md` (all rows UNCONFIRMED until the owner records
sources; `placeholder_course.png` is referenced but not committed). Core
Phase 0.6: added AGENTS.md pointer ("Follow CLAUDE.md verbatim") and updated
CLAUDE.md (no-illustrative-output, proactive commit disclosure). Core
Phase 0.4: ADR-0009 (file storage: local encrypted storage at rest with
AES-256-GCM, server-generated canonical UUID keys, os.Root containment with
TOCTOU single-writer assumption, atomic no-overwrite uploads via temp files and
hard links, version byte header and AAD binding to key, startup sweep of stale
temp files, streaming via OpenFile with per-call entitlement checks, no signed URLs,
and fail-closed DOCUMENT_ENCRYPTION_KEY policy) written (Status Proposed);
`shared/infra/storage` implemented and verified with exhaustive key/env matrix,
atomic upload, os.Root symlink containment, version header, temp sweep, and
cryptographic tamper tests; real contract tests added to `tests/contracts`;
APP_ENV allowlist enforced in auth and notification services.
Owner review recorded (2026-09-30, docs only, nothing implemented): SPEC
Section 2 D15-D19, Section 3 questions 12-16, Phase 0.0 public-repo
hygiene, Section 15 review notes (ADR numbering after 0010; leak response:
replace the video, change `youtube_video_id`, RUNBOOK to carry it when
written). ADR-0001 leak-response note added; ADR-0003 amended (manual
payment via InstaPay or e-wallets, admin activation). Section 15 corrected
same day: ADR-0007 reserves ADR-0008-0010.
Owner decisions recorded (2026-09-30, docs only, nothing implemented):
diplomas are admin-created under fixed diploma study type (server-generated
key, delete blocked while subjects exist, levels without published subjects
hidden from student listing); vocational training is one fixed level with
empty-term subjects (frontend hides term filter); open question 1 resolved
(Phase 2.2 seed contains bachelor years 1-4 and vocational level only);
Phase 4 gains diploma CRUD admin endpoints task before subject CRUD;
SPEC.md Sections 1, 3, 5, 11 and ADR-0007 amended. Notification bundled
fallback restricted to debug mode with empty state in release; live
notification deduplication added. User-facing messages routed through
ErrorMessages with complete sanitization (no raw exception text). Debug
diagnostics screen added under frontend/lib/debug/ (base URL, masked session,
SSE state, last 50 calls without bodies or query strings, absent in release
mode). End-to-end behavior matrix executed against real compose stack with 24
verified scenarios; docs/frontend/BEHAVIOR.md recorded with raw captured
outputs (masked), backend findings, and fail-closed audit evidence.
Lowest-friction Linux desktop target documented in frontend/README.md.
Race detector: `go test -race` is now the test gate in `.githooks/pre-push`
and CI for every Go module (owner-approved 2026-09-30). A pre-existing
data race in TestStream_BearerAndQueryToken (from 2a3b95d, invisible
because gates ran without -race) was fixed with a mutex-guarded test
recorder (e276fc3); no production code changed.
Owner amendment (2026-09-30, pushing): CLAUDE.md now carries the Auto-push
rule (auto-push `develop` via `make push` only when every gate and `make ci`
passed with output shown, tree clean, a secret scan of the range is clean,
no suspension/gating/admin-auth changes, pushing non-authored commits only
after gating that HEAD yourself, and `git rev-parse HEAD` equals
`git ls-remote origin develop`) and a Session start check (clean tree, no
second agent session in this directory, every queued commit accounted for).
SPEC Section 12 rule 5 now points to CLAUDE.md Auto-push.
Hardening Phase A (W-03): verified Markdown commit citations in CI and aligned pre-push hook to scan git-tracked markdown files with git ls-files.
Hardening Phase A (W-01, W-02): fail-closed configuration outside local/test across api-gateway, auth-service, and notification-service (requiring Mongo, Redis, TLS triple, and Resend mail credentials in production; refusing memory stores, LogSender, plain HTTP, and TLS without client CA outside dev; table test coverage for all required vars; startup logs disclosing active sender and store types).
Hardening Phase A (W-04): added --check-env flag to api-gateway, auth-service, and notification-service (validates configuration via config.Load() and exits 0/1 without starting resources; test coverage under cmd/checkenv_test.go in each service).
Hardening Phase A (W-05): real Mongo/Redis tests in CI via service containers (mongo:7, redis:7-alpine) and dual-implementation store test suites (MemoryStore and MongoStore for auth and notification services, atomic single-redemption for auth OTP with RedisStore and MemoryStore under -race, requireDB helper enforcing REQUIRE_DB=1 in CI).
Hardening Phase A / Core Phase 0.5 (W-06): split tests/e2e into sequential t.Run stages with E2E_REQUIRED fail-closed flag and requireOrSkip; added compose-backed CI job "E2E (compose)" executing full e2e chain with fresh ephemeral secrets.
Core Phase 1.1: added full_name, phone, status (active/suspended/deleted), status_reason, suspended_at, reactivated_at, deleted_at to models.User with EffectiveStatus() helper; implemented compare-and-set SetStatus and non-status Update semantics across MemoryStore and MongoStore preventing lost-update bug P-1; verified with full unit and store test suites under -race.
Core Phase 1.2: signup requires full_name (2-100 runes) and phone normalized to E.164 (github.com/nyaruka/phonenumbers v1.8.1 with DEFAULT_PHONE_REGION defaulting to EG); partial unique phone index on active/suspended accounts (P-6) across MongoStore and MemoryStore; HMAC-SHA256 blocklist checks on email and phone (R9, D15) with uniform generic refusal (P-5); fail-closed store error propagation returning 503 instead of 409/401 (P-3) and per-query timeouts on DB calls (P-4); BLOCKLIST_HMAC_KEY enforced outside dev in config.Load(), check-env, and .env.example; SetStatus rejects invalid target status with ErrInvalidStatus; Create explicitly stores active status.
Core Phase 1.3: status gate on Login, Refresh, VerifyOTP refusing non-active accounts (suspended, deleted) with uniform 401 generic error ("unauthorized", code "unauthorized", no oracle); Login checks status only after password verification (wrong password counts toward lockout and returns normal 401); Refresh resolves user without deleting key prior to status check; fail-closed 503 on store errors across Refresh, VerifyOTP, and Me; blocklistKey outside local/test returns 503 when empty; table tests across all three paths for active, legacy-empty, suspended, and deleted across MemoryStore and MongoStore; refresh refusal for tokens issued before suspension verified.
Frontend auth refresh: `_doRefresh` logs out only on 401/403 (invalid/revoked token or suspended account); preserves session and tokens on 5xx, timeout, or network errors; tested across status codes and network failure modes.
Core Phase 1.4: `admins` collection managed by `auth-service` with `models.Admin` and `IsActive(now)` method, unique index on `token_hash`; server-side CLIs `onboard-admin` (CSPRNG base64url >=32 bytes, max TTL 365d, prints token once to stdout, stores SHA-256 hash) and `revoke-admin` (sets `revoked_at`, idempotent); second internal listener (`ADMIN_LISTEN_ADDR`, default `:9001`, required outside local/test, not published in compose); `POST /internal/admin/verify` served ONLY on admin listener; client IP extracted from `X-Admin-Client-IP` ONLY after constant-time `X-Internal-Token` validation (falling back to connection `RemoteAddr` host, never reading `X-Forwarded-For`); lockout protection reusing existing `Lockout` interface and thresholds with separate key prefixes `admin-verify-ip:` and `admin-verify-tok:<hash>`; uniform 401 refusal for unknown, expired, and revoked tokens; 503 on store errors; gateway isolation contract (`TestContract_GatewayHasNoInternalRoute`) and compose port isolation contract (`TestContract_AdminPortNotPublishedInCompose`); dated amendments added to SPEC D16 and ADR-0008.
Core Phase 1.5: `auth-service` admin account endpoints served ONLY on internal admin listener (`/internal/admin/*`) authenticated with `X-Internal-Token` and `X-Admin-Token` via shared `authenticateAdmin` logic (lockout on IP and token hash): `GET /internal/admin/accounts?search=&status=&page=&limit=` (search by name, email, phone normalized like signup, or exact ID; search input capped at 100 runes; regex escaping with `regexp.QuoteMeta`; pagination with limit capped at 100; validated status filter; sanitized `UserDTO` never leaking password hash, OTP, or reset fields); `POST /internal/admin/accounts/{id}/suspend` {reason 1-1000} (order: RevokeAllUserTokens, CAS SetStatus active->suspended; same-state returns 409; student notification via notification-service push; CR/LF stripped from reason; audit log recorded); `POST /internal/admin/accounts/{id}/reactivate` (CAS SetStatus suspended->active; 409 if active or deleted; student notification; audit log recorded); `DELETE /internal/admin/accounts/{id}` {reason 1-1000} (order: HMAC blocklist entries for normalized email and phone via shared normalizeEmail, RevokeAllUserTokens, CAS SetStatus via store.FromActiveOrSuspended active|suspended->deleted; 409 if already deleted; prevents re-registration with blocked email or phone; student notification; audit log recorded); `GET /internal/admin/audit-log?page=&limit=` (paginated newest first; Section 5 schema without IP addresses); per-service `admin_audit_log` with compound indexes on (`actor_id`, `created_at`) and (`target_type`, `target_id`) in MemoryStore and MongoStore; resilient audit failure handling returning success with error logged with IDs only; dated amendments in SPEC Section 5/6 and ADR-0008.

Core Phase 1.6: notification-service stream caps: per-account concurrent stream cap (`STREAM_MAX_CONCURRENT`, default 3) with newest wins, oldest evicted policy (evicting oldest stream context without 429) and leak-free release on every exit path (defer with `sync.Once`); dead-connection detection in `Stream` via write/flush error checking and 10s write deadlines (`http.NewResponseController.SetWriteDeadline`); per-account connection open rate limit (`STREAM_OPEN_RATE_LIMIT`, default 10/min) keyed on JWT user id with 429 and `Retry-After` header; in-process memory tracking with zero slot leaks under concurrency, eviction, and context cancellation; limits added to config `Load()`, check-env, and `.env.example`.

SPEC Phase 2.1: `academy-service` skeleton (`github.com/omarmaarouf18/wael-app/academy-service` Go module on Go 1.26 / toolchain go1.26.6, added to `go.work`); config `Load()` with allowlist-based `APP_ENV` and table tests; `--check-env` flag and test suite; `/health` on public listener behind `GatewayAuth` (`X-Gateway-Secret`); internal admin listener (`ADMIN_LISTEN_ADDR` defaulting to `:9002`, not published on host) serving only `/internal/admin/*` behind `X-Internal-Token` (empty 404 for 2.1); `buildServer` enforcing TLS/mTLS parity across public and admin listeners; `Store` interface with `MemoryStore` and `MongoStore` with `EnsureIndexes` (no domain collections invented yet) and `REQUIRE_DB`-gated MongoDB integration tests; docker-compose service definition with mTLS certs and healthcheck; gateway route `/api/v1/academy/` forwarding to `academy-service` with `/api/v1` stripped; contract tests asserting gateway academy route exists, gateway has no `/internal/` route, and admin ports 9001/9002 are not published in compose; added to CI `Build & Test` and `Security Scan` matrices (`Build & Test (services/academy-service, academy-service)` and `Security Scan (services/academy-service, academy-service)`).

SPEC Phase 2.2: `levels` model, unique key index, and idempotent startup seed (bachelor years 1-4 and the vocational level only); `GET /academy/levels` student route behind `GatewayAuth` and `StudentAuth` (Bearer JWT validated with `jwtutil.ValidateToken` on every request against Redis revocation markers and denylist); levels with no published subjects are hidden; `LevelDTO` and `LevelsResponseDTO` response shape; `MemoryStore` and `MongoStore` implementations with `REQUIRE_DB` integration tests verifying seed idempotency, unique key index, and published subject filtering.

SPEC Phase 2.3: `subjects` and `videos` models per SPEC Section 5 (including `access_expires_at` on subjects per decision 18, and `published` flags); student read endpoints `GET /academy/subjects` and `GET /academy/subjects/{id}` returning metadata only behind `GatewayAuth` and `StudentAuth`; strict leak tests ensuring `youtube_video_id` and raw video IDs never appear in student JSON responses (lists, detail, errors); price and currency hidden unless `EXPOSE_PRICE_TO_STUDENTS=true` (D3); unpublished subjects and videos hidden; pagination capped at 20 default and 100 max; compound indexes on `subjects(level_key, status)`, `videos(subject_id, position)`, and `subject_files(subject_id)` in MemoryStore and MongoStore.

SPEC Phase 2.4: gateway route `/api/v1/academy/` with route test was already delivered in Phase 2.1 (commit 9ae2b082c389992cbac88012b47f93d3e3990ade).

Owner decisions recorded (2026-10-01, docs only, nothing implemented): subscriptions expire per subject date set by the admin, copied per activation (D21) with re-purchase allowed and no activation of an already-expired subject (D20); every activation writes an append-only payment record at the subject's price; Android first, iOS deferred. SPEC Sections 1, 2, 3, 5, 7 and ADR-0007 amended.

Frontend F0: `scripts/frontend_composition_gate.sh` ratchet gate (baseline `scripts/frontend_gate_baseline.txt`, 83 entries at F0) runs in `.githooks/pre-push` and the CI `flutter-test` job; `docs/frontend/DESIGN_SYSTEM.md` and `docs/frontend/STATUS.md` added.

Frontend F1 (token gaps): `AppTypography.uppercaseLabel`, semantic colours (success/warning/danger/info with Bg variants, WCAG AA verified by `frontend/test/theme_tokens_test.dart`) and glass/scrim tokens added to `theme.dart`; raw colours and `.toUpperCase()` removed from `lib/widgets/` and the non-catalog screens (catalog screens untouched); the composition gate no longer stops at a rule with zero matches (it was fail-open); baseline lowered from 83 entries (392 violations) to 68 (358).

Frontend F2: the composition gate fails closed (missing `frontend/lib/screens` is exit 2, `Color.fromARGB(`/`Color.fromRGBO(` counted) and is self-tested by `scripts/frontend_gate_test.sh` (run by `make ci` and CI); shared widget layer added under `frontend/lib/widgets/` (`AppShell`, `SecondaryButton`, `ThemedPanel`, `ThemedErrorBanner`, `ThemedEmptyState`, `ThemedLoadingIndicator`, `ThemedSectionHeader`, `ConfirmActionDialog`, `OtpPinInput`) with English and Arabic widget tests and a debug-only component library at `/components` (absent from release builds); no screen migrated yet (F3a).

Frontend F3a: all non-catalog screens (splash, OTP, forgot password, login, signup, notifications, settings, main shell) now build on `AppShell` and the shared widget layer, one commit per screen, with English and Arabic widget tests (`test/screen_harness.dart`); catalog screens (home, courses, course detail, ebook, payment) are untouched and carry the remaining baseline (35 entries, 298 violations). `DashboardScreenTemplate` was deleted and `PrimaryButton.isSecondary` removed. No screen has been run visually yet; the owner's manual Arabic check list is in the F3a report. Known finding: `courses_screen.dart:390` overflows a Row by 22px at 390px in Arabic.

## Open
Core academy service implementation (build contract: `docs/core-service/SPEC.md`; Phase 0 prerequisites first). Rebind providers to `AcademyRepository`. Deploy repo.
RUNBOOK, DEPLOYMENT, changelog (ADRs now exist). Owner to fill provenance
rows in `docs/asset-provenance.md`.
Hardening reference and backlog: docs/BOOTSTRAP-REFERENCE.md.
Owner question: reminder notification before a subscription expires (SPEC Section 3 question 17).

## Decisions
- [ADR-0001: Lesson Videos Hosted on YouTube](docs/adr/0001-youtube-video-hosting.md)
- [ADR-0002: Single-Role Student App With a Separate Web Admin Panel](docs/adr/0002-single-role-student-app.md)
- [ADR-0003: Payment Per Subject (Materia)](docs/adr/0003-per-subject-payment.md)
- [ADR-0004: No Limits on Video Access](docs/adr/0004-unlimited-video-access.md)
- [ADR-0005: Study Notes (Mozakkerat) Delivered Inside the App](docs/adr/0005-in-app-study-notes.md)
- [ADR-0006: Support Only Through a WhatsApp Number](docs/adr/0006-whatsapp-only-support.md)
- [ADR-0007: Core Academy Service Design](docs/adr/0007-core-academy-service.md) (Status Proposed; revised: catalog tree, PDFs, admin tokens; amended 2026-09-30: admin diplomas, vocational training)
- [ADR-0008: Admin Identity and Console Boundaries](docs/adr/0008-admin-identity.md) (Status Accepted; admin subdomain, thin proxy, admins collection, CLI lifecycle, verification without caching; amended 2026-10-01: trusted header, IP extraction, lockout prefixes, listener addr, per-service audit log ownership)
- [ADR-0009: File Storage (Local Encrypted Storage at Rest)](docs/adr/0009-file-storage.md) (Status Proposed; local disk, AES-256-GCM at rest, fail-closed key policy, symlink-proof containment, atomic upload, streaming via academy-service OpenFile, no signed URLs)

## Next task
SPEC Phase 3.1: entitlements store and the owned computation.




