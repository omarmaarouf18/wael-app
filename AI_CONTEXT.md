# AI_CONTEXT — wael-app

## Current state
Monorepo (`github.com/omarmaarouf18/wael-app`, private): api-gateway,
auth-service (single role `user`, no tenants), notification-service (SSE,
list, mark-read, internal push), shared/infra, Flutter app with real gateway
auth, local compose (mongo:7, redis:7, mTLS), pre-push hook, CI. Branch:
`develop` (work), `main` (fast-forward merges after CI).

## Done
Skeleton, shared/infra, gateway+auth, notifications, Flutter wiring+rename,
compose+certs, hooks+Makefile, CI (publish disabled). No domain logic beyond
auth/notifications/academy mocks.

## Open
Docs beyond AI_CONTEXT/CLAUDE (ADRs, RUNBOOK, DEPLOYMENT, changelog).
Core academy service (providers rebind to AcademyRepository). Deploy repo.

## Next task
None queued — confirm direction.
