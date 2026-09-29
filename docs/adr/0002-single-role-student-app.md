# ADR-0002: Single-Role Student App With a Separate Web Admin Panel

- **Status**: Accepted
- **Amended**: 2026-09-29
- **Date**: 2026-09-29
- **Related Commit SHA**: none (decision only, no implementation yet)
- **Related Audit Finding**: n/a

## Context

Student authentication in this repo is single-role (`user`): signup, login,
OTP verification, JWT, refresh, and two-phase password reset exist in
`services/auth-service`, with no role field beyond `user` and no
administrative endpoints. The mobile app (`frontend/`) is the student
surface. Platform management (content, entitlements, support operations)
needs an administrative surface, and the question is where it lives.

## Decision

1. The mobile app is student-only: no admin functionality in any form — no
   admin screens, no admin endpoints wired, no admin tokens in the binary.
2. Student auth stays single-role. No role-based navigation exists in the
   app, and none will be added.
3. Platform control and management live in a separate web admin panel, not
   in the mobile app.
4. The admin identity is separate from student accounts. It is created
   out-of-band (CLI), never through any student-facing endpoint.

## Consequences

### Positive

- The student binary cannot leak administrative workflows, endpoint shapes,
  or approval logic through decompilation: there is nothing administrative
  in it to find.
- One authentication model per surface (student JWT in the app; a separate
  admin identity in the panel) instead of a mixed-privilege client.
- Review scope stays small: anything admin-shaped in `frontend/` is a
  defect by construction.

### Negative and Tradeoffs

- Administrative work needs the web panel to exist before it can be done
  through UI; until then it stays manual/CLI.
- Two clients to maintain once the panel is built.

## Alternatives Considered

- **Admin mode inside the mobile app**: rejected. Bundling elevated
  capabilities and admin token flows into the end-user binary exposes them
  to reverse engineering, the same reasoning that keeps reviewer and
  support consoles out of consumer apps.

## To verify

- The out-of-band admin provisioning mechanism does not exist in the repo
  yet (no CLI, no panel); confirm what creates the first admin identity
  before the web panel ships.
- As of this ADR, no code exists for the web admin panel.

## Amendment (2026-09-29)

The first-admin question is answered in ADR-0007 (Proposed): the admin
identity is a named token issued only by a server-side CLI tool, not an
account, and the panel never mints it. Token mechanics are deferred to a
future ADR. No code exists for the panel yet.
