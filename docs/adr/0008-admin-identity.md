# ADR-0008: Admin Identity and Console Boundaries

- **Status**: Accepted
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

Student authentication in this system is single-role (`user`) with no tenants or administrative role (ADR-0002, ADR-0007). In accordance with ADR-0002, the student mobile application (`frontend/`) contains no administrative functionality, views, or endpoints.

Operating the academy requires administrative capabilities: managing the fixed catalog tree (levels and subjects), managing videos and PDF notes, reviewing and accepting or rejecting purchase requests, managing student account statuses (suspend, reactivate, delete), broadcasting notifications, and viewing audit trails (ADR-0007, `docs/core-service/SPEC.md` Section 1).

Per owner decisions locked in `docs/core-service/SPEC.md` (Section 1 decisions 14, 15, and 17), administrative identity is not an account, is issued only by a server-side CLI tool, and is completely isolated from the student gateway and student JWT authentication. This ADR records the approved admin identity model, token semantics, console architecture, internal listener separation, and verification contract across backend services.

## Decision

### 1. Distinct Identity Model (Not an Account)

1. Admin identity is a separate named identity, not a student account or role.
2. Administrative operations are tied to a specific named operator identity for auditability.

### 2. Server-Side CLI-Only Provisioning and Revocation

1. Admin identities and tokens are provisioned and revoked exclusively via server-side CLI tools (`onboard-admin` and `revoke-admin`).
2. No administrative endpoint or web interface is capable of minting or revoking admin tokens.
3. `onboard-admin` provisions a named admin token, stores its SHA-256 hash, and shows the token once. Tokens expire.
4. `revoke-admin` marks the record revoked in the database; revocation takes effect immediately across all services.

### 3. Cryptographic Token Semantics and Storage

1. **Generation**: Admin tokens are generated using a cryptographically secure pseudorandom number generator (CSPRNG).
2. **Display**: Plaintext tokens are shown once upon generation. Plaintext tokens are never stored, logged, or recoverable.
3. **Storage (`admins` collection)**: Stored solely as SHA-256 hashes in a dedicated `admins` collection managed by `auth-service`.
4. **Schema**: The `admins` collection document contains:
   - `_id`: Unique admin identifier
   - `name`: Human-readable name of the admin operator
   - `token_hash`: SHA-256 hash of the token (never plaintext)
   - `created_at`: Creation timestamp
   - `expires_at`: Expiration timestamp
   - `revoked_at`: Revocation timestamp (or unset/null if active)
5. **Revocation & Expiry**: Tokens expire and can be revoked. A token is valid if and only if `revoked_at` is empty or null and `expires_at` is in the future.

### 4. Admin Console and Subdomain Architecture

1. **Subdomain Isolation**: The admin web interface is hosted on a separate admin subdomain (for example, `admin.<domain>`), distinct from the student API gateway domain (`api.<domain>`).
2. **Thin Proxy Pattern**: The admin console (`services/admin-console`) is served on the admin subdomain and acts as a thin proxy to internal admin endpoints.
3. **Internal Token Injection**: The proxy attaches `X-Internal-Token` only on the internal Docker network.
4. **No Authorization Logic**: `admin-console` makes no authorization decisions; it forwards requests with the operator's admin token to internal services.
5. **Browser Memory Only**: In the administrator's browser, the admin token is held only in tab memory (`X-Admin-Token`). It is never stored in persistent browser storage (`localStorage`, `sessionStorage`, or cookies).

### 5. Network Separation and Internal Listeners

1. **Path Prefix**: Admin endpoints are under `/internal/admin/*` (e.g. on `auth-service`, `academy-service`, and `notification-service`).
2. **Listener Separation**: Admin endpoints are served on separate internal HTTP listeners (`ADMIN_LISTEN_ADDR`) per service, bound to the internal Docker network and never published on host ports.
3. **Gateway Exclusion**: Admin endpoints are never exposed through the student `api-gateway`. The student gateway strips any incoming `X-Internal-Token` and has no `/internal/` route.
4. **Authentication Separation**: Admin endpoints are never authenticated by a student JWT.

