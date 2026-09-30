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
identity and console boundaries) written and accepted.
Owner review recorded (2026-09-30, docs only, nothing implemented): SPEC
Section 2 D15-D19, Section 3 questions 12-16, Phase 0.0 public-repo
hygiene, Section 15 review notes (ADR numbering after 0010; leak response:
replace the video, change `youtube_video_id`, RUNBOOK to carry it when
written). ADR-0001 leak-response note added; ADR-0003 amended (manual
payment via InstaPay or e-wallets, admin activation).

## Open
Core academy service implementation (build contract: `docs/core-service/SPEC.md`; Phase 0 prerequisites first). Rebind providers to `AcademyRepository`. Deploy repo.
RUNBOOK, DEPLOYMENT, changelog (ADRs now exist).

## Decisions
Product decisions recorded in `docs/adr/`:
- [ADR-0001: Lesson Videos Hosted on YouTube](docs/adr/0001-youtube-video-hosting.md)
- [ADR-0002: Single-Role Student App With a Separate Web Admin Panel](docs/adr/0002-single-role-student-app.md)
- [ADR-0003: Payment Per Subject (Materia)](docs/adr/0003-per-subject-payment.md)
- [ADR-0004: No Limits on Video Access](docs/adr/0004-unlimited-video-access.md)
- [ADR-0005: Study Notes (Mozakkerat) Delivered Inside the App](docs/adr/0005-in-app-study-notes.md)
- [ADR-0006: Support Only Through a WhatsApp Number](docs/adr/0006-whatsapp-only-support.md)
- [ADR-0007: Core Academy Service Design](docs/adr/0007-core-academy-service.md) (Status Proposed; revised: catalog tree, PDFs, admin tokens)
- [ADR-0008: Admin Identity and Console Boundaries](docs/adr/0008-admin-identity.md) (Status Accepted; admin subdomain, thin proxy, admins collection, CLI lifecycle, verification without caching)

## Next task
Continue core academy work from `docs/core-service/SPEC.md`: Phase 0.4,
write ADR-0009 and restore `shared/infra/storage` with its tests. Complete
the remaining Phase 0 prerequisites before Phase 1.
