#!/usr/bin/env bash
# Backup: full mongodump --archive --gzip as the mongo root user.
# Runs from the deploybot crontab (17 0 * * * UTC); mirrors the previous
# hand-written server backup, now versioned here. Archives land in
# $WAEL_HOME/backups/ as mongo-<UTC-timestamp>.archive.gz, mode 600.
# The root password is read from $WAEL_HOME/secrets/mongo_root_password and
# is never printed; it travels only inside the docker exec argument vector.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

umask 077

BACKUP_KEEP_DAYS="${BACKUP_KEEP_DAYS:-7}"
[[ "$BACKUP_KEEP_DAYS" =~ ^[1-9][0-9]*$ ]] \
	|| fail "BACKUP_KEEP_DAYS must be a positive integer, got '$BACKUP_KEEP_DAYS'"

BACKUP_DIR="${WAEL_HOME}/backups"
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

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
DEST="$BACKUP_DIR/mongo-$STAMP.archive.gz"
TMP="$DEST.tmp.$$"
trap 'rm -f "$TMP"' EXIT

docker exec "$CID" mongodump \
	--username "$ROOT_USER" --password "$ROOT_PW" \
	--authenticationDatabase admin \
	--archive --gzip >"$TMP"
chmod 600 "$TMP"
[ -s "$TMP" ] || fail "mongodump produced an empty archive; nothing was kept"
mv "$TMP" "$DEST"
trap - EXIT
log "backup written: $DEST ($(du -h "$DEST" | cut -f1))"

# Retention: delete archives strictly older than BACKUP_KEEP_DAYS days.
CUTOFF_MINUTES=$((BACKUP_KEEP_DAYS * 24 * 60))
PRUNED="$(find "$BACKUP_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' -mmin "+$CUTOFF_MINUTES" -print -delete | wc -l)"
log "retention: kept last $BACKUP_KEEP_DAYS days, pruned $PRUNED archive(s)"
