# ADR-0007: Core Academy Service Design

- **Status**: Accepted
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

The catalog, purchase, and entitlement decisions are recorded (ADR-0003,
ADR-0004, ADR-0005) but no service implements them: as of this ADR, no code
exists for subjects, lessons, notes delivery, entitlements, or purchase
requests anywhere in the repo. Student auth is single-role (`user`) with no
tenants, and the service conventions to follow already exist:
`services/auth-service` and `services/notification-service` are each a
`cmd/main.go` plus `internal/{config,handlers,models,store}`, with a
`Store` interface backed by `MemoryStore` and `MongoStore`; user routes sit
behind `GatewayAuth` (`X-Gateway-Secret` on every non-health route, see
`Server.GatewayAuth`); the gateway maps `routeDefs` prefixes such as
`/api/v1/auth/` and `/api/v1/notifications/` to service URLs and strips
`/api/v1` before forwarding (so `/api/v1/auth/signup` reaches the backend
as `/auth/signup`).

## Decision

1. A new `academy-service` implements the catalog, purchase, and
   entitlement domain, following the existing service layout (`cmd/main.go`
   plus `internal/{config,handlers,models,store}`) and the `Store`
   interface with `MemoryStore`/`MongoStore` pattern.
2. Mongo collections, fields, and indexes:
   - `subjects`: `_id`, `title`, `title_ar`, `description`, `status`
     (`draft`/`published`), `created_at`, `updated_at`; index on `status`.
   - `lessons`: `_id`, `subject_id`, `position`, `title`, `youtubeVideoId`,
     `duration_sec`; unique compound index on (`subject_id`, `position`).
     Videos are YouTube references only (ADR-0001); no video bytes are
     stored.
   - `notes`: `_id`, `subject_id`, `lesson_id` (optional), `title`,
     `format`, `storage_key`, `created_at`; index on `subject_id`. Only
     the key is stored; note bytes live in the future object store (see
     ADR-0005, whose storage question is still open).
   - `entitlements`: `user_id`, `subject_id`, `granted_at`, `source`
     (activating purchase or manual grant); unique compound index on
     (`user_id`, `subject_id`) plus an index on `user_id`.
   - `purchase_requests`: `_id`, `user_id`, `subject_id`, `status`,
     `created_at`; index on (`user_id`, `subject_id`) and on `status`.
     Status values, amounts, and provider references are undecided (see
     Open Questions) and are not fixed here.
3. Student endpoints are served through the gateway under
   `/api/v1/academy/`, following the existing prefix/strip convention:
   catalog and subject metadata, lesson and note reads, the student's own
   entitlements, and purchase-request creation. No caps apply to owned
   content (ADR-0004): the read paths enforce no view counts, no expiry,
   and no device checks.
4. Admin endpoints (subject/lesson/note CRUD, purchase-request
   list/activation, entitlement grant/revoke) live on a separate listener
   that is not reachable through the student gateway routes. That listener
   authenticates the out-of-band admin identity (ADR-0002), never the
   student JWT, and no admin surface is added to the mobile app.
5. Entitlement rule: a student owns a subject if and only if an
   `entitlements` record exists for (`user_id`, `subject_id`), created by
   an activated purchase or by a manual grant.
6. Gating rule: `youtubeVideoId` is NEVER serialized in any student
   response unless the caller owns the subject. Unentitled catalog and
   lesson reads return metadata (titles, positions, durations) with the
   reference field omitted, never nulled-or-guessable, so nothing playable
   leaks before purchase.

## Consequences

### Positive

- Purchase (ADR-0003), no-caps (ADR-0004), YouTube-only video (ADR-0001),
  and in-app notes (ADR-0005) each reduce to checks against one
  `entitlements` collection plus the `youtubeVideoId` gating rule.
- Admin capability stays out of the student app and off the student routes
  (ADR-0002): the only admin attack surface is the separate listener with
  a separate identity.
- The design reuses proven repo patterns (Store interface, GatewayAuth
  header, gateway prefix/strip), so implementation risk is structural,
  not architectural.

### Negative and Tradeoffs

- A second listener per service instance is more to configure, observe,
  and firewall than a single port.
- Until the open questions below are decided, purchase requests can be
  recorded but not fulfilled by the system.

## Alternatives Considered

- **Entitlements embedded on the user record**: rejected; ownership is a
  growing set with its own lifecycle (grants, revokes, audits), not a
  user attribute.
- **Admin endpoints on the same routes with role checks**: rejected; it
  puts elevated endpoints on the student-reachable surface and contradicts
  ADR-0002.
- **Storing playable URLs instead of IDs**: rejected; IDs plus the gating
  rule keep the reference model minimal per ADR-0001.

## Open Questions

- Payment provider versus manual activation.
- What a subject contains beyond videos and notes.
- Price model per subject.
- Refund policy.

## To verify

- Confirm the admin listener port, bind address, and firewalling follow the
  existing service conventions at implementation time (auth-service and
  notification-service pattern), without reusing the student port.
- Confirm the Mongo database name for the new collections at
  implementation time.
- Check that no existing or future gateway route prefix accidentally
  exposes the admin listener paths.
