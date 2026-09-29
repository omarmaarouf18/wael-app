# wael-app

Fresh monorepo built from reference structure/tooling only (no domain logic copied).

- Services: `services/api-gateway`, `services/auth-service`, `services/notification-service`
- Shared: `shared/infra` (`jwtutil, ratelimit, handlerutil, redact, resilience, tlsutil`)
- Tests: `tests/contracts`, `tests/e2e`
- Tools: `tools/docgen` (`paritycheck` deferred until backend↔Flutter contracts exist)
- Go: pinned `1.26` / toolchain `go1.26.6` via `go.work`
- Local infra, hooks, CI, and docs land in follow-up steps (see build order).
