# wael-app production runbook

Status: **LIVE in production.** Production has been live since `557f367`, and the deploy pipeline has run three releases successfully (`557f367`, `6bd6c12`, `b6a11fe`). Publishing (`PUBLISH_ENABLED=true` in `wael-app`) and deploys (`DEPLOY_ENABLED=true` in `wael-app-deploy`) are active and enabled.

Full install/run/operate manual: [SERVER-MANUAL.md](SERVER-MANUAL.md) (same
folder, mirrored to `wael-app-deploy`). This runbook is the short day-2
checklist; procedures live in the manual, summaries and quick commands here.

## How a release reaches the server

```
wael-app main (fast-forward) -> CI Gate green
  -> build-and-publish.yml (workflow_run, PUBLISH_ENABLED)
       images ghcr.io/omarmaarouf18/wael-app-<svc>:<commit sha>   (no :latest)
       mirror infrastructure/deploy/ -> wael-app-deploy, write release.env
  -> wael-app-deploy push -> deploy.yml on the self-hosted runner [wael-vm]
       scripts/preflight.sh   (touches nothing running)
       docker compose up --wait (strict health checks)
        public check through Caddy
        success: record state/last-good/last-good.env (plus a legacy copy at
        state/last-good.env) and snapshot docker-compose.yml + Caddyfile
        failure: scripts/rollback.sh (the failed tag is recorded in
        state/failed-releases, which later deploys refuse)
```

Do not edit files in wael-app-deploy by hand. Change them in
`wael-app/infrastructure/deploy/`; the next publish mirrors them.

## One-time server setup

Full procedure: `SERVER-MANUAL.md` §3 (base + users), §4 (certs), §5 (secrets),
§6 (mongo users), §2 (DNS), §7 (GHCR login, runner). Checklist:

1. Base (`SERVER-MANUAL.md` §3, as `azureuser` with sudo): full-upgrade,
   2 GB swap, zram, journald cap, fail2ban, unattended-upgrades, Docker
   Engine + Compose v2, `deploybot` (docker group only; no password, sudo or
   SSH) and `/home/deploybot/wael/{certs,secrets,state,backups}` at 700.
2. Secrets (`SERVER-MANUAL.md` §5): generate on the server only with the
   script (writes `.env.production` as deploybot, mode 600), plus
   `secrets/mongo_root_password` and `secrets/redis.conf`.
3. Per-service Mongo users, once (`SERVER-MANUAL.md` §6): `compose up -d
   --wait mongo` (retry `mongosh` after 10 s on ECONNREFUSED), create
   `auth_svc` / `notif_svc` / `academy_svc`, passwords back into the URIs.
4. mTLS certificates (`SERVER-MANUAL.md` §4): CA on the laptop (`ca.key`
   never leaves it), `--sign-only` per service, install as
   `deploybot:deploybot` mode 644.
5. DNS (`SERVER-MANUAL.md` §2): A records for `api` and `admin`.
6. GHCR login (as deploybot) and self-hosted runner (`SERVER-MANUAL.md` §7):
   runner labels `self-hosted,wael-vm`, installed as a service.

## GitHub setup