### 6. Admin Verification Contract (`POST /internal/admin/verify`)

1. **Verification Endpoint**: `auth-service` exposes `POST /internal/admin/verify` on its internal admin listener.
2. **Headers**: The caller must provide both `X-Internal-Token` and `X-Admin-Token` headers.
3. **Response**: On successful validation, `auth-service` returns HTTP 200 with `{ "admin_id": "...", "name": "..." }`.
4. **No Caching**: Calling services (such as `academy-service`) verify the admin token with `auth-service` on every request without caching, ensuring revocation takes effect immediately.
5. **Fail Closed**: If `auth-service` is unreachable or unavailable, calling services fail closed with HTTP 503.
6. **Lockout Protection**: `auth-service` locks out repeated bad verification attempts, keyed on client IP and token hash. The lockout threshold is unspecified in the spec and is not invented here. Internal listener ports are likewise unspecified in the spec and are not invented here. *(Superseded by 2026-10-01 amendment below).*

### 7. Auditability, Privacy, and Safe Responses

1. **Audit Trail**: Every admin mutation writes an entry to `admin_audit_log` recording `actor_id`, `actor_name`, `action`, `target_type`, `target_id`, `detail`, and `created_at`.
2. **Privacy / No IP Persistence**: Consistent with `docs/core-service/SPEC.md` Section 5, `admin_audit_log` does not store IP addresses.
3. **Input Sanitization**: Operator-supplied reasons are length-capped (1–1000 characters) and stripped of CR/LF characters before writing to logs.
4. **Safe Errors**: Responses use safe errors (`handlerutil.WriteSafeError`) and never reveal internal token hashes or stack traces.

### 8. Amendment (2026-10-01, Owner Decisions for Phase 1.4)

*Supersedes the sentences in Section 6 stating that lockout thresholds and listener ports are unspecified.*

1. **Trusted header**: `X-Admin-Client-IP`. Set ONLY by `admin-console` (Phase 6), which overwrites any client-supplied value.
2. **IP Extraction & Fallback**: `auth-service` reads `X-Admin-Client-IP` ONLY on the admin listener and ONLY after `X-Internal-Token` is valid (constant-time). If absent, fall back to the connection `RemoteAddr` host. Never read `X-Forwarded-For` on the admin listener. No IP allowlist for now.
3. **Lockout**: Reuse the existing `Lockout` interface and thresholds, with separate key prefixes `"admin-verify-ip:"` and `"admin-verify-tok:<hash>"`.
4. **Listener Ports & Compose Isolation**: `ADMIN_LISTEN_ADDR` default `":9001"` for `auth-service` (`academy-service` will use `":9002"` later). Required outside local/test. No `ports:` in compose.

### 9. Amendment (2026-10-01, Owner Decision on Audit Log Ownership for Phase 1.5)

1. **Per-Service Ownership**: Each service owns its own `admin_audit_log` collection in its own database, following the exact `docs/core-service/SPEC.md` Section 5 schema (`_id`, `actor_id`, `actor_name`, `action`, `target_type`, `target_id`, `detail`, `created_at`; no IP addresses persisted).
2. **Action Segregation**: `auth-service` records account management actions (`account_suspend`, `account_reactivate`, `account_delete`) in its database (`auth_db`); `academy-service` records catalog, review, and entitlement actions in its database.
3. **Endpoint**: Each service exposes `GET /internal/admin/audit-log?page=&limit=` on its admin listener (`ADMIN_LISTEN_ADDR`), authenticated via `X-Internal-Token` and `X-Admin-Token` (validated via `auth-service`).
4. **Console Merging**: The admin console (Phase 6) queries both services and merges entries ordered by `created_at` descending. No shared database and no cross-service database writes.

### 10. Note (2026-10-02, Owner Decision on the Console Implementation, SPEC Phase 6.1)

*Records how Section 4 is implemented. It changes none of the decisions above.*

