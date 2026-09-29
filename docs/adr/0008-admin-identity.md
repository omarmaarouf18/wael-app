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
6. **Lockout Protection**: `auth-service` locks out repeated bad verification attempts, keyed on client IP and token hash. The lockout threshold is unspecified in the spec and is not invented here. Internal listener ports are likewise unspecified in the spec and are not invented here.

### 7. Auditability, Privacy, and Safe Responses

1. **Audit Trail**: Every admin mutation writes an entry to `admin_audit_log` recording `actor_id`, `actor_name`, `action`, `target_type`, `target_id`, `detail`, and `created_at`.
2. **Privacy / No IP Persistence**: Consistent with `docs/core-service/SPEC.md` Section 5, `admin_audit_log` does not store IP addresses.
3. **Input Sanitization**: Operator-supplied reasons are length-capped (1–1000 characters) and stripped of CR/LF characters before writing to logs.
4. **Safe Errors**: Responses use safe errors (`handlerutil.WriteSafeError`) and never reveal internal token hashes or stack traces.

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
