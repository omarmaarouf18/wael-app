#!/usr/bin/env bash
# Pull server backups off-site to this machine over SSH.
# No cloud storage, no new secrets: existing SSH key auth as azureuser plus
# passwordless *rsync-only* sudo on the server (the backups dir is mode 700
# under deploybot, so plain rsync cannot read it). No --delete: this machine
# keeps the newest BACKUP_KEEP_LOCAL archives (default 30) while the server
# keeps 7.
set -euo pipefail

WAEL_HOST="${WAEL_HOST:?set WAEL_HOST to the server hostname, e.g. export WAEL_HOST=<host>}"
WAEL_SSH_USER="${WAEL_SSH_USER:-azureuser}"
REMOTE_BACKUPS="${REMOTE_BACKUPS:-/home/deploybot/wael/backups/}"
LOCAL_DIR="${LOCAL_BACKUP_DIR:-$HOME/wael-offsite-backups}"
BACKUP_KEEP_LOCAL="${BACKUP_KEEP_LOCAL:-30}"
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

# Prune local history to the newest BACKUP_KEEP_LOCAL archives (oldest first,
# drop everything past the keep count).
mapfile -t STALE < <(find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' -printf '%T@ %p\n' |
	sort -n | head -n "-$BACKUP_KEEP_LOCAL" | cut -d' ' -f2-)
if [ "${#STALE[@]}" -gt 0 ]; then
	rm -f "${STALE[@]}"
	echo "pruned ${#STALE[@]} local archive(s)"
fi

COUNT="$(find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' | wc -l)"
NEWEST="$(find "$LOCAL_DIR" -maxdepth 1 -name 'mongo-*.archive.gz' -printf '%T@ %p\n' | sort -n | tail -n 1 | cut -d' ' -f2-)"
echo "off-site backups in $LOCAL_DIR: $COUNT archive(s); newest: ${NEWEST:-none}"
