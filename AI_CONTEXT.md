# AI_CONTEXT — wael-app

## Current state
Monorepo (`github.com/omarmaarouf18/wael-app`, public): api-gateway,
auth-service (single role `user`, no tenants), notification-service (SSE,
list, mark-read, internal push), shared/infra, Flutter app with real gateway
auth, local compose (mongo:7, redis:7, mTLS), pre-push hook, CI. Branch:
`develop` (work), `main` (fast-forward merges after CI).

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

Owner decisions recorded (2026-10-01, docs only, nothing implemented): subscriptions expire per subject date set by the admin, copied per activation (D21) with re-purchase allowed and no activation of an already-expired subject (D20); every activation writes an append-only payment record at the subject's price; Android first, iOS deferred. SPEC Sections 1, 2, 3, 5, 7 and ADR-0007 amended.

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
- [ADR-0008: Admin Identity and Console Boundaries](docs/adr/0008-admin-identity.md) (Status Accepted; admin subdomain, thin proxy, admins collection, CLI lifecycle, verification without caching)
- [ADR-0009: File Storage (Local Encrypted Storage at Rest)](docs/adr/0009-file-storage.md) (Status Proposed; local disk, AES-256-GCM at rest, fail-closed key policy, symlink-proof containment, atomic upload, streaming via academy-service OpenFile, no signed URLs)

## Next task
W-04: add --check-env flag on all services (gateway, auth, notification).
