# Orders API contract (academy-service)

- **Status**: Frozen for the 2026-11-05 parallel batch (agents 3 console and 4
  website build against it).
- **Date**: 2026-11-05.
- **Owners**: agent 1 (orders backend). Console UI: agent 3. Website cart and
  buy pages: agent 4.
- **Related**: ADR-0014 (W4, W5 and the amendments of 2026-11-05), SPEC
  Section 5 (`orders`, `purchase_requests`) and Section 6 (student and admin
  routes).

This document is additive after its first push: fields may be added, and
nothing is removed or renamed without a new dated amendment. Every change is
logged at the bottom.

## 1. Scope

An **order** is one payment event. It owns one or more purchase requests, one
per subject. Two sources exist:

- `web`: a website cart checkout. One order, one transfer, 1-50 subjects.
- `app`: the mobile access request flow. The app never sends an order id and
  never gets a cart; the server groups consecutive app requests of one student
  into one **open** app order automatically.

Web and app orders never mix. A web checkout always creates its own order,
even while the student has an open app order.

`payment_records` and the grant path are unchanged: one payment record per
granted subject with `price_at_grant` (owner decision 19). The amount the
student typed lives on the order only (`amount_paid`).

## 2. Transport, auth and common rules

- Routes below are academy-service paths. Through the gateway they are
  prefixed with `/api/v1`.
- Student routes require `X-Gateway-Secret` and a student Bearer JWT.
- `POST /academy/orders` additionally requires `claims.client == "web"`.
  A mobile token gets `403` code `web_client_required`.
- Admin routes are served on the internal admin listener only, behind
  `X-Internal-Token` and `X-Admin-Token` (unchanged). They are never exposed
  through the gateway.
- Student JSON bodies use `DisallowUnknownFields()`; an unknown field is
  `400` code `invalid_json`.
- Money is an integer in whole EGP.
- Timestamps are RFC 3339 UTC strings.
- The **normalized transfer reference** is the trimmed reference upper-cased
  (`strings.ToUpper`). It is unique across every order and every legacy
  single-subject request. Reuse is `409` code `duplicate_reference`.

### 2.1 Student error body

Business errors use the typed bilingual shape (the same shape the W4 purchase
request already returns):

```json
{
  "error": "already_owned",
  "code": "already_owned",
  "message_ar": "أنت مشترك في مادة «...» بالفعل",
  "message_en": "You already own the subject \"...\"",
  "subject_id": "subj-123"
}
```

`subject_id` is present when the failure names one subject. It is absent for
order-wide failures.

Generic middleware failures (missing gateway secret, missing/invalid JWT, rate
limit, 5xx) keep the existing `{error, code, request_id}` shape.

## 3. Student routes

### 3.1 `POST /academy/orders` (web cart checkout)

Creates one order and one `purchase_requests` row per subject in a single
all-or-none operation. When anything fails, nothing is created.

Request:

```json
{
  "subject_ids": ["subj-a", "subj-b"],
  "payment_method": "wallet",
  "transfer_reference": "TXN-2026-0001",
  "sender_number": "01012345678",
  "amount_paid": 300
}
```

Validation and checks:

| Check | Failure |
|---|---|
| web client | `403 web_client_required` |
| strict JSON, no unknown fields | `400 invalid_json` |
| `subject_ids` present, 1-50 entries | `400 invalid_subjects` |
| no duplicate subject id | `400 duplicate_subject` (`subject_id` set) |
| `payment_method` is `wallet` or `instapay` | `400 invalid_payment_method` |
| trimmed `transfer_reference` is 3-100 runes | `400 invalid_transfer_reference` |
| trimmed `sender_number` is 3-50 runes | `400 invalid_sender_number` |
| `amount_paid` > 0 | `400 invalid_amount` |
| normalized reference unused by any order or single request | `409 duplicate_reference` |
| each subject exists and is published | `404 subject_not_found` (`subject_id` set) |
| each subject's `access_expires_at` is empty or in the future | `409 subject_expired` (`subject_id` set) |
| the student does not own the subject now | `409 already_owned` (`subject_id` set) |
| the student has no pending request for the subject | `409 pending_request_exists` (`subject_id` set) |
| write rate tier (per user) | `429` with `Retry-After` |