1. **Shape**: the admin console is a Go service (`services/admin-console`) serving static pages. The pages are plain HTML, CSS and vanilla JavaScript ES modules embedded with `go:embed`, served by the Go standard library: no framework, no Node build step, no CDN. It is modelled on the owner's existing reviewer console (`kyc-reviewer-console`), not copied from it.
2. **Differences from that console**:
   1. The admin token is kept in a JavaScript module variable only (Section 4.5). The reviewer console used `sessionStorage`; here a reload means signing in again.
   2. Strict security headers on every response (CSP `default-src 'self'` with no inline script or style, `nosniff`, `Referrer-Policy: no-referrer`, `no-store` on `/api/*`). The reviewer console sent none.
   3. An upstream failure or timeout becomes a safe `503` after 10 seconds (the reviewer console returned `502` after 30 seconds).
   4. The client's `X-Internal-Token` and `X-Admin-Client-IP` never reach an upstream: the upstream request is built from scratch, `X-Internal-Token` comes from the environment, and `X-Admin-Client-IP` is set from the real client address. `X-Forwarded-For` is read only when the peer is in `TRUSTED_PROXY_IPS` (Caddy), as Section 8 requires.
   5. It lives in this monorepo, is built and released by the same pipeline, and runs behind Caddy on the admin subdomain.
   6. The JavaScript is split into small modules per tab (the reviewer console is one file of about 2,100 lines).
   7. Arabic first and right-to-left, with an English toggle, using the EL METR dark and crimson tokens.
3. **Routes are an explicit allowlist**, each forwarding to exactly one `/internal/admin/*` endpoint of auth-service: whoami (`verify`), accounts list, suspend, reactivate, delete, and the audit log. There is no generic pass-through and no route under `/internal/`.
4. **Audit log merge (Section 9.4)**: until the academy-service admin endpoints exist (SPEC Phase 4.1), the console serves auth-service's audit log only. Merging both logs by `created_at` descending is added with Phase 4.1; the academy routes (requests, catalog, files) are added as Phase 4 lands. Their tabs exist in the code and stay hidden until then.
5. **Held from `main`**: this is an admin-authorization surface (SPEC Section 12, rule 6). It stays off `main` until the owner confirms. *(2026-10-02: owner confirmed; the console released to `main` as `b6a11fe`.)*

### 11. Note (2026-10-03, Console Catalog Pages, SPEC 6.3 Part 1)

*Records the academy half of the console. It changes none of the decisions above.*

1. **Academy routes**: the allowlist gains fourteen catalog routes, all to `ACADEMY_ADMIN_URL` over mTLS with the same guarantees as the 6.1 routes: levels list/create/update/delete, subjects list/create/update/publish/unpublish, videos list/create/update/reorder/delete (with `?force=true` only when `force` is true). Ids travel in the body, like the account routes; only the fields the academy accepts are forwarded.
2. **Audit source switch**: `GET /api/audit` gains a `source` parameter (`auth`, the default, or `academy`). The page shows one source at a time behind a الحسابات / المحتوى switch; the two logs are not merged into one page, because merged pagination over two sources is wrong without a shared cursor. This supersedes the merge plan in Section 10 item 4, pending owner review.
3. **Catalog tab**: levels in the fixed بكالوريوس / دبلومات / تعليم مهني order, subjects with draft/published filter and pagination, videos with reorder and the force-delete flow; every mutation reloads from the server. Server error codes map to one Arabic/English map in `web/js/i18n.js`. Requests and Files stay hidden (4.5/4.6 and Phase 5).

### 12. Note (2026-10-03, Requests Review, Student Entitlements, Identity Join, and Idle Lock, SPEC 4.5, 4.6, 6.3 Part 2)

*Records the purchase-request review and entitlement management surface in the console. It changes none of the decisions above.*

1. **Proxy routes**: the allowlist gains six routes to `ACADEMY_ADMIN_URL` over mTLS with identical security guarantees (admin token verification, input validation, no raw server text exposure, 10s timeout, safe 503):
   - Requests: `GET /api/requests?status&subject_id&page&limit`, `POST /api/requests/accept` `{id}`, `POST /api/requests/reject` `{id, reason}`.
   - Entitlements: `GET /api/entitlements?user_id`, `POST /api/entitlements/grant` `{user_id, subject_id}`, `POST /api/entitlements/revoke` `{id, reason}`.
