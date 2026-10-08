#!/usr/bin/env bash
# Backup: full mongodump --archive --gzip as the mongo root user, plus the
# academy STORAGE_DIR (uploaded files, Phase 5 prerequisite) as a second
# archive with the same label/stamp, so the two files form one backup set.
# Runs from the deploybot crontab (17 0 * * * UTC) and from deploy.sh before
# every deploy (BACKUP_LABEL=predeploy). Archives land in $WAEL_HOME/backups/
# as mongo-[<label>-]<UTC-timestamp>.archive.gz and
# files-[<label>-]<UTC-timestamp>.archive.gz, mode 600. When STORAGE_DIR is
# unset or missing (files feature not deployed) the files archive is skipped
# with a log line and the backup still succeeds.
# The root password is read from $WAEL_HOME/secrets/mongo_root_password and
# is never printed; it travels only inside the docker exec argument vector.
#
# Safety (full review 2026-10-06, infra H2):
# - one backup at a time (flock on $STATE_DIR/backup.lock);
# - an archive is kept only if it is non-empty AND passes `gzip -t`;
# - success is recorded in $STATE_DIR/last-backup.env (BACKUP_FILE, BACKUP_AT);
# - optional dead-man switch: BACKUP_PING_URL (env or .env.production), e.g. a
#   healthchecks.io check URL, gets a GET on success and <url>/fail on failure,
#   so a cron run that fails or never runs raises an alert. Ping errors never
#   fail the backup;
# - retention never deletes the newest archive, even if backups stopped for
#   longer than BACKUP_KEEP_DAYS.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

umask 077

BACKUP_PING_URL="${BACKUP_PING_URL:-$(read_var "$ENV_FILE" BACKUP_PING_URL 2>/dev/null || true)}"

ping_monitor() {
	# $1: "" for success, "/fail" for failure.
	[ -n "$BACKUP_PING_URL" ] || return 0
	curl -fsS --max-time 10 --retry 3 -o /dev/null "${BACKUP_PING_URL%/}$1" >/dev/null 2>&1 \
		|| log "warning: could not reach BACKUP_PING_URL (backup result unaffected)"
}

TMP=""
FILES_TMP=""
on_exit() {
	local rc=$?
	[ -z "$TMP" ] || rm -f "$TMP"
	[ -z "$FILES_TMP" ] || rm -f "$FILES_TMP"
	if [ "$rc" -ne 0 ]; then
		ping_monitor /fail
	fi
	exit "$rc"
}
trap on_exit EXIT

BACKUP_KEEP_DAYS="${BACKUP_KEEP_DAYS:-7}"
[[ "$BACKUP_KEEP_DAYS" =~ ^[1-9][0-9]*$ ]] \
	|| fail "BACKUP_KEEP_DAYS must be a positive integer, got '$BACKUP_KEEP_DAYS'"

BACKUP_LABEL="${BACKUP_LABEL:-}"
[[ -z "$BACKUP_LABEL" || "$BACKUP_LABEL" =~ ^[a-z0-9][a-z0-9-]{0,31}$ ]] \
	|| fail "BACKUP_LABEL must be lowercase letters, digits and dashes, got '$BACKUP_LABEL'"

BACKUP_DIR="${WAEL_HOME}/backups"
mkdir -p "$BACKUP_DIR" "$STATE_DIR"
chmod 700 "$BACKUP_DIR" "$STATE_DIR"

# One backup at a time: the nightly cron and a deploy must not overlap.
BACKUP_LOCK_WAIT="${BACKUP_LOCK_WAIT:-600}"
[[ "$BACKUP_LOCK_WAIT" =~ ^[0-9]+$ ]] || fail "BACKUP_LOCK_WAIT must be a whole number of seconds, got '$BACKUP_LOCK_WAIT'"
exec 9>"$STATE_DIR/backup.lock"
flock -w "$BACKUP_LOCK_WAIT" 9 \
	|| fail "another backup is still running (waited ${BACKUP_LOCK_WAIT}s for $STATE_DIR/backup.lock)"

PW_FILE="${WAEL_HOME}/secrets/mongo_root_password"
[ -s "$PW_FILE" ] || fail "mongo root password file is missing or empty: $PW_FILE"
ROOT_PW="$(cat "$PW_FILE")"

ROOT_USER="$(read_var "$ENV_FILE" MONGO_ROOT_USERNAME)"
[ -n "$ROOT_USER" ] || fail "MONGO_ROOT_USERNAME is empty in $ENV_FILE"

# Prefer the override (rehearsals), then the running compose service, then
# the conventional container name.
CID="${MONGO_CONTAINER:-$(compose ps -q mongo 2>/dev/null | head -n 1 || true)}"
[ -n "$CID" ] || CID="wael-mongo-1"

