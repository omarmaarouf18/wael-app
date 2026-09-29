# ADR-0003: Payment Per Subject (Materia)

- **Status**: Accepted
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
