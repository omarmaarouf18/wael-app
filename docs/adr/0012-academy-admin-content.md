# ADR-0012: Academy Admin Content API (Phase 4.1-4.4)

- **Status**: Accepted (brief Phase 4.1-4.4, 2026-10-02)
- **Date**: 2026-10-02
- **Related Commit SHA**: none (decision record; implementation lands as four academy-service commits reviewed by the owner before push)
- **Related finding**: n/a

## Context

The owner creates diplomas, subjects and videos through an API (SPEC Section 1 decisions 2 and 16, SPEC Section 11 Phase 4). Student visibility rules (SPEC Section 7 R1, R2, R8), the admin identity model (ADR-0008) and the audit schema (SPEC Section 5) already exist. The brief for Phase 4.1-4.4 fixes the admin endpoints but leaves several points open: verify caching, audit failure handling, the meaning of level `published` against the always-visible catalog axes (SPEC Section 1 decision 2, amended 2026-10-02), video deletion semantics, and which service URL verifies admin tokens.

## Decision

1. **Verify without caching.** Academy verifies `X-Admin-Token` on every request via auth-service `POST {AUTH_ADMIN_URL}/internal/admin/verify` over mTLS with a 3 s timeout. No caching (ADR-0008 Section 6.4), so revocation and expiry take effect immediately. Any verify failure that is not a clean 401/429 fails closed with 503.
2. **New `AUTH_ADMIN_URL` env var.** `AUTH_SERVICE_URL` points at the public port (3002); the verify endpoint lives on the admin listener (9001). Academy therefore reads `AUTH_ADMIN_URL` (default `https://auth-service:9001`, required outside local/test). Compose and `.env.production` must set it; this ADR does not change them (infra lane).
3. **Audit is best-effort, like auth-service.** Every academy admin mutation writes its `admin_audit_log` entry in the same flow; a write failure is logged at error level (action + target id, no secrets) and does not fail the call — the same behaviour as auth-service account actions. Shape and response match auth-service so the console merges both logs. No IP addresses are persisted (SPEC Section 5, ADR-0008 Sections 7 and 9); `X-Admin-Client-IP` is forwarded to the verify call for auth lockout keying but never stored.
4. **Level `published` refines the axes rule.** Seeded levels are published; admin diplomas start unpublished and students see published levels only. The SPEC amendment text ("each with all of its levels") now reads "each with all of its published levels"; empty study types still render an empty list. Store `ListLevels` still returns all levels; the student handler filters.
5. **No subject hard delete.** Unpublish is the only way to hide a subject (students may own it). Publishing (via the publish endpoint, creation with `published: true`, or a draft-to-published patch) requires at least one non-deleted video: 409 `subject_has_no_videos`.
6. **Videos are soft-deleted.** Deleted videos are hidden from listings, counts, detail and `/play`, and count as absent for the publish gate. Deleting the last video of a published subject needs `?force=true` (409 `last_video_of_published_subject`), which also unpublishes the subject. The server never calls YouTube; it stores only the validated 11-char id.
7. **Admin-created videos are published.** The subject's own draft/published state gates student visibility; there is no per-video publish toggle in this phase.
8. **Subject `order` sorts listings.** Both student and admin subject lists sort by `(order, created_at)`; existing rows default to 0, preserving current order.

## Consequences

### Positive

- Revocation is immediate across services; auth outages fail closed.
- The audit trail is complete per mutation and mergeable in the console.
- No orphaned published-but-empty subjects and no resurrection of deleted videos in student views.
- Audit failures behave exactly like auth-service (logged, call succeeds), so the two services stay consistent.

### Negative and Tradeoffs

- Every admin request pays one internal verify round-trip (same tradeoff as ADR-0008).
- A lost audit write leaves a gap the error log must explain; audit rows are never written before the mutation.

## Alternatives Considered

- **30 s verify cache keyed by token SHA-256** (allowed by the brief): rejected, keeps ADR-0008 immediate revocation.
- **Hard video delete**: rejected, play logs and entitlements reference videos; soft delete keeps history consistent.
- **Separate `AUTH_ADMIN_URL` vs reusing `AUTH_SERVICE_URL`**: reusing the public URL would send admin tokens to the public listener, which has no `/internal/` route; a dedicated admin URL was required.
