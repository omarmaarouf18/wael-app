# admin-console

Thin Go proxy plus static pages for operating the academy (SPEC Phase 6.1,
ADR-0008). It serves the admin UI on `admin.<domain>` behind Caddy and forwards
a fixed set of requests to the internal admin listeners. It makes **no
authorization decisions**: auth-service decides on every call.

The design follows the owner's existing reviewer console (decision 2026-10-02),
with the differences recorded in ADR-0008.

## Routes

Only these routes reach an upstream. There is no generic pass-through and no
route under `/internal/`.

| Console route | Upstream (auth-service admin listener) |
|---|---|
| `GET /api/whoami` | `POST /internal/admin/verify`. Returns `{"name"}` only. |
| `GET /api/accounts?search&status&page&limit` | `GET /internal/admin/accounts` |
| `POST /api/accounts/suspend` `{id, reason}` | `POST /internal/admin/accounts/{id}/suspend` `{reason}` |
| `POST /api/accounts/reactivate` `{id}` | `POST /internal/admin/accounts/{id}/reactivate` |
| `POST /api/accounts/delete` `{id, reason}` | `DELETE /internal/admin/accounts/{id}` `{reason}` |
| `GET /api/audit?page&limit` | `GET /internal/admin/audit-log` |

`GET /healthz` is unauthenticated and calls nothing. Everything else is the
embedded static UI (`web/`).

The academy-service routes (requests, catalog, files) and the academy half of
the audit log are added as SPEC Phase 4 lands. Until then `/api/audit` returns
auth-service's log only.

Every handler: requires `X-Admin-Token` (401 with no upstream call when
missing or malformed), validates input (UUID id, reason 1-1000 characters with
control characters replaced by spaces, search at most 100 characters, status
from a fixed list, bounded page and limit), builds the upstream request from
scratch, caps bodies at 1 MiB, and times out after 10 s. A client-supplied
`X-Internal-Token` or `X-Admin-Client-IP` can never reach the upstream:
the console sets `X-Internal-Token` from its environment and
`X-Admin-Client-IP` from the real client address (`X-Forwarded-For` is read
only when the peer is in `TRUSTED_PROXY_IPS`, right-to-left, skipping other
trusted proxies). An upstream failure, timeout, 5xx or unusable body becomes a
safe `503`. Tokens, reasons, queries and bodies are never logged; log lines
carry the route, the validated target id and status codes only.

## Configuration

`config.Load()` (also run by `--check-env`). Only `APP_ENV=local|test` relaxes
anything; an empty `APP_ENV` is production and an unknown value is refused.

| Variable | Required | Notes |
|---|---|---|
| `INTERNAL_SERVICE_TOKEN` | always | Sent as `X-Internal-Token`. Empty or blank fails startup. |
| `AUTH_ADMIN_URL` | outside local/test | `https://auth-service:9001`. Must be `https` outside dev, `scheme://host[:port]` only. |
| `ACADEMY_ADMIN_URL` | outside local/test | `https://academy-service:9002`. Validated now; used once the academy admin routes exist. |
| `TLS_CERT_PATH`, `TLS_KEY_PATH`, `TLS_CA_PATH` | outside local/test | Server certificate for the listener, and the mTLS client certificate and local CA for calls to the admin listeners. |
| `TRUSTED_PROXY_IPS` | outside local/test | Comma-separated IPs or CIDRs (Caddy). `/0` is refused. |
| `PORT` | no | Default `3005`. |

## Browser side

`web/` is plain HTML, CSS and ES modules served with `go:embed`: no framework,
no build step, no CDN. The admin token lives in a module variable in
`web/js/auth.js` and nowhere else (no `localStorage`, `sessionStorage`, cookie
or URL), so reloading the page signs the admin out. Any `401` clears the token
and returns to the sign-in page. The page is Arabic first (right-to-left) with
an English toggle; the choice is kept in the URL hash only.

Requests, Catalog and Files tabs exist in `web/js/tabs.js` but are hidden until
their APIs exist.

## Tests

```bash
cd services/admin-console
go test -race ./...
node --test web/test/*.test.mjs     # Node 20+, no npm packages
```

The JavaScript tests run the real modules on a small fake DOM
(`web/test/fake-dom.mjs`); `web/test/static.test.mjs` guards the shipped files
(no inline script or style, no storage or cookie use, no third-party URL,
contrast of the colour tokens).
