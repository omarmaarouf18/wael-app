# wael-app production runbook

Status: **LIVE in production.** Production has been live since `557f367`, and the deploy pipeline has run three releases successfully (`557f367`, `6bd6c12`, `b6a11fe`). Publishing (`PUBLISH_ENABLED=true` in `wael-app`) and deploys (`DEPLOY_ENABLED=true` in `wael-app-deploy`) are active and enabled.

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
       success: record state/last-good.env   failure: scripts/rollback.sh
```

Do not edit files in wael-app-deploy by hand. Change them in
`wael-app/infrastructure/deploy/`; the next publish mirrors them.

## One-time server setup

Run as a sudo-capable admin unless stated otherwise.

1. **Deploy user.** `sudo useradd -m -s /bin/bash deploybot` and add it to
   the `docker` group. Accepted trade-off: the docker group is
   root-equivalent (saas-core S-07). Nobody else logs in as deploybot.
2. **Docker Engine and Compose v2** (`docker compose version` must work).
3. **Firewall.** Allow inbound 22 (admin SSH, key only), 80 and 443 (TCP)
   and 443/UDP. Nothing else. Mongo and Redis are never published.
4. **Directory layout** (as deploybot):
   ```bash
   export WAEL_HOME=/home/deploybot/wael
   mkdir -p "$WAEL_HOME"/{certs,secrets,state}
   chmod 700 "$WAEL_HOME" "$WAEL_HOME"/{certs,secrets,state}
   ```
5. **Secrets.**
   ```bash
   cp env.production.example "$WAEL_HOME/.env.production"   # then fill every PASTE_ value
   chmod 600 "$WAEL_HOME/.env.production"
   openssl rand -hex 24 > "$WAEL_HOME/secrets/mongo_root_password"
   printf 'requirepass %s\nappendonly yes\n' "$(openssl rand -hex 24)" > "$WAEL_HOME/secrets/redis.conf"
   chmod 644 "$WAEL_HOME"/secrets/*   # readable inside the containers; the 700 dir blocks other host users
   ```
   Put the same Redis password into `REDIS_URI`.
6. **Per-service Mongo users** (once, before the first deploy). The three
   database-backed services cannot start until their users exist, so start
   only mongo first. From the deploy checkout, with the step 5 files in place
   and a `release.env` (the compose file needs `IMAGE_TAG` to render):
   ```bash
   export WAEL_HOME=/home/deploybot/wael
   source scripts/lib.sh        # defines the `compose` helper used by the scripts
   compose up -d --wait mongo
   # Note: A first mongosh right after `up --wait mongo` can get ECONNREFUSED while
   # mongo restarts during initialization. If so, retry after 10 seconds.
   compose exec -T mongo mongosh -u wael_root -p "$(cat "$WAEL_HOME/secrets/mongo_root_password")" --authenticationDatabase admin --eval '
     db.getSiblingDB("auth_db").createUser({user:"auth_svc",pwd:"<pw>",roles:[{role:"readWrite",db:"auth_db"}]});
     db.getSiblingDB("notification_db").createUser({user:"notif_svc",pwd:"<pw>",roles:[{role:"readWrite",db:"notification_db"}]});
     db.getSiblingDB("academy_db").createUser({user:"academy_svc",pwd:"<pw>",roles:[{role:"readWrite",db:"academy_db"}]});'
   ```
   Use a different `openssl rand -hex 24` for each `<pw>` (hex needs no URL
   escaping). The passwords go into `AUTH_MONGO_URI`, `NOTIFICATION_MONGO_URI`
   and `ACADEMY_MONGO_URI`.
7. **Internal mTLS certificates.** Generate with the wael-app script
   (`infrastructure/certs/generate-certs.sh`) on an admin machine, copy
   `ca.crt` and the `.crt/.key` of `api-gateway`, `auth-service`,
   `notification-service`, `academy-service` and `admin-console` into
   `$WAEL_HOME/certs/` (never `ca.key`: keep it offline). Until W-10 (fixed
   container UID) lands, key files must be `644`; the `700` certs directory
   keeps other host users out.

   **Adding one certificate later** (for example `admin-console` on a server
   that already has the others): do **not** run the script without arguments
   again. A full run creates a new CA and replaces every certificate, which
   would break the running stack. Sign only the missing one with the existing
   CA (kept offline), from a wael-app checkout:
   ```bash
   ./infrastructure/certs/generate-certs.sh --sign-only admin-console \
     --ca-dir /path/to/offline-ca --out-dir ./signed
   ```
   `--ca-dir` must hold the original `ca.crt` and `ca.key`; it is only read (no
   file is written there). The command refuses to overwrite an existing
   `admin-console.crt/.key` in `--out-dir` unless you add `--force`. Copy the
   two new files into `$WAEL_HOME/certs/` and run `scripts/preflight.sh`.
8. **DNS.** Create an A (and AAAA if used) record for `api.elmetracademy.app`
   and one for `admin.elmetracademy.app` pointing at the server. Caddy obtains
   the public certificates on first start; the deploy's public health check
   (API host only) fails until the API record resolves. The admin host is not
   part of that check: confirm it yourself (see "Admin console").
9. **GHCR login** (as deploybot), with a fine-grained token that has only
   `read:packages`: `docker login ghcr.io -u omarmaarouf18`.
10. **Self-hosted runner** (as deploybot): add a runner to
    **wael-app-deploy only**, labels `self-hosted,wael-vm`, installed as a
    service. The runner polls GitHub; no inbound port is opened.

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

## Turning it on (order matters)

Production is live since `557f367`. For reference, the setup order was:

1. Prerequisites in wael-app: W-04 (`--check-env`, preflight step 7 needs
   it), W-08 (strict probes in the Dockerfiles), W-10 (fixed UID), and a
   release gate (E2E) that publishing waits for (saas-core S-01). Per owner
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
no page or API can mint or revoke one. It prints the admin ID and token once on
stdout and stores only its SHA-256 hash. `--ttl` takes a Go duration (for example
`2160h` for 90 days; bare numbers like `90` fail with "missing unit" and there is
no `d` unit). The maximum TTL is `8760h` (365 days). Give each person their own named token.

On the server (run directly via `sudo docker exec`; `azureuser` cannot cd into the 700 directory and `sudo cd` does not exist):

```bash
sudo docker exec wael-auth-service-1 /bin/onboard-admin --name "<name>" --ttl 2160h
```

Copy the printed token straight into the operator's password manager and note
the admin ID. The token is shown once and cannot be recovered; a lost token is
revoked and replaced. Never paste it into chat, tickets or the shell history
of a shared machine.

**Revoking a token.** `revoke-admin` takes the admin id (`adm_...`) printed
when the token was minted:

```bash
sudo docker exec wael-auth-service-1 /bin/revoke-admin --id <id>
```

Revocation takes effect on the next request: auth-service checks the token on
every call.

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

The defaults in `docker-compose.yml` fit a 2 GB host (container limits add up
to about 1.8 GB). For a 1 GB host, uncomment the "1 GB host" block at the end
of `env.production.example` in `$WAEL_HOME/.env.production`:

| Service | 2 GB default | 1 GB host |
|---|---|---|
| mongo (`MONGO_MEM_LIMIT`, WiredTiger cache `MONGO_CACHE_GB`) | 768m, 0.25 GB | 420m, 0.25 GB |
| redis (`REDIS_MEM_LIMIT`) | 128m | 64m |
| api-gateway (`GATEWAY_MEM_LIMIT`) | 128m | 96m |
| auth-service (`AUTH_MEM_LIMIT`) | 192m | 96m |
| notification-service (`NOTIFICATION_MEM_LIMIT`) | 192m | 96m |
| academy-service (`ACADEMY_MEM_LIMIT`) | 192m | 96m |
| admin-console (`ADMIN_MEM_LIMIT`) | 64m | 48m |
| caddy (`CADDY_MEM_LIMIT`) | 128m | 64m |

The 1 GB limits total about 980 MB, which leaves very little for the
operating system and the Docker daemon. Add swap to the host first (for
example a 1 GB swap file), run nothing else on it, and watch
`docker stats --no-stream` and `docker inspect -f '{{.State.OOMKilled}}' <container>`
after the first deploy: a container that is OOM-killed needs a higher limit
or a bigger host. This profile has not been tried.

## Manual trial deploy (no GHCR)

Use this to run the stack on the server before publishing is switched on, or
without GHCR at all. The publish and deploy workflows stay off; you build the
images yourself and load them on the host. Nothing here has been run yet.

Prerequisites: server setup steps 1 to 8 are done (GHCR login and the
runner, steps 9 and 10, are not needed), `$WAEL_HOME/.env.production` is
filled, the certificates are in place and the DNS record resolves (the last
check goes through Caddy and needs a public certificate, so ports 80 and 443
must be reachable; otherwise the deploy fails). The admin machine needs
Docker and a checkout of the commit to deploy. Build on the same CPU
architecture as the host, or add `--platform linux/amd64` (or the host's).

1. **Build each production image from the repo root** (the Dockerfiles expect
   the repo root as build context):
   ```bash
   SHA="$(git rev-parse HEAD)"          # full 40-character commit sha
   for svc in api-gateway auth-service notification-service academy-service admin-console; do
     docker build -f services/$svc/Dockerfile --target prod \
       -t ghcr.io/omarmaarouf18/wael-app-$svc:$SHA .
   done
   ```
2. **Send the images to the host:**
   ```bash
   docker save $(for svc in api-gateway auth-service notification-service academy-service admin-console; do
       echo ghcr.io/omarmaarouf18/wael-app-$svc:$SHA; done) \
     | gzip | ssh deploybot@<host> 'gunzip | docker load'
   ```
3. **Send the deploy files** (they are the same files the publish workflow
   mirrors): `rsync -a --delete --exclude '.git/' infrastructure/deploy/ deploybot@<host>:wael-deploy/`
4. **On the host, write `release.env` by hand** (the publish workflow does
   this normally):
   ```bash
   cd ~/wael-deploy
   printf 'IMAGE_TAG=%s\n' "<the same 40-character sha>" > release.env
   ```
5. **First time only:** create the Mongo users (server setup step 6).
6. **Pre-flight, then deploy:**
   ```bash
   export WAEL_HOME=/home/deploybot/wael
   SKIP_PULL=1 ./scripts/preflight.sh      # read every line
   SKIP_PULL=1 ./scripts/deploy.sh
   ```

`SKIP_PULL=1` is a shell-only switch (the deploy workflow never sets it). Without it, `preflight.sh`
runs `docker compose pull` on the five app images, which fails for images
that exist only on the host. With it, preflight skips that pull, checks that
all five images are already loaded under the exact `IMAGE_TAG`, and still
pulls mongo, redis and caddy from Docker Hub. Note that `docker compose up`
itself pulls missing images automatically; with `SKIP_PULL=1`, if images for a
release or rollback are not already loaded locally on the host, `compose up`
will attempt to pull them from GHCR and fail.

To deploy a newer build, repeat steps 1 to 4 with the new sha. Keep the
previously loaded images: rollback needs the last good release's images on
the host, so do not `docker image prune` between deploys. The first deploy
has no last good release to roll back to; if it fails, read
`docker compose -p wael logs --tail 100 <service>` and fix the cause.

## Operations

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
- No scheduled backups or restore drill yet (Bootstrap Phase B item 14).
- `REDIS_URI` and Mongo URIs carry passwords in container env (`docker inspect`).
- Base images are pinned by tag, not digest (W-07).
- No certificate rotation job; preflight only refuses certs expiring within 14 days.
- The admin console shows accounts and the auth-service audit log only; its academy pages and the academy half of the audit log arrive with SPEC Phase 4. `deploy.sh` checks the API host through Caddy but not `ADMIN_DOMAIN`.
- The admin listeners of auth-service (:9001) and academy-service (:9002) run inside their containers and are not published.
