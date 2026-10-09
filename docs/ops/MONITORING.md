# Monitoring — what exists vs TODO

## Exists (wired in the repo or the manual)

- Service `/health` endpoints with strict compose healthchecks (they override
  the Dockerfile fallbacks): gateway `:8080`, auth `:3002`, notification
  `:3004`, academy and console each with their own check
  (`infrastructure/deploy/docker-compose.yml`). `deploy.sh` gates every deploy
  on the public check through Caddy.
- Deploy-time cert expiry check: preflight refuses any cert expiring within
  14 days (`infrastructure/deploy/scripts/preflight.sh`; manual §4/§8). There
  is no rotation job and no continuous expiry watch (see TODO).
- Backup verification: `scripts/backup.sh` keeps an archive only if it passes
  `gzip -t`, runs one at a time (`flock`), records `state/last-backup.env`,
  and pings `BACKUP_PING_URL` (healthchecks.io) on success AND failure — a
  missed nightly run alerts via the check's grace period (manual §10).
- Off-site pull: `scripts/pull-backups.sh` exits 1 with `STALE BACKUPS` when
  the newest archive is older than `BACKUP_MAX_AGE_HOURS` (default 30) and
  with `CORRUPT BACKUP` when `gzip -t` fails, so a failing systemd timer run
  shows up in `systemctl --user status wael-pull-backups` (manual §10).
- Log rotation: compose `x-logging` anchor (json-file, 10 MB × 3). Caddy
  access logs are off by design; 404s from scanners are expected noise.
- Redis memory checklist: `INFO memory` (`used_memory_human`,
  `maxmemory_human`, `evicted_keys` must stay 0) — commands in manual §5
  "Redis memory" and RUNBOOK "Redis at maxmemory".

## TODO (not set up)

- Uptime monitor: TODO — no external uptime check exists (open since the
  2026-10-06 review tracker). Point it at `https://api.<domain>/health` and
  alert the owner on non-200.
- Cert expiry watch: TODO — preflight only checks at deploy time. Renew via
  `--sign-only` before the 14-day preflight window (manual §4).
- Disk watch: TODO — no automated alert. Weekly, as deploybot:

```bash
[server azureuser] docker system df
[server azureuser] sudo -u deploybot bash -c 'du -sh ~/wael/backups/ && ls -la ~/wael/backups/ | tail -5'
```

  Prune only non-last-good images (manual §10 "Disk cleanup"); never
  `docker image prune` between deploys (rollback needs last-good images).

- Daily log checks: TODO — no log review exists. Daily, as deploybot:

```bash
[server azureuser] sudo -u deploybot bash -c 'cd <deploy-checkout> && docker compose -p wael logs --since 24h --tail 50 auth-service academy-service api-gateway notification-service admin-console 2>&1 | grep -iE "error|oom|503|panic" | tail -20'
[server azureuser] journalctl -u docker.service --since -1h | tail -20
```

- Restore drill on production data: TODO — restore is rehearsed locally only
  (manual §10); a production drill is still open.
