# ADR-0003: Payment Per Subject (Materia)

- **Status**: Accepted
- **Amended**: 2026-09-29, 2026-09-30
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

The catalog is organized into subjects (materia): a student studies specific
subjects, not whole courses and not the academy on a monthly basis. Pricing
per course forces students to buy content they do not need; pricing per
month charges for time rather than for the material studied. As of this
ADR, no code exists for entitlements, pricing, or payment in the repo.

## Decision

1. Students purchase access per subject (materia).
2. Access is not sold per course and not sold per month.
3. The purchase unit follows the catalog unit: one subject, one purchase.

## Consequences

### Positive

- Lower entry price per purchase, matched to what the student actually
  studies.
- Entitlement checks stay simple: access is a set of owned subjects.

### Negative and Tradeoffs

- More, smaller transactions to track instead of a few large ones.
- Bundles, discounts across subjects, and expiry/renewal semantics are
  undefined until the open questions below are decided.

## Alternatives Considered

- **Per-course purchase**: rejected; it bundles unneeded subjects into the price.
- **Monthly subscription**: rejected; it charges for time instead of material.

## Open Questions

- Payment provider versus manual activation.
- Price model per subject.
- Refund policy.
- What a "subject" contains (videos, notes, exams) for entitlement purposes.

## To verify

- App store rules for selling digital content, if the app is distributed
  through stores (in-app purchase requirements and their cut).

## Amendment (2026-09-29)

Answered in ADR-0007 (Proposed): a subject contains videos (title,
description) and PDFs, and its price is set by the admin. The payment
provider question is deferred: the payment flow will be specified
separately and ADR-0007 defines only the boundary (an admin-accepted
request grants ownership). Refund policy remains open.

## Amendment (2026-09-30)

The owner specified the payment channel: payment is **manual, via InstaPay
or e-wallets, outside the app**, and **activation is by the admin** (the
admin-accepted request creates the entitlement, per the ADR-0007 boundary).
This answers the "Payment provider versus manual activation" open question.
The payment flow itself remains out of scope, and the refund policy remains
open.

## Amendment (2026-10-08, owner decision; recorded 2026-10-09): center activation and price display

Source: `docs/core-service/SPEC.md` Section 2 D3 (amendments 2026-10-08) and
`AI_CONTEXT.md`. The earlier text above is unchanged.

1. Activation is by the center: a student who opens a locked subject creates an
   access request and contacts support/the center over WhatsApp to activate it.
   No payment action is named anywhere in the app.
2. A locked subject may show its server-sent price (`price` + `currency`) only
   when the API sends it (`EXPOSE_PRICE_TO_STUDENTS=true`) AND the public
   app-config flag `show_prices` is true. Either switch hides prices again with
   no new APK.
3. Under the consumption-only rule, content may be bought outside the app, but
   the app must not lead users to an outside payment method: no Play Billing,
   no pay/buy/purchase wording, no payment methods or links in-app. The
   store-safety test (`frontend/test/store_safety_l10n_test.dart`) pins the
   banned wording and the price-label allowlist.
