# ADR-0007: Core Academy Service Design

- **Status**: Proposed
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

The catalog, purchase, and entitlement decisions are recorded (ADR-0003,
ADR-0004, ADR-0005) but no service implements them: as of this ADR, no code
exists for study types, levels, subjects, lessons, files, entitlements,
purchase requests, admin management, or broadcasts anywhere in the repo.
Student auth is single-role (`user`) with no tenants; signup takes only
email, password, and role (see `signupRequest`), and the account record
carries no phone number, no full name, and no status. The conventions to
follow already exist: `services/auth-service` and
`services/notification-service` are each a `cmd/main.go` plus
`internal/{config,handlers,models,store}`, with a `Store` interface backed
by `MemoryStore` and `MongoStore`; user routes sit behind `GatewayAuth`
(`X-Gateway-Secret` on every non-health route); the gateway maps
`routeDefs` prefixes to service URLs and strips `/api/v1` before
forwarding (today only `/api/v1/auth/` and `/api/v1/notifications/`
exist). Token revocation exists in `shared/infra/jwtutil`
(`RevokeAllUserTokens`, `ValidateToken`). Notification push is per-user
only (`channelFor`, per-user `user_id` push).

## Decision

1. Catalog tree: study type -> level/programme -> subject. Study types and
   levels are FIXED (seeded, not admin-editable): bachelor (four years),
   diplomas, vocational training; each type has different subjects. The
   admin creates subjects inside a level, and adds videos and PDFs inside
   a subject.
2. A subject has an admin-set price, a description, ordered videos
   (unlisted YouTube references, each with title and description), and PDF
   files. Catalog entities carry bilingual fields (Arabic and English),
   with Arabic preferred (default).
3. Owning a subject grants its videos and PDFs automatically; PDFs are not
   sold separately. PDFs can be downloaded to the student's device.
4. Removed from scope: live events, progress tracking, ratings, student
   and view counts, free previews. Students upload nothing; the app only
   receives.
5. The payment flow is OUT OF SCOPE (the owner will specify it later).
   This ADR defines only the boundary: an admin-accepted payment request
   creates the entitlement. A rejected request can be resubmitted as a
   new request.
6. The admin can suspend, reactivate, and delete student accounts.
   Suspension and deletion are for abuse, content leakage, or suspicious
   behavior.
7. Admin identity is NOT an account: it is a named token issued only by a
   server-side tool (CLI), separate from student auth, never issued
   through any student-facing or panel endpoint. Token internals belong to
   future ADR-0008.
8. Registration requires full name and phone number. The OTP is sent to
   email only for now; the phone is collected but not verified until
   decided otherwise.
9. The admin can send an app-wide notification.
10. The admin can edit app UI text and images. Details belong to future
    ADR-0009 (media storage) and ADR-0010 (app content).
11. Out of scope here by explicit pointer: admin token internals (future
    ADR-0008); file storage (future ADR-0009); app content model (future
    ADR-0010).
12. Mongo collections, fields, and indexes (new `academy-service`,
    existing service layout and Store pattern):
    - `levels` (seed): `key`, `study_type`, `title_ar`, `title_en`,
      `position`; unique index on `key`. Seeded at deploy, never
      admin-edited.
    - `subjects`: `_id`, `level_key`, `term` (`first`/`second`/empty),
      `title_ar`, `title_en`,
      `description_ar`, `description_en`, `price`, `status`
      (`draft`/`published`), `created_at`, `updated_at`; index on
      (`level_key`, `status`).
    - `videos`: `_id`, `subject_id`, `position`, `title_ar`, `title_en`,
      `description_ar`, `description_en`, `youtube_video_id`; non-unique
      index on (`subject_id`, `position`). References only; no bytes stored.
    - `subject_files`: `_id`, `subject_id`, `kind` (`book`/`note`),
      `title_ar`, `title_en`, `format`, `storage_key`, `created_at`;
      index on `subject_id`.
      Content bytes live in the future object store (ADR-0009).
    - `entitlements`: `user_id`, `subject_id`, `granted_at`, `source`;
      unique compound index on (`user_id`, `subject_id`) plus an index
      on `user_id`.
    - `purchase_requests` (boundary only): `_id`, `user_id`,
      `subject_id`, `status` (`pending`/`accepted`/`rejected`),
      `created_at`, `decided_at`; partial unique index on
      (`user_id`, `subject_id`) where `status = pending`, and an index
      on `status`.
    - `admin_audit_log`: `_id`, `actor`, `action`, `target`, `detail`,
      `created_at`; index on (`actor`, `created_at`) and on `target`.
      Every admin mutation writes one entry.
13. The user record gains a `status` field. It is enforced on every
    token-issuing path — `Login`, `Refresh`, and `VerifyOTP` all refuse
    non-active accounts before minting anything.
14. Student endpoints are served through the gateway under
    `/api/v1/academy/`, following the existing prefix/strip convention
    (a new route entry alongside `/api/v1/auth/` and
    `/api/v1/notifications/` at implementation time): catalog tree,
    subject metadata, lesson and file reads, the student's own
    entitlements, and payment-request submission.