Checks run in the order listed; the first failing subject is named in
`subject_id` and in the Arabic/English message. All rows, the order and its
requests, are created together: either every subject becomes a pending
request or none does.

Response `201 Created`:

```json
{
  "id": "ord-7f3c...",
  "source": "web",
  "status": "open",
  "created_at": "2026-11-05T10:00:00Z",
  "payment_method": "wallet",
  "transfer_reference": "TXN-2026-0001",
  "sender_number": "01012345678",
  "amount_paid": 300,
  "total": 300,
  "amount_mismatch": false,
  "items": [
    {
      "request_id": "req-1",
      "subject_id": "subj-a",
      "title_ar": "المادة أ",
      "title_en": "Subject A",
      "level_name_ar": "الفرقة الأولى",
      "price": 150,
      "status": "pending"
    },
    {
      "request_id": "req-2",
      "subject_id": "subj-b",
      "title_ar": "المادة ب",
      "title_en": "Subject B",
      "level_name_ar": "الفرقة الأولى",
      "price": 150,
      "status": "pending"
    }
  ]
}
```

`total` is the sum of the current prices of the order's subjects.
`amount_mismatch` is `true` when `amount_paid != total`.

### 3.2 `GET /academy/me/orders`

Returns the authenticated student's orders, newest first. Orders of both
sources appear. No pagination in this version.

```json
{
  "items": [
    {
      "id": "ord-7f3c...",
      "source": "web",
      "status": "closed",
      "created_at": "2026-11-05T10:00:00Z",
      "decided_at": "2026-11-05T12:00:00Z",
      "payment_method": "wallet",
      "transfer_reference": "TXN-2026-0001",
      "sender_number": "01012345678",
      "amount_paid": 300,
      "total": 300,
      "amount_mismatch": false,
      "items": [
        {
          "request_id": "req-1",
          "subject_id": "subj-a",
          "title_ar": "المادة أ",
          "title_en": "Subject A",
          "level_name_ar": "الفرقة الأولى",
          "price": 150,
          "status": "accepted",
          "decided_at": "2026-11-05T12:00:00Z"
        },
        {
          "request_id": "req-2",
          "subject_id": "subj-b",
          "title_ar": "المادة ب",
          "title_en": "Subject B",
          "level_name_ar": "الفرقة الأولى",
          "price": 150,
          "status": "rejected",
          "decided_at": "2026-11-05T12:00:00Z",
          "reject_reason": "التحويل غير مطابق"
        }
      ]
    }
  ]
}
```

Order `status`:

- `open` while at least one request in it is `pending`.
- `closed` once every request is `accepted` or `rejected`. `decided_at` is the
  close time (the last decision).
- An admin may accept some subjects and reject others; each item carries its
  own status and rejection reason.

App orders carry no proof fields: `payment_method`, `transfer_reference`,
`sender_number` and `amount_paid` are omitted (`amount_mismatch` is `false`,
`total` is still the sum of subject prices).

### 3.3 `GET /academy/me/orders/{id}`

Same order object as one item of 3.2. Another student's order, an unknown id or
a malformed id is `404` code `not_found` (IDOR protection, same rule as
`GET /academy/me/requests/{id}`).

### 3.4 App access requests attach to the student's open app order

`POST /academy/subjects/{id}/access-request` is unchanged for the app: same
request body (none), same response, no order id in the response.

Server behavior:

1. If a pending request already exists for (`user_id`, `subject_id`), return it
   (unchanged idempotent R5 behavior).
2. Otherwise, find the student's open `app` order. If there is none, create one
   in the same operation. Attach the new pending request to it (`order_id`).
3. Two app requests from the same student at the same moment end in **one**
   open app order. This is enforced by a partial unique index on
   `orders (user_id)` where `source = "app"` and `status = "open"`, with a
   re-read on duplicate key (proved by a parallel test).
