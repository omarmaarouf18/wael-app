# Core Service Specification (academy-service)

- **Status**: Draft for implementation. Suggested location: `docs/core-service/SPEC.md`.
- **Date**: 2026-09-29
- **Companions**: ADR-0007 (Proposed, decision record), ADR-0008 (admin identity, written in Phase 0), ADR-0009 (file storage, written in Phase 0).
- **Reference project (read-only)**: `omarmaarouf18/saas-core`, branch `logic-exploitation`. Copy patterns, never business logic (wallets, escrow, KYC, chat, tenants do not apply here).

## 0. How to use this document

This is the build contract for the core of the application. It is written for implementing agents and for the owner.

1. **Section 1 (Owner decisions) is locked.** An agent may not change, reinterpret, or extend it.
2. **Section 2 (Defaults) is this spec's own choices.** They exist so work can start; only the owner may override them. Overrides are recorded as a dated note in Section 2, never silently in code.
3. **Section 3 (Open questions) must not be implemented.** If a task needs one of them, stop and report which one.
4. Work in the phases of Section 11, one task per session, one logical change per commit, gates from `CLAUDE.md`.
5. Never claim behavior you did not read in code. Never fabricate command output. Commit hash rules: see CLAUDE.md, section 'Commit hashes in Markdown'.

## 1. Owner decisions (locked)

**Product**
1. A single-teacher learning platform. Students only receive content; they upload nothing.
2. Catalog tree: **study type -> level/programme -> subject**. Study types and levels are **fixed** (seeded, never admin-edited): bachelor (four years), diplomas, vocational training. Each type has different subjects.
   - *Amended 2026-09-30 (owner decision)*: Study types stay fixed. The bachelor levels (years 1-4) and the vocational level stay seeded. Under the diploma study type, the admin creates, edits, and deletes individual diplomas (example: a criminal-law diploma). Each diploma is a row in `levels` with `study_type = diploma` and a server-generated `key`. Inside a diploma the admin creates subjects (each with term first or second), then videos and files, exactly as for bachelor subjects. Deleting a diploma is blocked while it has subjects. Levels with no published subjects are hidden from `GET /academy/levels`. Vocational training is one fixed level with subjects that have an empty `term`; the frontend hides the term filter for this study type.
   - *Amended 2026-10-02 (owner decision)*: The catalog axes are always visible. `GET /academy/levels` returns all three study types in the fixed order bachelor, diploma, vocational, each with all of its levels (bachelor years 1-4, every admin-created diploma, the vocational level), whether or not they have published subjects. The diploma study type is present with an empty `levels` list when no diploma exists. This replaces the sentence "Levels with no published subjects are hidden from `GET /academy/levels`" in the 2026-09-30 amendment above (kept visible). Subject lists and subject details still hide unpublished subjects. The app always shows the three study-type tabs; a study type or level with nothing in it shows an honest empty state ("No diplomas yet" for the diploma tab, "No subjects yet" for a level).
3. The admin creates **subjects** inside a level. A subject has an admin-set price, a description, and a term (first or second) where applicable.
4. A subject contains **videos** (unlisted YouTube references, each with a title and a description) and **PDF files** (books and study notes).
5. **Owning a subject grants all its videos and PDFs automatically.** PDFs are not sold separately. PDFs can be downloaded to the student's device from inside the app.
6. Removed from scope: live events, progress tracking on the server, ratings, student counts, view counts, free previews.
7. Content is bilingual (Arabic and English), Arabic preferred.
8. Payment happens outside the app. The payment flow itself is **out of scope** and the owner will specify it later. The only boundary: an admin-accepted request creates the entitlement. A rejected request may be resubmitted as a new request.
9. A student who opens a locked subject automatically creates an access request (no manual step). The app then shows a "contact support to activate" message with a WhatsApp link (ADR-0006).
10. Support is WhatsApp only (ADR-0006).

**Accounts**
11. Registration requires **full name and phone number**. The OTP goes to **email only** for now; the phone is collected but not verified.
12. The admin can **suspend, reactivate, and delete** student accounts. Suspension and deletion are for abuse, content leakage, or suspicious behavior.
13. Suspension takes effect immediately on already-issued tokens.

**Admin**
14. Admin identity is **not an account**. It is a **named token issued only by a server-side CLI tool**, separate from student auth.
15. The admin panel is a web app on a **subdomain of the same server** (for example `admin.<domain>`), following the saas-core reviewer-console pattern.
16. The admin can: create and edit subjects and prices; add, edit, reorder, and delete videos and files; review and accept or reject access requests; suspend, reactivate, and delete accounts; send app-wide notifications; edit app UI text and images (the last one is a later phase, see Section 11).
17. Admin endpoints are never reachable through the student gateway routes and never authenticated by a student JWT.

*Amended 2026-10-01 (owner decisions; earlier text above unchanged):*
18. **Subscriptions expire.** The admin sets an expiry date for a subject in the admin panel when creating it (typically about one week after the end of the term) and can change it later for the next run of the same subject. An expired subject is locked again for the student. The student may buy it again; the admin activates it normally. A request for an old term's subject from a student who has moved on is ignored by the admin (a human decision, no system rule).
19. **Every activation is recorded financially, as history.** The recorded amount is the subject's price as set by the admin at the moment of activation; the admin does not type an amount. This applies to accepted requests and to manual grants.
20. **Android first.** iOS is deferred (Section 3 question 8).
21. **Video IDs released only at play time.** YouTube video IDs are never returned in catalog or subject metadata (subject detail returns `playable: bool` where `playable = owned now AND video published AND youtube_video_id non-empty`). Video IDs are released only by `POST /academy/videos/{id}/play` at play time when the student owns the subject and it is published. Target audience is ordinary students; protected host is a later option. Every 200 from `/play` writes an append-only `video_plays` record (no IP) for audit; write failure is logged with IDs only and never blocks playback.
22. **Device cap per account.** At most 2 signed-in devices per account concurrently, newest wins, no monthly cap. (added 2026-10-01)