2. **Student identity join**: `GET /api/accounts` gains an `ids` query parameter (validated comma-separated UUIDs, max 100). The Requests tab loads requests from `academy-service`, extracts unique `user_id`s, and batches a single lookup to `auth-service` to display the student's full name, email, and phone. If the lookup fails, the tab falls back to displaying the raw user ID and a localized note ("تعذر تحميل بيانات الطالب").
3. **Requests tab**: unhidden, displays purchase requests (pending by default) formatted with Cairo date-time, live badge counter for `pending_count` with 60-second background polling, accept dialog showing subject price and Cairo expiry, and reject dialog with a mandatory reason (1-1000 runes).
4. **Student entitlements modal**: Accounts tab rows gain a "المواد" button opening a student-specific modal that lists active, expired, and revoked entitlements with revocation details, manual grant (level and published subject cascading select), and manual revoke with a mandatory reason. Revoking an entitlement leaves the underlying payment record intact.
5. **Idle lock (UI/UX audit A1)**: After 19 minutes of inactivity across mouse, keyboard, touch, and scroll events, a modal warning appears with a live 60-second countdown. User interaction dismisses the warning and resets the timer. If 20 minutes elapse without activity, the admin token is wiped from memory and the console returns to the sign-in screen with an idle-lock notice.

### 13. Amendment (2026-10-08, App-Facing Settings Control in Admin Console)

*Records the owner decision to allow the admin console to control app-facing settings that were previously environment-variable only.*

1. **Storage & Defaults**: Stored in a single document in academy-service's `app_settings` collection (`_id: "app"`). Fields: `show_prices` (bool, default `false`), `support_whatsapp` (digits only, international without "+", 10-15 digits, default from `SUPPORT_WHATSAPP`), `center_name_ar`/`center_name_en` (0-100 runes), `center_address_ar`/`center_address_en` (0-300 runes), `center_hours_ar`/`center_hours_en` (0-200 runes), `center_map_url` (empty or https only, max 500 runes), `updated_at`, `updated_by`. Environment values serve as defaults until an admin explicitly saves settings in the console. Read paths are cached in-memory for 60 seconds and invalidated on save.
2. **Safety & Validation**: All text fields reject store-safety payment/purchase words (`دفع`, `ادفع`, `شراء`, `اشتري`, `استرداد`, `اشتراك مدفوع`, `payment`, `pay`, `buy`, `purchase`, `refund`, `InstaPay`, `فودافون كاش`, `محفظة`/`wallet`) on word boundaries with HTTP 400 `settings_forbidden_word`. Unknown fields are rejected.
3. **Internal Admin Endpoints**:
   - `GET /internal/admin/settings`: returns current effective settings with env defaults merged, plus `updated_at` and `updated_by`.
   - `PUT /internal/admin/settings`: full replace of editable settings fields; writes an `admin_audit_log` entry `settings_updated` listing changed field names only (values omitted).
4. **Proxy Routes & Console UI**:
   - Proxy routes `GET /api/settings` and `POST /api/settings/update` forward to `ACADEMY_ADMIN_URL` over mTLS with standard admin token verification and security headers.
   - New "الإعدادات / Settings" tab provides controls for the price toggle ("إظهار الأسعار في التطبيق" with notice "التغيير بيوصل للتطبيق خلال 5 دقايق تقريباً"), WhatsApp support number, center details (Arabic and English), and map URL.
   - Includes unsaved-changes guard, double-submit guard, `Retry-After` countdown on 429, success toast, and localized error messages.
5. **Decoupling from Server Expose Flag**: `show_prices` in `app_settings` is independent of `EXPOSE_PRICE_TO_STUDENTS`. The backend flag `EXPOSE_PRICE_TO_STUDENTS` determines whether the API sends subject price fields; `show_prices` controls client-side display visibility via `GET /academy/app-config`.

