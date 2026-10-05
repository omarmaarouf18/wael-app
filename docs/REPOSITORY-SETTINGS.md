# Repository settings (W-15)

GitHub settings are not visible from the repository, so this file records
what was applied and when. Update it in the same change as any settings change.

Applied 2026-09-30 through the GitHub web UI (owner session).

## wael-app

| Setting | Value |
|---|---|
| Ruleset "main: CI-verified fast-forward only" | Target `main`. Restrict deletions, block force pushes, require linear history. Required status checks (observed via the GitHub API 2026-10-02; NOT yet the `CI OK` aggregate — see below): `Lint & Formatting`, eight `Build & Test` matrix entries (api-gateway, auth-service, notification-service, academy-service, shared-infra, docgen, contracts, e2e), five `Security Scan` entries (api-gateway, auth-service, notification-service, academy-service, shared-infra), `Secret Scan (gitleaks)`, `Flutter Lint & Test`, `E2E (compose)`. Missing from the list: `CI OK`, `Prod Image Build`, `Deploy Script Tests`/`deploy-config-check`, `admin-console-web`. No pull request required, no bypass actors. |
| Ruleset "develop: no force push, no deletion" | Target `develop`. Restrict deletions, block force pushes. No required status check is configured on `develop` (observed via the GitHub API 2026-10-02). |
| Secret Protection | Enabled (secret scanning alerts) |
| Push protection | Enabled |
| Dependency graph / Dependabot alerts | Enabled |
| Dependabot version updates, CodeQL | Partial (2026-10-02: weekly docker updates for `infrastructure/deploy` via `.github/dependabot.yml`; no gomod/github-actions updates, no CodeQL; *amended 2026-10-06: `.github/dependabot.yml` now has 17 weekly entries (docker-compose, gomod per module, pub, github-actions, docker per service), all targeting `develop`. GitHub reads that file from the default branch, so it takes effect only after `main` is fast-forwarded to a `develop` that contains it. Still no CodeQL.*) |
| Branches | `main`, `develop`. `wire/existing-services` deleted on GitHub (fully merged into `develop`). |

### Required check architecture (owner decision 2026-10-02, Q2 — DECIDED, NOT YET APPLIED)

Instead of listing brittle individual matrix jobs in branch rulesets (which breaks whenever a service is added or removed), `ci.yml` defines a single aggregate job: `CI OK`.
- `CI OK` depends on all CI Gate jobs via `needs:` (including `E2E (compose)`, `Deploy Script Tests`, the admin-console jobs, and `Prod Image Build`).
- It runs with `if: always()` and fails if any required dependency does not succeed.
- Intended: `CI OK` becomes the sole required status check on both `main` and `develop`.

Status 2026-10-02: the `CI OK` job exists in `ci.yml` (`ci-ok`, `name: CI OK`), but the
GitHub rulesets still enforce the old per-job lists: `main` requires 17 per-job checks
(without `CI OK`, `Prod Image Build`, `deploy-config-check` or `admin-console-web`) and
`develop` requires none. Owner action needed: replace the ruleset check lists with the
single `CI OK` check on both branches (and add it to `develop`), then this file returns
to saying "`CI OK` is the sole required check". Verified with
`gh api repos/omarmaarouf18/wael-app/rulesets/24259214` (main) and `.../24259229` (develop).

## wael-app-deploy

| Setting | Value |
|---|---|
| Ruleset "main: no force push, no deletion" | Target `main`. The publish workflow pushes normal commits through the GitHub App. |

## wael-app-mobile

No ruleset on purpose: it is a mirror that the sync workflow force-pushes (ADR-0011).