## 2. Defaults chosen by this spec (owner may override)

| # | Topic | Default |
|---|---|---|
| D1 | Titles/descriptions | Arabic required, English optional (API falls back to Arabic). |
| D2 | Price | Integer in whole EGP (`price`), currency fixed to `EGP`. |
| D3 | Price visibility to students | **Hidden** by default (`EXPOSE_PRICE_TO_STUDENTS=false`), reversible by config. Reason: unresolved iOS review risk (Section 3). |
| D4 | Locked-subject view | Student sees titles and descriptions of videos and files, but never `youtube_video_id` or download access. |
| D5 | File kinds | `subject_files.kind` is `book` or `note`; format is PDF only. "Classes" (حصص) is not modeled: it means videos. |
| D6 | Video duration | Not stored. |
| D7 | Phone | Normalized to E.164, default region `EG` (config). Unique per active account. |
| D8 | Account delete | Soft delete (`status = deleted`) plus a blocklist of SHA-256 hashes of normalized email and phone so an abused identity cannot re-register. |
| D9 | Entitlement revocation | Allowed by admin, reason mandatory, audited. |
| D10 | Learning progress | Local to the device only; no server field. |
| D11 | Video ordering | `position` integer, no unique index (reorder rewrites positions in one bulk write; code enforces contiguity). This replaces the unique compound index in ADR-0007 decision 12. |
| D12 | Levels seed | Bachelor years 1-4 only until the diploma/vocational lists are provided. *(Amended 2026-10-02: resolved — the seed holds bachelor years 1-4 and the single vocational level; diplomas are admin-created under the fixed diploma study type; see Section 1 decision 2.)* |
| D13 | Rate-limit tiers (ADR-0016 in saas-core) | Read 30/min, download 10/min, access-request and admin writes 5/min, per user (or per admin). Configurable. *(Amended 2026-10-01)*: Read default raised to 120/min; play video (`/academy/videos/{id}/play`) given its own tier Play with default 60/min (`RATE_LIMIT_PLAY`). Configurable via env (`RATE_LIMIT_READ`, `RATE_LIMIT_PLAY`, etc.); owner may change. |
| D14 | Max PDF size | 50 MB (`MAX_PDF_BYTES`). |
| D15 | Blocklist hashing | **Supersedes the hashing in D8.** Blocklist hashes use HMAC-SHA256 with a secret key from env (required in production), not plain SHA-256. |
| D16 | Admin verify lockout IP | The lockout keys on a client IP derived from a trusted proxy header that only Caddy and admin-console can set; the trust chain is documented. *(2026-10-01, owner amendment)*: Trusted header is `X-Admin-Client-IP`. Trust chain: Caddy -> admin-console sets `X-Admin-Client-IP` (overwriting any client-supplied value) -> auth-service admin listener (`:9001`, internal network only). auth-service reads `X-Admin-Client-IP` ONLY on the admin listener and ONLY after `X-Internal-Token` is valid (constant-time); if absent, it falls back to the connection `RemoteAddr` host; it never reads `X-Forwarded-For` on the admin listener. No IP allowlist for now. |
| D17 | Admin-console delivery | Served with a strict `Content-Security-Policy` and no third-party scripts. |
| D18 | Suspension and SSE | Suspension also closes the account's open SSE streams. |
| D19 | SSE auth migration | Once Q9 is settled, notification-service stops accepting `?token=` (the Flutter client already sends the `Authorization` header). |

*D15, D17-D19 added 2026-09-30 (owner review); D16 amended 2026-10-01 (owner) and implemented in Phase 1.4; D13 amended 2026-10-01 (owner, dated defaults for Read 120/min and Play 60/min).*


| # | Topic | Default |
|---|---|---|
| D20 | Activation of an expired subject | The server refuses to accept a request or grant a subject while the subject's `access_expires_at` is in the past (409, generic message); the admin sets the new date first. No activation that is already expired, and no payment record for it, is ever created. The automatic access request of decision 9 is not created for such a subject. |
| D21 | Expiry per activation | Each entitlement stores its own `expires_at`, copied from the subject's `access_expires_at` at activation. Moving the subject's date later does not revive expired entitlements; only a new paid activation gets the new date. |
| D22 | Code issuance and failure caps | Reset code issuance is capped at 1 code per 60 s cooldown (`CodeCooldown = 60 s`) and max 5 codes per rolling hour window (`MaxCodesPerHour = 5`) per email. Verification failures across all codes for an email are capped at 15 wrong codes per rolling hour (`MaxFailuresPerHour = 15`), returning 429 `too_many_attempts`. Retains existing 5 wrong tries per single code and code TTL 10 min. *(added 2026-10-02, QA H1)* |
| D23a | Device definition and enforcement | A "device" is a login session owned by auth-service. The cap is enforced where sessions are created (`Login`, `VerifyOTP`). `Refresh` keeps the session. *(added 2026-10-02)* |
| D23b | Replacement policy | A 3rd device signs in successfully; the other session with the oldest `last_used_at` is ended. Re-login from the same `device_id` replaces its own session and does not take a second slot. *(added 2026-10-02)* |
| D23c | Replaced session response | The ended device's next refresh gets 401 code `session_replaced`; its access token is rejected immediately. App message (owner wording): ar: "عفوًا، لقد تجاوزت الحد المسموح لاستخدام هذا الحساب", en: "Sorry, this account's usage limit has been exceeded". *(added 2026-10-02)* |
| D23d | Device ID generation and validation | `device_id`: random UUIDv4 created by the app on first launch, kept in secure storage, sent in `Login` and `VerifyOTP` bodies with optional `device_label` (<=64 runes, trimmed). No fingerprinting. Reinstall = new device. Missing or invalid `device_id` -> 400. *(added 2026-10-02)* |
| D23e | Sessions storage | Mongo `sessions` (auth-service): `_id` (`sid`, UUID), `user_id`, `device_id`, `device_label`, `refresh_hash`, `created_at`, `last_used_at`, `ended_at`, `end_reason` (`replaced`\|`logout`\|`admin`). No IP. Index `(user_id, ended_at, last_used_at)`. TTL index deletes rows 30 days after `ended_at`. *(added 2026-10-02)* |
| D24 | Password length (bcrypt) | Passwords are validated in bytes: min 8, max 72 (an Arabic character is 2 bytes). Longer passwords return 400 code `password_too_long`, never 500. Length is validated before a reset token is consumed, so a bad password never burns the token. Existing hashes are unchanged. *(added 2026-10-03, owner brief)* |
| D25 | Unverified signup replacement, resend, expiry, pending binding | An unverified signup reserves nothing: a new signup with the same email replaces the record in place (new name, phone, password, fresh `created_at`, rotated `pending_id`, new OTP; old OTP invalid). Only verified accounts count for email/phone uniqueness (verified duplicates get the generic 409). Unverified records expire after 24 h (Mongo TTL index on `created_at` with `email_verified != true`, plus lazy handler check). `POST /auth/signup/resend {email}` is always generic 200; it sends a new OTP (10 min TTL) only for a live unverified active signup, with 60 s cooldown and max 5/hour per email plus the gateway per-IP tier. Signup returns `pending_id` (>=128-bit random, stored hashed); `POST /auth/verify-otp` must send it; replacement rotates it (old id + new code fails generic 401); resend keeps it; verification clears it. *(added 2026-10-03, owner brief)* |
| D26 | Login lockout (no IP-wide lock) | `(email, IP)`: 5 failures in a row lock that pair for 15 min. `(email)` across all IPs: 20 failures in 1 h lock the account for 1 h. No lockout keyed on IP alone; IP volume is handled only by the gateway rate limit. Successful login clears the `(email, IP)` counter; successful password reset clears both locks for that email. Locked responses are generic 429 code `too_many_attempts` with a `Retry-After` header and do not reveal whether the email exists; unknown emails go through the same counters (plus a dummy bcrypt compare, S4). Counters live in Redis with TTLs. *(added 2026-10-03, owner brief)* |

