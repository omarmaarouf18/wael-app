# wael-app-deploy

Deploy-only repository for wael-app. The production server's runner reads
this repo and nothing else: no source code, Go toolchain or Flutter SDK is
needed on the host (ADR-0011 in wael-app).

- `docker-compose.yml`, `Caddyfile`, `scripts/`, `.github/workflows/deploy.yml`, `RUNBOOK.md`, `SERVER-MANUAL.md`, `env.production.example`, `README.md`, `.github/actionlint.yaml`:
  mirrored from `wael-app/infrastructure/deploy/` by the publish workflow.
  Do not edit them here.
- `release.env`: the commit sha whose images run. Written by the publish workflow.

Status: LIVE in production. See [RUNBOOK.md](RUNBOOK.md).