STAMP="$(date -u +%Y-%m-%dT%H%M%SZ)"
DEST="$BACKUP_DIR/mongo-${BACKUP_LABEL:+$BACKUP_LABEL-}$STAMP.archive.gz"
TMP="$DEST.tmp.$$"

docker exec "$CID" mongodump \
	--username "$ROOT_USER" --password "$ROOT_PW" \
	--authenticationDatabase admin \
	--archive --gzip >"$TMP" \
	|| fail "mongodump failed; nothing was kept"
chmod 600 "$TMP"
[ -s "$TMP" ] || fail "mongodump produced an empty archive; nothing was kept"
gzip -t "$TMP" 2>/dev/null || fail "archive failed the gzip integrity check; nothing was kept"
mv "$TMP" "$DEST"
TMP=""

# Uploaded files (Phase 5 prerequisite, ADR-0009 STORAGE_DIR): archived with
# the same label/stamp, so the mongo archive plus the files archive form one
# combined backup set that restore.sh never mixes. Objects are only read
# (tar never modifies them) and services keep running.
STORAGE_DIR="${STORAGE_DIR:-$(read_var "$ENV_FILE" STORAGE_DIR 2>/dev/null || true)}"
FILES_DEST=""
if [ -z "$STORAGE_DIR" ]; then
	log "no STORAGE_DIR in env (files feature not deployed); skipping the files archive"
elif [ ! -d "$STORAGE_DIR" ]; then
	log "STORAGE_DIR $STORAGE_DIR does not exist (files feature not deployed?); skipping the files archive"
else
	FILES_DEST="$BACKUP_DIR/files-${BACKUP_LABEL:+$BACKUP_LABEL-}$STAMP.archive.gz"
	FILES_TMP="$FILES_DEST.tmp.$$"
	TAR_RC=0
	if tar --warning=no-file-changed -czf "$FILES_TMP" -C "$STORAGE_DIR" .; then
		TAR_RC=0
	else
		TAR_RC=$?
	fi
	if [ "$TAR_RC" -gt 1 ]; then
		rm -f "$FILES_TMP"
		fail "files archive failed (tar exited $TAR_RC); nothing was kept"
	fi
	if [ "$TAR_RC" -eq 1 ]; then
		log "warning: files changed during the read; the archive below still passed verification"
	fi
	chmod 600 "$FILES_TMP"
	[ -s "$FILES_TMP" ] || fail "files archive is empty; nothing was kept"
	gzip -t "$FILES_TMP" 2>/dev/null || fail "files archive failed the gzip integrity check; nothing was kept"
	mv "$FILES_TMP" "$FILES_DEST"
	FILES_TMP=""
	log "files archive written and verified: $FILES_DEST ($(du -h "$FILES_DEST" | cut -f1))"
fi
printf 'BACKUP_FILE=%s\nBACKUP_FILES_FILE=%s\nBACKUP_AT=%s\n' "$DEST" "$FILES_DEST" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$STATE_DIR/last-backup.env"
log "backup written and verified: $DEST ($(du -h "$DEST" | cut -f1))"

# Retention: delete archives strictly older than BACKUP_KEEP_DAYS days, but
# never the archive just written (it is the newest by construction). The
# files kind keeps its own newest archive when this run wrote none.
CUTOFF_MINUTES=$((BACKUP_KEEP_DAYS * 24 * 60))
PRUNED="$(find "$BACKUP_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' ! -path "$DEST" -mmin "+$CUTOFF_MINUTES" -print -delete | wc -l)"
log "retention: kept last $BACKUP_KEEP_DAYS days, pruned $PRUNED archive(s)"
if [ -n "$FILES_DEST" ]; then
	PRUNED_FILES="$(find "$BACKUP_DIR" -maxdepth 1 -name 'files-*.archive.gz' ! -path "$FILES_DEST" -mmin "+$CUTOFF_MINUTES" -print -delete | wc -l)"
else
	NEWEST_FILES="$(find "$BACKUP_DIR" -maxdepth 1 -name 'files-*.archive.gz' -printf '%T@ %p\n' 2>/dev/null | sort -n | tail -n 1 | cut -d' ' -f2-)"
	if [ -n "$NEWEST_FILES" ]; then
		PRUNED_FILES="$(find "$BACKUP_DIR" -maxdepth 1 -name 'files-*.archive.gz' ! -path "$NEWEST_FILES" -mmin "+$CUTOFF_MINUTES" -print -delete | wc -l)"
	else
		PRUNED_FILES=0
	fi
fi
log "retention: kept last $BACKUP_KEEP_DAYS days, pruned $PRUNED_FILES files archive(s)"

ping_monitor ""
