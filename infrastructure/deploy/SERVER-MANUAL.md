# wael-app server manual

Complete manual: install, run and operate wael-app on any fresh server.
Day-to-day checklist: [RUNBOOK.md](RUNBOOK.md) (short; this manual and the
runbook link to each other — each procedure lives in exactly one place).

This file lives in `wael-app/infrastructure/deploy/`, so the publish workflow
mirrors it into `wael-app-deploy` together with the rest of that folder.

Conventions: every command says who runs it where —
`[laptop]`, `[server azureuser]`, `[server deploybot]`, `[GitHub UI]`.
`azureuser` is the admin (sudo) account; `deploybot` runs the stack and the
runner and has no password, no sudo and no SSH login — act as it with
`sudo -u deploybot` from `azureuser`.
Never write a real secret into any file: use placeholders such as
`<GENERATE: openssl rand -hex 32>`.

Status of rehearsal: the pipeline path (build, publish, deploy, rollback) has
run three releases (557f367, 6bd6c12, b6a11fe). Backup and restore (§10) are
rehearsed locally as of 2026-10-02 (throwaway containers; a production drill
is still recommended). Migration (§11) is written from the repo state, **not
yet rehearsed**.

## 0. Overview

### Architecture

```
internet (80/443) ──► Caddy ──┬──► https://api-gateway:8080 ──┬──► https://auth-service:3002 ──► mongo / redis
                              │                                ├──► https://notification-service:3004 ──► mongo / redis
                              │                                └──► https://academy-service:3003 ──► mongo / redis
                              └──► https://admin-console:3005 ──► https://auth-service:9001 (admin listener, mTLS)
                                                              └──► https://academy-service:9002 (admin listener, mTLS)
```

