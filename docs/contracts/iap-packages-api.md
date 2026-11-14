# IAP Packages API Contract (academy-service)

- **Status**: Frozen for the 2026-11-14 parallel batch (Lane S and Lane A build against it).
- **Date**: 2026-11-14.
- **Owners**: Lane S (backend and admin console), Lane A (iOS frontend).
- **Related**: ADR-0015 (iOS IAP Packages), SPEC Section 5 (`iap_packages`, `iap_purchases`),
  SPEC Section 6 (routes), tracking issue #50 (issues #51-#61).

This document is additive after its initial creation: fields may be added, and
nothing is removed or renamed without a new dated amendment. Every change is
logged in the change log at the bottom.

## 1. Scope and Architecture

To comply with App Store Review Guideline 3.1.1 following the rejection of iOS 1.0.1 (10),
the academy offers **packages of subjects** for purchase on **iOS only** using Apple
In-App Purchase (StoreKit 2 Non-Renewing Subscriptions).

Existing systems remain byte-for-byte unchanged:
- Android app, website (`web/`), public catalog, and existing endpoints remain identical.
- Outside payment (manual transfer via website/wallets/InstaPay) and admin manual
  activations remain available in parallel across all platforms.
- Single subjects continue to support access requests; locked subjects on iOS point
  to their corresponding package.
- The app decides presentation by platform (`StorePolicy`, `Theme.of(context).platform`),
  never by a server flag.
- Product pricing lives exclusively in App Store Connect; our API never returns an IAP price.
- Buying grants immediately and automatically (no admin approval step) all subjects in
  the package at purchase time.
- Entitlement expiration equals the subject's `access_expires_at`, set via the existing
  `Store.Grant` mechanism.
- Valid Apple transactions are always honoured even if the package is unpublished or past
  its `available_until` date.

## 2. Transport, Auth, and Common Rules

- Routes below are academy-service paths. Through the gateway they are prefixed with `/api/v1`.
- Student routes require `X-Gateway-Secret` and a verified student Bearer JWT.
- **Web client exclusion**: Tokens minted for the website (`claims.Client == "web"`) are
  forbidden on all `/academy/iap/*` routes and receive HTTP `403` with code `app_client_required`.
- Admin routes are served exclusively on the internal admin listener (`:9002`), behind
  `X-Internal-Token` and `X-Admin-Token`. They are never exposed through the gateway.
- Student JSON request bodies enforce `DisallowUnknownFields()`. An unknown or malformed
  field returns HTTP `400` code `invalid_body`.
- Maximum request body size for `POST /academy/iap/transactions` is 16 KiB.
- Timestamps are RFC 3339 UTC strings (e.g., `2026-11-14T12:00:00Z`).
- Error responses follow standard academy-service JSON `{ "code": "...", "message": "..." }`.

## 3. Deterministic `app_account_token` Namespace

Apple StoreKit 2 supports an `appAccountToken` (UUID) in purchase requests, which Apple binds
to the signed transaction JWS payload. To securely tie transactions to academy students
without storing random tokens, the server and app use a deterministic UUID v5 derived from
the student's `user_id` and a fixed repository namespace constant.

### Namespace Constant

```text
AppAccountTokenNamespace = "8e77e759-b33e-4ba9-8283-ac53aca1f8c8"
```

### Derivation Rules

1. The namespace UUID is parsed as RFC 4122 namespace: `8e77e759-b33e-4ba9-8283-ac53aca1f8c8`.
2. The user ID is the UTF-8 bytes of `user_id` (e.g., `"usr_abc123"`).
3. The derived UUID is generated via standard UUID v5 (SHA-1 over namespace + data).
   - In Go: `uuid.NewSHA1(uuid.MustParse("8e77e759-b33e-4ba9-8283-ac53aca1f8c8"), []byte(userID)).String()`
4. Returned in `GET /academy/iap/packages` as `app_account_token`.
5. The iOS client passes this string as `appAccountToken` to StoreKit 2 when initiating purchase.
6. The server verifies that `payload.appAccountToken` in the decoded JWS matches the caller's
   deterministic token before granting content.

## 4. Student Endpoints

### 4.1 `GET /academy/iap/packages` (TierRead)