*D20-D21 added 2026-10-01 to implement owner decisions 18-19; D22 added 2026-10-02 (QA H1); D23a-e added 2026-10-02 (owner decision 2026-10-01); D24-D26 added 2026-10-03 (owner brief: auth hardening + QA Lane A); owner may override.*

## 3. Open questions (do not implement)

1. **[Resolved 2026-09-30]** The exact fixed lists of diplomas and vocational programmes (blocks the seed only). Closed by owner decisions: diplomas are admin-created under the fixed diploma study type; vocational training is one fixed seeded level; Phase 2.2 seed contains bachelor years 1-4 and the vocational level only. See Section 1 decision 2 amendment and ADR-0007 (2026-09-30 amendment).
2. Per-user watermark on downloaded PDFs (leak traceability). Design downloads so this stays possible (Section 7, R6).
3. Where PDFs live long term (ADR-0009 decides the first answer: local encrypted storage).
4. Whether English content becomes mandatory.
5. Phone OTP (SMS or WhatsApp) and its provider.
6. Hard delete after a soft-delete retention period.
7. Payment flow (owner will specify).
8. **[Resolved 2026-10-01: Android first, iOS deferred; see Section 1 decision 20]** **iOS App Store review**: unlocking content after an outside payment can be rejected outside the US storefront. Decide between Android first, in-app purchase on iOS, or accepting the risk. Does not block the backend.
9. SSE authentication (the stream currently takes the JWT in `?token=`); a short-lived one-time ticket is the leading option.
10. Refresh-token reuse detection (revoking a session when an old refresh token is replayed).
11. Whether "new lesson" notifications go to the subject's owners (needs the targeted-audience broadcast of Phase 7).
12. **[Resolved 2026-10-01: recorded as append-only history at the subject's price; see Section 1 decision 19 and `payment_records` in Section 5]** A financial record per entitlement (amount paid, payment method, optional reference, `price_at_grant`), required for the client's per-subject financial reports. Payment flow stays out of scope; this is only the accounting record.
13. **[Resolved 2026-10-01: yes, expiry date set per subject by the admin, copied per activation; see Section 1 decision 18, D20-D21]** Do subject subscriptions expire (per term or year)? `entitlements` currently have no `expires_at`.
14. Student self-service account deletion (store requirement, to verify against current Apple and Google rules) and how it differs from an admin ban (no blocklist for self-deletion).
15. Student community group, external link or in-app.
16. Should decision 9 keep creating an access request automatically when a student opens a locked subject (queue noise)?

*Questions 12-16 added 2026-09-30 (owner review).*
17. Reminder notification to the student before a subscription expires: yes or no, and how many days before. *(added 2026-10-01)*

## 4. Architecture

```
Student app --HTTPS--> Caddy (api.<domain>) --> api-gateway --> auth-service
                                                             --> academy-service (student routes)
                                                             --> notification-service

Admin browser --HTTPS--> Caddy (admin.<domain>) --> admin-console
   (holds only X-Admin-Token in tab memory)             |  adds X-Internal-Token, docker network only
                                                        v
                          /internal/admin/* on auth-service, academy-service, notification-service
```

| Component | Change |
|---|---|
| `services/auth-service` | User gets `full_name`, `phone`, `status`. Status gate on token paths. `admins` collection, admin CLIs, `/internal/admin/*` endpoints. |
| `services/academy-service` | **New.** Catalog, entitlements, access requests, files, admin CRUD. Same layout as auth-service: `cmd/main.go` + `internal/{config,handlers,models,store}`, `Store` interface with `MemoryStore` and `MongoStore`. |
| `services/notification-service` | Stream caps (rate + concurrent). Later: batch push for broadcast. |
| `services/api-gateway` | New route `/api/v1/academy/` (prefix and `StripPrefix: "/api/v1"` as existing). **Must stop injecting `X-Internal-Token`** (Phase 0). |
| `services/admin-console` | **New.** Thin Go service + static UI. Proxies to internal admin endpoints. Makes no authorization decisions. |
| `shared/infra/storage` | Restored from saas-core (local disk, AES-256-GCM at rest). |

**Path rule.** Every admin and service-to-service endpoint lives under `/internal/`. The gateway has no `/internal/` route, and a test asserts it. (This follows the existing `/internal/push` convention and is stricter than saas-core, which put reviewer endpoints under `/auth/`.)

**Listener rule** (ADR-0007 decision 15). Admin endpoints are served by a **second HTTP listener** per service (`ADMIN_LISTEN_ADDR`), bound to the internal docker network and never published on the host.

## 5. Data model

Field names are snake_case everywhere, including `youtube_video_id`.

### academy-service database

| Collection | Fields | Indexes |
|---|---|---|
| `levels` | `key`, `study_type` (`bachelor`/`diploma`/`vocational`), `title_ar`, `title_en`, `position` | unique `key` |
| `subjects` | `_id`, `level_key`, `term` (`first`/`second`/empty), `title_ar`, `title_en`, `description_ar`, `description_en`, `price`, `status` (`draft`/`published`), `created_at`, `updated_at` | (`level_key`, `status`) |
| `videos` | `_id`, `subject_id`, `position`, `title_ar`, `title_en`, `description_ar`, `description_en`, `youtube_video_id` | (`subject_id`, `position`) non-unique |
| `subject_files` | `_id`, `subject_id`, `kind` (`book`/`note`), `title_ar`, `title_en`, `size_bytes`, `storage_key`, `created_at` | `subject_id` |
| `entitlements` | `_id`, `user_id`, `subject_id`, `granted_at`, `source` (`request`/`admin_grant`), `granted_by`, `request_id` | **unique** (`user_id`, `subject_id`); `user_id` |
| `payment_records` *(added 2026-10-01, decision 19)* | `_id`, `user_id`, `subject_id`, `entitlement_id`, `request_id` (optional), `amount` (integer EGP, equals `price_at_grant`), `price_at_grant`, `source` (`request`/`admin_grant`), `recorded_by`, `recorded_at`, `corrects_id` (optional) | (`subject_id`, `recorded_at`); (`user_id`, `recorded_at`) |
| `purchase_requests` | `_id`, `user_id`, `subject_id`, `status` (`pending`/`accepted`/`rejected`), `created_at`, `decided_at`, `decided_by`, `reject_reason` | **partial unique** (`user_id`, `subject_id`) where `status = pending`; `status` |
| `video_plays` *(added 2026-10-01, decision 21)* | `_id`, `user_id`, `video_id`, `subject_id`, `played_at` | (`user_id`, `played_at`); (`video_id`, `played_at`) |
| `admin_audit_log` | `_id`, `actor_id`, `actor_name`, `action`, `target_type`, `target_id`, `detail`, `created_at` | (`actor_id`, `created_at`); (`target_type`, `target_id`) |

Notes:
- *Amended 2026-10-01 (decisions 18-19, D20-D21):* `subjects` gains `access_expires_at` (date-time, Africa/Cairo, required on create, editable). `entitlements` gains `expires_at` (copied from the subject at activation); the unique (`user_id`, `subject_id`) index above is replaced by a non-unique (`user_id`, `subject_id`, `expires_at`) index, and the store guarantees at most one unexpired entitlement per (`user_id`, `subject_id`). `payment_records` is append-only: rows are never edited or deleted; a correction is a new row with `corrects_id` plus an audit-log entry. Students never see `payment_records`.
- `video_plays` is an append-only playback log written on every 200 from `POST /academy/videos/{id}/play`. It stores **no IP addresses** (privacy). Failures to write the play log are logged (IDs only) and never block student playback.
- `levels` is no longer purely seeded (amended 2026-09-30): bachelor years 1-4 and the vocational level are seeded, while diplomas are created, edited, and deleted by the admin (`study_type = diploma`, server-generated `key`). Deleting a diploma is blocked while it has subjects.
- `storage_key` is a server-generated UUID. Never derive it from a user-supplied filename.
- The audit log **does not store IP addresses** (saas-core removed IP persistence for privacy). Reasons must be length-capped (1-1000) and stripped of CR/LF before any log line.
- Student API responses are **DTOs**, never raw models. `storage_key`, `granted_by`, `decided_by`, and audit data never reach students.

### auth-service additions

- `users`: `full_name`, `phone` (normalized), `status` (`active`/`suspended`/`deleted`), `status_reason`, `suspended_at`, `reactivated_at`, `deleted_at`. Unique partial index on `phone` for non-deleted accounts. Existing documents with no `status` are treated as `active` via a helper such as `EffectiveStatus()`.
- `admins`: `_id` (admin id), `name`, `token_hash` (SHA-256, never plaintext), `created_at`, `expires_at`, `revoked_at`.
- `blocklist`: `kind` (`email`/`phone`), `hash`, `reason`, `created_at`; unique (`kind`, `hash`).
- `admin_audit_log` *(added 2026-10-01, owner)*: `_id`, `actor_id`, `actor_name`, `action`, `target_type`, `target_id`, `detail`, `created_at`; no IP. Compound indexes on (`actor_id`, `created_at`) and (`target_type`, `target_id`).
- `sessions` *(added 2026-10-02, owner decision 2026-10-01, Phase 1.7)*: `_id` (`sid`, UUID), `user_id`, `device_id` (UUID), `device_label` (string, <=64 runes), `refresh_hash`, `created_at`, `last_used_at`, `ended_at` (nullable), `end_reason` (`replaced`|`logout`|`admin`, nullable). No IP addresses. Compound index on (`user_id`, `ended_at`, `last_used_at`). TTL index deletes rows 30 days after `ended_at`.

## 6. API

Student routes are served through the gateway as `/api/v1/auth/...` and `/api/v1/academy/...` (the services see `/auth/...` and `/academy/...`). All require `X-Gateway-Secret` (existing `GatewayAuth`) and a valid Bearer JWT validated with `jwtutil.ValidateToken` on **every request** (this is what makes suspension and session replacement immediate).

### Student (auth-service additions, Phase 1.7)

| Method and path | Purpose | Notes |
|---|---|---|
| `POST /auth/signup` | Register unverified account | Returns `pending_id` (>=128-bit, D25); unverified replaces unverified (D25); password bytes <= 72 else 400 `password_too_long` (D24) |
| `POST /auth/signup/resend` | Resend signup OTP | Always generic 200; sends only for live unverified active; 60 s cooldown, 5/hour/email; keeps `pending_id` (D25) |
| `POST /auth/login` | Authenticate with email+password | Requires `device_id` (UUIDv4) and optional `device_label` (string, <=64 runes) in body. Enforces 2-device cap (newest wins, oldest ended with `end_reason=replaced`, refresh deleted, session revoked). Lockout per D26 (429 `too_many_attempts` + `Retry-After`); dummy bcrypt compare for unknown emails |
| `POST /auth/verify-otp` | Verify OTP code | Requires `device_id` (UUIDv4), optional `device_label` (<=64 runes), and `pending_id` (D25). Enforces 2-device cap (same semantics as login) |
| `POST /auth/reset/request` | Request password-reset code | Always generic 200; sends only for `active` accounts, in background (detached 10 s context, no PII in failure logs) |
| `POST /auth/reset/verify` | Verify reset code | Mints single-use 10-minute reset token |
| `POST /auth/reset/confirm` | Set new password | Validates length before consuming the token (atomic `Take`); ends all sessions, no tokens issued (R7 amendment) |
| `POST /auth/refresh` | Rotate refresh token | Keeps session, updates `last_used_at` and `refresh_hash`. Returns 401 code `session_replaced` if session ended with reason `replaced` |
| `POST /auth/logout` | End current session | Authenticated (student JWT Bearer). Ends caller's session (`end_reason=logout`), deletes its refresh key, revokes session via `RevokeSession(sid)`. 204 No Content |

### Student (academy-service)

| Method and path | Purpose | Notes |
|---|---|---|
| `GET /academy/levels` | Fixed tree of study types and levels | Read tier |
| `GET /academy/subjects?level=<key>&term=<t>` | Published subjects of a level | Each item has `owned` and `counts` (`videos`, `books`, `notes`) |
| `GET /academy/subjects/{id}` | Subject detail | Shape below. 404 for unknown or unpublished |
| `POST /academy/subjects/{id}/access-request` | Idempotent access request | Returns the existing pending request if there is one. Write tier |
| `POST /academy/videos/{id}/play` | Play video | Returns `{"video_id", "youtube_video_id"}` with `Cache-Control: private, no-store`. 404 if unowned, expired, unpublished, or empty ID. Appends to `video_plays` (best-effort, fail-open for playback). Play tier |
| `GET /academy/subjects/{id}/files/{fileId}/download` | Stream a PDF | 403 unless owned. Entitlement checked on every call. Download tier |
| `GET /academy/me/entitlements` | Owned subject ids | |

*Amended 2026-10-02 (owner decision, Section 1 decision 2):* `GET /academy/levels` always returns the three study types, `bachelor`, `diploma`, `vocational`, in that order, each as `{"key", "title": {"ar","en"}, "levels": [...]}` with all of its levels ordered by `position`, then `key`; `levels` is an empty array (never `null`) for a study type with none. The flat `levels` list holds the same levels in that tree order. A level appears whether or not it has published subjects; the table's earlier "Fixed tree" wording stands. The response carries no subject, count, price or ownership data. Subject lists and details are unchanged (published subjects only).

Subject detail, owned:

```json
{
  "id": "...", "level_key": "bachelor-y3", "term": "first",
  "title": {"ar": "...", "en": "..."}, "description": {"ar": "...", "en": "..."},
  "owned": true,
  "counts": {"videos": 12, "books": 1, "notes": 3},
  "videos": [{"id": "...", "position": 1, "title": {"ar": "", "en": ""},
              "description": {"ar": "", "en": ""}, "playable": true}],
  "files": [{"id": "...", "kind": "note", "title": {"ar": "", "en": ""}, "size_bytes": 123456}]
}
```

Subject detail, not owned: same, but `videos[]` items carry `playable: false`, `files[]` carry no download capability, `owned` is false, and `request` is `{"status": "pending"}` or absent. `price` and `currency` appear only when `EXPOSE_PRICE_TO_STUDENTS=true`. Neither owned nor unowned detail ever includes `youtube_video_id` (amended 2026-10-01, decision 21).

The access-request response carries the request status and the support link (`whatsapp_url` built from `SUPPORT_WHATSAPP`). It contains no payment wording.

### Admin (academy-service, `/internal/admin/...` on the admin listener)

Auth on every route: `X-Internal-Token` **and** `X-Admin-Token`. The token is verified through auth-service (`POST {AUTH_ADMIN_URL}/internal/admin/verify` over mTLS, 3 s timeout, no caching so revocation is immediate). **If auth-service is unreachable, fail closed (503).** Every mutation writes one `admin_audit_log` entry in the same operation flow; an audit write failure is logged at error level and does not fail the call (same as auth-service). Audit entries follow the Section 5 schema and store no IP addresses. Levels carry a `published` flag: students see published levels only (seeded levels are published; admin diplomas start unpublished).

| Method and path | Purpose |
|---|---|
| `GET /levels` | All levels including unpublished, with subject counts |
| `POST /levels` | Create a diploma only (`study_type` must be `diploma`); server-generated key; `published` defaults to false |
| `PATCH /levels/{id}` | Rename, reorder, (un)publish. Seeded levels editable, never deleted; `study_type` immutable |
| `DELETE /levels/{id}` | Only an empty diploma. Seeded levels return 409 `level_not_deletable`; non-empty diplomas 409 `level_has_subjects` |
| `GET /subjects?level_id=&published=&page=&limit=` | Filtered subject list including drafts (limit at most 100) |
| `POST /subjects` | Create a draft (level must exist; price integer EGP >= 0; `access_expires_at` future RFC3339; `term` first/second/empty). `published: true` is refused with 409 `subject_has_no_videos` (a new subject has no videos yet) |
| `PATCH /subjects/{id}` | Same fields, all optional. A draft-to-published change requires at least one video |
| `POST /subjects/{id}/publish`, `.../unpublish` | Status change. Publishing requires at least one video (409 `subject_has_no_videos`). No hard delete: unpublish hides the subject. Owners with an active entitlement keep list/detail/`/play` access until the entitlement expires; non-owners get 404 |
| `GET /subjects/{id}/videos` | Admin video list including the YouTube id, in order |
| `POST /subjects/{id}/videos` | Add a video (`title_ar`; `youtube` URL or bare id; `order` defaults to append; optional `duration_seconds`). Only the validated 11-char id is stored (400 `invalid_youtube_id`) |
| `PATCH /videos/{id}` | Edit `title_ar`, `youtube`, `order`, `duration_seconds` |
| `POST /subjects/{id}/videos/reorder` | Body is the full ordered video-id list; missing/extra/foreign/duplicate ids return 400 `invalid_video_order` |
| `DELETE /videos/{id}` | Soft delete (hidden from students and `/play`). Deleting the last video of a published subject needs `?force=true` (409 `last_video_of_published_subject`), which also unpublishes the subject |
| `POST /subjects/{id}/files` (multipart), `PATCH/DELETE /files/{id}` | PDF files |
| `GET /requests?status=&page=&limit=` | Review queue (limit at most 100) |
| `POST /requests/{id}/accept`, `POST /requests/{id}/reject` | Reject requires a reason (1-1000). Both notify the student |
| `POST /entitlements` (grant), `DELETE /entitlements/{id}` (revoke, reason required) | Manual grant and revoke |
| `GET /audit-log?page=&limit=` | Audit trail (same `{items,total,page,limit}` shape as auth-service, newest first, limit at most 100) |

### Admin (auth-service, `/internal/admin/...`)

| Method and path | Purpose |
|---|---|
| `POST /verify` | Validate `X-Admin-Token`, return `{admin_id, name}`. Lockout after repeated failures, keyed on client IP and on the token hash (saas-core `authenticateReviewer` pattern) |
| `GET /accounts?search=&status=&page=&limit=` | Search by name, email, phone, or id |
| `POST /accounts/{id}/suspend` (reason 1-1000), `POST /accounts/{id}/reactivate` | Atomic compare-and-set. Same-state change returns 409 |
| `DELETE /accounts/{id}` (reason required) | Soft delete plus blocklist entries (D8) |
| `GET /audit-log?page=&limit=` | Audit trail (auth actions, newest first) |

## 7. Access rules (the heart)

- **R1 Ownership.** A student owns a subject if and only if an `entitlements` row exists for (`user_id`, `subject_id`).
  - *Amended 2026-10-01 (decision 18, D21):* a student owns a subject if and only if an `entitlements` row exists for (`user_id`, `subject_id`) whose `expires_at` is in the future. Checked on every request, never cached. After expiry the subject behaves exactly like an unowned subject (R2, R3, R6 apply), and the student can request it again.
- **R2 Gating.** `youtube_video_id` is returned only when R1 holds. Never on listing endpoints, never in error bodies, never in logs.
  - *Amended 2026-10-01 (decision 21):* YouTube video IDs are never returned on catalog or subject detail listing endpoints (which return `playable: bool` where `playable = owned now AND video published AND youtube_video_id non-empty`). YouTube video IDs are released only at play time via `POST /academy/videos/{id}/play` when R1 holds, the subject is published and unexpired, and `youtube_video_id` is non-empty. Never in error bodies, never in logs. Responses for 200 and 404 set `Cache-Control: private, no-store`.
- **R3 Every download re-checks R1.** No cached decision, no public or signed URL that outlives the check.
- **R4 Accept order (no Mongo transactions on a standalone node).** Upsert the entitlement first (idempotent), then compare-and-set the request from `pending` to `accepted`. If the second step fails, calling accept again is safe and completes it.
- **R5 One pending request per (student, subject).** The partial unique index enforces it; the endpoint returns the existing request instead of erroring.
- **R6 Downloads stream through academy-service** (`Content-Disposition: attachment`, `Cache-Control: private, no-store`, `X-Content-Type-Options: nosniff`). Do not hand out static URLs, so a per-user watermark can be added later without changing the API.
- **R7 Suspension.** A non-`active` account is refused on **all three token-issuing paths**: `Login`, `Refresh`, `VerifyOTP`. (`ConfirmReset` only changes the password; it issues no tokens.) Suspend also calls `jwtutil.RevokeAllUserTokens`. Because the refresh key is `refresh:<hash>` -> user id with no per-user index, the refresh gate is the status check, not deleting entries. Verify that the revocation marker TTL is at least the refresh lifetime plus the access lifetime.
  - *Amended 2026-10-03 (owner brief):* a successful `ConfirmReset` additionally ends **every** session of that user: `RevokeAllUserTokens` first (fail-closed backstop), then password update, then `EndAllUserSessions` with refresh-key deletion (fail-closed) and per-`sid` revocation. No fresh tokens are issued; the user logs in again on every device. The reset token itself is redeemed with atomic `Take` (exactly one winner under concurrency) after length validation, so a bad password never burns it.
- **R8 Unpublished subjects** are invisible to students (404), including their files.
- **R9 Deleted or suspended accounts** cannot register again with the same email or phone (blocklist), and the error is generic (no oracle).

## 8. Security requirements

1. **Gateway**: strips `X-Internal-Token` from clients and injects nothing. Test: a client-supplied internal token is removed; no internal token reaches a service.
2. **No `/internal/` route in the gateway**, asserted by a test.
3. **Empty-secret guard** on every secret comparison (an empty configured secret must never authenticate) and constant-time compare.
4. **Admin tokens**: generated with a CSPRNG, shown once, stored as SHA-256, have an expiry, and can be revoked with a CLI. Never in URLs or logs. In the browser they live in memory only.
5. **Caddy → gateway** must verify the local CA. Do not copy `tls_insecure_skip_verify` from saas-core.
6. **Uploads**: PDF only, check the `%PDF-` magic bytes, enforce `MAX_PDF_BYTES`, ignore client content-type, sanitize the download filename.
7. **Storage**: AES-256-GCM at rest; `DOCUMENT_ENCRYPTION_KEY` is required when `APP_ENV=production` (fail fast).
   - *Amended 2026-09-30 (owner decision)*: Storage: AES-256-GCM at rest. DOCUMENT_ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars) in every environment. Only APP_ENV=local or test may omit it, in which case an ephemeral key is generated with a logged warning. Any other APP_ENV value, including empty, is treated as production. An invalid key fails startup and is never padded or truncated.
