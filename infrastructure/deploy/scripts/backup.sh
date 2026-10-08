#!/usr/bin/env bash
# Backup: full mongodump --archive --gzip as the mongo root user.
# Runs from the deploybot crontab (17 0 * * * UTC) and from deploy.sh before
# every deploy (BACKUP_LABEL=predeploy). Archives land in $WAEL_HOME/backups/
# as mongo-[<label>-]<UTC-timestamp>.archive.gz, mode 600.
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
on_exit() {
	local rc=$?
	[ -z "$TMP" ] || rm -f "$TMP"
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
printf 'BACKUP_FILE=%s\nBACKUP_AT=%s\n' "$DEST" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$STATE_DIR/last-backup.env"
log "backup written and verified: $DEST ($(du -h "$DEST" | cut -f1))"

# Retention: delete archives strictly older than BACKUP_KEEP_DAYS days, but
# never the archive just written (it is the newest by construction).
CUTOFF_MINUTES=$((BACKUP_KEEP_DAYS * 24 * 60))
PRUNED="$(find "$BACKUP_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' ! -path "$DEST" -mmin "+$CUTOFF_MINUTES" -print -delete | wc -l)"
log "retention: kept last $BACKUP_KEEP_DAYS days, pruned $PRUNED archive(s)"

ping_monitor ""
