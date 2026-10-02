# Repository settings (W-15)

GitHub settings are not visible from the repository, so this file records
what was applied and when. Update it in the same change as any settings change.

Applied 2026-09-30 through the GitHub web UI (owner session).

## wael-app

| Setting | Value |
|---|---|
| Ruleset "main: CI-verified fast-forward only" | Target `main`. Restrict deletions, block force pushes, require linear history, require status check (GitHub Actions): `CI OK`. No pull request required, no bypass actors. A commit can reach `main` only after `CI OK` passes on it (fast-forward from `develop`). |
| Ruleset "develop: no force push, no deletion" | Target `develop`. Restrict deletions, block force pushes, require status check: `CI OK`. |
| Secret Protection | Enabled (secret scanning alerts) |
| Push protection | Enabled |
| Dependency graph / Dependabot alerts | Enabled |
| Dependabot version updates, CodeQL | Not enabled (W-07 adds `.github/dependabot.yml`) |
| Branches | `main`, `develop`. `wire/existing-services` deleted on GitHub (fully merged into `develop`). |

### Required check architecture (owner decision 2026-10-02, Q2)

Instead of listing brittle individual matrix jobs in branch rulesets (which breaks whenever a service is added or removed), `ci.yml` defines a single aggregate job: `CI OK`.
- `CI OK` depends on all CI Gate jobs via `needs:` (including `E2E (compose)`, `Deploy Script Tests`, the admin-console jobs, and `Prod Image Build`).
- It runs with `if: always()` and fails if any required dependency does not succeed.
- `CI OK` is the sole required status check on both `main` and `develop`.

## wael-app-deploy

| Setting | Value |
|---|---|
| Ruleset "main: no force push, no deletion" | Target `main`. The publish workflow pushes normal commits through the GitHub App. |

## wael-app-mobile

No ruleset on purpose: it is a mirror that the sync workflow force-pushes (ADR-0011).