8. **Logging**: never log query strings, tokens, phone numbers, or emails (use `shared/infra/redact`); strip CR/LF from anything user-controlled.
9. **Errors**: use `handlerutil.WriteSafeError`; no internal detail in responses.
10. **`--check-env`** flag on every new service (saas-core ADR-0015): loads config and exits 0 or 1 without starting anything, so a deploy can validate before it replaces running containers.
11. **Fail closed** on Redis or auth-service outage for anything that gates access.

## 9. Configuration (academy-service)

Follow the naming already used in `services/auth-service/internal/config`. Required unless a default is stated:
`APP_ENV`, listen address, `ADMIN_LISTEN_ADDR`, Mongo URI and database name, Redis URL, `GATEWAY_SECRET`, `INTERNAL_SERVICE_TOKEN`, `AUTH_SERVICE_URL`, `NOTIFICATION_SERVICE_URL`, `STORAGE_DIR`, `DOCUMENT_ENCRYPTION_KEY`, `MAX_PDF_BYTES` (default 50 MB), `SUPPORT_WHATSAPP`, `EXPOSE_PRICE_TO_STUDENTS` (default `false`), `DEFAULT_PHONE_REGION` (default `EG`, auth-service).

## 10. Testing requirements

- Every `Store` behavior is tested against both `MemoryStore` and `MongoStore` (Mongo tests skip cleanly without a database).
- Handler tests use `httptest` with `MemoryStore`.
- **Authorization table tests** for the student endpoints: owned, not owned, suspended token, unpublished subject, missing gateway secret, missing JWT.
- **Concurrency tests** with `-race`: 20 parallel access requests produce one pending row; 20 parallel accepts produce one entitlement.
- **Leak tests**: unowned subject detail, list endpoints, and all error bodies contain no `youtube_video_id`.
- Admin endpoints: missing internal token, missing admin token, wrong admin token, revoked, expired, auth-service down (503).
- Extend `tests/e2e` (env-gated, one `t.Run` per stage, never a whole-test skip for a missing optional step) to cover signup with name and phone, access request, admin accept via the internal path, and owned subject detail.

