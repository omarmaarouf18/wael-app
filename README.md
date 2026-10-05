# wael-app

Go monorepo (api-gateway, auth-service, notification-service,
academy-service, admin-console), shared Go libraries, and a Flutter app with
real gateway auth.

- Services: `services/api-gateway`, `services/auth-service`
  (signup/login/OTP/JWT refresh, single `user` role, two-device sessions, and on
  `develop` the self-service account endpoints: sessions, password, profile, email,
  account deletion),
  `services/notification-service` (SSE stream, list, mark-read, internal
  push), `services/academy-service` (catalog, entitlements, access
  requests, admin review and manual grant/revoke, the protected-video play check), `services/admin-console` (thin admin proxy and static pages,
  Go plus Node tests, see its README).
- Shared: `shared/infra` (`jwtutil, ratelimit, handlerutil, redact,
  resilience, tlsutil`).
- Tests: `tests/contracts`, `tests/e2e` (env-gated gateway chain, see
  `tests/e2e/chain_test.go` header).
- Tools: `tools/docgen` (skeleton).
- Frontend: `frontend/` (Flutter; backend URL via
  `--dart-define=API_BASE_URL`, see `frontend/README.md`).
- Go: pinned `1.26.0` / toolchain `go1.26.6` via `go.work` (drift-guarded in
  the hook and CI). *(Owner decision 2026-10-02: canonical Go language line is
  `go 1.26.0`; toolchain stays `go1.26.6`.)*

## Make targets

`setup` (hook install), `ci` (full pre-push gate), `contract-test`, `e2e`,
`commit MSG="..."`, `push`, `since-last-report`, `report-hash`.

## Local stack

See `infrastructure/docker-compose.yml` header: generate local mTLS certs
with `infrastructure/certs/generate-certs.sh`, copy
`infrastructure/.env.example` to `.env.local` with fresh secrets, then
`docker compose --env-file infrastructure/.env.local -f
infrastructure/docker-compose.yml up --build -d` (mongo:7, redis:7).

## Docs & workflow

Decisions live in `docs/adr/` (index: `docs/adr/README.md`). Work happens
on `develop`; merges to `main` go by fast-forward after CI passes
(`CLAUDE.md`, `AI_CONTEXT.md`).
