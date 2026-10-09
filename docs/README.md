# Docs index

One line per document. "Source of truth" means the file decides on its topic;
everything else follows it or records a point in time.

- `AI_CONTEXT.md` (repo root) — current state, pending release, next task.
  Source of truth for where the repo stands.
- `CHANGELOG.md` (repo root) — one entry per production release.
  Reconstructable from git; source of truth for release history.
- `docs/core-service/SPEC.md` — build contract for the academy service.
  Source of truth for behavior (Section 1 locked, Section 2 owner-overridable).
- `docs/adr/` — locked product/architecture decisions. Source of truth for
  decisions (see `docs/adr/README.md` for process and numbering).
- `docs/HISTORY.md` — finished-phase record moved out of `AI_CONTEXT.md`.
  History only, not source of truth for current state.
- `docs/RELEASE-CHECKLIST.md` — ordered first-deploy checklist. Follows the
  manual and runbook below.
- `infrastructure/deploy/SERVER-MANUAL.md` — install/run/operate manual.
  Source of truth for the server.
- `infrastructure/deploy/RUNBOOK.md` — short day-2 checklist. Follows the
  manual; each procedure lives in exactly one place.
- `docs/mobile/RELEASE.md` — Android builds, signing, version rule. Follows
  `frontend/android/app/build.gradle.kts` and the build-apk workflow.
- `docs/mobile/PLAY-STORE.md` — Play Console answers mapped to code.
  Unknown answers are marked TODO(owner).
- `docs/admin/CONSOLE-GUIDE.ar.md` — Arabic console guide for center staff.
  Follows `services/admin-console/web`.
- `docs/ops/INCIDENTS.md` — incident response (leaks, rotation, restore).
  Follows the ADRs and the manual.
- `docs/ops/MONITORING.md` — what monitoring exists vs TODO.
- `services/admin-console/README.md` — console routes, config, browser side.
  Source of truth for the console surface.
- `frontend/README.md` — app behavior, backend connection, quality gates.
  Source of truth for the app.
- `docs/frontend/CONTENT-GAPS.md` — owner's exists-vs-missing list.
- `docs/frontend/BEHAVIOR.md` — captured end-to-end outputs (2026-09-30).
- `docs/frontend/VIDEO_PLAYER.md` — player contract and device checks.
- `docs/frontend/DESIGN_SYSTEM.md`, `docs/frontend/STATUS.md` — design tokens
  and per-file composition-gate counts.
- `docs/REPOSITORY-SETTINGS.md` — observed GitHub rulesets and variables.
- `docs/asset-provenance.md` — asset sources; rows stay UNCONFIRMED until the
  owner records them.
- `docs/BOOTSTRAP-REFERENCE.md` — hardening reference and backlog.
- `docs/archive/DOCS-AUDIT-2026-10-02.md` — point-in-time docs audit
  (2026-10-02). Superseded; kept for the fix table, not current state.
