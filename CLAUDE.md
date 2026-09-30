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

## Project-specific rules

Follow Section 12 of `docs/core-service/SPEC.md` for implementation work,
including its task scope, open-question, and reporting rules. Treat the
reference implementation pointers and corrections in later SPEC sections as
guidance for the academy service; do not silently turn them into decisions.
