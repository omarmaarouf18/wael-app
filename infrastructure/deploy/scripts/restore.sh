#!/usr/bin/env bash
# Restore: writes one backup set back: the mongodump --archive --gzip file
# into mongo, plus (when present) the matching files archive into the academy
# STORAGE_DIR. The set shares one label/stamp and is never mixed: a sibling
# files archive is auto-discovered from the mongo archive name, or passed
# explicitly with --files (its stamp must match, else restore refuses).
# Refuses to run without an explicit --yes. Every archive is validated
# (exists, readable, non-empty, gzip -t) BEFORE any service is stopped
# (review M6). Stops the five app services first (writers must be quiet);
# mongo and redis stay up. After the restore the app services are started
# again and the public health gate must pass. On a restore failure the
# services are NOT restarted as if healthy: the script prints the exact
# retry command and exits non-zero, leaving the services stopped for a
# deliberate human decision. --skip-restart is a rehearsal escape hatch:
# it restores without touching services and skips the health gate
# (orchestration is covered by the mocked tests in
# scripts/deploy_scripts_test.sh instead).
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

umask 077

usage() {
	echo "usage: restore.sh <mongo-archive> [--files <files-archive>] --yes [--skip-restart]" >&2
	exit 2
}

YES=0
SKIP_RESTART=0
ARCHIVE=""
FILES_GIVEN=""
for arg in "$@"; do
	case "$arg" in
	--yes) YES=1 ;;
	--skip-restart) SKIP_RESTART=1 ;;
	--files)
		WANT_FILES=1
		;;
	-h | --help) usage ;;
	*)
		if [ "${WANT_FILES:-0}" = 1 ]; then
			FILES_GIVEN="$arg"
			WANT_FILES=0
		else
			ARCHIVE="$arg"
		fi
		;;
	esac
done

[ -n "$ARCHIVE" ] || usage
[ "${WANT_FILES:-0}" = 0 ] || fail "--files needs a file argument"
[ "$YES" = 1 ] || fail "refusing to restore $ARCHIVE without --yes"

# Set stamp: the archive name minus the kind prefix and the suffix, so
# mongo-predeploy-<stamp> and files-predeploy-<stamp> compare equal and two
# different sets never mix.
set_stamp() {
	basename "$1" | sed -e 's/^mongo-//' -e 's/^files-//' -e 's/\.archive\.gz$//'
}

validate_archive() {
	[ -f "$1" ] || fail "archive not found: $1"
	[ -r "$1" ] || fail "archive not readable: $1"
	[ -s "$1" ] || fail "archive is empty: $1"
	gzip -t "$1" 2>/dev/null || fail "archive fails the gzip integrity check: $1"
}

validate_archive "$ARCHIVE"

# Sibling files archive: explicit --files wins; otherwise auto-discover the
# files archive with the same label/stamp next to the mongo archive; a
# legacy mongo-only set restores mongo only, with a log line.
FILES_ARCHIVE=""
if [ -n "$FILES_GIVEN" ]; then
	validate_archive "$FILES_GIVEN"
	[ "$(set_stamp "$FILES_GIVEN")" = "$(set_stamp "$ARCHIVE")" ] \
		|| fail "refusing to mix backup sets: $FILES_GIVEN does not match the set of $ARCHIVE (pass the files archive with the same stamp, or omit --files)"
	FILES_ARCHIVE="$FILES_GIVEN"
else
	SIBLING="$(dirname "$ARCHIVE")/$(basename "$ARCHIVE" | sed 's/^mongo-/files-/')"
	if [ "$SIBLING" != "$ARCHIVE" ] && [ -f "$SIBLING" ]; then
		validate_archive "$SIBLING"
		FILES_ARCHIVE="$SIBLING"
		log "found the matching files archive of this set: $FILES_ARCHIVE"
	else
		log "no matching files archive next to $ARCHIVE; restoring mongo only"
	fi
fi

# A files restore needs somewhere to extract to; decide before stopping.
# An exported STORAGE_DIR wins, then the env file (which may not set it yet
# when the files feature is new).
if [ -n "$FILES_ARCHIVE" ]; then
	STORAGE_DIR="${STORAGE_DIR:-$(read_var "$ENV_FILE" STORAGE_DIR 2>/dev/null || true)}"
	[ -n "$STORAGE_DIR" ] || fail "cannot restore $FILES_ARCHIVE: STORAGE_DIR is not set (files have nowhere to extract to)"
fi

PW_FILE="${WAEL_HOME}/secrets/mongo_root_password"
[ -s "$PW_FILE" ] || fail "mongo root password file is missing or empty: $PW_FILE"
ROOT_PW="$(cat "$PW_FILE")"

ROOT_USER="$(read_var "$ENV_FILE" MONGO_ROOT_USERNAME)"
[ -n "$ROOT_USER" ] || fail "MONGO_ROOT_USERNAME is empty in $ENV_FILE"

CID="${MONGO_CONTAINER:-$(compose ps -q mongo 2>/dev/null | head -n 1 || true)}"
[ -n "$CID" ] || CID="wael-mongo-1"

recovery() {
	# $1 names what failed. Never restart the apps as if healthy: print the
	# exact command to retry once the cause is fixed, and exit non-zero with
	# the services stopped.
	log "FAILED: $1; app services are still stopped and were NOT restarted as healthy"
	if [ -n "$FILES_ARCHIVE" ]; then
		log "to retry once the cause is fixed:"
		log "  $(dirname "$0")/restore.sh \"$ARCHIVE\" --files \"$FILES_ARCHIVE\" --yes"
	else
		log "to retry once the cause is fixed:"
		log "  $(dirname "$0")/restore.sh \"$ARCHIVE\" --yes"
	fi
	log "to inspect instead: compose ps, then 'compose logs --tail 100 <service>' from the deploy checkout"
	exit 1
}

if [ "$SKIP_RESTART" = 0 ]; then
	log "stopping app services (mongo and redis stay up)"
	compose stop "${APP_SERVICES[@]}" || fail "could not stop app services; aborting restore"
fi

log "restoring $ARCHIVE (collections are dropped before re-insertion)"
if ! docker exec -i "$CID" mongorestore \
	--username "$ROOT_USER" --password "$ROOT_PW" \
	--authenticationDatabase admin \
	--archive --gzip --drop <"$ARCHIVE"; then
	recovery "mongorestore failed"
fi

if [ -n "$FILES_ARCHIVE" ]; then
	log "restoring $FILES_ARCHIVE into $STORAGE_DIR"
	mkdir -p "$STORAGE_DIR" || recovery "could not create STORAGE_DIR $STORAGE_DIR"
	if ! tar -xzf "$FILES_ARCHIVE" -C "$STORAGE_DIR"; then
		recovery "files restore failed"
	fi
fi

if [ "$SKIP_RESTART" = 0 ]; then
	log "starting app services again"
	compose up -d --remove-orphans --wait --wait-timeout 180 "${APP_SERVICES[@]}" \
		|| fail "app services did not become healthy after restore; manual recovery needed"
	external_health \
		|| fail "restore is up but the public health check fails; manual recovery needed"
fi

if [ -n "$FILES_ARCHIVE" ]; then
	log "restore of $ARCHIVE and $FILES_ARCHIVE complete and healthy"
else
	log "restore of $ARCHIVE complete and healthy"
fi
