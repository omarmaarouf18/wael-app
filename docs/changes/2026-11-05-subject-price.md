# Changes: Per-Subject Price Visibility (2026-11-05)

## Overview
Adds a per-subject price visibility setting (`price_visibility`) with three values:
- `follow_global` (default): follows the global `show_prices` setting in app configuration.
- `show`: forces the price to be visible in the mobile app regardless of the global `show_prices` setting.
- `hide`: forces the price to be hidden in the mobile app regardless of the global `show_prices` setting.

### Surface Behaviors
- **Android App**: Renders a subject's price only when its effective visibility setting resolves to `show` (indicated by the server sending a non-null `price` and `currency`).
- **iOS App**: Unchanged; never shows prices per Apple App Store store policy decision (fails closed on `TargetPlatform.iOS`).
- **Website**: Unchanged; always shows prices per ADR-0014 Decision 5. The per-subject mobile app visibility switch does not affect website public endpoints (`/academy/public/catalog` and `/academy/public/subjects/{id}`).
- **Data Migration**: Existing subjects with missing, null, or empty `price_visibility` are migrated to `"follow_global"`.

---

## Proposed Amendments to Shared Documentation

### 1. Target Doc: `docs/core-service/SPEC.md`

#### Section 1: Locked Decisions
**Amendment to Decision 3 (Amended 2026-11-05, Part D - Per-Subject Price Visibility):**
> *Amended 2026-11-05 (owner decision, Part D):* In addition to the global `show_prices` setting in app settings, each subject supports a `price_visibility` configuration with three values: `"follow_global"` (default), `"show"`, and `"hide"`.
> - Effective price visibility is calculated as:
>   - `"show"`: visible (`true`)
>   - `"hide"`: hidden (`false`)
>   - `"follow_global"` or empty: equals global `show_prices` boolean.
> - Student endpoints `GET /academy/subjects` and `GET /academy/subjects/{id}` expose `price` and `currency` only when `EXPOSE_PRICE_TO_STUDENTS=true` AND `subject.EffectivePriceVisible(settings.ShowPrices)`. Otherwise both fields are omitted/null.
> - Android mobile app displays subject price when `price != null` and subject is unowned.
> - iOS mobile app never displays subject prices (store decision).
> - Website public catalog (`GET /academy/public/catalog`, `/academy/public/subjects/{id}`) always includes prices per ADR-0014 Decision 5; `price_visibility` does not affect it.

#### Section 5: Data Model
**Amendment to Subject Entity (Amended 2026-11-05, Part D):**
> Add field `price_visibility` (string):
> - Values: `"follow_global"` (default), `"show"`, `"hide"`.
> - BSON: `bson:"price_visibility"`, JSON: `json:"price_visibility"`.
> - Existing records without this field default to `"follow_global"` via store startup migration `MigrateSubjectPriceVisibility`.

#### Section 6: API Endpoints
**Amendment to Admin Endpoints:**
> - `POST /internal/admin/subjects`:
>   Accepts optional `price_visibility` string (`"follow_global"`, `"show"`, `"hide"`). Defaults to `"follow_global"` when omitted or empty. Invalid values return 400 with code `"invalid_price_visibility"`.
> - `PATCH /internal/admin/subjects/{id}`:
>   Accepts optional `price_visibility` string. Validated against the three allowed values. Invalid values return 400 with code `"invalid_price_visibility"`.

**Amendment to Student Endpoints:**
> - `GET /academy/subjects` & `GET /academy/subjects/{id}`:
>   `price` and `currency` are included only if `EXPOSE_PRICE_TO_STUDENTS=true` AND `subject.EffectivePriceVisible(settings.ShowPrices)` is true. If either is false, `price` is `nil` and `currency` is `""`.

#### Section 16: Configuration & Migrations
**Amendment to Startup Migrations (Amended 2026-11-05, Part D):**
> On startup, `academy-service` executes `Store.MigrateSubjectPriceVisibility(ctx)`. Any subject record with `price_visibility` missing, null, or empty string (`""`) is updated to `"follow_global"`.

---

### 2. Target Doc: `docs/adr/0014-sales-website.md`

#### Section: Decision 5 (Public catalog pricing)
**Confirmation Note (2026-11-05):**
> Decision 5 stands: the website always displays prices for published subjects in the catalog and subject detail pages. The per-subject `price_visibility` switch added for the mobile app in Part D does not affect the website public endpoints (`GET /academy/public/catalog`, `GET /academy/public/subjects/{id}`).

---

### 3. Target Doc: `docs/web/README.md`

#### Section: Catalog and Pricing
**Confirmation Note (2026-11-05):**
> Public catalog APIs used by the website always return `price` and `currency` for published subjects. The per-subject `price_visibility` switch (`follow_global`, `show`, `hide`) applies exclusively to the mobile application student API responses (`GET /academy/subjects`, `GET /academy/subjects/{id}`) and the admin console editor.
