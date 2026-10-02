# Repository instructions

## Read first

Before starting, read `AI_CONTEXT.md` for the current state and next task. Keep
it accurate and update it in the same commit when your change alters that
state. Read `docs/core-service/SPEC.md`, especially Section 12, and
`docs/adr/README.md` plus the ADRs relevant to the task. Before touching
academy-service code, read ADR-0001 through ADR-0009 as required by SPEC
Section 12. For frontend work, read `frontend/README.md` first.

## Scope and decisions

- Work on `develop`. Keep each task and commit focused on its stated scope.
- Follow locked decisions in `docs/adr/` and `AI_CONTEXT.md`. If work touches
  an open question or conflicts with a locked decision, stop and report it;
  do not decide it yourself.
- Record owner decisions as dated amendments, preserving the existing decision
  text and showing the amendment.
- Changes to suspension, gating, or admin authorization stay off `main` until
  the owner confirms.
- `main` moves only by an owner-approved fast-forward of a `develop` that is green on `CI OK` (owner amendment 2026-10-02). Agents never push `main`.
- Merge to `main` by fast-forward only, after the full test suite and CI pass.
- Never push, rewrite history, or force-push. SPEC Section 12 currently
  prohibits pushes; change that policy in the governing documentation before
  considering any push workflow.

## Verification

Run the relevant gates before committing and report what was run and anything
not verified. Never invent command output or commit hashes.

- Go: `gofmt -l services shared tests tools` must be empty; run `go build
  ./...`, `go vet ./...`, and `go test ./...` for each touched module. Run with
  `-race` for changes involving concurrency, tokens, or stores.
- Security: run the pinned `gosec` and `govulncheck` checks for every touched
  service or shared module.
- Frontend: run `dart format --output=none --set-exit-if-changed lib/ test/`,
  `flutter analyze`, and `flutter test` when `frontend/` changes.
- Run `make ci` before reporting work that touches shared code, CI files, or
  more than one module.
- Configure hooks before staging or committing with `git config core.hooksPath
  .githooks`, or use `make commit MSG="..."`.

## Commits and reports

- Use one logical change per commit and a specific commit message.
- Never amend a commit whose hash is cited in a repository document. Do not
  write a commit hash into a document until the commit exists; verify it with
  `git rev-parse HEAD`, `git cat-file -e <sha>^{commit}`, and, when describing
  a feature, `git show --stat <sha>`.
- Full 40-character hashes in Markdown are live citations and must resolve to
  real commits. Use only verified hashes; truncate historical hashes that are
  not live citations.
- Call out an incidental change under `frontend/lib/widgets/` separately in
  both the commit message and report, titled **Unrelated shared-widget fix**.
- Reports should identify changed files, verification performed, deviations,
  and anything unverified. Quote captured command output exactly when quoting
  it; otherwise summarize it without presenting it as literal output.
- Skipped tests: paste the literal output of
  for m in services/* shared/infra tests/*; do (cd $m && go test -v ./... 2>&1 | grep -- '--- SKIP'); done
  and grep -n 'skip:' frontend/test/*.dart. Never count or describe
  skips from memory.

## Project-specific rules

Follow Section 12 of `docs/core-service/SPEC.md` for implementation work,
including its task scope, open-question, and reporting rules. Treat the
reference implementation pointers and corrections in later SPEC sections as
guidance for the academy service; do not silently turn them into decisions.

## Security defaults
- Environment handling is allowlist-based: only APP_ENV=local|test relaxes
  security. Empty or unknown values are treated as production. Follow the
  pattern in services/*/internal/config/config.go.
- Code ported from a reference project (saas-core) gets a security review
  before commit: list every fail-open or silent-fallback path found and
  report it, even if the SPEC does not mention it.

## Definition of "unverified"
Anything not executed, skipped, or not shown as captured output is
unverified. Skipped tests are listed by name and count. "Unverified: None"
is valid only with zero skipped tests and every gate's output shown.

## Scope
Doc sentences made false by your change are in scope; fix them and list
them under "Docs corrected". If a fix exceeds the stated scope, report it
instead of leaving the doc wrong.

## Output evidence
For each gate (gofmt, vet, test, gosec, govulncheck, contract tests,
flutter analyze/test) show the command and its last 3 output lines, or
write "not captured". Never summarize a gate as passed without this.

## Commit hashes in Markdown
A full 40-character hash in Markdown is allowed only if verified with
`git cat-file -e <sha>^{commit}` in the same session; otherwise use 7
characters. This section is the single source of truth.

## Auto-push (owner amendment 2026-09-30)
Previous rule: never push without explicit confirmation in the current
session. Amended: after finishing a task, the agent pushes `develop`
automatically with `make push`, only when ALL hold:
- every gate for touched modules passed and `make ci` passed (output shown);
- working tree clean, no skipped gate;
- a secret scan of the diff is clean (`gitleaks detect --log-opts
  "origin/develop..HEAD"` or the pre-push equivalent);
- the task did not touch suspension, gating, or admin authorization
  (SPEC Section 12 rule 6): those are pushed to `develop` only after the
  owner says so;
- pushing commits the agent did not author is allowed only after the agent
  runs the full gates and `make ci` on that HEAD itself, with a clean tree,
  plus a secret scan of the range;
- the push is verified: `git rev-parse HEAD` equals
  `git ls-remote origin develop` (show both).
Never push `main`, never force-push, never use --no-verify, never amend or
rewrite pushed commits. On any failure: stop and report, do not retry with
workarounds.

## Session start check (owner amendment 2026-09-30)
At session start, and again before staging, committing, or pushing, verify
ALL of:
- `git status --short` is empty, or every change in it is the agent's own;
- no other agent session is running in this directory (unexpected reflog
  entries between commands, files changing between reads);
- every commit in `origin/develop..HEAD` is accounted for: author session
  known, gates known.
If any check fails: stop and ask the owner. Two agent sessions shared this
worktree on 2026-09-30 and raced; this check exists to prevent a repeat.