- Caddy terminates public TLS (Let's Encrypt) for `api.<domain>` and
  `admin.<domain>` and re-encrypts every backend hop, verifying the local CA
  (`tls_trusted_ca_certs /certs/ca.crt` in `Caddyfile`; never
  `tls_insecure_skip_verify`).
- Service-to-service traffic is mTLS with per-service certificates signed by
  the offline CA (§4). The `:9001`/`:9002` admin listeners are bound inside
  their containers and never published.
- 8 containers: `caddy`, `api-gateway`, `auth-service`, `notification-service`,
  `academy-service`, `admin-console`, `mongo:7`, `redis:7-alpine`. Only Caddy
  publishes ports (`80/443 TCP, 443/UDP`).

### What lives where

- **Laptop (offline):** the CA directory with `ca.crt` **and `ca.key`**.
  `ca.key` never leaves the laptop; keep an offline backup of it.
- **GitHub:** `wael-app` (source + CI + publish + mobile sync), `wael-app-deploy`
  (private mirror of `infrastructure/deploy/` + `release.env`), `wael-app-mobile`
  (private mirror of `frontend/`), the GitHub App (writes the two mirrors),
  secrets (`APP_ID`, `APP_PRIVATE_KEY`) and vars (`PUBLISH_ENABLED`,
  `MOBILE_SYNC_ENABLED`, `DEPLOY_ENABLED`, `API_BASE_URL`).
- **Server:** `$WAEL_HOME` = `/home/deploybot/wael`, holding `certs/`,
  `secrets/`, `state/` (incl. `state/last-good/`), `backups/` and the git-ignored
  `.env.production` (mode `600`).

### Release flow

```
push main (owner fast-forward of a develop green on CI OK; agents never push main)
  → CI Gate green (incl. E2E (compose) + Prod Image Build)
  → Build and Publish (PUBLISH_ENABLED): 5 sha-tagged images → mirror → wael-app-deploy
  → Deploy on the self-hosted runner [wael-vm]: preflight → up --wait → health gate
  → success: write state/last-good/ (tag + compose/Caddyfile snapshot)
  → failure: automatic rollback.sh to last-good + failed tag recorded in state/failed-releases
```

`deploy.sh` refuses any tag listed in `failed-releases` (override:
`ALLOW_FAILED_RELEASE=1` via the Deploy workflow input). Recovery after a bad
release is always a new commit on `main` (fix forward), never re-running the
old `release.env`. Details: §10; quick commands: RUNBOOK "Operations".

## 1. Requirements

- OS: Ubuntu 24.04 x86_64.
- RAM: recommended 2 GB. Minimum 1 GB **only** with the "1 GB host" env block
  (see below) **plus** 2 GB swap (§3) and zram; expect no headroom.
- Disk: 20 GB or more.
- Open ports: `22` (key-only SSH), `80` and `443` (TCP) and `443/UDP`.
- A domain with `api` and `admin` A records pointing at the server (§2).
- Optional: Resend for email (required by the stack in practice: auth-service
  needs `RESEND_API_KEY`/`RESEND_FROM_EMAIL` outside local/test).

Memory sizing (`docker-compose.yml` defaults fit 2 GB; limits add to ~1.8 GB):

| Service | 2 GB default | 1 GB host |
|---|---|---|
| mongo (`MONGO_MEM_LIMIT`, WiredTiger `MONGO_CACHE_GB`) | 768m, 0.25 GB | 420m, 0.25 GB |
| redis (`REDIS_MEM_LIMIT`) | 128m | 64m |
| api-gateway (`GATEWAY_MEM_LIMIT`) | 128m | 96m |
| auth-service (`AUTH_MEM_LIMIT`) | 192m | 96m |
| notification-service (`NOTIFICATION_MEM_LIMIT`) | 192m | 96m |
| academy-service (`ACADEMY_MEM_LIMIT`) | 192m | 96m |
| admin-console (`ADMIN_MEM_LIMIT`) | 64m | 48m |
| caddy (`CADDY_MEM_LIMIT`) | 128m | 64m |

For a 1 GB host, uncomment the "1 GB host" block at the end of
`env.production.example` in `$WAEL_HOME/.env.production` (≈980 MB total),
add swap first, run nothing else on the host, and watch
`docker stats --no-stream` and OOM kills after the first deploy. The 1 GB
profile has not been tried.

Inside those containers: Redis data is capped at `REDIS_MAXMEMORY`
(default `96mb`, below the 128m container limit) with a fixed `noeviction`
policy, and each Go service carries a `GOMEMLIMIT` at ~85% of its container
`mem_limit` so the runtime garbage-collects before the container OOMs
(defaults: gateway `108MiB`, auth/notification/academy `160MiB` each,
admin-console `54MiB`). Details and the OOM checklist: §5 "Redis memory".

Measured reference (owner-measured, not re-measured here): on Azure B2ats_v2
(887 MB RAM) the running stack uses about 320–400 MB and needs swap.

## 2. DNS

- [GitHub UI / registrar] Create an `A` record for `api.<domain>` and one for
  `admin.<domain>` pointing at the server (add `AAAA` too if the host has IPv6).
- [GitHub UI / Resend dashboard] Resend DKIM/SPF/DMARC (generic forms — exact
  values come from the Resend dashboard for the sending domain):
  - `resend._domainkey.<domain> TXT "<dkim-value-from-resend>"`
  - `<domain> TXT "v=spf1 include:<resend-spf-include> ~all"`
  - `_dmarc.<domain> TXT "v=DMARC1; p=none; rua=mailto:<owner-mailbox>"`
- Verify [laptop or server]:
  ```bash
  dig +short api.<domain> A
  dig +short api.<domain> A @<authoritative-nameserver>   # bypasses local cache
  ```
  A fresh record can look missing locally because of negative caching — if the
  authoritative NS answers but the default resolver does not, wait out the
  negative TTL instead of recreating the record.
- `.app` is HSTS-preloaded: HTTPS only, no plain-HTTP fallback. Caddy sends
  `Strict-Transport-Security: max-age=31536000; includeSubDomains` on both
  site blocks (owner decision 2026-10-05; no `preload` directive).

## 3. Base server setup ([server azureuser])

`azureuser` (the admin user) runs everything here with sudo. `deploybot` runs
the stack and the runner: member of the `docker` group only (accepted
trade-off: the docker group is root-equivalent), no password, no sudo, no SSH
login.

```bash
[server azureuser] sudo apt update && sudo apt full-upgrade -y && sudo reboot
# reconnect, then:
[server azureuser] sudo apt update && sudo apt install -y zram-tools fail2ban unattended-upgrades
```

Swap (2 GB file, swappiness 10):

```bash
[server azureuser] sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile \
  && sudo mkswap /swapfile && sudo swapon /swapfile
[server azureuser] echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
[server azureuser] echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-wael.conf
[server azureuser] sudo sysctl --system && swapon --show && cat /proc/sys/vm/swappiness
```

zram (zstd, 50 %, priority 100):

```bash
[server azureuser] sudo tee /etc/default/zramswap <<'EOF'
ALGO=zstd
PERCENT=50
PRIORITY=100
EOF
[server azureuser] sudo systemctl enable --now zramswap.service && zramctl
```

journald cap (100M):

```bash
[server azureuser] sudo mkdir -p /etc/systemd/journald.conf.d
[server azureuser] printf '[Journal]\nSystemMaxUse=100M\n' | sudo tee /etc/systemd/journald.conf.d/99-wael-cap.conf
[server azureuser] sudo systemctl restart systemd-journald
```

fail2ban (sshd jail):

```bash
[server azureuser] printf '[DEFAULT]\nbantime = 1h\nfindtime = 10m\nmaxretry = 5\n[sshd]\nenabled = true\n' | sudo tee /etc/fail2ban/jail.local
[server azureuser] sudo systemctl enable --now fail2ban && sudo fail2ban-client status sshd
```

Automatic security updates:

```bash
[server azureuser] sudo dpkg-reconfigure -plow unattended-upgrades
```

Small hosts: disable and mask the heavy/unneeded units (ignore "not found" —
minimal images may not ship them):

```bash
[server azureuser] sudo systemctl disable --now multipathd.service multipathd.socket 2>/dev/null || true
[server azureuser] sudo systemctl mask multipathd
[server azureuser] sudo systemctl disable --now packagekit.service 2>/dev/null || true
[server azureuser] sudo systemctl mask packagekit.service
[server azureuser] sudo systemctl disable --now fwupd.service fwupd-refresh.timer 2>/dev/null || true
[server azureuser] sudo systemctl mask fwupd.service
```

Docker Engine + Compose v2 from Docker's apt repo:

```bash
[server azureuser] sudo install -m 0755 -d /etc/apt/keyrings
[server azureuser] curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
[server azureuser] sudo chmod a+r /etc/apt/keyrings/docker.gpg
[server azureuser] echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list
[server azureuser] sudo apt update && sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
[server azureuser] docker compose version && sudo systemctl enable --now docker
```

deploybot + layout (mode 700):

```bash
[server azureuser] sudo useradd -m -s /bin/bash deploybot && sudo usermod -aG docker deploybot
[server azureuser] sudo passwd -l deploybot && sudo usermod -p '!' deploybot   # no password login, no sudo, no authorized_keys
[server azureuser] sudo mkdir -p /home/deploybot/wael/{certs,secrets,state,backups} /home/deploybot/wael/bin
[server azureuser] sudo chown -R deploybot:deploybot /home/deploybot/wael
[server azureuser] sudo chmod 700 /home/deploybot/wael /home/deploybot/wael/{certs,secrets,state,backups} /home/deploybot/wael/bin
[server azureuser] sudo -u deploybot bash -c 'ls -la ~/wael'
```

### Common pitfalls

- `sudo cd` does not exist (`cd` is a shell builtin). `cd` as deploybot via
  `sudo -u deploybot bash -c 'cd ~/wael && ...'`, or `cd` to a world-readable
  path first.
- A glob inside a 700 dir fails for anyone but the owner ("No such file"):
  run the whole glob as root or as the owner, e.g.
  `sudo sh -c 'ls -la /home/deploybot/wael/certs/'`.
- Never pipe a heredoc through `sudo -iu … bash -c` (quoting breaks); use
  `sudo tee <file> <<'EOF'` for root-owned files and `sudo -u deploybot`
  for deploybot-owned ones.

## 4. Certificates (mTLS)

One CA for the whole fleet. Create it once [laptop] from a wael-app checkout:

```bash
[laptop] ./infrastructure/certs/generate-certs.sh
```

This writes `ca.crt`/`ca.key` (4096-bit, 825 days) plus one 2048-bit cert per
service (`api-gateway`, `auth-service`, `notification-service`,
`academy-service`, `admin-console`; each 825 days, SANs
`DNS:<svc>,DNS:localhost,IP:127.0.0.1`). `ca.key` never leaves the laptop —
keep an offline backup (e.g. encrypted USB). Expiry check any time:

```bash
[laptop] openssl x509 -enddate -noout -in ca.crt
[laptop] openssl x509 -checkend $((14*86400)) -noout -in <svc>.crt && echo "valid 14+ days"
```

Preflight refuses any cert expiring within 14 days
(`infrastructure/deploy/scripts/preflight.sh`), so renew before that.

Sign each service (or one service later) with the existing CA — never re-run
the script without arguments on a live fleet (a full run creates a NEW CA and
replaces every certificate, breaking the running stack):

```bash
[laptop] ./infrastructure/certs/generate-certs.sh --sign-only <svc> --ca-dir <offline-ca-dir> --out-dir ./signed
```

`--ca-dir` must hold the original `ca.crt` and `ca.key` (read-only; nothing is
written there). It refuses to overwrite an existing `<svc>.crt/.key` in
`--out-dir` unless `--force` is added. Adding a new service later means a new
cert the same way — the `admin-console` case (server setup step 7 in RUNBOOK
history) is the example.

Install on the server (`scp` to `/tmp`, then move as root because the 700
`certs/` dir blocks traversal):

```bash
[laptop] scp ./signed/<svc>.crt ./signed/<svc>.key azureuser@<host>:/tmp/
[server azureuser] sudo install -o deploybot -g deploybot -m 644 /tmp/<svc>.crt /tmp/<svc>.key /home/deploybot/wael/certs/
[server azureuser] rm /tmp/<svc>.crt /tmp/<svc>.key
[server azureuser] sudo sh -c 'ls -la /home/deploybot/wael/certs/'
```

Repeat for `ca.crt` and all 5 services on first install. Ownership
`deploybot:deploybot`, mode `644` for keys — the key MUST stay world-readable
because every service container runs as `USER appuser` (see each
`services/*/Dockerfile`) with a dynamically assigned UID, reading host-mounted
`:ro` files owned by `deploybot` (see the `volumes:` in `docker-compose.yml`):
anything stricter denies the container. (W-10, a fixed container UID that
would allow `640`, is still open.)

Renewal: sign fresh certs with the same CA, install as above, then re-run
preflight and deploy the current `release.env` so containers pick them up
(§10). Compromised or replaced CA means re-issuing everything and restarting
the stack — plan a maintenance window.

## 5. Secrets and .env.production

The real file lives ONLY at `$WAEL_HOME/.env.production` (owner deploybot,
mode `600`); it is never committed. `APP_ENV` must NOT appear in it (compose
pins `APP_ENV=production`; preflight fails otherwise). Every name below is
checked against each service's `config.Load()` and `env.production.example`.

| Name | Service(s) | Required? | Meaning | Value / generation |
|---|---|---|---|---|
| `API_DOMAIN` | caddy (+ gateway `APP_DOMAIN`) | yes | Public API host | `api.<domain>` (DNS §2) |
| `ADMIN_DOMAIN` | caddy, admin-console reachability | yes | Public admin host | `admin.<domain>` (DNS §2) |
| `ACME_EMAIL` | caddy | yes | Let's Encrypt account mail | operator email address |
| `ALLOWED_ORIGIN` | api-gateway | yes | CORS origin for browser clients | `https://api.<domain>` |
| `JWT_SECRET` | auth, notification, academy | yes | Signs access/refresh tokens | `<GENERATE: openssl rand -hex 32>` |
| `GATEWAY_SECRET` | gateway + auth/notification/academy | yes | `X-Gateway-Secret` edge auth | `<GENERATE: openssl rand -hex 32>` |
| `INTERNAL_SERVICE_TOKEN` | auth, notification, academy, admin-console | yes | `X-Internal-Token` for admin listeners | `<GENERATE: openssl rand -hex 32>` |
| `MONGO_ROOT_USERNAME` | mongo | yes | InitDB root user | `wael_root` |
| `AUTH_MONGO_URI` | auth-service | yes | Least-privilege DB user URI | `mongodb://auth_svc:<pw>@mongo:27017/auth_db?authSource=auth_db`, pw `<GENERATE: openssl rand -hex 24>` (hex needs no URL escaping) |
| `NOTIFICATION_MONGO_URI` | notification-service | yes | Least-privilege DB user URI | `mongodb://notif_svc:<pw>@mongo:27017/notification_db?authSource=notification_db`, pw as above (different value) |
| `ACADEMY_MONGO_URI` | academy-service | yes | Least-privilege DB user URI | `mongodb://academy_svc:<pw>@mongo:27017/academy_db?authSource=academy_db`, pw as above (different value) |
| `REDIS_URI` | gateway, auth, notification, academy | yes | Must match `requirepass` in `secrets/redis.conf` | `redis://:<pw>@redis:6379/0`, pw `<GENERATE: openssl rand -hex 24>` |
| `RESEND_API_KEY` | auth-service | yes (production) | Email delivery | from the Resend dashboard |
| `RESEND_FROM_EMAIL` | auth-service | yes (production) | Sender identity | `no-reply@<domain>` (verify domain in Resend) |
| `BLOCKLIST_HMAC_KEY` | auth-service | yes (production) | HMAC key for blocked email/phone identities — do NOT rotate casually: existing entries stop matching | `<GENERATE: openssl rand -hex 32>` |
| `DEFAULT_PHONE_REGION` | auth-service | no (default `EG`) | Phone normalization region | `EG` |
| `JWT_ACCESS_TTL` | auth-service | no (default `24h`) | Access-token lifetime, Go duration within `5m`..`24h`; out of range or unparsable fails startup and `--check-env` (added 2026-10-08) | leave unset unless the owner decides a shorter lifetime |
| `SUPPORT_WHATSAPP` | academy-service | yes (production) | Support link shown after access requests | international format, e.g. `+20...` |
| `TERMS_URL` | academy-service | yes (production) | Terms page in the public app config (F-UX2 A7) | `https://` required outside dev |
| `PRIVACY_URL` | academy-service | yes (production) | Privacy page in the public app config (F-UX2 A7) | `https://` required outside dev |
| `MIN_VERSION` / `LATEST_VERSION` / `UPDATE_URL` | academy-service | no (empty = no update prompt) | Update metadata in the public app config (F-UX2 A7) | `UPDATE_URL` must use `https://` when set |
| `EXPOSE_PRICE_TO_STUDENTS` | academy-service | no (default `false`) | Show subject prices | leave `false` unless the owner decides otherwise |
| `RATE_LIMIT_READ/PLAY/DOWNLOAD/WRITE` | academy-service | no (defaults 120/60/10/5) | Per-user per-minute tiers | uncomment to override |
| `STORAGE_DIR` | academy-service (compose bind mount), backup.sh | yes (added 2026-10-09, Phase 5) | HOST directory of the encrypted PDFs; compose mounts it at `/data/files` | `/home/deploybot/wael/storage`, created by deploybot with mode `700` (see "Files (Phase 5)" below) |
| `DOCUMENT_ENCRYPTION_KEY` | academy-service | yes (added 2026-10-09) | AES-256-GCM key of every stored PDF (ADR-0009). Never change it once files exist: they become unreadable | `<GENERATE: openssl rand -hex 32>`; copy it into the owner's password manager BEFORE the first upload |
| `WAEL_UID` / `WAEL_GID` | compose (`user:` of academy-service) | yes (added 2026-10-09) | academy-service runs as deploybot so it and backup/restore share the files | `id -u deploybot` / `id -g deploybot` |
| `MAX_PDF_BYTES` | academy-service, admin-console | no (default 20971520 = 20 MB, SPEC D14) | Max PDF upload; the console's upload-route body cap | uncomment to override; keep one value for both |
| `MAX_CONCURRENT_DOWNLOADS` | academy-service | no (default `3`, added 2026-10-09, owner decision) | Student PDF downloads in flight at once; each holds one copy of its file in memory. When all are busy the next download gets `429` with `Retry-After: 5` (nothing queues) | raise only with `ACADEMY_MEM_LIMIT` headroom: about 20 MB per slot at the 20 MB cap |
| `FEATURES_FILES` | academy-service | no (default `false`) | App-config `features.files`: `true` shows notes & books downloads in the app | `false` until a test upload and download worked (see "Files (Phase 5)") |
| `STREAM_MAX_CONCURRENT` / `STREAM_OPEN_RATE_LIMIT` | notification-service | no (defaults 3 / 10) | SSE caps | uncomment to override |
| `NOTIFICATION_SERVICE_URL` | academy-service | yes (production) | Internal mTLS notification push URL for student notifications (request accept/reject, grant/revoke) | fixed in compose: `https://notification-service:3004` (requires https in production) |
| `*_MEM_LIMIT`, `MONGO_CACHE_GB` | compose only | no | Container memory / WiredTiger cache | §1 table; "1 GB host" block for small hosts |
| `REDIS_MAXMEMORY` | redis (compose `command:`) | no (default `96mb`) | Redis data cap, kept below `REDIS_MEM_LIMIT` (128m) for AOF-rewrite fork copy-on-write and client buffers | `96mb` (2 GB host) / `48mb` ("1 GB host" block); see "Redis memory" below |
| `GATEWAY_GOMEMLIMIT`, `AUTH_GOMEMLIMIT`, `NOTIFICATION_GOMEMLIMIT`, `ACADEMY_GOMEMLIMIT`, `ADMIN_GOMEMLIMIT` | compose only (Go runtime) | no (defaults `108MiB` / `160MiB` / `160MiB` / `160MiB` / `54MiB`, ~85% of each container `mem_limit`) | Go heap caps so the runtime GCs before the container OOMs; read by the Go runtime itself, ignored by `config.Load()` | uncomment to override per service |

Fixed by compose (never in `.env.production`): `APP_ENV=production`, `PORT`,
`ADMIN_LISTEN_ADDR` (`:9001`/`:9002`), `*_MONGO_DATABASE`, internal
`https://<svc>:<port>` URLs (including `NOTIFICATION_SERVICE_URL=https://notification-service:3004` on `academy-service`), `TRUSTED_PROXY_IPS=172.30.0.10`,
`TLS_*_PATH=/app/certs/...`, and on `academy-service` `STORAGE_DIR=/data/files`
(the container side of the bind mount; added 2026-10-09). Infra-provided (not secrets): `IMAGE_TAG` comes
from `release.env` (written by publish), `WAEL_HOME` from the runner env.

Generate everything on the server only, with a script that writes the file as
deploybot (mode `600`) — values never leave the server. Save as
`/tmp/gen-env.sh`, review it, then run it:

```bash
[server azureuser] cat > /tmp/gen-env.sh <<'SCRIPT'
#!/usr/bin/env bash
# Scaffold $WAEL_HOME/.env.production as deploybot. Review, then fill the
# OPERATOR_* values (or pass them as env). Re-running overwrites secrets.
set -euo pipefail
umask 077
: "${WAEL_HOME:=/home/deploybot/wael}"
: "${OPERATOR_API_DOMAIN:=api.<domain>}"
: "${OPERATOR_ADMIN_DOMAIN:=admin.<domain>}"
: "${OPERATOR_ACME_EMAIL:=<operator-mailbox>}"
: "${OPERATOR_RESEND_KEY:=<paste-from-resend-dashboard>}"
: "${OPERATOR_WHATSAPP:=+<international-number>}"
MONGO_AUTH_PW="$(openssl rand -hex 24)"
MONGO_NOTIF_PW="$(openssl rand -hex 24)"
MONGO_ACADEMY_PW="$(openssl rand -hex 24)"
REDIS_PW="$(openssl rand -hex 24)"
{
echo "API_DOMAIN=${OPERATOR_API_DOMAIN}"
echo "ADMIN_DOMAIN=${OPERATOR_ADMIN_DOMAIN}"
echo "ACME_EMAIL=${OPERATOR_ACME_EMAIL}"
echo "ALLOWED_ORIGIN=https://${OPERATOR_API_DOMAIN}"
echo "JWT_SECRET=$(openssl rand -hex 32)"
echo "GATEWAY_SECRET=$(openssl rand -hex 32)"
echo "INTERNAL_SERVICE_TOKEN=$(openssl rand -hex 32)"
echo "MONGO_ROOT_USERNAME=wael_root"
echo "AUTH_MONGO_URI=mongodb://auth_svc:${MONGO_AUTH_PW}@mongo:27017/auth_db?authSource=auth_db"
echo "NOTIFICATION_MONGO_URI=mongodb://notif_svc:${MONGO_NOTIF_PW}@mongo:27017/notification_db?authSource=notification_db"
echo "ACADEMY_MONGO_URI=mongodb://academy_svc:${MONGO_ACADEMY_PW}@mongo:27017/academy_db?authSource=academy_db"
echo "REDIS_URI=redis://:${REDIS_PW}@redis:6379/0"
echo "RESEND_API_KEY=${OPERATOR_RESEND_KEY}"
echo "RESEND_FROM_EMAIL=no-reply@${OPERATOR_API_DOMAIN#api.}"
echo "BLOCKLIST_HMAC_KEY=$(openssl rand -hex 32)"
echo "DEFAULT_PHONE_REGION=EG"
echo "SUPPORT_WHATSAPP=${OPERATOR_WHATSAPP}"
echo "EXPOSE_PRICE_TO_STUDENTS=false"
echo "TERMS_URL=https://${OPERATOR_API_DOMAIN#api.}/terms"
echo "PRIVACY_URL=https://${OPERATOR_API_DOMAIN#api.}/privacy"
# Phase 5 files (added 2026-10-09). On a server that already stores files,
# never re-run this script: a new DOCUMENT_ENCRYPTION_KEY makes them unreadable.
echo "STORAGE_DIR=$WAEL_HOME/storage"
echo "DOCUMENT_ENCRYPTION_KEY=$(openssl rand -hex 32)"
echo "WAEL_UID=$(id -u)"
echo "WAEL_GID=$(id -g)"
echo "FEATURES_FILES=false"
} > "$WAEL_HOME/.env.production"
chmod 600 "$WAEL_HOME/.env.production"
mkdir -p -m 700 "$WAEL_HOME/storage"
SCRIPT
[server azureuser] sudo -u deploybot bash /tmp/gen-env.sh && rm /tmp/gen-env.sh
[server azureuser] sudo -u deploybot bash -c 'ls -la ~/wael/.env.production'
```

Then the two secret files (passwords must match the URIs above — read them
back from the file, §6):

```bash
[server azureuser] sudo -u deploybot bash -c 'openssl rand -hex 24 > ~/wael/secrets/mongo_root_password && chmod 600 ~/wael/secrets/mongo_root_password'
[server azureuser] sudo -u deploybot bash -c 'R="$(sed -n "s#^REDIS_URI=redis://:\\([^@]*\\)@.*#\\1#p" ~/wael/.env.production)"; printf "requirepass %s\nappendonly yes\n" "$R" > ~/wael/secrets/redis.conf && chmod 600 ~/wael/secrets/redis.conf'
```

- `secrets/mongo_root_password` is injected as `MONGO_INITDB_ROOT_PASSWORD_FILE`
  (a file, never an env var, so it stays out of `docker inspect`); the compose
  file reads it via the `mongo_root_password` secret.
- `secrets/redis.conf` is mounted as the redis config (`requirepass` +
  `appendonly yes`); the password never appears on a command line.
- `--check-env`: all 5 services accept `--check-env` (runs `config.Load()`,
  exits 0/1 without starting anything). For `academy-service`, this validates
  required production variables including `NOTIFICATION_SERVICE_URL` (must use
  `https://` outside dev), `SUPPORT_WHATSAPP`, `MONGO_URI`, `REDIS_URI`,
  `JWT_SECRET`, `GATEWAY_SECRET`, `INTERNAL_SERVICE_TOKEN`, `AUTH_SERVICE_URL`,
  `AUTH_ADMIN_URL`, and mTLS cert paths (`TLS_CERT_PATH`, `TLS_KEY_PATH`,
  `TLS_CA_PATH`). Preflight (§8/§10) runs it for every
  service in one-off containers (`compose run --rm --no-deps -T <svc>
  --check-env`) after pulling the new images and before touching anything
  running. For `academy-service` this also validates `TERMS_URL` and
  `PRIVACY_URL` (required, `https://` outside dev).
  *(Added 2026-10-08, review P1:)* `api-gateway`, `auth-service`,
  `notification-service` and `academy-service` also refuse to start (and fail
  `--check-env`) when `JWT_SECRET`, `GATEWAY_SECRET` or `INTERNAL_SERVICE_TOKEN`
  (each one the service loads) is shorter than 32 bytes or contains `PASTE_`,
  `CHANGE_ME` or `devpassword123` (any case), the same rule as preflight item 3.
  `admin-console` does not apply this check to its `INTERNAL_SERVICE_TOKEN` yet.
  *(Added 2026-10-09, Phase 5:)* `academy-service` also validates `STORAGE_DIR`
  (required), `DOCUMENT_ENCRYPTION_KEY` (exactly 64 hex characters, never
  printed), `MAX_PDF_BYTES` (positive integer) and `FEATURES_FILES` (`true` or
  `false` only) and `MAX_CONCURRENT_DOWNLOADS` (positive integer); `admin-console`
  validates `MAX_PDF_BYTES`.

### Files (Phase 5)

*(Added 2026-10-09, branch `feat/phase5-files`; applies once that release is
deployed.)* Notes and books are PDFs the admin uploads in the console's Files
tab. academy-service encrypts each one with `DOCUMENT_ENCRYPTION_KEY`
(AES-256-GCM, ADR-0009) into `STORAGE_DIR` and streams it only to students who
own the subject, checking ownership on every download. The app shows downloads
only while `FEATURES_FILES=true`.

Adding it to the running server (once, before the deploy that brings Phase 5;
the deploy fails at preflight until these exist):

```bash
[server azureuser] sudo -u deploybot mkdir -m 700 /home/deploybot/wael/storage
[server azureuser] sudo -u deploybot bash -c 'K="$(openssl rand -hex 32)"; printf "STORAGE_DIR=/home/deploybot/wael/storage\nDOCUMENT_ENCRYPTION_KEY=%s\nWAEL_UID=%s\nWAEL_GID=%s\nFEATURES_FILES=false\n" "$K" "$(id -u)" "$(id -g)" >> ~/wael/.env.production'
[server azureuser] sudo -u deploybot sed -n 's/^DOCUMENT_ENCRYPTION_KEY=//p' /home/deploybot/wael/.env.production
```

The last command prints the key once: put it in the owner's password manager
now, before the first upload, and nowhere else (not in the repo, not next to
a backup). A files backup is useless without it, and a new key makes every
stored file unreadable, so the key is never rotated while files exist.

Preflight then checks (item 3b below): the key is 64 hex characters,
`WAEL_UID`/`WAEL_GID` are the ids of the user running it (deploybot), and
`STORAGE_DIR` is an absolute path to an existing mode-`700` directory owned by
that uid. academy-service runs as that uid/gid (compose `user:`), not as the
image's `appuser`, so the files it writes (mode `600`) are the same owner that
`backup.sh` and `restore.sh` use. *(Owner decision 2026-10-09: accepted. W-10,
a pinned container uid for every image, stays open as its own item; the other
services still run as `appuser`.)*

Turning downloads on (owner; this order accepted by the owner 2026-10-09).
The app shows downloads only while the flag is on, so the phone check
happens with the flag on, while only a test file exists:

1. Deploy with `FEATURES_FILES=false` (students see nothing new).
2. In the console, Catalog: create a test diploma and leave it unpublished
   (students never see an unpublished level or what is in it), add a subject
   in it with one video, and publish the subject (the grant dialog lists
   published subjects only; the diploma stays unpublished). Grant that subject
   to your own test student account (Accounts, "المواد"). Files tab: pick the
   subject and upload a small test PDF; it appears in the list with its size.
3. Set `FEATURES_FILES=true` in `.env.production` and redeploy the current
   release (Actions -> Deploy -> Run workflow). Students now see the notes tab
   as "Notes & books", empty for everyone except your test account, because no
   real subject has files yet.
4. Within about 5 minutes (the app's config cache), on a phone signed in with
   the test account: open the test subject, download the file, open it. Sign
   in with another account and check the file is not offered and that the
   download is refused.
5. If anything is wrong: `FEATURES_FILES=false` and redeploy; no new APK.
   Otherwise upload the real notes and books into the real subjects.

Limits: one PDF per upload, at most `MAX_PDF_BYTES` (20 MB). A download is
decrypted in memory before it is sent (about one copy of the file per
download in flight), so academy-service serves at most
`MAX_CONCURRENT_DOWNLOADS` (default 3) at once (owner decision 2026-10-09):
about 60 MB at the 20 MB cap, inside `ACADEMY_MEM_LIMIT` (192m). The next
student gets `429` with `Retry-After: 5`, which the app shows as "try again
later"; nothing waits in a queue. Raise the cap only together with
`ACADEMY_MEM_LIMIT` (and `ACADEMY_GOMEMLIMIT`).

Completeness proof (run from `wael-app/infrastructure/deploy/`): every name
set in `env.production.example` is referenced by `docker-compose.yml`, and the
only compose-referenced names missing from the example are the commented
tuning overrides, `IMAGE_TAG` (from `release.env`) and `WAEL_HOME` (host env):

```bash
[laptop] grep -o -E '\$\{[A-Z_][A-Z_0-9]*' docker-compose.yml Caddyfile | sed 's/.*\${//' | sort -u > /tmp/used
[laptop] grep -E '^[A-Z_]+=' env.production.example | cut -d= -f1 | sort -u > /tmp/set
[laptop] grep -E '^# (RATE_LIMIT_[A-Z]+|STREAM_[A-Z_]+|[A-Z]+_MEM_LIMIT|MONGO_CACHE_GB|REDIS_MAXMEMORY|[A-Z]+_GOMEMLIMIT|MAX_PDF_BYTES|MAX_CONCURRENT_DOWNLOADS)=' env.production.example | cut -d= -f1 | sed 's/^# //' | sort -u > /tmp/tuning
[laptop] comm -23 /tmp/used <(sort -u /tmp/set /tmp/tuning); echo "only IMAGE_TAG and WAEL_HOME may remain"
```

### Redis memory

Without a cap, Redis grows until the kernel OOM-kills the 128m container —
and because JWT checks are fail-closed (Redis down = every authenticated
call refused), the whole platform goes down with it. The compose file caps
Redis on the `command:` line (`--maxmemory ${REDIS_MAXMEMORY:-96mb}`), NOT in
`secrets/redis.conf` (that file lives only on the server; a repo change must
not need a manual server step — CLI args after the config file override it,
so a normal deploy picks the cap up).

The eviction policy is fixed to `noeviction` in the same `command:` line and
preflight refuses any rendered compose without it. Never change it to an
LRU/LFU/random/volatile policy: eviction could drop denylist keys
(jti/sid/user revocation) and revive revoked tokens. Preflight also refuses a
rendered compose that has no non-zero `--maxmemory` cap (amended 2026-10-06,
`b79aaaa`: the earlier check matched any `--maxmemory` text, including
`--maxmemory-policy`, so it passed with no cap).

At the limit, writes fail with OOM errors while reads keep working: new
logins, refreshes and OTP issues fail, existing sessions keep being
validated. That is intended — degraded, not dead, and fail-closed.

If Redis reports OOM (write errors in service logs, `auth-service` 503s on
login/refresh), check usage from the server:

```bash
[server azureuser] sudo -u deploybot bash -c 'R="$(sed -n "s/^requirepass //p" ~/wael/secrets/redis.conf)"; docker exec -i -e REDISCLI_AUTH="$R" wael-redis-1 redis-cli INFO memory' | grep -E 'used_memory_human|maxmemory_human|evicted_keys|expired_keys'
[server azureuser] sudo -u deploybot bash -c 'R="$(sed -n "s/^requirepass //p" ~/wael/secrets/redis.conf)"; docker exec -i -e REDISCLI_AUTH="$R" wael-redis-1 redis-cli CONFIG GET "maxmemory*"'
```

Expect `maxmemory_human:96.00M` (or the `REDIS_MAXMEMORY` override),
`maxmemory_policy:noeviction`, and `evicted_keys:0` always — a non-zero
`evicted_keys` means an eviction policy is active and must be fixed
immediately. If `used_memory_human` sits at the cap, find what grew (key
count by prefix, TTLs on OTP/attempt keys) before raising the cap; raising
`REDIS_MAXMEMORY` without raising `REDIS_MEM_LIMIT` headroom risks the
container OOM-kill this cap exists to prevent.

## 6. Mongo first-time init

Three database-backed services, one least-privilege user each (`auth_svc` on
`auth_db`, `notif_svc` on `notification_db`, `academy_svc` on `academy_db`).
They cannot start until their users exist, so bring up only mongo first (from
the deploy checkout on the server, `$WAEL_HOME/.env.production` in place and a
`release.env` present so the compose file renders):

```bash
[server azureuser] sudo -u deploybot bash -c 'cd <deploy-checkout> && export WAEL_HOME=/home/deploybot/wael && source scripts/lib.sh && compose up -d --wait mongo'
```

`mongod` restarts once during init: a first `mongosh` right after
`up --wait mongo` can get `ECONNREFUSED` — wait 10 s and retry:

```bash
[server azureuser] sudo -u deploybot bash -c 'cd <deploy-checkout> && export WAEL_HOME=/home/deploybot/wael && source scripts/lib.sh && sleep 10 && compose exec -T mongo mongosh -u wael_root -p "$(cat "$WAEL_HOME/secrets/mongo_root_password")" --authenticationDatabase admin --eval '\''
  db.getSiblingDB("auth_db").createUser({user:"auth_svc",pwd:"<pw-auth>",roles:[{role:"readWrite",db:"auth_db"}]});
  db.getSiblingDB("notification_db").createUser({user:"notif_svc",pwd:"<pw-notif>",roles:[{role:"readWrite",db:"notification_db"}]});
  db.getSiblingDB("academy_db").createUser({user:"academy_svc",pwd:"<pw-academy>",roles:[{role:"readWrite",db:"academy_db"}]});'\'
```

Read each `<pw-…>` back from its URI in `.env.production` (they were written
by the §5 script — never invent new ones here, or the URIs and the users
diverge):

```bash
[server azureuser] sudo -u deploybot bash -c 'sed -n "s#^AUTH_MONGO_URI=mongodb://auth_svc:\\([^@]*\\)@.*#\\1#p" ~/wael/.env.production'
```

Repeat for `NOTIFICATION_MONGO_URI` / `notif_svc` and `ACADEMY_MONGO_URI` /
`academy_svc`, paste each as its `<pw-…>`. Uses one different
`openssl rand -hex 24` per service (hex needs no URL escaping).

## 7. GitHub setup

`wael-app` ([laptop] with `gh`, or [GitHub UI]):

```bash
[laptop] gh variable set PUBLISH_ENABLED --repo omarmaarouf18/wael-app --body true
[laptop] gh variable set MOBILE_SYNC_ENABLED --repo omarmaarouf18/wael-app --body true
[laptop] gh secret set APP_ID --repo omarmaarouf18/wael-app --body "<numeric-app-id>"
[laptop] gh secret set APP_PRIVATE_KEY --repo omarmaarouf18/wael-app < app-private-key.pem
```

The GitHub App: permissions **Contents read/write** and **Workflows
read/write**; installed on `wael-app-deploy` and `wael-app-mobile` only
([GitHub UI] App settings → Install). After any permission change, accept the
new permissions on each installation, or the mirror steps fail (past failure:
mirror failed with "Invalid keyData" when `APP_PRIVATE_KEY` was malformed, and
with permission errors before Workflows R/W was granted and accepted).

`wael-app-deploy`: environment `production`; variables `DEPLOY_ENABLED=true`
(and optional `WAEL_HOME` if it differs from `/home/deploybot/wael`).
`wael-app-mobile`: variable `API_BASE_URL=https://api.<domain>` (must be
`https://*`; enforced by the APK workflow).

Self-hosted runner (pull-only: it polls GitHub; the host opens no inbound
port, GitHub holds no SSH key). Register [GitHub UI]
`wael-app-deploy → Settings → Actions → Runners → New self-hosted runner`
(copy the token — it expires within the hour), then on the server:

```bash
[server azureuser] sudo mkdir -p /home/deploybot/actions-runner && sudo chown deploybot:deploybot /home/deploybot/actions-runner
[server azureuser] sudo -u deploybot bash -c 'cd ~/actions-runner && curl -fsSL -o runner.tar.gz https://github.com/actions/runner/releases/download/v2.XXX.X/actions-runner-linux-x64-2.XXX.X.tar.gz && tar xzf runner.tar.gz && rm runner.tar.gz'
[server azureuser] sudo -u deploybot bash -c 'cd ~/actions-runner && ./config.sh --unattended --url https://github.com/omarmaarouf18/wael-app-deploy --token <registration-token> --labels wael-vm --name wael-prod-01 --work _work'
[server azureuser] sudo ./home/deploybot/actions-runner/svc.sh install deploybot   # service file needs root; the service itself runs as deploybot
[server azureuser] sudo ./home/deploybot/actions-runner/svc.sh start && sudo ./home/deploybot/actions-runner/svc.sh status
```

(Replace `2.XXX.X` with the version the "New runner" page shows.) Check it is
online: [GitHub UI] the Runners page lists `wael-prod-01` idle, and
`sudo ./home/deploybot/actions-runner/svc.sh status` is active. The Deploy
workflow runs on `[self-hosted, wael-vm]` (see `deploy.yml`).

Rulesets: the intent (owner decision Q2) is that `CI OK` — the aggregate gate
in `ci.yml` covering every job — is the sole required check on `main` and
`develop`. As of 2026-10-02 the rulesets still enforce the older per-job lists
(see `docs/REPOSITORY-SETTINGS.md` and the audit); applying Q2 is an owner
action in [GitHub UI] `wael-app → Settings → Rules`.

GHCR: images are `ghcr.io/omarmaarouf18/wael-app-<svc>:<sha>` (no `:latest`).
Package visibility could not be verified from the repo — check it in
[GitHub UI] profile → Packages. If the packages are private, the server needs
a login before pulls (`docker login ghcr.io -u <user>` with a
`read:packages` token, as deploybot); if public, pulls work without one.
Either way the tag is always the full commit sha.

## 8. First deploy

### What preflight checks

`scripts/preflight.sh` changes nothing that is running. It exits non-zero when any
check fails; items 1 to 5 fail before any image is pulled, and no check touches a
running container (added 2026-10-06 from the script; the items are in its order):

1. Files and permissions: `.env.production` exists with mode `600`; `secrets/` and
   `certs/` have mode `700`; `secrets/mongo_root_password` is non-empty;
   `secrets/redis.conf` has a `requirepass` line.
2. `IMAGE_TAG` in `release.env` is a full 40-character commit sha (no `:latest`).
3. Required values are set and are not placeholders (`PASTE_`, `CHANGE_ME`,
   `devpassword123`): `API_DOMAIN`, `ADMIN_DOMAIN`, `ACME_EMAIL`, `ALLOWED_ORIGIN`,
   `JWT_SECRET`, `GATEWAY_SECRET`, `INTERNAL_SERVICE_TOKEN`, `MONGO_ROOT_USERNAME`,
   the three `*_MONGO_URI`, `REDIS_URI`, `RESEND_API_KEY`, `RESEND_FROM_EMAIL`,
   `BLOCKLIST_HMAC_KEY`, `SUPPORT_WHATSAPP`, and (added 2026-10-09, Phase 5)
   `STORAGE_DIR`, `DOCUMENT_ENCRYPTION_KEY`, `WAEL_UID`, `WAEL_GID`; `JWT_SECRET`,
   `GATEWAY_SECRET`, `INTERNAL_SERVICE_TOKEN` and `BLOCKLIST_HMAC_KEY` are at
   least 32 characters; `APP_ENV` is absent.
   3b. *(Added 2026-10-09.)* `DOCUMENT_ENCRYPTION_KEY` is 64 hex characters;
   `WAEL_UID`/`WAEL_GID` equal the running user's ids; `STORAGE_DIR` is an
   absolute path to an existing directory with mode `700` owned by `WAEL_UID`;
   `FEATURES_FILES` is empty, `true` or `false`.
4. Certificates: the CA and the five service certificates are valid for 14 more
   days, and the five service keys exist.
5. The compose file renders with this env. `TERMS_URL` and `PRIVACY_URL` are not in
   the list in item 3; compose itself refuses to render without them (`${VAR:?}`),
   which fails here as "compose config renders". Then the rendered Redis command
   must have a non-zero `--maxmemory` cap and `noeviction`, and no LRU, LFU, random
   or volatile policy.
6. The images are pulled (with `SKIP_PULL=1`, the app images must already be loaded
   locally, and only mongo, redis and caddy are pulled).
7. Each app service passes `--check-env` in a one-off container.

### Path 1 — pipeline (normal)

First-run behaviour: with no `state/last-good/` yet, a successful deploy
creates it; if the very first deploy fails there is nothing to roll back to —
read the failure logs and fix forward. (No seeding needed.)

1. Finish §3–§7. Run preflight once by hand with a real `release.env` and
   read every line ([server azureuser]):
   ```bash
   [server azureuser] sudo -u deploybot bash -c 'cd <deploy-checkout> && export WAEL_HOME=/home/deploybot/wael && ./scripts/preflight.sh'
   ```
2. [laptop] Fast-forward `main` to the green `develop` (owner-approved;
   agents never push `main`):
   ```bash
   [laptop] git fetch origin && git checkout main && git merge --ff-only origin/develop && git push origin main
   ```
3. Watch the runs ([GitHub UI] Actions, or `[laptop] gh run list --repo
   omarmaarouf18/wael-app`): CI Gate → Build and Publish (images + mirror +
   `release.env`) → Deploy on `[wael-vm]` (preflight → up → health gate →
   last-good). E2E is the slow job; while it runs, "Deploy not started yet"
   is normal — the pipeline is sequential.

### Path 2 — manual, no GHCR (trial / pre-pipeline)

Build on the same CPU architecture as the host (or add
`--platform linux/amd64`). [laptop] from the repo root (Dockerfiles expect the
repo root as context):

```bash
[laptop] SHA="$(git rev-parse HEAD)"   # full 40-character sha
[laptop] for svc in api-gateway auth-service notification-service academy-service admin-console; do
  docker build -f services/$svc/Dockerfile --target prod \
    -t ghcr.io/omarmaarouf18/wael-app-$svc:$SHA .
done
[laptop] docker save $(for svc in api-gateway auth-service notification-service academy-service admin-console; do
    echo ghcr.io/omarmaarouf18/wael-app-$svc:$SHA; done) \
  | gzip | ssh azureuser@<host> 'gunzip | sudo -u deploybot docker load'
[laptop] rsync -a --delete --exclude '.git/' infrastructure/deploy/ azureuser@<host>:/tmp/wael-deploy/
```

`rsync --delete` deletes `release.env` (it is written by publish, never
mirrored), so rewrite it on the host after every rsync, then move the tree
into place and deploy with pulls skipped:

```bash
[server azureuser] printf 'IMAGE_TAG=%s\n' "<same-40-char-sha>" > /tmp/wael-deploy/release.env
[server azureuser] sudo rm -rf /home/deploybot/wael-deploy-new && sudo mv /tmp/wael-deploy /home/deploybot/wael-deploy-new && sudo chown -R deploybot:deploybot /home/deploybot/wael-deploy-new
[server azureuser] sudo -u deploybot bash -c 'cd ~/wael-deploy-new && export WAEL_HOME=/home/deploybot/wael && SKIP_PULL=1 ./scripts/preflight.sh'
[server azureuser] sudo -u deploybot bash -c 'cd ~/wael-deploy-new && export WAEL_HOME=/home/deploybot/wael && SKIP_PULL=1 ./scripts/deploy.sh'
```

`SKIP_PULL=1` is shell-only (the Deploy workflow never sets it): preflight
checks the five app images are loaded locally under the exact tag but still
pulls mongo/redis/caddy; without it, `compose up` would try GHCR and fail.
For a newer build repeat all steps with the new sha — and keep the old images
loaded: rollback needs the last-good images on the host (`docker image prune`
between deploys breaks it).

### Verification (both paths)

```bash
[server azureuser] sudo -u deploybot bash -c 'cat ~/wael/state/last-good/last-good.env'
[server azureuser] sudo -u deploybot bash -c 'cd <deploy-checkout> && docker compose -p wael ps'
[laptop] curl -fsS https://api.<domain>/health        # {"status":"ok"}
[laptop] curl -fsS -o /dev/null -w '%{http_code}\n' https://admin.<domain>   # 200
```

Expect: `last-good.env` shows the deployed tag; `docker ps` shows 8 healthy
containers (`wael-caddy-1`, `wael-api-gateway-1`, `wael-auth-service-1`,
`wael-notification-service-1`, `wael-academy-service-1`,
`wael-admin-console-1`, `wael-mongo-1`, `wael-redis-1`); both curls return 200.

## 9. Admin tokens

Mint (prints the admin id + token ONCE to stdout, stores only the SHA-256
hash). `--ttl` accepts a Go duration or the day shorthand the CLI also takes
(`2160h` = `90d` = 90 days; default `90d`; max 365 days; a bare `90` fails
with "missing unit"):

```bash
[server azureuser] sudo docker exec wael-auth-service-1 /bin/onboard-admin --name "<name>" --ttl 2160h
```

(`sudo docker exec` directly: `azureuser` cannot `cd` into the 700 dirs and
`sudo cd` does not exist.) Copy the token straight into the operator's
password manager with the admin id. In the console the token lives in a JS
module variable (tab memory only — no storage, cookie or URL): reload, tab
close or any 401 signs out. One token per person, for the audit trail. A lost
token cannot be recovered — revoke and replace.

Revoke with the printed id (`adm_…`):

```bash
[server azureuser] sudo docker exec wael-auth-service-1 /bin/revoke-admin --id <id>
```

Revocation applies on the next request (auth-service verifies the token on
every call). Listing admins: there is no list CLI (only these two CLIs exist
in `services/auth-service/cmd/`); list/search operators in the console
Accounts tab (`GET /api/accounts` proxy). Sign-in problems (401/429/503):
RUNBOOK "Admin console".

## 10. Day-2 operations

Release: the owner fast-forwards `main` (§8 Path 1). Agents never push `main`.
E2E is the slow gate job — a quiet Deploy workflow while CI runs is normal.

Rollback — automatic: `deploy.sh` runs `rollback.sh <failed-tag>` whenever the
new release fails its health gate. Manual ([server azureuser], deploybot has
no login so drive it with `sudo -u`):

```bash
[server azureuser] sudo -u deploybot bash -c 'cd <runner-checkout-of-wael-app-deploy> && export WAEL_HOME=/home/deploybot/wael && ./scripts/rollback.sh'
```

Find the runner checkout under `~/actions-runner/_work/<repo>/<repo>`. What
rollback does: reads `state/last-good/last-good.env` (fallback: legacy
`state/last-good.env`), appends the failed tag to `state/failed-releases`
(unless it equals the good tag), and brings up the **last-good compose
snapshot** (`state/last-good/docker-compose.yml`, fallback: current file with
a loud warning) with `--remove-orphans --wait` plus the public health check.
Rollback covers images only — no DB migration framework exists, so data
changes are NOT rolled back. Afterwards: fix forward with a new commit on
`main`; never re-run the old `release.env` — `deploy.sh` refuses failed tags
unless the Deploy workflow's "Force deploy even if previously rolled back"
input (`ALLOW_FAILED_RELEASE=1`) is set.

Backups — status: the server already runs a hand-written backup
(`$WAEL_HOME/backup.sh`: `mongodump --archive --gzip` as the mongo root user
via `docker exec` into `wael-mongo-1`) from the deploybot crontab
`17 0 * * *`, keeping 7 days in `~/wael/backups/` (dir 700). That behaviour
now lives versioned in `scripts/backup.sh` (`umask 077`, archives
`mongo-<UTC-timestamp>.archive.gz` at mode `600`, keep window via
`BACKUP_KEEP_DAYS`, default 7; the root password is read from
`$WAEL_HOME/secrets/mongo_root_password` and never printed). Switch the cron
to the repo script (from the runner checkout of `wael-app-deploy`, which the
publish workflow keeps in sync):

```bash
[server azureuser] sudo -u deploybot crontab -l
[server azureuser] (sudo -u deploybot crontab -l 2>/dev/null | grep -v "wael/backup.sh\|wael/bin/mongo-backup.sh"; echo "17 0 * * * /home/deploybot/<deploy-checkout>/scripts/backup.sh") | sudo -u deploybot crontab -
[server azureuser] sudo -u deploybot crontab -l   # confirm exactly one backup line
```

*(Amended 2026-10-08, full review infra H1/H2:)* `backup.sh` now keeps an
archive only if it passes `gzip -t`, runs one at a time (`flock` on
`state/backup.lock`), records the last good archive in
`state/last-backup.env`, and never prunes the archive it just wrote. Every
deploy runs it first with `BACKUP_LABEL=predeploy`
(`mongo-predeploy-<stamp>.archive.gz`); if it fails, nothing is deployed. If a
deploy then fails, the rollback output names the restore command. To get an
alert when the nightly backup fails or does not run, create a free
healthchecks.io check (period 1 day, grace 6 hours) and put its URL in
`.env.production` as `BACKUP_PING_URL`.

(`<deploy-checkout>` is the runner checkout path used for manual rollback
above; `lib.sh` defaults `WAEL_HOME` so no env is needed in cron. The old
`$WAEL_HOME/backup.sh` can be deleted once the new cron has produced its
first archive.)

Off-site copy: `scripts/pull-backups.sh` on the owner's laptop (rsync over
SSH as `azureuser` with the `--rsync-path='sudo rsync'` trick for the 700
dir; no cloud storage, no new secrets — existing SSH key auth). It keeps the
newest 30 archives locally at mode 600. *(Amended 2026-10-08:)* it exits 1
with `STALE BACKUPS` when the newest archive is older than
`BACKUP_MAX_AGE_HOURS` (default 30) and with `CORRUPT BACKUP` when it fails
`gzip -t`, so a failing timer run shows up in `systemctl --user status
wael-pull-backups`:

```bash
[laptop] export WAEL_HOST=<host>   # plus WAEL_SSH_USER / LOCAL_BACKUP_DIR / BACKUP_KEEP_LOCAL to override
[laptop] ./infrastructure/deploy/scripts/pull-backups.sh
```

First run explains itself if passwordless rsync-sudo is missing (it prints
the one `sudoers.d` line to add). Run it daily via a systemd user timer
[laptop]:

```ini
# ~/.config/systemd/user/wael-pull-backups.service
[Unit]
Description=Pull wael-app backups off-site
[Service]
Type=oneshot
Environment=WAEL_HOST=<host>
ExecStart=%h/wael-app/infrastructure/deploy/scripts/pull-backups.sh

# ~/.config/systemd/user/wael-pull-backups.timer
[Unit]
Description=Daily wael-app backup pull
[Timer]
OnCalendar=daily
Persistent=true
[Install]
WantedBy=timers.target
```

```bash
[laptop] systemctl --user daemon-reload && systemctl --user enable --now wael-pull-backups.timer
[laptop] systemctl --user list-timers | grep wael
```

Verify off-site archives are mode 600 and restorable — an untested backup is
not a backup.

Restore with `scripts/restore.sh` (rehearsed locally 2026-10-02: throwaway
`mongo:7` containers, seed 3 users + 5 orders, backup → restore into a fresh
container → counts match, `--drop` confirmed by re-restoring over extra
rows; orchestration path covered by mocked tests in
`scripts/deploy_scripts_test.sh`; a production drill is still recommended):

```bash
[server azureuser] sudo -u deploybot bash -c 'cd /home/deploybot/<deploy-checkout> && ./scripts/restore.sh ~/wael/backups/<archive>.archive.gz --yes'
```

Without `--yes` it refuses and touches nothing. It stops the five app
services first (mongo and redis stay up), runs `mongorestore --archive
--gzip --drop`, restarts the services with `--wait` and must pass the public
health gate. `--skip-restart` is a rehearsal-only escape hatch.

Logs: `docker compose -p wael logs --tail 100 <service>` (from the deploy
checkout as deploybot); host side: `journalctl -u docker.service --since -1h`.
Container logs rotate via the compose `x-logging` anchor (json-file,
10 MB × 3). Caddy access logs are off by design (query strings must not be
logged); 404s from internet scanners hitting random paths are expected noise.

Disk cleanup — keep the last-good images:

```bash
[server azureuser] sudo -u deploybot bash -c 'GOOD=$(sed -n "s/^IMAGE_TAG=//p" ~/wael/state/last-good/last-good.env); docker images "ghcr.io/omarmaarouf18/wael-app-*" --format "{{.Repository}}:{{.Tag}}" | grep -v "$GOOD" | grep -v "$(sed -n "s/^IMAGE_TAG=//p" <checkout>/release.env)" | xargs -r docker rmi'
[server azureuser] docker system df && docker builder prune -f
```

Secret rotation (change the value in `$WAEL_HOME/.env.production`, then
re-deploy the current `release.env` so every container picks it up together):
- `JWT_SECRET`: logs **everyone** out (all access + refresh tokens invalidate).
- `GATEWAY_SECRET`: must land on gateway AND all three user-facing services
  atomically — a partial rollout returns 401s on proxied calls.
- `INTERNAL_SERVICE_TOKEN`: must land on auth/notification/academy AND the
  console together, or admin calls fail.
- Never rotate `BLOCKLIST_HMAC_KEY` casually: existing blocklist entries stop
  matching (documented in the env table, §5).

Updating mongo/redis/caddy: the three third-party images are pinned by digest
in `docker-compose.yml` (W-07 resolved 2026-10-02), with weekly Dependabot
docker updates for `/infrastructure/deploy`. Bump the digest (and tag) in
`docker-compose.yml`, run preflight + deploy on a test host first, then ship
via the pipeline and review the Dependabot PRs (they target `develop`, never
`main`; amended 2026-10-05). *(Amended 2026-10-06: `.github/dependabot.yml` now
also covers gomod, pub, github-actions and the service Dockerfiles, weekly; GitHub
reads that file from the default branch, so it takes effect only after `main` is
fast-forwarded to a `develop` that contains it.)*

## 11. Moving to a new server

**Not yet rehearsed.** Expected downtime with the steps below (TTL lowered a
day ahead, backup/restore practiced): roughly 15–45 minutes of API/admin
unavailability, dominated by DNS propagation and Caddy's first ACME issuance.

1. Build the new server with §3–§7 (base, certs — same CA, or a fresh CA with
   all certs replaced at cutover — secrets, `.env.production` with the SAME
   secret values if sessions must survive, GitHub runner registered as a
   second `wael-vm` runner).
2. Lower the DNS TTL to 300 s at least a day before (see §2).
3. Take a final backup on the old server (§10) and restore it on the new one
   (restore procedure, §10) with the stack stopped.
4. Switch the `api`/`admin` A records to the new host; verify with
   `dig @<authoritative-NS>` then the public curls (§8 verification).
5. Confirm the new runner took the next Deploy (or disable the old runner
   first to force it), then remove the old runner ([GitHub UI] Runners →
   Remove) and decommission the old host: `docker compose -p wael down -v`,
   shred `$WAEL_HOME/secrets`, revoke its GHCR token, terminate the VM.

## 12. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Cloud Shell opens in PowerShell | Azure Cloud Shell default shell | Type `bash` and work from there. |
| `sudo cd ...` fails | `cd` is a shell builtin, not a binary | `sudo -u deploybot bash -c 'cd ~/wael && …'`; see §3 pitfalls. |
| Glob in a 700 dir: "No such file" | Only the owner traverses mode-700 dirs | Run the whole command as root/owner: `sudo sh -c '…'` (§3). |
| `mongosh` ECONNREFUSED right after init | `mongod` restarts once during init | Wait 10 s and retry (§6). |
| Caddy container unhealthy | Healthcheck used `localhost` (resolves to `::1`) | Uses `http://127.0.0.1:2019/config/` — do not "fix" it back (`docker-compose.yml`). |
| `APP_PRIVATE_KEY` "Invalid keyData" | Malformed PEM in the secret | Re-export the App's PEM unchanged: `gh secret set APP_PRIVATE_KEY < key.pem` (§7). |
| Mirror fails on permissions | App lacks Workflows R/W or install not updated | Grant Contents + Workflows R/W and accept on both installations (§7). |
| `release.env` missing after rsync | `rsync --delete` removes it (never mirrored) | Rewrite it after every rsync (§8 Path 2). |
| `--ttl 90` reports "missing unit" | Bare numbers carry no unit | Use `2160h` (or `90d`); see §9. |
| Preflight fails on a new service's cert/env | Missing `<svc>.crt/.key` or env var | Sign with `--sign-only` (§4), add the var (§5), re-run preflight. |
| Deploy not started yet after push to main | Pipeline is sequential; E2E is the slow job | Wait for CI Gate → publish → deploy (§8 Path 1). |
| Fresh DNS record looks missing locally | Negative caching | Check the authoritative NS directly; wait out the TTL (§2). |
| 404s for random paths in logs | Internet scanners | Expected noise; access logs stay off by design (§10). |
| Deploy refuses a tag ("was rolled back") | `state/failed-releases` guard | Fix forward with a new commit; force only via the workflow input (§10). |
| Logins/refresh/OTP fail with Redis OOM errors, reads still work | Redis hit `maxmemory` (writes refused, fail-closed) | Degraded-by-design, not dead: see §5 "Redis memory" (`INFO memory`, `used_memory_human`) and RUNBOOK "Redis at maxmemory". |
| Containers OOM-killed on a small host | Limits exceed RAM | 1 GB block + 2 GB swap + zram (§1, §3), or a bigger host. |

## Appendix A — cost and size reference

- Reference host: Azure B2ats_v2, region UAE North (owner-measured; re-check
  current Azure pricing — no price is quoted here and no free-tier coverage is
  claimed).
- Reference load: 887 MB host RAM; running stack ≈ 320–400 MB; swap required
  (see §1, §3).

## Appendix B — new server checklist (in order)

1. [§1] Size the host (2 GB, or 1 GB only with the small profile + swap).
2. [§2] DNS `api` + `admin` A records; Resend records.
3. [§3] Base setup: upgrade, swap, zram, journald, fail2ban, auto-updates,
   Docker, `deploybot` + 700 layout.
4. [§4] Certificates: CA on laptop, sign 5 services, install 644.
5. [§5] Secrets: server-side script, 600 file, secret files.
6. [§6] Mongo users (once).
7. [§7] GitHub vars/secrets, App install, `DEPLOY_ENABLED`, runner online,
   rulesets.
8. [§8] First deploy (pipeline, or manual without GHCR) + verification.
9. [§9] Mint admin tokens (one per operator).
10. [§10] Day-2: release/rollback/backups/logs/cleanup/rotation/updates.
11. [§11] Migration plan (when needed). [§12] Troubleshooting.
