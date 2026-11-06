# Changes: Console Requests Grouped by Student (2026-11-05, Part F)

## Overview

Rebuilds the Admin Console Requests tab to support grouped request handling per student and bulk decision actions:
- **Default Grouped View**: Pending requests are grouped by student. One student requesting multiple subjects (e.g., 30 subjects) is rendered as a single row displaying student identity (name, email, phone, user ID), subject count, total amount (EGP), oldest request timestamp, and source badges (`web` / `app`).
- **Expandable Orders & Subjects**: An expand arrow reveals the student's orders (web orders and app orders). Each order displays metadata (for web orders: payment method, transfer reference, sender number, amount paid, and amount mismatch warning badge if `amount_paid != total`). Under each order, its subjects are listed with title, level, price, source, and decision status.
- **Hierarchical Selection & Bulk Actions**:
  - Individual subject checkbox per item.
  - "Select all" checkbox on the student row selects/unselects all pending items for that student.
  - "Select order" checkbox on each order card selects/unselects all pending items in that order.
  - Bulk action bar appears when items are selected, showing selection count and total amount in EGP, with Bulk Accept, Bulk Reject, and Clear Selection buttons.
  - Bulk Accept and Bulk Reject flows open confirmation dialogs displaying selected count and total amount before execution.
  - Bulk Reject requires a valid rejection reason (1–1000 characters) that is applied to all selected requests and reaches each student.
- **No Partial Silent Failure**:
  - Server endpoints: `GET /api/request-groups` proxies to academy-service `GET /internal/admin/request-groups`, and `POST /api/requests/bulk` proxies to `POST /internal/admin/requests/bulk`.
  - The bulk endpoint returns a status per request ID (`accepted`, `rejected`, or `failed` with reason/error code).
  - When mixed or partial failures occur, the console shows which IDs failed and why in the error banner with no silent failure.
- **Amount Check Warning**:
  - For web orders, an amount check badge (`⚠️`) warns administrators when `amount_paid` differs from the sum of its subjects' prices (`amount_mismatch`).
- **Filters & Flat View Toggle**:
  - Filters (status, source, search by name, email, phone, transfer reference, order ID, request ID) work seamlessly in both grouped and flat modes.
  - A view mode selector allows toggling between the default grouped view and the legacy flat list view.

---

## Proposed Amendments to Shared Documentation

### 1. Target Doc: `SPEC.md`

#### Section 1: Locked Decisions
**Amendment (Dated 2026-11-05, Part F - Console Requests Grouped View):**
> *Amended 2026-11-05 (owner decision, Part F):* In the admin console, the Requests tab defaults to a grouped read model where pending requests are grouped by student rather than displayed as individual flat rows.
> - One row per student displays student identity (name, email, phone, user ID), subject count, total price, oldest request date, and source badges (`web` / `app`).
> - The row expands to display the student's orders (web orders with transfer details and app orders), each containing its constituent subjects.
> - Hierarchical selection supports selecting individual subjects, selecting all subjects in an order ("select order"), or selecting all subjects of a student ("select all").
> - Bulk accept and bulk reject operate on the selected requests, sending a single bulk request to the backend. Bulk reject requires a single reason applied to all selected requests.
> - Results report per-id outcome; partial failures are explicitly presented in the console banner with the failing ID and error reason.
> - The flat list view remains available via a view mode toggle in the console UI.

#### Section 5: Data Model
**Amendment to Orders and Requests Entities (Dated 2026-11-05, Part F):**
> - In `admin-console`, the request data model consumes the grouped read model from `GET /internal/admin/request-groups`:
>   - Groups by `user_id`, containing orders array.
>   - Orders contain order metadata (`order_id`, `source`, `payment_method`, `transfer_reference`, `sender_number`, `amount_paid`, `total`, `amount_mismatch`, `items`).
>   - Items contain individual purchase request records (`request_id`, `subject_id`, `title_ar`, `title_en`, `level_name_ar`, `price`, `status`, `created_at`).
>   - Client-side fallback: If `/api/request-groups` is unavailable, `admin-console` falls back to querying `/api/requests` and groups requests client-side.

#### Section 6: API Endpoints
**Amendment to Admin Endpoints (Dated 2026-11-05, Part F):**
> - `GET /api/request-groups`:
>   Admin console proxy to `GET /internal/admin/request-groups` on `academy-service`.
>   Query parameters: `status` (optional: `pending`, `accepted`, `rejected`), `user_id` (optional).
>   Returns `{ items: [...], total_students, total_orders, total_requests, pending_count }`.
> - `POST /api/requests/bulk`:
>   Admin console proxy to `POST /internal/admin/requests/bulk` on `academy-service`.
>   Request body: `{ action: "accept" | "reject", ids: string[], reason?: string }`.
>   Returns `{ total, accepted, rejected, failed, results: [{ id, status, error? }] }`.

#### Section 12: Security & Authentication
**Note on Rule 6 (Admin & Auth Changes):**
> No changes were made to authentication mechanics, token handling, or permission models. Both `/api/request-groups` and `/api/requests/bulk` enforce the existing admin authentication model (`X-Admin-Token` forwarded as `X-Admin-Token` and `X-Internal-Token` to internal listeners). No privilege escalation or public endpoint exposure was introduced.

#### Section 16: Audit Log
**Amendment to Audit Logging (Dated 2026-11-05, Part F):**
> - Bulk accept and bulk reject actions generate one audit log entry per affected request row, recording the admin actor ID, timestamp, target request ID, student user ID, action (`request.accept` or `request.reject`), and rejection reason if applicable.

---

### 2. Target Doc: `docs/adr/0014-sales-website.md`

#### Section: Amendments (2026-11-05)
**Amendment (Part F - Admin Console Orders & Bulk Operations):**
> - **Requests Tab Rebuild**: The admin console Requests tab reflects the order grouping introduced in ADR-0014 amendment 2026-11-05 (Part C2 & Part F). Students with multiple requests appear as one consolidated row.
> - **Web Order Payment Validation**: For web checkout orders, `amount_paid` is validated against the sum of subject prices. If `amount_mismatch` is `true`, the console displays an amount warning badge (`⚠️`) with the recorded transfer details (method, reference, sender number).
> - **Bulk Actions**: Administrators can accept or reject multiple requests across orders simultaneously with confirmation dialogues and explicit per-item failure reporting.

---

### 3. Target Doc: `docs/web/README.md`

#### Section: Admin Console Integration
**Amendment (Dated 2026-11-05, Part F):**
> - Web orders submitted through the website cart checkout flow (`POST /academy/orders`) appear in the admin console Requests tab under the corresponding student row.
> - Web orders display the payment transfer reference, payment method (InstaPay or Wallet), sender phone number, and student-entered amount paid alongside individual course items.
> - If the amount paid does not equal the order's subject prices total, an amount check warning badge is displayed to alert the reviewer.