Returns the published packages available for iOS in-app purchase.

#### Request

- Method: `GET`
- Path: `/academy/iap/packages` (gateway `/api/v1/academy/iap/packages`)
- Headers:
  - `Authorization: Bearer <student-jwt>`
  - `X-Gateway-Secret: <secret>`

#### Response `200 OK`

```json
{
  "app_account_token": "a1b2c3d4-e5f6-5a7b-8c9d-0e1f2a3b4c5d",
  "packages": [
    {
      "id": "pkg_year1_2026",
      "product_id": "com.wael.app.pkg.year1",
      "title": {
        "ar": "الفرقة الأولى",
        "en": "Year 1"
      },
      "description": {
        "ar": "باقة مواد الفرقة الأولى كاملة لعام 2026-2027",
        "en": "All Year 1 subjects for the 2026-2027 academic year"
      },
      "available_until": "2027-01-20T00:00:00Z",
      "on_sale": true,
      "position": 1,
      "subjects": [
        {
          "id": "subj_law101",
          "title": {
            "ar": "مدخل العلوم القانونية",
            "en": "Introduction to Law"
          },
          "level_key": "bachelor-y1",
          "term": "first",
          "owned": false
        },
        {
          "id": "subj_const101",
          "title": {
            "ar": "القانون الدستوري",
            "en": "Constitutional Law"
          },
          "level_key": "bachelor-y1",
          "term": "first",
          "owned": true
        }
      ]
    }
  ]
}
```

#### Field Specifications

- `app_account_token` (string, UUID): Deterministic UUID v5 for the authenticated student.
- `packages` (array): List of packages, ordered by `position` ascending, then `title.ar` ascending.
  - `id` (string): Internal package identifier.
  - `product_id` (string): Apple Product ID (e.g. `com.wael.app.pkg.year1`).
  - `title` (object): Bilingual title (`ar` required, `en` optional).
  - `description` (object): Bilingual description (`ar` and `en`).
  - `available_until` (string, optional): RFC 3339 UTC timestamp, or `null`.
  - `on_sale` (boolean): `true` if package is published AND (`available_until` is null OR `now < available_until`) AND `subjects` has at least 1 item.
  - `position` (integer): Display order.
  - `subjects` (array): Subjects in the package, ordered as configured in the package:
    - Only published subjects under published levels whose `access_expires_at` is in the future.
    - `id` (string): Subject identifier.
    - `title` (object): Bilingual subject title.
    - `level_key` (string): Level key (e.g. `bachelor-y1`).
    - `term` (string): Academic term (`first`, `second`, or empty).
    - `owned` (boolean): `true` if caller currently holds an active, unexpired, unrevoked entitlement.

#### Constraints
- Returns only packages where `published == true`.
- Never returns prices, content IDs (YouTube/PDF storage keys), or unmanaged product IDs.

---

### 4.2 `POST /academy/iap/transactions` (TierWrite)

Validates a StoreKit 2 JWS transaction, grants package subjects, and records the purchase.
Also used for restoring purchases.

#### Request

- Method: `POST`
- Path: `/academy/iap/transactions` (gateway `/api/v1/academy/iap/transactions`)
- Headers:
  - `Authorization: Bearer <student-jwt>`
  - `X-Gateway-Secret: <secret>`
  - `Content-Type: application/json`
- Max Body Size: 16 KiB
- Body (`DisallowUnknownFields`):

```json
{
  "signed_transaction": "eyJhbGciOiJFUzI1NiIsIng1YyI6WyJNSUlE...JWS"
}
```

#### Response `200 OK` (Newly Granted)

```json
{
  "status": "granted",
  "transaction_id": "2000000123456789",
  "product_id": "com.wael.app.pkg.year1",
  "package_id": "pkg_year1_2026",
  "granted_subject_ids": [
    "subj_law101"
  ],
  "skipped": [
    {
      "subject_id": "subj_const101",
      "reason": "already_owned"
    }
  ]
}
```

#### Response `200 OK` (Already Processed / Restored)

When the transaction ID has already been successfully processed by the same student, the
endpoint returns the stored purchase outcome without creating duplicate grants or errors:

```json
{
  "status": "already_processed",
  "transaction_id": "2000000123456789",
  "product_id": "com.wael.app.pkg.year1",
  "package_id": "pkg_year1_2026",
  "granted_subject_ids": [
    "subj_law101"
  ],
  "skipped": [
    {
      "subject_id": "subj_const101",
      "reason": "already_owned"
    }
  ]
}
```

#### `skipped.reason` Values

| Reason | Meaning |
|---|---|
| `already_owned` | Student already owns an active, unexpired, unrevoked entitlement for this subject. |
| `expired` | Subject's `access_expires_at` is in the past at time of grant. |
| `unavailable` | Subject is deleted, draft, hidden, or no longer exists in catalog. |

#### Verification Procedure (Standard Library Crypto)

Verification is executed locally in Go using `crypto/x509` and `crypto/ecdsa` without third-party JWT libraries:

1. **JWS Format**:
   - Header must specify `alg: "ES256"`. Any other algorithm -> HTTP 422 `invalid_transaction`.
   - Header must contain `x5c` certificate chain with at least 3 certificates (leaf -> intermediate -> root).
2. **Apple Root CA Verification**:
   - Root certificate must chain to **Apple Root CA - G3** (PEM embedded in repository, pinned SHA-256 fingerprint verified in code).
   - If root does not match pinned Apple Root CA - G3 -> HTTP 422 `invalid_transaction`.
3. **Chain Validation**:
   - Verify signatures from leaf up to root.
   - Verify certificate validity window at transaction's `signedDate` timestamp.
4. **Apple Marker OIDs**:
   - Leaf certificate must contain extension OID `1.2.840.113635.100.6.11.1` (Apple App Store receipt signing).
   - Intermediate certificate must contain extension OID `1.2.840.113635.100.6.2.1` (Apple WWDR intermediate).
   - Missing or invalid marker OIDs -> HTTP 422 `invalid_transaction`.
5. **JWS Signature Verification**:
   - Verify ES256 signature over JWS signing input using the public key from the leaf certificate.
   - Signature mismatch -> HTTP 422 `invalid_transaction`.
6. **Payload Field Verification**:
   - `bundleId`: Must match configured `IAP_BUNDLE_ID` (default `com.wael.app`), else HTTP 422 `wrong_bundle`.
   - `productId`: Must match a known package in `iap_packages`, else HTTP 404 `unknown_product`.
   - `type`: Must equal `"Non-Renewing Subscription"`, else HTTP 422 `unsupported_product_type`.
   - `revocationDate`: If present and non-zero (indicating Apple refund or revocation), reject with HTTP 422 `revoked_transaction`.
   - `environment`: Must be `"Production"` or `"Sandbox"`. If `"Sandbox"`, caller's `user_id` must be present in `IAP_SANDBOX_ALLOWED_USER_IDS` (or `IAP_SANDBOX_ALLOW_ALL=true` in local/test), else HTTP 403 `sandbox_not_allowed`.
   - `appAccountToken`: Must equal the student's deterministic UUID v5 under `AppAccountTokenNamespace`, else HTTP 403 `account_mismatch`.
7. **Idempotency and Account Ownership**:
   - Query `iap_purchases` by `transaction_id`.
   - If found and `purchase.user_id == caller.user_id`: return HTTP 200 with `status: "already_processed"`, echoing stored `granted_subject_ids` and `skipped`.
   - If found and `purchase.user_id != caller.user_id`: return HTTP 409 `transaction_owned_by_other_user`.
8. **Grant Execution**:
   - Look up the matching package by `product_id`.
   - Iterate over current `subject_ids` of the package at purchase time.
   - Call existing `Store.Grant(user_id, subject_id, source="iap", granted_by="apple_iap:<transaction_id>")`.
   - Record results:
     - Success -> add to `granted_subject_ids`.
     - `ErrDuplicate` -> add to `skipped` with reason `already_owned`.
     - `ErrSubjectExpired` -> add to `skipped` with reason `expired`.
     - `ErrNotFound` -> add to `skipped` with reason `unavailable`.
   - Note: If Apple accepted payment, the server grants current subjects even if package `published == false` or past `available_until`.
   - Write row to `iap_purchases`.
   - `payment_records` are NOT written for IAP purchases (owner decision 144).