## 11. Build plan

Each numbered item is **one commit** with its own gates and its own `AI_CONTEXT.md` update. Do not start a phase before the previous one is merged.

**Phase 0 - prerequisites**
- 0.0 Public-repo hygiene: gitleaks job, `permissions: contents: read` in `ci.yml`, extended `.gitignore`. *(Added 2026-09-30, owner review; not yet implemented.)*
- 0.1 Gateway: stop injecting `X-Internal-Token`; add the two tests from Section 8.
- 0.2 Correct ADR-0007 (Section 14).
- 0.3 Write ADR-0008 (admin: console pattern, `admins`, CLIs, `/internal/admin/verify`, subdomain).
- 0.4 Write ADR-0009 and restore `shared/infra/storage` with its tests.
- 0.5 Fix the e2e test to use `t.Run` per stage.
- 0.6 Add pointer files for the other agent tools ("follow CLAUDE.md verbatim") and the no-illustrative-output rule to `CLAUDE.md`.

**Phase 1 - auth-service and notifications**
- 1.1 User fields (`full_name`, `phone`, `status`) and `EffectiveStatus()`.
- 1.2 Signup with name and phone (normalization, unique index, blocklist check).
- 1.3 Status gate on `Login`, `Refresh`, `VerifyOTP` with tests for each.
- 1.4 `admins` collection, `onboard-admin` and `revoke-admin` CLIs, `POST /internal/admin/verify` with lockout, second listener.
- 1.5 Admin account endpoints (list, suspend, reactivate, delete) with audit, student notification, and `RevokeAllUserTokens`.
- 1.6 Notification-service stream caps: per-account registration rate limit and a concurrent stream cap (pattern: saas-core `streamLimiter` and `acquireStreamSlot`).
- 1.7 Two devices per account (owner decision 2026-10-01, D23a-e): `sessions` collection in Mongo with `(user_id, ended_at, last_used_at)` index and 30-day TTL; 2-device cap enforced on `Login` and `VerifyOTP` (newest wins, oldest `ended_at` set with `end_reason=replaced`); `sid` claim in access tokens, `RevokeSession(sid)` on Redis denylist; refresh token carries `sid`, ended session returns 401 code `session_replaced`; `POST /auth/logout` ends caller's session.

