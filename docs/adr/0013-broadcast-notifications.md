# ADR-0013: Admin Broadcast Notifications (SPEC Phase 7)

- **Status**: Accepted
- **Date**: 2026-10-12
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

SPEC Section 1 decision 16 (locked) says the admin can "send app-wide
notifications". SPEC Section 11 Phase 7 requires a short ADR first, then:
admin creates a broadcast with audience `all` or `subject:<id>`; fan-out
pages through target user ids and pushes per user in bounded batches,
idempotent per (broadcast, user); failures are logged and retryable;
per-user rows are kept because the existing list, read and SSE path serves
per-user rows. Owner decisions 2026-10-08 (admin sends from the console) and
2026-10-12 (v1 needs no app change) scope this record.

## Decision

1. Audiences: `all` (every active student) and `subject:<id>` (students
   with an active entitlement for that subject: active flag true,
   `expires_at` in the future, no `revoked_at` marker — the same rule the
   academy uses everywhere, SPEC R1).
2. `all` excludes suspended accounts and accounts in `pending_deletion`.
   This is decided at resolution time by filtering on the auth-service
   account status (`status=active`, empty treated as active).
3. Resolution pages through user ids and never loads all users or
   documents into memory at once: `all` pages auth-service
   `GET /internal/admin/accounts?status=active`; `subject:<id>` pages a new
   academy-service internal route returning active owner ids per subject.
4. A `broadcasts` collection holds one record per send: id, audience,
   titles/bodies (ar required, en optional), subject id, creating admin id,
   created time, status (`queued`, `sending`, `done`, `partial`, `failed`),
   and total/delivered/failed counts.
5. Per-user rows go into the existing `notifications` collection, so list,
   read and SSE stay unchanged. Row ids are deterministic per
   (broadcast, user), and a `broadcast_targets` collection with a unique
   per-(broadcast, user) key makes redelivery idempotent: a retry, a
   double send or a restart mid-fan-out never creates a second row.
6. Fan-out runs in bounded batches through the existing per-user
   create-then-publish path, so open SSE streams receive the row live.
   A broadcast left in `sending` resumes after a restart. One user's
   failure is recorded on its target row and never stops the broadcast;
   failed targets can be retried.
7. Validation is server side (console checks are a convenience only):
   Arabic title and body required; title at most 80 runes, body at most
   500 runes; control characters rejected; the shared store-safety word
   rule (no payment wording) enforced on every text field; `subject_id`
   must exist (subjects have no deleted flag; existence means not
   deleted). An empty audience is refused with a clear error and nothing
   is sent.
8. Admin API lives on a new notification-service admin listener
   (`:9003`, internal only, never published) under `/internal/admin/*`,
   with the same auth chain as the other admin routes: internal token,
   per-request admin-token verify via auth-service (no caching, fail
   closed), lockout relayed from auth-service, one `admin_audit_log`
   entry per send and per retry (admin, audience, counts — never the
   message text; no IP persisted).
9. Sends are rate limited to 10 per hour per admin, answered 429 with
   `Retry-After`.
10. Type is `event` (the app styles it with its own icon). Arabic is
    required, English optional; when English is empty the base
    title/body carry the Arabic text so the English UI never shows an
    empty row.
11. In-app only: there is NO phone push (no FCM/APNs). Students see the
    notification inside the app — next time they open it, or live if it
    is open. Real push is a later, separate task that needs app changes.

## Consequences

### Positive

- Owners and all students are reachable from the console without any app
  change; the app's list, unread dot and live stream work as before.
- Idempotent fan-out with resume makes large sends safe across retries
  and restarts.
- Audit and rate limits match the existing admin-route posture.

### Negative and Tradeoffs

- No phone push: a student who never opens the app never sees the
  message (stated on the compose screen and in the report).
- Audience membership is resolved at send time; accounts that change
  status between preview and send may shift the final count.
- A sent broadcast cannot be withdrawn in v1 (open owner question).

## Alternatives Considered

- Reusing `/internal/push` in a loop from the console: no record, no
  idempotency, no resume, no audit — rejected.
- Resolving `subject:<id>` by paging all active accounts and checking
  entitlements per user: N+1 upstream calls per send — rejected in favour
  of the paged owner-ids route.

## Open Questions

- SPEC Section 3 question 11: automatic "new lesson" notifications to
  subject owners. Not built here.
- Whether a sent broadcast can be withdrawn. Not built in v1.

## To verify

- Check a broadcast row carries exactly the fields the app parses
  (title, title_ar, body, body_ar, type, created_at, read, subject_id).
- Check Arabic-only input fills the base title/body.
- Check retry after a mid-fan-out restart delivers exactly one row per
  user.
- Check the audit entry carries counts but no message text.