---

## 5. Admin Endpoints (Internal Listener)

Served on `ADMIN_LISTEN_ADDR` (`:9002`), requiring `X-Internal-Token` and `X-Admin-Token`.
Every mutating call generates an entry in `admin_audit_log`.

### 5.1 `GET /internal/admin/iap/packages`

List all packages (published and unpublished).

#### Response `200 OK`

```json
{
  "packages": [
    {
      "id": "pkg_year1_2026",
      "product_id": "com.wael.app.pkg.year1",
      "title_ar": "الفرقة الأولى",
      "title_en": "Year 1",
      "description_ar": "باقة مواد الفرقة الأولى كاملة",
      "description_en": "All Year 1 subjects",
      "subject_ids": ["subj_law101", "subj_const101"],
      "available_until": "2027-01-20T00:00:00Z",
      "published": true,
      "position": 1,
      "created_at": "2026-11-14T08:00:00Z",
      "updated_at": "2026-11-14T08:00:00Z"
    }
  ]
}
```

### 5.2 `POST /internal/admin/iap/packages`

Create a new package.

#### Request Body

```json
{
  "product_id": "com.wael.app.pkg.year1",
  "title_ar": "الفرقة الأولى",
  "title_en": "Year 1",
  "description_ar": "باقة مواد الفرقة الأولى كاملة",
  "description_en": "All Year 1 subjects",
  "subject_ids": ["subj_law101", "subj_const101"],
  "available_until": "2027-01-20T00:00:00Z",
  "published": false,
  "position": 1
}
```

#### Validation:
- `product_id`: Must match regex `^com\.wael\.app\.pkg\.[a-z0-9_.]+$`. Unique across packages.
- `title_ar`: Required, 1-80 runes.
- `title_en`: Required, 1-80 runes.
- `description_ar`, `description_en`: Optional, max 1000 runes.
- `subject_ids`: Array of up to 100 unique subject IDs. Every ID must exist in `subjects`, else HTTP 400 `unknown_subject`.
- `available_until`: Optional RFC 3339 timestamp or `null`.
- Audit action: `iap_package_create`.

### 5.3 `GET /internal/admin/iap/packages/{id}`

Returns single package details. Unknown ID returns HTTP 404 `not_found`.

### 5.4 `PATCH /internal/admin/iap/packages/{id}`

Update package details.

- `product_id` is **immutable**. Sending `product_id` in request body returns HTTP 400 `product_id_immutable`.
- Other fields optional: `title_ar`, `title_en`, `description_ar`, `description_en`, `subject_ids`, `available_until`, `published`, `position`.
- Audit action: `iap_package_update`.

### 5.5 `DELETE /internal/admin/iap/packages/{id}`

Delete an unused package.

- If any record exists in `iap_purchases` with this `package_id`, deletion is blocked with HTTP 409 `package_has_purchases` (package should be unpublished instead).
- Audit action: `iap_package_delete`.

### 5.6 `GET /internal/admin/iap/purchases`

List purchases for auditing and student support.

- Query parameters:
  - `user_id`: Filter by student user ID.
  - `package_id`: Filter by package ID.
  - `limit`: Integer (1-100, default 50).
- Returns list of `iap_purchases` records, newest first.

### 5.7 `GET /internal/admin/iap/coverage`

Reports published subjects that are not included in any published package.

#### Response `200 OK`

```json
{
  "uncovered_subject_ids": [
    "subj_unassigned_1"
  ]
}
```

Used by the admin console to display an informative warning banner.

### 5.8 Cascading Deletion Rule

When a subject is deleted through the admin API (`DELETE /internal/admin/subjects/{id}`),
the store automatically removes that `subject_id` from all `iap_packages.subject_ids` arrays.

---

## 6. Data Model Additions (MongoDB)

### Collection `iap_packages`