4. When the last pending request of an app order is accepted or rejected, the
   order becomes `closed`. The next app request starts a new order.
5. A web checkout always creates its own web order and never attaches to the
   app order.

Existing app checks are unchanged: published subject, not expired, not owned,
one pending request per subject, write rate limit. The app's request/response
contract does not change.

### 3.5 Existing `POST /academy/subjects/{id}/purchase-request`

Kept for compatibility and treated as an order of one:

- It creates one `web` order with one request and returns the existing
  `PurchaseRequestResponseDTO` (`id`, `subject_id`, `status`, `source`,
  `amount`, `created_at`).
- Its transfer reference is checked against orders and single requests.
- The order holds the proof fields; the request row keeps them as before so
  the existing console list is unchanged.
- All existing validation and errors are unchanged.

`GET /academy/me/requests` and `GET /academy/me/requests/{id}` are unchanged.

## 4. Admin routes (internal listener)

### 4.1 Grouped pending read model

`GET /internal/admin/request-groups`

Query parameters:

- `status` (optional): `pending` (default), `accepted` or `rejected`.
- `user_id` (optional): one student, `[A-Za-z0-9_-]{1,100}`.

Shows matching requests grouped first by student, then by order. Within a
student, orders are newest first; within an order, requests keep creation
order. Students are ordered by their newest matching request.

Response `200`:

```json
{
  "items": [
    {
      "user_id": "user-1",
      "orders": [
        {
          "order_id": "ord-7f3c...",
          "source": "web",
          "status": "open",
          "created_at": "2026-11-05T10:00:00Z",
          "payment_method": "wallet",
          "transfer_reference": "TXN-2026-0001",
          "sender_number": "01012345678",
          "amount_paid": 300,
          "total": 320,
          "amount_mismatch": true,
          "items": [
            {
              "request_id": "req-1",
              "subject_id": "subj-a",
              "title_ar": "المادة أ",
              "title_en": "Subject A",
              "level_name_ar": "الفرقة الأولى",
              "price": 160,
              "status": "pending",
              "created_at": "2026-11-05T10:00:00Z"
            }
          ]
        }
      ]
    }
  ],
  "total_students": 1,
  "total_orders": 1,
  "total_requests": 1,
  "pending_count": 1
}
```

