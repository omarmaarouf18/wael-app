#!/usr/bin/env bash
# Restore: writes one mongodump --archive --gzip file back into mongo.
# Refuses to run without an explicit --yes. Stops the five app services
# first (writers must be quiet); mongo and redis stay up. After the restore
# the app services are started again and the public health gate must pass.
# --skip-restart is a rehearsal escape hatch: it restores without touching
# any service and skips the health gate (orchestration is covered by the
# mocked tests in scripts/deploy_scripts_test.sh instead).
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

umask 077

usage() {
	echo "usage: restore.sh <archive> --yes [--skip-restart]" >&2
	exit 2
}

YES=0
SKIP_RESTART=0
ARCHIVE=""
for arg in "$@"; do
	case "$arg" in
	--yes) YES=1 ;;
	--skip-restart) SKIP_RESTART=1 ;;
	-h | --help) usage ;;
	*) ARCHIVE="$arg" ;;
	esac
done

[ -n "$ARCHIVE" ] || usage
[ -f "$ARCHIVE" ] || fail "archive not found: $ARCHIVE"
[ "$YES" = 1 ] || fail "refusing to restore $ARCHIVE without --yes"

PW_FILE="${WAEL_HOME}/secrets/mongo_root_password"
[ -s "$PW_FILE" ] || fail "mongo root password file is missing or empty: $PW_FILE"
ROOT_PW="$(cat "$PW_FILE")"

ROOT_USER="$(read_var "$ENV_FILE" MONGO_ROOT_USERNAME)"
[ -n "$ROOT_USER" ] || fail "MONGO_ROOT_USERNAME is empty in $ENV_FILE"

CID="${MONGO_CONTAINER:-$(compose ps -q mongo 2>/dev/null | head -n 1 || true)}"
[ -n "$CID" ] || CID="wael-mongo-1"

if [ "$SKIP_RESTART" = 0 ]; then
	log "stopping app services (mongo and redis stay up)"
	compose stop "${APP_SERVICES[@]}" || fail "could not stop app services; aborting restore"
fi

log "restoring $ARCHIVE (collections are dropped before re-insertion)"
RESTORED=1
if ! docker exec -i "$CID" mongorestore \
	--username "$ROOT_USER" --password "$ROOT_PW" \
	--authenticationDatabase admin \
	--archive --gzip --drop <"$ARCHIVE"; then
	RESTORED=0
	log "mongorestore reported failure"
fi

if [ "$SKIP_RESTART" = 0 ]; then
	log "starting app services again"
	compose up -d --remove-orphans --wait --wait-timeout 180 "${APP_SERVICES[@]}" \
		|| fail "app services did not become healthy after restore; manual recovery needed"
	external_health \
		|| fail "restore is up but the public health check fails; manual recovery needed"
fi

[ "$RESTORED" = 1 ] || fail "mongorestore failed; services were restarted above — verify data before use"
log "restore of $ARCHIVE complete and healthy"
