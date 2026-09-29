# ADR-0005: Study Notes (Mozakkerat) Delivered Inside the App

- **Status**: Accepted
- **Amended**: 2026-09-29
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

Study notes (mozakkerat) are part of what a subject purchase contains (see
ADR-0003), alongside videos. Distributing them externally (files passed
around outside the app) disconnects notes from entitlements: anyone with
the file has the content regardless of purchase. As of this ADR, no code
exists for notes storage, delivery, or rendering.

## Decision

1. Study notes are delivered inside the app, not externally.
2. Notes availability follows subject entitlement: only owned subjects show
   their notes.

## Consequences

### Positive

- Notes stay behind the same entitlement check as videos.
- Reading progress and notes can evolve together in one surface.

### Negative and Tradeoffs

- The app must render note formats natively (reader work per format).
- In-app delivery raises the bar but does not prevent copying: screenshots
  and transcription always exist outside any technical control.

## Alternatives Considered

- **External file distribution**: rejected; it bypasses entitlements entirely.

## Open Questions

- Note format (PDF, HTML, other) and how each renders in-app.
- Storage of note content (backend store, CDN, caching strategy).
- Protection against copying (watermarks, viewer restrictions), with the
  understanding that no measure is complete.

## To verify

- `shared/infra/storage` was dropped from this repo; confirm it is restored
  (or an equivalent built) before the content service ships, since note
  content needs server-side storage.

## Amendment (2026-09-29)

Study notes are PDF files attached to a subject. Owning the subject makes
them available in the app automatically, and every download requires
ownership. This amends decision 1: the app is the only delivery channel,
and a copy saved to the student's device from inside the app is
permitted; distribution outside the app by the platform (email, public
links) remains excluded. Traceability of leaked copies is an open
question in ADR-0007 (Proposed).