## Consequences


### Positive

- Clear separation between student accounts and administrative access; admin capabilities are not accessible through student credentials.
- The student mobile application and student gateway contain no admin routes or tokens.
- Storing only SHA-256 hashes ensures database records do not expose plaintext admin tokens.
- Holding the token only in browser tab memory avoids persisting admin credentials in browser storage (such as `localStorage` or cookies).
- Verifying the token on every request without caching ensures token revocation and expiry take effect immediately.
- Administrative mutations are attributed to named admin identities in the audit trail without persisting IP addresses.

### Negative and Tradeoffs

- Every administrative request across services (e.g. `academy-service`) requires an internal HTTP call to `auth-service`, adding internal latency.
- Out-of-band CLI onboarding requires operator access to the host CLI (`onboard-admin`, `revoke-admin`).
- If an admin loses their token, it cannot be retrieved; it must be revoked via CLI and reissued.
- Availability dependency: if `auth-service` is unreachable, internal admin operations fail closed (503).

## Alternatives Considered

- **Admin mode inside the mobile app**: Rejected in ADR-0002; bundling elevated capabilities into the student binary exposes them to reverse engineering.
- **Admin endpoints on the student routes with checks**: Rejected in ADR-0002 and ADR-0007; elevated endpoints must not exist on the student-reachable surface.

## Planned: internal trust hardening (D1)

Amended 2026-10-03 (owner decision D1), recorded 2026-10-05. **Status: planned,
not built.** Nothing below is implemented; the Decision section above is
unchanged.

**Today (checked against the code on 2026-10-05):**

- One shared secret, `INTERNAL_SERVICE_TOKEN`, is read by auth-service,
  academy-service, notification-service and admin-console
  (`services/*/internal/config/config.go`). Callers send it as
  `X-Internal-Token`: admin-console to the auth and academy admin listeners
  (`services/admin-console/internal/proxy/proxy.go`), academy-service to the
  auth admin listener for admin-token verification
  (`services/academy-service/internal/handlers/admin.go`), and academy-service
  and auth-service to `POST /internal/push` on notification-service
  (`services/academy-service/internal/notify/notify.go`,
  `services/auth-service/internal/notify/notify.go`). Receivers compare it in
  constant time and reject an empty value (auth and academy also guard an empty
  configured secret; notification-service relies on its config refusing an empty
  `INTERNAL_SERVICE_TOKEN`)
  (`services/auth-service/internal/handlers/admin.go`,
  `services/academy-service/internal/handlers/handlers.go`,
  `services/notification-service/internal/handlers/notifications.go`).
  Every receiver accepts the same token from any caller.
- The admin listeners (auth `:9001`, academy `:9002`) and notification-service
  use `tlsutil.LoadServerTLSConfig` (`shared/infra/tlsutil/tlsutil.go`):
  `ClientAuth: tls.RequireAndVerifyClientCert` against the one internal CA, with
  no `VerifyPeerCertificate` and no check of the peer's name. Outside
  `APP_ENV=local|test`, mTLS is required (`buildServer` in each service's
  `cmd/main.go`). So any peer holding a certificate signed by that CA, plus the
  shared token, is accepted.

**Planned steps (owner decision D1):**

1. **Client identity allowlist** on the admin listeners, using
   `VerifyPeerCertificate` on the peer certificate's SAN DNS name: auth-service
   `:9001` accepts only `admin-console` and `academy-service`; academy-service
   `:9002` accepts only `admin-console`. Each service certificate already
   carries `DNS:<service-name>` (`infrastructure/certs/generate-certs.sh`); it
   also carries `DNS:localhost`, which the allowlist must not accept.
2. **A separate internal token per pair of calling services**, replacing the
   single `INTERNAL_SERVICE_TOKEN`, including `/internal/push` on
   notification-service. This needs new secrets in `.env.production` and a
   deploy-config change.
3. **Later, not scheduled:** asymmetric service JWTs.

Until step 1 and step 2 are built and verified, the Today description above is
the actual trust model.
