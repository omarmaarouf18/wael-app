# ADR-0007: Core Academy Service Design

- **Status**: Accepted
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

The catalog, purchase, and entitlement decisions are recorded (ADR-0003,
ADR-0004, ADR-0005) but no service implements them: as of this ADR, no code
exists for subjects, lessons, entitlements, purchase requests, admin
management, or broadcasts anywhere in the repo. Student auth is
single-role (`user`) with no tenants, and the account record carries no
suspension state. The conventions to follow already exist:
`services/auth-service` and `services/notification-service` are each a
`cmd/main.go` plus `internal/{config,handlers,models,store}`, with a
`Store` interface backed by `MemoryStore` and `MongoStore`; user routes sit
behind `GatewayAuth` (`X-Gateway-Secret` on every non-health route, see
`Server.GatewayAuth`); the gateway maps `routeDefs` prefixes such as
`/api/v1/auth/` and `/api/v1/notifications/` to service URLs and strips
`/api/v1` before forwarding. Token revocation exists: `ValidateToken`
rejects tokens whose `iat` predates the Redis marker
`jwt:invalidated_before:<userID>` (fail-closed), set by
`RevokeAllUserTokens`. Notification push is per-user only: `Push` requires
`user_id` and `RedisBus.Publish` fans out on `notif:user:<userID>` (see
`channelFor`).

## Decision

1. A new `academy-service` serves subjects (materia), following the
   existing service layout and the `Store` interface with
   `MemoryStore`/`MongoStore` pattern. Each subject has a price set by the
   admin, a description, and an ordered list of videos, each with a title
   and a description.
2. Videos are references to unlisted YouTube videos (ADR-0001); the
   service stores identifiers, never bytes.
3. Mongo collections, fields, and indexes:
   - `subjects`: `_id`, `title`, `title_ar`, `description`, `price`,
     `status` (`draft`/`published`), `created_at`, `updated_at`; index on
     `status`.
   - `lessons`: `_id`, `subject_id`, `position`, `title`, `description`,
     `youtubeVideoId`, `duration_sec`; unique compound index on
     (`subject_id`, `position`).
   - `entitlements`: `user_id`, `subject_id`, `granted_at`, `source`
     (activating purchase or manual grant); unique compound index on
     (`user_id`, `subject_id`) plus an index on `user_id`.
   - `purchase_requests`: `_id`, `user_id`, `subject_id`, `status`
     (`pending`/`accepted`/`rejected`), `created_at`, `decided_at`;
     index on (`user_id`, `subject_id`) and on `status`.
   - `admin_audit_log`: `_id`, `actor`, `action`, `target`, `detail`,
     `created_at`; index on (`actor`, `created_at`) and on `target`.
     Every admin mutation writes one entry.
4. Student endpoints are served through the gateway under
   `/api/v1/academy/`, following the existing prefix/strip convention:
   subject catalog and metadata, lesson reads, the student's own
   entitlements, and payment-request submission. No caps apply to owned
   content (ADR-0004).
5. Admin endpoints (subject/price CRUD, video add/edit, purchase-request
   review/accept, account suspend/reactivate/delete, app-wide broadcast)
   live on a separate listener that is not reachable through the student
   gateway routes. That listener authenticates the out-of-band admin
   identity (ADR-0002), never the student JWT. A separate web admin panel
   calls these endpoints directly, and the student mobile app contains no
   admin functionality.
6. Payment is manual: the student submits a payment request, the admin
   accepts it, and acceptance creates the `entitlements` record that
   grants ownership of that subject.
7. Entitlement rule: a student owns a subject if and only if an
   `entitlements` record exists for (`user_id`, `subject_id`).
8. Gating rule: `youtubeVideoId` is NEVER serialized in any student
   response unless the caller owns the subject. Unentitled reads return
   metadata (titles, descriptions, positions, durations) with the
   reference field omitted, so nothing playable leaks before purchase.
9. Suspension takes effect on already-issued access tokens through the
   existing revocation mechanism: suspending an account sets its status
   and calls `RevokeAllUserTokens`, so `ValidateToken` rejects every
   token issued before the marker; the account's refresh entries are
   deleted at the same time so no new access tokens can be minted.
   (This locks access; see the reported conflict with ADR-0004 below —
   recorded here as stated, not resolved.)
10. App-wide broadcast is stored and delivered per recipient: the admin
    call fans out into one notification-service internal push per user
    (each carrying that user's `user_id`), because push is per-user only.
    Each recipient row then flows through the normal list/read/SSE path.

## Consequences

### Positive

- One entitlement collection plus the `youtubeVideoId` gating rule cover
  purchase (ADR-0003), no-caps reads (ADR-0004), and reference-only video
  (ADR-0001).
- Suspension reuses the read-and-verified revocation path instead of
  inventing a second session system.
- Broadcast needs no new transport: per-user push plus existing SSE
  delivery already reach every client.
- Every admin mutation is auditable via `admin_audit_log`.

### Negative and Tradeoffs

- Broadcast cost scales with the user base (one stored row and one push
  per recipient).
- Suspension visibly locks a student out of content they could previously
  open — the ADR-0004 conflict reported below.
- Manual payment keeps a human in the fulfillment loop for every purchase.

## Alternatives Considered

- **Entitlements embedded on the user record**: rejected; ownership is a
  growing set with its own lifecycle, not a user attribute.
- **Admin endpoints on the student routes with checks**: rejected; it puts
  elevated endpoints on the student-reachable surface (ADR-0002).
- **Single shared broadcast row**: rejected; per-user rows are what the
  existing list/read/SSE path serves.

## Open Questions

- What the student submits with a payment request, and whether receipt
  images need storage.
- Whether rejected requests can be resubmitted.
- Hard versus soft account delete, and what happens to entitlements in
  each case.
- Whether study notes (ADR-0005) belong to a subject.
- How the first admin identity is created.
- Refund and revocation of entitlement.

## To verify

- Confirm the admin listener port, bind address, and firewalling follow
  the existing service conventions at implementation time, without
  reusing the student port, and that no gateway route prefix exposes the
  admin paths.
- Confirm the Mongo database name for the new collections at
  implementation time.
- If receipt images are required, confirm object storage is restored
  first (see ADR-0005 storage note).
