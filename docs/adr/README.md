# Architecture Decision Records (ADRs)

Product and architecture decisions for wael-app. Every significant
decision must be documented here as a numbered markdown file
(`NNNN-short-title.md`).

## ADR Process and Template

When adding a new ADR, use the following template:

```markdown
# ADR-NNNN: <Title>

- **Status**: Accepted | Proposed | Deprecated | Superseded
- **Date**: YYYY-MM-DD
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context
<What problem/tradeoff prompted this decision.>

## Decision
<What was decided, precisely, as a numbered list. Record only what was
stated; undecided points go under Open Questions, never into the Decision.>

## Consequences

### Positive
<Benefits.>

### Negative and Tradeoffs
<Costs and tradeoffs.>

## Alternatives Considered
<Other options and why they were rejected.>

## Open Questions
<Undecided points, if any.>

## To verify
<Things to check, phrased as checks — not as facts.>
```

Rules: never write a commit hash you did not capture from git (use the
`none (decision only...)` line instead), never write 40-hex strings, and
say explicitly where nothing is built yet.

## Record Index

*   [ADR-0001: Lesson Videos Hosted on YouTube](0001-youtube-video-hosting.md)
*   [ADR-0002: Single-Role Student App With a Separate Web Admin Panel](0002-single-role-student-app.md)
*   [ADR-0003: Payment Per Subject (Materia)](0003-per-subject-payment.md)
*   [ADR-0004: No Limits on Video Access](0004-unlimited-video-access.md)
*   [ADR-0005: Study Notes (Mozakkerat) Delivered Inside the App](0005-in-app-study-notes.md)
*   [ADR-0006: Support Only Through a WhatsApp Number](0006-whatsapp-only-support.md)
*   [ADR-0007: Core Academy Service Design](0007-core-academy-service.md)
*   [ADR-0008: Admin Identity and Console Boundaries](0008-admin-identity.md)
*   [ADR-0009: File Storage (Local Encrypted Storage at Rest)](0009-file-storage.md)
*   [ADR-0011: Separate Deploy and Mobile Repositories](0011-deploy-and-mobile-repositories.md)
*   [ADR-0012: Academy Admin Content API (Phase 4.1-4.4)](0012-academy-admin-content.md)

### Numbering notes (added 2026-10-06)

- **There is no ADR-0010 file.** ADR-0007 and `docs/core-service/SPEC.md` Section 15 reserved
  0010 for the app-content model (admin-editable UI text and images); it has not been written.
  New ADRs use the next free number after 0012, and the missing 0010 stays reserved for that topic.
- `docs/BOOTSTRAP-REFERENCE.md` cites "ADR-0010" and "ADR-0012", and ADR-0011 mentions "its
  ADR-0010". Those are numbers from saas-core, the reference project that document describes
  (its header says so), not wael-app's ADRs.
