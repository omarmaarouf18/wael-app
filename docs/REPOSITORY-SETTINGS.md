# Repository settings (W-15)

GitHub settings are not visible from the repository, so this file records
what was applied and when. Update it in the same change as any settings change.

Applied 2026-09-30 through the GitHub web UI (owner session).

## wael-app

| Setting | Value |
|---|---|
| Ruleset "main: CI-verified fast-forward only" | Target `main`. Restrict deletions, block force pushes, require linear history, require status checks (GitHub Actions): `Lint & Formatting`, every `Build & Test (...)` matrix entry (7), every `Security Scan (...)` entry (4), `Secret Scan (gitleaks)`, `Flutter Lint & Test`. No pull request required, no bypass actors. A commit can reach `main` only after those checks passed on it (fast-forward from `develop`). |
| Ruleset "develop: no force push, no deletion" | Target `develop`. Restrict deletions, block force pushes. No required checks, so work can still be pushed and checked by CI after the push. |
| Secret Protection | Enabled (secret scanning alerts) |
| Push protection | Enabled |
| Dependency graph / Dependabot alerts | Enabled |
| Dependabot version updates, CodeQL | Not enabled (W-07 adds `.github/dependabot.yml`) |
| Branches | `main`, `develop`. `wire/existing-services` deleted on GitHub (fully merged into `develop`). |

When a matrix entry or job name in `ci.yml` changes, update the required
checks in the `main` ruleset in the same change, or `main` can no longer be
updated.

## wael-app-deploy

| Setting | Value |
|---|---|
| Ruleset "main: no force push, no deletion" | Target `main`. The publish workflow pushes normal commits through the GitHub App. |

## wael-app-mobile

No ruleset on purpose: it is a mirror that the sync workflow force-pushes (ADR-0011).
