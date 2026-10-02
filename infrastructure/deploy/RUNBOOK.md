# wael-app production runbook

Status: **scaffolded, not live.** Deploys are off until the repository
variable `DEPLOY_ENABLED` is `true`, and publishing in wael-app is off until
`PUBLISH_ENABLED` is `true` (see "Turning it on" for the order).

Nothing in this runbook has been executed yet. Treat every command as a
checklist to run and verify, not as a record of what exists.

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
6. **Per-service Mongo users** (after the first successful start of mongo):
   ```bash
   docker compose -p wael exec mongo mongosh -u wael_root -p "$(cat $WAEL_HOME/secrets/mongo_root_password)" --authenticationDatabase admin --eval '
     db.getSiblingDB("auth_db").createUser({user:"auth_svc",pwd:"<pw>",roles:[{role:"readWrite",db:"auth_db"}]});
     db.getSiblingDB("notification_db").createUser({user:"notif_svc",pwd:"<pw>",roles:[{role:"readWrite",db:"notification_db"}]});'
   ```
   The passwords go into `AUTH_MONGO_URI` and `NOTIFICATION_MONGO_URI`.
7. **Internal mTLS certificates.** Generate with the wael-app script on an
   admin machine, copy `ca.crt` and each service `.crt/.key` into
   `$WAEL_HOME/certs/` (never `ca.key`: keep it offline). Until W-10 (fixed
   container UID) lands, key files must be `644`; the `700` certs directory
   keeps other host users out.
8. **DNS.** Create an A (and AAAA if used) record for `api.<domain>` pointing
   at the server. Caddy obtains the public certificate on first start; the
   deploy's public health check fails until DNS resolves.
9. **GHCR login** (as deploybot), with a fine-grained token that has only
   `read:packages`: `docker login ghcr.io -u omarmaarouf18`.
10. **Self-hosted runner** (as deploybot): add a runner to
    **wael-app-deploy only**, labels `self-hosted,wael-vm`, installed as a
    service. The runner polls GitHub; no inbound port is opened.

## GitHub setup

| Where | What |
|---|---|
| GitHub App (owner account) | Permission: Contents read/write. Installed on `wael-app-deploy` and `wael-app-mobile` only. |
| wael-app secrets | `APP_ID`, `APP_PRIVATE_KEY` (the App's key) |
| wael-app variables | `PUBLISH_ENABLED`, `MOBILE_SYNC_ENABLED` |
| wael-app-deploy variables | `DEPLOY_ENABLED`, optional `WAEL_HOME` |
| wael-app-deploy environment | `production` (add required reviewers if your plan allows it on private repos) |
| wael-app-mobile variables | `API_BASE_URL` (for example `https://api.<domain>`) |
| wael-app-mobile secrets (optional) | `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` |

## Turning it on (order matters)

1. Prerequisites in wael-app: W-04 (`--check-env`, preflight step 7 needs
   it), W-08 (strict probes in the Dockerfiles), W-10 (fixed UID), and a
   release gate (E2E) that publishing waits for (saas-core S-01).
2. Server setup above, then run `WAEL_HOME=... ./scripts/preflight.sh` by hand
   with a real `release.env` and read every line.
3. Set `PUBLISH_ENABLED=true` in wael-app. Merge to main. Check that images
   and the deploy-repo commit appear.
4. Set `DEPLOY_ENABLED=true` in wael-app-deploy and re-run the Deploy
   workflow.

## Operations

- **Manual deploy of the current release.env:** Actions -> Deploy -> Run workflow.
- **Manual rollback:** as deploybot, `cd` into the runner's checkout and run
  `WAEL_HOME=/home/deploybot/wael ./scripts/rollback.sh`.
- **Logs:** `docker compose -p wael logs --tail 100 <service>`.

## Known gaps (not solved by this scaffold)

- No database migration framework; rollback restores images only (saas-core S-08).
- No scheduled backups or restore drill yet (Bootstrap Phase B item 14).
- `REDIS_URI` and Mongo URIs carry passwords in container env (`docker inspect`).
- Base images are pinned by tag, not digest (W-07).
- No certificate rotation job; preflight only refuses certs expiring within 14 days.
- academy-service and admin-console are not in the stack yet.
