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