`total_requests` counts matching request rows; `pending_count` counts pending
rows across the whole system (the console badge value, same number as the
existing queue's `pending_count`).

For web orders the proof fields are exposed; `amount_mismatch` compares
`amount_paid` with the sum of the order's subject prices at read time. For app
orders those fields are omitted and `amount_mismatch` is `false`.

A matching request with no `order_id` (legacy data) appears under an order
block with `"order_id": ""` and `source` equal to the request's source. The
Mongo migration in 5 attaches pending legacy app requests, so this is a
fallback only.

### 4.2 Bulk accept / reject

`POST /internal/admin/requests/bulk`

Request:

```json
{
  "action": "reject",
  "ids": ["req-1", "req-2"],
  "reason": "التحويل غير مطابق"
}
```

- `action` is required: `accept` or `reject` (`400 invalid_action` otherwise).
- `ids` is required: 1-100 unique well-formed ids (`400 invalid_ids` for empty,
  over 100, malformed or duplicate ids).
- `reason` is required for `reject`, 1-1000 runes after trimming and stripping
  CR/LF (`400 invalid_reason`). It is ignored for `accept`.
- Unknown fields are refused (`400 invalid_json`).

Every id is processed independently with the same rules as the single
`POST /internal/admin/requests/{id}/accept` and
`POST /internal/admin/requests/{id}/reject`. One id failing never stops the
others and never rolls back an accepted/rejected row.

Response `200` (a valid bulk request always answers 200; per-id outcomes are
in the body):

```json
{
  "results": [
    {"id": "req-1", "status": "accepted"},
    {"id": "req-2", "status": "failed", "error_code": "request_not_pending",
     "message_ar": "الطلب لم يعد معلقًا", "message_en": "Request is not pending"}
  ]
}
```

`status` is `accepted`, `rejected` or `failed`. For `failed`, `error_code` is
one of the single-endpoint codes: `request_not_found`, `request_not_pending`,
`subject_not_found`, `subject_expired`, `service_unavailable`. `message_ar`
and `message_en` are always present on a failure. There is no silent partial
failure: every requested id has exactly one result, in input order.

Accept keeps the existing R4 order and creates the entitlement and one
payment record with `price_at_grant` per subject. Reject stores the shared
reason on every rejected request. A `web` order (and an `app` order) closes
once its last pending request is decided.

Audit:

- one `admin_audit_log` entry per affected request
  (`request_accept` / `request_reject`, actor = the verified admin, detail
  carries subject id, user id and, for reject, the reason);
- one summary entry per bulk call:
  - `request_bulk_accept` or `request_bulk_reject`,
    `target_type: "request_bulk"`, `target_id` = first id,
    detail = `count=<n> ids=<comma-separated ids>`.

The actor of every entry is the admin who sent the bulk call.

## 5. Data model additions

`orders` (new collection):

| Field | Type | Notes |
|---|---|---|
| `_id` | string | order id |
| `user_id` | string | student |
| `source` | `web` \| `app` | |
| `status` | `open` \| `closed` | |
| `payment_method` | `wallet` \| `instapay` | web only |
| `transfer_reference` | string | web only |
| `normalized_reference` | string | web only, uppercase, unique |
| `sender_number` | string | web only |
| `amount_paid` | int | web only |
| `created_at` | time | |
| `closed_at` | time (optional) | set when the order closes |

Indexes: unique partial `(user_id)` where `source="app"` and `status="open"`
(one open app order per student); unique partial `(normalized_reference)`
where it is a non-empty string; `(user_id, created_at)`.

`purchase_requests` gains `order_id` (string, optional). Index `(order_id)`.
No existing field changes.

### Migration of existing data

On startup, MongoStore runs an idempotent migration after index creation:

- Every student's **pending** app/mobile requests (no `order_id`, source empty
  or `mobile`) are grouped into **one open app order per student**. The order's
  `created_at` is the earliest request's `created_at`.
- Accepted/rejected history gets **no order** (`order_id` stays empty). It is
  not needed for the pending console view or for the student order list,
  which only expose orders created from the migration onward.

The migration is safe to run on every start: requests that already have an
`order_id` are skipped.

## 6. Error code catalog

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_json` | 400 | body is not strict JSON or has unknown fields |
| `invalid_subjects` | 400 | `subject_ids` missing, empty or over 50 |
| `duplicate_subject` | 400 | a subject id appears twice |
| `invalid_payment_method` | 400 | not `wallet` / `instapay` |
| `invalid_transfer_reference` | 400 | length outside 3-100 |
| `invalid_sender_number` | 400 | length outside 3-50 |
| `invalid_amount` | 400 | `amount_paid` <= 0 |
| `subject_not_found` | 404 | unknown or unpublished subject |
| `subject_expired` | 409 | `access_expires_at` passed |
| `already_owned` | 409 | active entitlement exists |
| `pending_request_exists` | 409 | a pending request already exists for this subject |
| `duplicate_reference` | 409 | normalized reference already used |
| `not_found` | 404 | unknown order/request for this student, or other student's |
| `web_client_required` | 403 | mobile token on a web-only route |
| `invalid_action` | 400 | bulk action is not `accept` / `reject` |
| `invalid_ids` | 400 | bulk ids empty, over 100, malformed or duplicated |
| `invalid_reason` | 400 | reject reason missing or outside 1-1000 |
| `request_not_found` | 404 | bulk/single decision: unknown request |
| `request_not_pending` | 409 | bulk/single decision: already decided |
| `service_unavailable` | 503 | store or dependency failure (fail closed) |
| `too_many_requests` | 429 | rate limit, `Retry-After` present |

## Change log

- 2026-11-05: first version.