| Field | Type | Description |
|---|---|---|
| `_id` | string | Internal package ID (e.g. `pkg_...`) |
| `product_id` | string | Apple StoreKit Product ID (immutable) |
| `title_ar` | string | Arabic package title |
| `title_en` | string | English package title |
| `description_ar` | string | Arabic description |
| `description_en` | string | English description |
| `subject_ids` | array of strings | Hand-picked subject IDs in package |
| `available_until` | date-time or null | Sale expiration timestamp |
| `published` | boolean | Publication toggle |
| `position` | integer | UI sorting order |
| `created_at` | date-time | Creation timestamp |
| `updated_at` | date-time | Last update timestamp |

**Indexes**:
- `product_id`: Unique index (`uniq_iap_packages_product_id`).
- `published`, `position`: Index for student catalog queries (`idx_iap_packages_pub_pos`).

---

### Collection `iap_purchases`

| Field | Type | Description |
|---|---|---|
| `_id` | string | Purchase record ID |
| `transaction_id` | string | Apple Transaction ID |
| `original_transaction_id` | string | Apple Original Transaction ID |
| `user_id` | string | Academy student user ID |
| `package_id` | string | Target package ID |
| `product_id` | string | Apple Product ID |
| `environment` | string | `"Production"` or `"Sandbox"` |
| `purchase_date` | date-time | Apple purchase timestamp |
| `granted_subject_ids` | array of strings | Subjects successfully granted |
| `skipped` | array of `{subject_id, reason}` | Subjects skipped during grant |
| `created_at` | date-time | Server record creation timestamp |

**Indexes**:
- `transaction_id`: Unique index (`uniq_iap_purchases_transaction_id`).
- `user_id`, `created_at`: Index for student lookup (`idx_iap_purchases_user_created`).
- `package_id`: Index for deletion safety check (`idx_iap_purchases_package_id`).

---

### Entitlements Updates

- `entitlements.source`: Gains value `"iap"`.
- `entitlements.granted_by`: Set to `"apple_iap:<transaction_id>"`.
- `payment_records`: Schema unchanged; no payment record is written for IAP transactions.

---

## 7. Configuration Variables

Configured in `services/academy-service/internal/config/config.go` following allowlist security defaults:

| Variable | Type | Default | Description |
|---|---|---|---|
| `IAP_BUNDLE_ID` | string | `com.wael.app` | Expected iOS App Bundle ID |
| `IAP_SANDBOX_ALLOWED_USER_IDS` | string | `""` | Comma-separated allowlist of user IDs permitted to submit Sandbox transactions |
| `IAP_SANDBOX_ALLOW_ALL` | boolean | `false` | Relax sandbox check for testing. Permitted **only** when `APP_ENV=local` or `test`. If set to `true` when `APP_ENV=production` (or empty/unknown), startup and `--check-env` fail immediately. |

---

## 8. Error Code Catalog

| Error Code | HTTP Status | Meaning |
|---|---|---|
| `invalid_body` | 400 | Request body is not strict JSON, unknown fields present, or payload over 16 KiB |
| `app_client_required` | 403 | Token carries `claims.Client == "web"`; route is mobile-only |
| `account_mismatch` | 403 | Transaction `appAccountToken` is missing or does not match caller's deterministic token |
| `sandbox_not_allowed` | 403 | Transaction is from Apple Sandbox but caller is not in sandbox allowlist |
| `not_found` | 404 | Package ID not found |
| `unknown_product` | 404 | Transaction `productId` does not match any known package |
| `transaction_owned_by_other_user` | 409 | Transaction ID was already processed by a different student account |
| `product_id_immutable` | 400 | Attempted to change `product_id` in package update |
| `unknown_subject` | 400 | Provided subject ID does not exist in `subjects` collection |
| `package_has_purchases` | 409 | Cannot delete package with existing purchases (unpublish instead) |
| `invalid_transaction` | 422 | JWS decoding failed, bad signature, certificate chain invalid, or missing marker OIDs |
| `wrong_bundle` | 422 | Transaction `bundleId` does not match configured `IAP_BUNDLE_ID` |
| `unsupported_product_type` | 422 | Transaction `type` is not `"Non-Renewing Subscription"` |
| `revoked_transaction` | 422 | Transaction contains a `revocationDate` indicating a refund or cancellation |
| `iap_unavailable` | 503 | Database or internal service failure during transaction processing |

---

## Change Log

- 2026-11-14: Initial version frozen for the 2026-11-14 parallel batch (Lane S and Lane A).
