#!/usr/bin/env bash
# Deploy: pre-flight -> pre-deploy backup -> up with health gate ->
# external check -> record.
# On any failure after containers were touched, roll back to the last
# release that passed this script (fixes saas-core S-03: no HEAD~1 guess).
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"

"$(dirname "$0")/preflight.sh"

tag="$(read_var "$RELEASE_FILE" IMAGE_TAG)"
log "deploying IMAGE_TAG=$tag"

# Refuse to deploy a release that was rolled back unless explicitly allowed
if [ -f "$FAILED_RELEASES_FILE" ] && grep -qxF "$tag" "$FAILED_RELEASES_FILE"; then
	if [ "${ALLOW_FAILED_RELEASE:-0}" != "1" ]; then
		fail "IMAGE_TAG=$tag was rolled back and is listed in $FAILED_RELEASES_FILE. Fix forward with a new commit on main, or set ALLOW_FAILED_RELEASE=1 to force."
	fi
	log "WARNING: IMAGE_TAG=$tag is listed in $FAILED_RELEASES_FILE, but ALLOW_FAILED_RELEASE=1 is set; proceeding"
fi

# Pre-deploy backup (full review 2026-10-06, infra H1): a release can migrate
# data (indexes, purge jobs) and rollback.sh restores images only, so take a
# verified backup before any container changes. A failed backup stops the
# deploy before anything is touched. The first deploy (no mongo running yet)
# has nothing to back up. SKIP_PREDEPLOY_BACKUP=1 is an explicit, logged
# escape hatch for emergencies only.
PREDEPLOY_BACKUP=""
if [ "${SKIP_PREDEPLOY_BACKUP:-0}" = "1" ]; then
	log "WARNING: SKIP_PREDEPLOY_BACKUP=1 is set; deploying without a pre-deploy backup"
elif [ -z "${MONGO_CONTAINER:-}" ] && [ -z "$(compose ps -q mongo 2>/dev/null | head -n 1 || true)" ]; then
	log "no running mongo container (first deploy?); skipping the pre-deploy backup"
else
	log "taking the pre-deploy backup"
	BACKUP_LABEL=predeploy "$(dirname "$0")/backup.sh" \
		|| fail "pre-deploy backup failed; nothing was deployed (fix the backup, or set SKIP_PREDEPLOY_BACKUP=1 to force)"
	PREDEPLOY_BACKUP="$(read_var "$STATE_DIR/last-backup.env" BACKUP_FILE)"
	log "pre-deploy backup: $PREDEPLOY_BACKUP"
fi
export PREDEPLOY_BACKUP

deployed=0
if compose up -d --remove-orphans --wait --wait-timeout 180; then
	# Health through Caddy with real public TLS; allow time for ACME on first run.
	for _ in $(seq 1 12); do
		if external_health; then deployed=1; break; fi
		sleep 5
	done
	[ "$deployed" -eq 1 ] || log "external health check through Caddy failed"
else
	log "containers did not become healthy within 180s"
fi

if [ "$deployed" -eq 1 ]; then
	mkdir -p "$LAST_GOOD_DIR"
	chmod 700 "$LAST_GOOD_DIR"
	printf 'IMAGE_TAG=%s\nDEPLOYED_AT=%s\n' "$tag" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$LAST_GOOD_FILE"
	cp "$LAST_GOOD_FILE" "$LEGACY_LAST_GOOD_FILE"
	cp "${REPO_DIR}/docker-compose.yml" "${LAST_GOOD_DIR}/docker-compose.yml"
	cp "${REPO_DIR}/Caddyfile" "${LAST_GOOD_DIR}/Caddyfile"
	log "deploy succeeded; recorded $tag as last good release and preserved compose config"
	exit 0
fi

compose ps || true
for svc in "${APP_SERVICES[@]}"; do
	log "last log lines: $svc"
	compose logs --no-color --tail 30 "$svc" || true
done
"$(dirname "$0")/rollback.sh" "$tag" || true
fail "deploy of $tag failed (see rollback output above)"