**Phase 2 - academy-service read path**
- 2.1 Skeleton: config, `--check-env`, health, `Store` interface with Memory and Mongo, both listeners.
- 2.2 `levels` seed (bachelor years 1-4 and the vocational level only) and `GET /academy/levels` (levels with no published subjects are hidden). *(Amended 2026-10-02: all levels are returned, with or without published subjects; see Section 1 decision 2.)*
- 2.3 Subjects and videos models with student read endpoints (metadata only, no video IDs yet).
- 2.4 Gateway route `/api/v1/academy/` with a route test.

**Phase 3 - entitlements and gating**
- 3.1 `entitlements` store and the `owned` computation.
- 3.2 R2 gating in subject detail plus the leak tests.
- 3.3 `purchase_requests` and the idempotent access-request endpoint with the concurrency test.
- 3.4 Rate-limit tiers (D13).

**Phase 4 - academy admin surface**
- 4.1 Admin listener, verify client (fail closed), audit log.
- 4.2 Diploma create/edit/delete admin endpoints with an audit log entry per mutation (server-generated `key`; delete blocked while diploma has subjects).
- 4.3 Subject CRUD and publish.
- 4.4 Video CRUD, reorder, YouTube ID extraction.
- 4.5 Request review (R4 order), reject with reason, student notification.
- 4.6 Manual grant and revoke.

