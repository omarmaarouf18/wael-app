# Read AI_CONTEXT.md first

Before doing anything in this repo, read `AI_CONTEXT.md` at the repo root
(current state, done, open, next task). Update it in the same commit as any
change you make — do not leave it stale.

## Verification before commit

1. `gofmt -l services shared tests tools` must return empty.
2. `go build ./...`, `go vet ./...`, `go test ./...` must succeed for every
   module touched.
3. `dart format`, `flutter analyze`, `flutter test` must pass when frontend/
   is touched.
4. Never fabricate commit SHAs or command output; paste real output.
5. One logical change per commit. Never push without explicit confirmation.

`make setup` configures the pre-push hook (`.githooks/pre-push`); `make ci`
runs the same gate locally. Work on `develop`; merge to `main` by
fast-forward after CI passes.