| Where | What |
|---|---|
| GitHub App (owner account) | Permissions: Contents read/write and Workflows read/write. Installed on `wael-app-deploy` and `wael-app-mobile` only. After any permission update, the installation must accept the updated permissions. |
| wael-app secrets | `APP_ID`, `APP_PRIVATE_KEY` (the App's key) |
| wael-app variables | `PUBLISH_ENABLED`, `MOBILE_SYNC_ENABLED` |
| wael-app-deploy variables | `DEPLOY_ENABLED`, optional `WAEL_HOME` |
| wael-app-deploy environment | `production` (add required reviewers if your plan allows it on private repos) |
| wael-app-mobile variables | `API_BASE_URL` (`https://api.elmetracademy.app`) |
| wael-app-mobile secrets (optional) | `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` |

Full procedure with copy-paste commands: `SERVER-MANUAL.md` §7.

## Turning it on (order matters)

Production is live since `557f367`. For reference, the setup order was:

1. Prerequisites in wael-app: W-04 (`--check-env`, preflight step 7 needs
    it), W-08 (strict CA-verified probes in the production compose
    healthchecks, which override the Dockerfile `HEALTHCHECK` fallbacks),
    and a release gate (E2E) that publishing waits for (saas-core S-01).
    W-10 (fixed container UID) is still open, so certificate keys stay
    world-readable (`644`) inside the `700` certs directory. Per owner
    decision Q1 (2026-10-02), `E2E (compose)` and `Prod Image Build` inside CI Gate
    are sufficient and run on `main` before publishing.
2. Server setup above, then run `WAEL_HOME=... ./scripts/preflight.sh` by hand
   with a real `release.env` and read every line.
3. Set `PUBLISH_ENABLED=true` in wael-app. Merge to main. Check that images
   and the deploy-repo commit appear.
4. Set `DEPLOY_ENABLED=true` in wael-app-deploy and re-run the Deploy
   workflow.

## Admin console

`admin-console` (SPEC Phase 6.1, ADR-0008) runs in the stack with the other
services. It is reached only through Caddy on `ADMIN_DOMAIN`
(`admin.elmetracademy.app`), has no published port, and calls the admin
listeners of auth-service (`:9001`) over mTLS. It decides nothing itself: every
request is checked by auth-service against the operator's token.

**Before the first deploy that includes it**

1. DNS record for `ADMIN_DOMAIN` (server setup step 8).
2. `ADMIN_DOMAIN` in `$WAEL_HOME/.env.production` (the template has it;
   `preflight.sh` fails while it is empty).
3. The `admin-console` certificate and key in `$WAEL_HOME/certs/` (server setup
   step 7; on a server that already has the other certificates use
   `--sign-only`). `preflight.sh` checks both files.
4. Deploy as usual. The container has its own health check, so `deploy.sh`
   waits for it. It does not check the admin host through Caddy; do that by
   hand once DNS resolves: `curl -fsS https://admin.elmetracademy.app/healthz`
   prints `ok`.

**Minting an admin token (the first one, and one per operator)**

Tokens are created only by the server-side CLI `onboard-admin` (ADR-0008):
no page or API can mint or revoke one. Full procedure, TTL rules and token
handling: `SERVER-MANUAL.md` §9. Quick commands (on the server, `sudo docker
exec` directly — `azureuser` cannot `cd` into the 700 dirs and `sudo cd`
does not exist):

```bash
sudo docker exec wael-auth-service-1 /bin/onboard-admin --name "<name>" --ttl 2160h
```

```bash
sudo docker exec wael-auth-service-1 /bin/revoke-admin --id <id>
```

Revocation takes effect on the next request: auth-service checks the token on
every call. There is no list-admins CLI; list operators in the console
Accounts tab.

**Using it.** Open `https://admin.elmetracademy.app` and enter the token. It is
kept in the memory of that browser tab only (never in storage, a cookie or the
URL), so each tab asks for it, and reloading, closing the tab or any `401`
signs out. The page is Arabic first with an English toggle. Today it offers
Accounts (search, suspend, reactivate, delete, each with a recorded reason) and
the Audit log; the Requests, Catalog and Files tabs stay hidden until their
APIs exist (SPEC Phase 4), and until then the audit log shows auth-service
actions only.

**Sign-in problems.** `401`: wrong, expired or revoked token. `429`: five bad
attempts from the same client address lock that address out for 30 seconds,
growing on repeats. The address is the browser's, taken from Caddy's
`X-Forwarded-For`; it is what auth-service keys the lockout on. `503`:
auth-service did not answer within 10 seconds: check
`docker compose -p wael ps` and `docker compose -p wael logs --tail 100 auth-service`.

## Memory profiles

Sizing table, the "1 GB host" block, swap and the measured reference:
`SERVER-MANUAL.md` §1. The 1 GB limits total about 980 MB — add swap first,
run nothing else on the host, and watch for OOM kills after the first
deploy. This profile has not been tried.

## Redis at maxmemory

Redis is capped at `REDIS_MAXMEMORY` (default `96mb`, below the 128m
container limit) with a fixed `noeviction` policy — eviction is forbidden
because it could drop denylist keys (jti/sid/user revocation) and revive
revoked tokens. Preflight refuses a rendered compose with no non-zero
`--maxmemory` cap or with an eviction policy. Full procedure and rationale:
`SERVER-MANUAL.md` §5 "Redis memory".

**Symptoms.** New logins, token refreshes and OTP issues fail (write OOM;
services log Redis OOM errors, `auth-service` returns 503 on
login/refresh) while reads keep working — existing sessions keep validating.
Degraded, not dead, and fail-closed. The redis container itself stays up
(`docker compose -p wael ps` shows it healthy); do NOT restart it — a
restart does not free a full dataset served from AOF, and the cap, not the
process, is refusing the writes.

**Response.**

1. Confirm: `INFO memory` shows `used_memory_human` at the cap and
   `evicted_keys:0` (commands: `SERVER-MANUAL.md` §5 "Redis memory"). A
   non-zero `evicted_keys` means an eviction policy is active — fix the
   compose `command:` back to `noeviction` and redeploy instead.
2. Find what grew before raising anything: key count by prefix and TTLs on
   OTP/attempt keys. A leak (keys without TTL, unbounded growth) must be
   fixed in code and shipped; raising the cap only buys time.
3. If the data is legitimate, raise `REDIS_MAXMEMORY` in
   `$WAEL_HOME/.env.production` **together with** `REDIS_MEM_LIMIT` headroom
   (data cap must stay clearly below the container limit for AOF-rewrite
   fork copy-on-write and client buffers), then deploy the current
   `release.env` so the container picks it up.

## Manual trial deploy (no GHCR)

Full procedure with copy-paste commands: `SERVER-MANUAL.md` §8 Path 2
(build `--target prod`, `docker save | ssh … docker load`, rsync the deploy
dir, rewrite `release.env` after `rsync --delete`, `SKIP_PULL=1` preflight +
deploy). Notes that still apply: `SKIP_PULL=1` is shell-only (the Deploy
workflow never sets it); without it `compose up` pulls from GHCR and fails
for host-only images. Keep loaded images: rollback needs the last-good
release's images on the host, so do not `docker image prune` between deploys.
The first deploy has no last-good release to roll back to. Nothing here has
been run yet.

## Operations

Full procedures: `SERVER-MANUAL.md` §10 (release, rollback, failed-releases
guard, backups, restore, logs, cleanup, rotation, base-image updates).

- **Manual deploy of the current release.env:** Actions -> Deploy -> Run workflow.
  **Warning:** after a rollback, `release.env` in `wael-app-deploy` still points at
  the bad SHA. Do NOT re-run Deploy with that `release.env`. Recovery after a bad release
  means fixing forward with a new commit on `main`, never re-running the rolled-back release.
  `deploy.sh` checks `$WAEL_HOME/state/failed-releases` and refuses to deploy any SHA listed
  there. If a manual forced re-deploy of a rolled-back SHA is ever truly required, trigger
  Deploy via Actions -> Deploy -> Run workflow and check the "Force deploy even if this
  commit was previously rolled back" option (which passes `ALLOW_FAILED_RELEASE=1` to `deploy.sh`).
  Because `deploybot` has no login, this workflow input is the supported operational override.
- **Manual rollback:** as deploybot, `cd` into the runner's checkout and run
  `WAEL_HOME=/home/deploybot/wael ./scripts/rollback.sh`.
- **Logs:** `docker compose -p wael logs --tail 100 <service>`.

## Known gaps (not solved by this scaffold)

- No database migration framework; rollback restores images only (saas-core S-08).
- Server backup cron exists (deploybot `17 0 * * *`, adopt repo
  `scripts/backup.sh`); restore rehearsed locally 2026-10-02 (`SERVER-MANUAL.md`
  §10); production restore drill still open (Bootstrap Phase B item 14).
- `REDIS_URI` and Mongo URIs carry passwords in container env (`docker inspect`).
- App images are tagged by commit sha; third-party bases (`caddy`, `mongo`,
  `redis`) are pinned by digest with weekly Dependabot updates (W-07 done
  2026-10-02).
- No certificate rotation job; preflight only refuses certs expiring within 14 days.
- The admin console shows accounts and the auth-service audit log only; its academy pages and the academy half of the audit log arrive with SPEC Phase 4. `deploy.sh` checks the API host through Caddy but not `ADMIN_DOMAIN`.
- The admin listeners of auth-service (:9001) and academy-service (:9002) run inside their containers and are not published.