**Phase 5 - files**
- 5.1 Upload (Section 8 item 6).
- 5.2 Download streaming with R3 and R6.
- 5.3 Delete (removes the stored object).

**Phase 6 - admin console and deployment**
- 6.1 `services/admin-console` skeleton: static shell, proxy that adds the internal token, no authorization logic. *(Done 2026-10-02, held from `main` pending owner confirmation (rule 6 of Section 12). Built as the owner directed: Go standard library plus static pages modelled on the reviewer console, see ADR-0008 Section 10. It already serves the Accounts and Audit pages; the compose service, Caddy admin host, preflight checks and memory limit that Section 6.2 lists were added with it, and 6.2 is otherwise not reviewed.) (2026-10-02: owner confirmed; released to `main` as `b6a11fe`.)*
- 6.2 Compose: Caddy with a persistent certificate volume, `api.` and `admin.` hosts, `--check-env` preflight, memory limits sized for the small host, Mongo cache size set.
- 6.3 Console pages (separate spec). *(2026-10-02: the Accounts and Audit pages were built with 6.1 at the owner's direction; Requests, Catalog and Files remain, hidden in the console until their APIs exist.) (2026-10-03: the Catalog part is done on `feat/console-catalog` — diplomas/levels, subjects and videos over the Phase 4.2–4.4 academy admin API, plus the academy half of the audit log behind a source switch; Requests and Files remain hidden.)*

**Phase 7 - broadcast (write a short ADR first)**
- Admin creates a broadcast with audience `all` or `subject:<id>`. Fan-out pages through target user ids and pushes per user in bounded batches, idempotent per (broadcast, user). Failures are logged and retryable. Per-user rows are kept, because the existing list, read, and SSE path serves per-user rows.

**Phase 8 - hardening and release gate**
- Short `RELEASE-GATE.md`, `RUNBOOK.md`, single source of truth for the production `.env`, staging notes.

## 12. Rules for the implementing agent

1. Read `CLAUDE.md`, `AI_CONTEXT.md`, this spec, and ADR-0001 to ADR-0009 before touching code.
2. One task per session. Edit only files inside the task's scope.
3. If a task touches an Open Question or contradicts a locked decision, stop and report. Do not resolve it.
4. Run the gates for every touched module and paste the real output. If you did not capture a line, say so.
5. Never rewrite history, never amend a commit whose hash is cited in a document. For the push policy, see CLAUDE.md Auto-push (owner amendment 2026-09-30).
6. Access-control changes (suspension, gating, admin auth) are held from `main` until the owner confirms (saas-core ADR-0022 "Deployment Gate").
7. Report: files changed (`git show --stat`), gates, deviations, anything unverified.

## 13. Reference implementation pointers (saas-core, read-only)

| Topic | Where |
|---|---|
| Two-token admin auth, lockout | `services/auth-service/internal/handlers/auth.go` (`authenticateReviewer`) |
| Token issuance CLI, hash at rest | `services/auth-service/cmd/onboard-reviewer/main.go`, `internal/store/mongodb.go` (`AddReviewer`) |
| Suspension design | `docs/adr/0022-account-suspension-and-reviewer-directory.md` |
| Console architecture | `docs/adr/0021-kyc-kyb-kye-reviewer-console.md` |
| Encrypted storage | `shared/infra/storage/storage.go` |
| Pre-flight env check, rollback | `docs/adr/0015-strict-cd-preflight-validation.md` |
| Tiered rate limits | `docs/adr/0016-tiered-rate-limits-for-ux-over-uniform-security-floor.md` |
| Stream caps | `services/notification-service/internal/handlers/handlers.go` (`Stream`) |

## 14. Corrections ADR-0007 needs (Phase 0.2)

1. Decision 13: remove `ConfirmReset` from the token-issuing paths. The paths are `Login`, `Refresh`, `VerifyOTP`.
2. Decision 18: remove "refresh entries are deleted"; rely on the status check in `Refresh`. Do not make a Decision depend on the "Proposed mechanism (not decided)" section: either move the mechanism into Decision or state the decision without it.
3. Add the broadcast design (Phase 7) or point to a dedicated ADR.
4. Decision 12: use `youtube_video_id` (snake_case), remove `duration_sec`, make the `purchase_requests` pending index a partial unique index, and replace the unique (`subject_id`, `position`) index with a plain one (D11).
5. Add `term` to `subjects` and `kind` to `subject_files`.
6. Record the admin-panel decision (subdomain, admin-console, `/internal/admin/*`) or point to ADR-0008.

## 15. Owner review notes (2026-09-30)

Dated notes from the owner's review. Nothing from this review has been
implemented yet; D15-D19 (Section 2), questions 12-16 (Section 3), and
task 0.0 (Section 11) were added with it.

1. **Leak response**: when a lesson video leaks, replace the video and change
   `youtube_video_id`. Recorded as a dated note in ADR-0001. No RUNBOOK
   exists yet; when Phase 8 writes `RUNBOOK.md`, it must carry the same
   leak-response note.
2. **ADR numbering**: ADR-0007 reserves ADR-0008 (admin identity; since
   written in Phase 0.3), ADR-0009 (file storage) and ADR-0010 (app
   content). The new ADRs for WhatsApp OTP and for client
   ownership use numbers after 0010.
3. **ADR-0003 amendment applied** (docs only): payment is manual via
   InstaPay or e-wallets, with admin activation. The payment flow itself
   stays out of scope (decision 8); the refund policy stays open.
