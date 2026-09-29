# ADR-0004: No Limits on Video Access

- **Status**: Accepted
- **Amended**: 2026-09-29
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

Once a student owns a subject (see ADR-0003), the question is what "access"
means over time. Caps are a common lever (view counts, expiry windows,
device limits), but each cap is also a support burden and a reason for a
legitimate student to feel cheated by a product they paid for. As of this
ADR, no code exists for entitlements or playback gating.

## Decision

1. No view-count cap on owned videos.
2. No expiry on owned videos.
3. No device cap on owned videos.

## Consequences

### Positive

- The purchase promise is simple and absolute: bought once, watchable
  always.
- No cap-related support load (resets, extensions, device migrations).

### Negative and Tradeoffs

- Account-sharing exposure: one purchase can serve several viewers, since
  nothing technical stops concurrent or sequential use across devices.
- Abuse detection, if ever wanted, must be observational (impossible-travel
  logins, concurrent streams) rather than preventive, so it can be added
  later without reversing this decision.
- Later additions that do not reverse this decision: login anomaly alerts,
  per-account concurrent-stream visibility, and voluntary device naming.
  Anything that caps, expires, or locks access would reverse it.

## Alternatives Considered

- **View-count caps**: rejected; punishes rewatching, which is core to study.
- **Expiry windows**: rejected; converts a purchase into a rental.
- **Device caps**: rejected; penalizes students with shared or replaced devices.

## Amendment (2026-09-29)

Administrative suspension or deletion of an account, decided by an admin
for abuse, content leakage, or suspicious behavior, is the sole exception
to decisions 1-3 and to the sentence 'Anything that caps, expires, or
locks access would reverse it.' It is never automatic and suspension is
reversible. Abuse detection stays observational: the platform may surface
signals to an admin but does not lock access itself. No view-count,
expiry, or device cap is introduced.
