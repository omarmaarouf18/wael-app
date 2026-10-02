# ADR-0011: Separate Deploy and Mobile Repositories

- **Status**: Accepted (owner request, 2026-09-30)
- **Date**: 2026-09-30
- **Related Commit SHA**: none (decision only; the implementing commit is cited in AI_CONTEXT.md once it exists)
- **Related finding**: BOOTSTRAP-REFERENCE Section 1.2, S-01, S-02, S-03, S-06; SPEC Section 11 Phase 6.2

## Context

The production host should never need wael-app's source, a Go toolchain or a
Flutter SDK, and the mobile build should not need the Go monorepo. saas-core
solved this with a deploy-only repo and a mobile mirror (its ADR-0010), and
also shipped known weaknesses in that pipeline: publishing in parallel with
the E2E gate (S-01), a deploy step that could take production down before
validation (S-02), rollback to `HEAD~1` (S-03) and health probes with an
HTTP fallback (S-06).

The owner asked on 2026-09-30 to create both repositories now, with
publishing and deploying kept off until the prerequisites exist.
*(Production is now live since `557f367` with `PUBLISH_ENABLED=true` and `DEPLOY_ENABLED=true`.)*

## Decision

1. **`wael-app-deploy` (private).** Contains only `docker-compose.yml`,
   `Caddyfile`, deploy scripts, `RUNBOOK.md`, `release.env` and the deploy
   workflow. Its source of truth is `wael-app/infrastructure/deploy/`; the
   publish workflow mirrors that folder (rsync with delete) and writes
   `release.env` with `IMAGE_TAG=<40-char commit sha>`. Images are tagged by
   commit sha only; no `latest`.
2. **`wael-app-mobile` (private).** A force-pushed `git subtree split` of
   `frontend/`. Its build workflow lives at `frontend/.github/workflows/` in
   wael-app. Nobody commits to it directly.
3. **Ordering.** Publishing and mobile sync trigger on `workflow_run` of
   "CI Gate" with `conclusion == success` on a push to `main`, never in
   parallel with CI. When a release gate (E2E) exists, publishing must wait
   for it too. *(2026-10-02 Q1 decision: satisfied — `E2E (compose)` and `Prod Image Build`
   are part of the CI Gate itself, which runs on `main` before publishing.)*
4. **Switches.** `PUBLISH_ENABLED` and `MOBILE_SYNC_ENABLED` (wael-app) and
   `DEPLOY_ENABLED` (wael-app-deploy) are repository variables, off by
   default. *(2026-10-02 update: Production is live since `557f367` with `PUBLISH_ENABLED=true`
   and `DEPLOY_ENABLED=true`; releases `557f367`, `6bd6c12`, and `b6a11fe` deployed successfully.)*
5. **Deploy behaviour.** Pull-only self-hosted runner (`wael-vm`).
   `preflight.sh` validates files, permissions, placeholders, certificate
   expiry, compose rendering and each new image's `--check-env` before any
   running container is touched. `docker compose up --wait` uses strict
   health checks (CA-verified, no `-k`, no HTTP fallback), then a public
   check through Caddy. Rollback uses the last release recorded by a
   successful deploy, not the previous git commit.
6. **Cross-repo writes** use a GitHub App installation token scoped to the
   one target repository per job; no personal access tokens.

## Consequences

- Deploy files are edited only in wael-app; a hand edit in wael-app-deploy
  is overwritten by the next publish.
- This starts parts of SPEC Phase 6.2 (compose, Caddy, `--check-env`
  pre-flight) before Phases 1-5, as scaffolding that is switched off. The
  academy-service and admin-console entries are added in their phases.
  *(2026-10-02: both entries are now in the stack; admin-console arrived with
  SPEC Phase 6.1.)*
- Rollback covers images only; database changes are not rolled back until a
  migration framework exists.
- Release APKs are debug-signed until the owner provides an upload keystore;
  such builds are uploaded only as labelled workflow artifacts, not releases.

## Alternatives Considered

- **Deploy from wael-app directly (SSH from Actions).** Rejected: needs an
  inbound port and an SSH key stored in GitHub, and puts source on the host.
- **Mobile builds inside wael-app CI.** Possible, but mixes the release APK
  with the Go pipeline; kept separate to mirror saas-core and keep the
  public repo free of signing secrets.

## Open Questions

- Android application id is still `com.wael.app` (template TODO). Changing
  it after the first store upload is not possible; owner to confirm.
- iOS distribution (SPEC Q8) is not covered.

## To verify

- The first sync run pushes a `frontend/`-rooted tree to wael-app-mobile and
  its Build Android workflow starts.
- The first publish run creates five images tagged with the CI-verified
  commit and one commit in wael-app-deploy.
- `scripts/preflight.sh` on the server refuses a placeholder value and an
  `IMAGE_TAG` that is not a full commit sha.