15. Admin endpoints (catalog and price CRUD, video/file add, request
    review/accept, account suspend/reactivate/delete, broadcast, UI
    text/images) live on a separate listener unreachable through the
    student gateway routes, authenticated by the admin named token — never
    the student JWT. A separate web admin panel, served as a subdomain,
    is provided by `admin-console` and calls `/internal/admin/*` endpoints.
    Admin identity details belong to future ADR-0008. The student mobile
    app contains no admin functionality.
16. Entitlement rule: a student owns a subject if and only if an
    `entitlements` record exists for (`user_id`, `subject_id`).
17. Gating rule: `youtube_video_id` and PDF download are NEVER served unless
    the caller owns the subject, with the entitlement checked on every
    download, not just on listing.
18. Suspension takes effect immediately on issued access tokens. Refresh
    checks account status and refuses non-active accounts before minting a
    new access token.

19. Broadcasts support audience `all` or `subject:<id>`. Fan-out pages
    through target user IDs and pushes per user in bounded batches. Delivery
    is idempotent per (`broadcast`, `user`); failures are logged and
    retryable. Per-user rows are retained because the existing list, read,
    and SSE paths serve per-user rows.

### Proposed mechanism (not decided)

The revocation approach found in `shared/infra/jwtutil` is the candidate:
suspending an account calls `RevokeAllUserTokens`, which sets the Redis
marker `jwt:invalidated_before:<userID>`, and `ValidateToken` rejects any
token whose `iat` predates the marker (per-jti denylist
`jwt:denylist:<jti>` covers single tokens). Refresh independently checks
account status and refuses to mint for non-active accounts; refresh-entry
deletion is not part of this mechanism. When Redis is unreachable, both
lookups fail closed — the token is rejected — after a single retry on
transient blips. None of this is wired to any account status today;
adopting it for suspension is proposed, not decided.

## Consequences

### Positive

- The fixed catalog tree bounds the admin surface: only subjects, videos,
  and PDFs inside them are editable, and levels cannot drift.
- One entitlement collection plus per-download gating covers purchase,
  videos, and PDFs with a single rule.
- Suspension and broadcast reuse verified mechanisms (revocation markers,
  per-user push) instead of new session or transport systems.
- Removed-scope items (events, tracking, ratings, counts, previews,
  uploads) cannot accrete implicitly: each would contradict this record.

### Negative and Tradeoffs

- Suspension visibly locks a student out of content they could previously
  open — the ADR-0004 conflict reported below.
- PDF download puts files on student devices, in tension with ADR-0005's
  in-app-only notes rule — reported below.
- Manual steps (payment acceptance, UI text edits) keep a human in the
  loop for operations that could later be automated.

## Alternatives Considered

- **Entitlements embedded on the user record**: rejected; ownership is a
  growing set with its own lifecycle, not a user attribute.
- **Admin endpoints on the student routes with checks**: rejected; it puts
  elevated endpoints on the student-reachable surface (ADR-0002).
- **Single shared broadcast row**: rejected; per-user rows are what the
  existing list/read/SSE path serves.

## Open Questions

1. The exact fixed list of levels for diplomas and vocational training.
2. Per-user watermark on downloaded PDFs (leak traceability).
3. Where PDFs are stored (future ADR-0009).
4. Whether English content is required or optional with Arabic fallback.
5. Phone number uniqueness and normalization.
6. Soft vs hard delete, and a blocklist of email/phone hashes after abuse
   deletion.
7. Revocation of an entitlement.
8. When or whether OTP is added on the phone (needs a provider).

## To verify

- Confirm the admin listener port, bind address, and firewalling follow
  the existing service conventions at implementation time, without
  reusing the student port, and that no gateway route prefix exposes the
  admin paths.
- Confirm the Mongo database name for the new collections at
  implementation time.

## Amendment (2026-09-30): diplomas and vocational training

The owner decided the catalog hierarchy for diplomas and vocational training:

1. **Diplomas are admin-created**: Study types stay fixed (`bachelor`, `diploma`,
   `vocational`). The bachelor levels (years 1-4) and the vocational level stay
   seeded. Under the diploma study type, the admin creates, edits, and deletes
   individual diplomas (example: a criminal-law diploma). Each diploma is a
   row in `levels` with `study_type = diploma` and a server-generated `key`.
   Inside a diploma the admin creates subjects (each with term `first` or
   `second`), then videos and files, exactly as for bachelor subjects.
   Deleting a diploma is blocked while it has subjects. Levels with no
   published subjects are hidden from `GET /academy/levels`.
2. **Vocational training**: Vocational training is one fixed level with subjects
   that have an empty `term`. The frontend hides the term filter for this
   study type.
3. **Open question 1 closed**: The seed in Phase 2.2 contains bachelor years
   1-4 and the vocational level only; individual diplomas are created by the
   admin. `levels` is no longer purely seeded.

## Amendment (2026-10-01): expiry, payment history, Android first

Owner decisions recorded in `docs/core-service/SPEC.md` (Section 1 decisions
18-20, Section 2 D20-D21, Section 3 questions 8, 12, 13 resolved and 17 added,
Section 5 `access_expires_at` / `expires_at` / `payment_records`, Section 7 R1
amendment). In short: the admin sets a subject's expiry date when creating it;
each activation copies that date and expires on it; an expired subject can be
bought and activated again; every activation writes an append-only payment
record at the subject's price; Android ships first and iOS is deferred.
Earlier decisions in this ADR are unchanged except where SPEC Section 5
replaces the unique (`user_id`, `subject_id`) entitlement index.
