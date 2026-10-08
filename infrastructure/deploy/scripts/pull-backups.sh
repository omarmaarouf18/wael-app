#!/usr/bin/env bash
# Pull server backups off-site to this machine over SSH.
# No cloud storage, no new secrets: existing SSH key auth as azureuser plus
# passwordless *rsync-only* sudo on the server (the backups dir is mode 700
# under deploybot, so plain rsync cannot read it). No --delete: this machine
# keeps the newest BACKUP_KEEP_LOCAL archives (default 30) while the server
# keeps 7.
#
# Freshness (full review 2026-10-06, infra H2): the run FAILS (exit 1) when the
# newest archive is older than BACKUP_MAX_AGE_HOURS (default 30, one missed
# nightly run plus slack) or fails `gzip -t`, so a silent server-side backup
# failure is noticed here even without a monitoring service.
set -euo pipefail

WAEL_HOST="${WAEL_HOST:?set WAEL_HOST to the server hostname, e.g. export WAEL_HOST=<host>}"
WAEL_SSH_USER="${WAEL_SSH_USER:-azureuser}"
REMOTE_BACKUPS="${REMOTE_BACKUPS:-/home/deploybot/wael/backups/}"
LOCAL_DIR="${LOCAL_BACKUP_DIR:-$HOME/wael-offsite-backups}"
BACKUP_KEEP_LOCAL="${BACKUP_KEEP_LOCAL:-30}"
BACKUP_MAX_AGE_HOURS="${BACKUP_MAX_AGE_HOURS:-30}"
[[ "$BACKUP_MAX_AGE_HOURS" =~ ^[1-9][0-9]*$ ]] \
	|| {
		echo "BACKUP_MAX_AGE_HOURS must be a positive integer, got '$BACKUP_MAX_AGE_HOURS'" >&2
		exit 1
	}
[[ "$BACKUP_KEEP_LOCAL" =~ ^[1-9][0-9]*$ ]] \
	|| {
		echo "BACKUP_KEEP_LOCAL must be a positive integer, got '$BACKUP_KEEP_LOCAL'" >&2
		exit 1
	}

command -v rsync >/dev/null 2>&1 || {
	echo "rsync is not installed on this machine" >&2
	exit 1
}

REMOTE="$WAEL_SSH_USER@$WAEL_HOST"
if ! ssh -o BatchMode=yes "$REMOTE" command -v rsync >/dev/null 2>&1; then
	echo "rsync is missing on the server: ssh in and run: sudo apt install -y rsync" >&2
	exit 1
fi
if ! ssh -o BatchMode=yes "$REMOTE" sudo -n true 2>/dev/null; then
	cat >&2 <<EOF
Passwordless sudo for rsync is not set up on the server. On the server, as azureuser:
  echo '$WAEL_SSH_USER ALL=(root) NOPASSWD: /usr/bin/rsync --server *' | sudo tee /etc/sudoers.d/wael-backup-pull
  sudo chmod 440 /etc/sudoers.d/wael-backup-pull
EOF
	exit 1
fi

mkdir -p "$LOCAL_DIR"
chmod 700 "$LOCAL_DIR"

rsync -a --rsync-path='sudo rsync' "$REMOTE:$REMOTE_BACKUPS" "$LOCAL_DIR/"
find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' -exec chmod 600 {} +
find "$LOCAL_DIR" -maxdepth 1 -name 'files-*.archive.gz' -exec chmod 600 {} +

# Prune local history to the newest BACKUP_KEEP_LOCAL archives of each kind
# (oldest first, drop everything past the keep count). Mongo and files sets
# are pruned independently so a set always keeps both of its archives.
for kind in mongo files; do
	mapfile -t STALE < <(find "$LOCAL_DIR" -maxdepth 1 -name "$kind-*.archive.gz" -printf '%T@ %p\n' |
		sort -n | head -n "-$BACKUP_KEEP_LOCAL" | cut -d' ' -f2-)
	if [ "${#STALE[@]}" -gt 0 ]; then
		rm -f "${STALE[@]}"
		echo "pruned ${#STALE[@]} local $kind archive(s)"
	fi
done

COUNT="$(find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' | wc -l)"
FILES_COUNT="$(find "$LOCAL_DIR" -maxdepth 1 -name 'files-*.archive.gz' | wc -l)"
NEWEST="$(find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' -printf '%T@ %p\n' | sort -n | tail -n 1 | cut -d' ' -f2-)"
echo "off-site backups in $LOCAL_DIR: $((COUNT + FILES_COUNT)) archive(s) ($COUNT mongo, $FILES_COUNT files); newest: ${NEWEST:-none}"

if [ -z "$NEWEST" ]; then
	echo "STALE BACKUPS: no archive was pulled at all; check backup.sh and the deploybot crontab on the server" >&2
	exit 1
fi
AGE_HOURS=$(( ($(date +%s) - $(stat -c %Y "$NEWEST")) / 3600 ))
if [ "$AGE_HOURS" -ge "$BACKUP_MAX_AGE_HOURS" ]; then
	echo "STALE BACKUPS: the newest archive is ${AGE_HOURS}h old (limit ${BACKUP_MAX_AGE_HOURS}h); the nightly backup on the server is failing or not running" >&2
	exit 1
fi
if ! gzip -t "$NEWEST" 2>/dev/null; then
	echo "CORRUPT BACKUP: the newest archive fails the gzip integrity check: $NEWEST" >&2
	exit 1
fi
echo "newest mongo archive is ${AGE_HOURS}h old and passes gzip -t"

# The files archive rides with the same backup set: when the server sends
# any, the newest local one must be fresh and intact too. No files archives
# at all means the files feature is not deployed; skip, do not fail.
NEWEST_FILES="$(find "$LOCAL_DIR" -maxdepth 1 -name 'files-*.archive.gz' -printf '%T@ %p\n' | sort -n | tail -n 1 | cut -d' ' -f2-)"
if [ -z "$NEWEST_FILES" ]; then
	echo "no files archives pulled (files feature not deployed?); skipping files checks"
else
	FILES_AGE_HOURS=$(( ($(date +%s) - $(stat -c %Y "$NEWEST_FILES")) / 3600 ))
	if [ "$FILES_AGE_HOURS" -ge "$BACKUP_MAX_AGE_HOURS" ]; then
		echo "STALE BACKUPS: the newest files archive is ${FILES_AGE_HOURS}h old (limit ${BACKUP_MAX_AGE_HOURS}h); the server stopped archiving uploaded files" >&2
		exit 1
	fi
	if ! gzip -t "$NEWEST_FILES" 2>/dev/null; then
		echo "CORRUPT BACKUP: the newest files archive fails the gzip integrity check: $NEWEST_FILES" >&2
		exit 1
	fi
	echo "newest files archive is ${FILES_AGE_HOURS}h old and passes gzip -t"
fi
