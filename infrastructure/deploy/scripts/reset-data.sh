#!/usr/bin/env bash
# Reset production data (owner decision 2026-10-09, part B): after the store
# reviews pass, production data is wiped ONCE and real data is entered.
# "Production data" means the test accounts, the test catalog, the reviewer
# accounts and the test PDFs. This is a scripted, rehearsable reset — never
# manual mongo commands.
#
# Refuses to run unless ALL of these hold:
# - --confirm "<exact phrase containing the API domain>" is given (the phrase
#   is printed by the refusal message);
# - the whole compose project is up;
# - a FRESH backup was taken by this script first: backup.sh runs with
#   BACKUP_LABEL=pre-reset, the archive is verified with gzip -t and its path
#   is printed. If the backup fails, the script stops before touching
#   anything.
#
# Scope flags (default: all):
# - --auth: users, sessions (access + refresh), account events, admin tokens,
#   the admin audit log and the blocklist;
# - --academy: subjects, videos, entitlements, requests, payment records,
#   plays, subject_files, app_settings — the seeded `levels` are KEPT
#   (academy-service re-seeds them idempotently at startup anyway);
# - --notifications: notifications and the Phase 7 broadcast collections
#   (broadcasts, broadcast_targets, admin_audit_log);
# - --files: empties the STORAGE_DIR contents (the encrypted test PDFs), keep
#   the directory itself at mode 700;
# - --redis: FLUSHALL with the requirepass from secrets/redis.conf. The
#   password is never printed and never appears on a command line (it
#   travels by stdin into the container).
#
# Collections are DROPPED, never databases and never users: the per-service
# Mongo users (auth_svc, notif_svc, academy_svc) live in auth_db,
# notification_db and academy_db (authSource) and dropping a database would
# break every service login. Collections are enumerated with
# getCollectionNames (listCollections) and dropped one by one, skipping
# system.*. Counts are printed before and after — counts only, never
# documents.
#
# Order: 1 backup; 2 stop the five app services (mongo, redis and caddy stay
# up); 3 wipe; 4 start the services; 5 wait for health (same checks as
# deploy.sh); 6 print the next owner steps.
#
# On any failure after the stop: print the restore.sh command for the
# pre-reset backup set and exit non-zero. Nothing is ever auto-restored —
# that is a deliberate human decision (same policy as restore.sh).
#
# Secrets: never print secrets, URIs with passwords or document contents.
# DOCUMENT_ENCRYPTION_KEY and every file under secrets/ and certs/ stay
# untouched.
#
# --dry-run prints exactly what would be dropped (db.collection: count) and
# exits 0 without stopping anything, without a backup and without wiping.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

umask 077

usage() {
	echo "usage: reset-data.sh --confirm \"<phrase>\" [--auth] [--academy] [--notifications] [--files] [--redis] [--dry-run]" >&2
	echo "  no scope flag means all scopes. Always dry-run first." >&2
	exit 2
}

CONFIRM=""
DRY_RUN=0
SCOPE_AUTH=0
SCOPE_ACADEMY=0
SCOPE_NOTIFICATIONS=0
SCOPE_FILES=0
SCOPE_REDIS=0
GOT_SCOPE=0
while [ $# -gt 0 ]; do
	case "$1" in
	--confirm)
		[ $# -ge 2 ] || usage
		CONFIRM="$2"
		shift 2
		;;
	--auth) SCOPE_AUTH=1; GOT_SCOPE=1; shift ;;
	--academy) SCOPE_ACADEMY=1; GOT_SCOPE=1; shift ;;
	--notifications) SCOPE_NOTIFICATIONS=1; GOT_SCOPE=1; shift ;;
	--files) SCOPE_FILES=1; GOT_SCOPE=1; shift ;;
	--redis) SCOPE_REDIS=1; GOT_SCOPE=1; shift ;;
	--dry-run) DRY_RUN=1; shift ;;
	-h | --help) usage ;;
	*) usage ;;
	esac
done
if [ "$GOT_SCOPE" -eq 0 ]; then
	SCOPE_AUTH=1
	SCOPE_ACADEMY=1
	SCOPE_NOTIFICATIONS=1
	SCOPE_FILES=1
	SCOPE_REDIS=1
fi

# --- precondition 1: the confirm phrase -------------------------------------
[ -f "$ENV_FILE" ] || fail "env file is missing: $ENV_FILE"
API_DOMAIN="$(read_var "$ENV_FILE" API_DOMAIN)"
[ -n "$API_DOMAIN" ] || fail "API_DOMAIN is empty in $ENV_FILE"
EXPECTED_CONFIRM="reset production data on ${API_DOMAIN}"
[ "$CONFIRM" = "$EXPECTED_CONFIRM" ] \
	|| fail "refusing to reset: --confirm must be exactly: ${EXPECTED_CONFIRM}"

# --- precondition 2: the whole compose project is up ------------------------
compose_up() {
	local svc
	for svc in "${APP_SERVICES[@]}" mongo redis caddy; do
		[ -n "$(compose ps -q "$svc" 2>/dev/null | head -n 1 || true)" ] || return 1
	done
}
compose_up || fail "the compose project is not up (all 8 containers must be running); nothing was touched"

# --- mongo access (same sources as backup.sh / restore.sh) ------------------
CID="${MONGO_CONTAINER:-$(compose ps -q mongo 2>/dev/null | head -n 1 || true)}"
[ -n "$CID" ] || CID="wael-mongo-1"
PW_FILE="${WAEL_HOME}/secrets/mongo_root_password"
[ -s "$PW_FILE" ] || fail "mongo root password file is missing or empty: $PW_FILE"
ROOT_PW="$(cat "$PW_FILE")"
ROOT_USER="$(read_var "$ENV_FILE" MONGO_ROOT_USERNAME)"
[ -n "$ROOT_USER" ] || fail "MONGO_ROOT_USERNAME is empty in $ENV_FILE"

# mongo_list DB prints one "name|count" line per collection (counts only).
mongo_list() {
	docker exec "$CID" mongosh \
		--username "$ROOT_USER" --password "$ROOT_PW" \
		--authenticationDatabase admin --quiet --eval \
		"var dbx=db.getSiblingDB(\"$1\");dbx.getCollectionNames().forEach(function(n){print(n+\"|\"+dbx.getCollection(n).countDocuments({}))});"
}

# mongo_drop DB NAME... drops the named collections. Names are validated by
# the caller (safe to embed in the JS string).
mongo_drop() {
	local db="$1" list="" name
	shift
	for name in "$@"; do
		list+="\"$name\","
	done
	list="${list%,}"
	docker exec "$CID" mongosh \
		--username "$ROOT_USER" --password "$ROOT_PW" \
		--authenticationDatabase admin --quiet --eval \
		"var dbx=db.getSiblingDB(\"$db\");[$list].forEach(function(n){dbx.getCollection(n).drop()});"
}

# is_kept DB NAME: the only collection any reset keeps.
is_kept() {
	# the seeded academy levels survive every reset (re-seeded at startup)
	[ "$1" = "academy_db" ] && [ "$2" = "levels" ]
}

# mongo_counts DB -> prints "name|count" lines for the collections that
# remain, after filtering system.* and unparseable names.
filtered_counts() {
	local out line name count
	out="$(mongo_list "$1")" || return 1
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		name="${line%%|*}"
		count="${line#*|}"
		[[ "$name" =~ ^[A-Za-z0-9_.-]+$ ]] || continue
		case "$name" in system.*) continue ;; esac
		printf '%s|%s\n' "$name" "$count"
	done <<<"$out"
}

# --- dry-run: show, then leave everything alone -----------------------------
dry_run_db() {
	# dry_run_db DB: print what would be dropped (db.collection: count) and
	# what is kept. Reads only; changes nothing.
	local db="$1" out name count
	out="$(filtered_counts "$db")" || return 1
	while IFS='|' read -r name count; do
		[ -n "$name" ] || continue
		if is_kept "$db" "$name"; then
			log "$db.$name: $count (kept)"
		else
			log "$db.$name: $count (would drop)"
		fi
	done <<<"$out"
}

if [ "$DRY_RUN" -eq 1 ]; then
	log "DRY-RUN: nothing is backed up, stopped, dropped, emptied or flushed"
	if [ "$SCOPE_AUTH" -eq 1 ]; then
		dry_run_db auth_db || fail "dry-run enumeration failed for auth_db"
	fi
	if [ "$SCOPE_ACADEMY" -eq 1 ]; then
		dry_run_db academy_db || fail "dry-run enumeration failed for academy_db"
	fi
	if [ "$SCOPE_NOTIFICATIONS" -eq 1 ]; then
		dry_run_db notification_db || fail "dry-run enumeration failed for notification_db"
	fi
	if [ "$SCOPE_FILES" -eq 1 ]; then
		STORAGE_DIR="${STORAGE_DIR:-$(read_var "$ENV_FILE" STORAGE_DIR 2>/dev/null || true)}"
		if [ -n "$STORAGE_DIR" ] && [ -d "$STORAGE_DIR" ]; then
			log "would empty $STORAGE_DIR ($(find "$STORAGE_DIR" -mindepth 1 -maxdepth 1 | wc -l) entries; directory kept, mode 700)"
		else
			log "no STORAGE_DIR set or missing (files feature not deployed?); nothing would be emptied"
		fi
	fi
	if [ "$SCOPE_REDIS" -eq 1 ]; then
		log "would run FLUSHALL on redis (OTPs, session keys, rate-limit counters)"
	fi
	log "DRY-RUN complete; nothing was changed"
	exit 0
fi

# --- step 1: fresh, verified pre-reset backup -------------------------------
log "taking the fresh pre-reset backup (BACKUP_LABEL=pre-reset)"
BACKUP_LABEL=pre-reset "$(dirname "$0")/backup.sh" \
	|| fail "the pre-reset backup failed; nothing was stopped or wiped"
[ -f "$STATE_DIR/last-backup.env" ] || fail "backup.sh left no $STATE_DIR/last-backup.env"
BACKUP_FILE="$(read_var "$STATE_DIR/last-backup.env" BACKUP_FILE)"
[ -f "$BACKUP_FILE" ] || fail "the pre-reset backup is missing: $BACKUP_FILE"
gzip -t "$BACKUP_FILE" 2>/dev/null || fail "the pre-reset backup failed the gzip integrity check: $BACKUP_FILE"
BACKUP_FILES_FILE="$(read_var "$STATE_DIR/last-backup.env" BACKUP_FILES_FILE)"
if [ -n "$BACKUP_FILES_FILE" ]; then
	gzip -t "$BACKUP_FILES_FILE" 2>/dev/null \
		|| fail "the pre-reset files backup failed the gzip integrity check: $BACKUP_FILES_FILE"
fi
log "pre-reset backup verified: $BACKUP_FILE"

# --- failure policy after this point: print the restore command -------------
recovery() {
	# $1 names what failed. Never restart the apps as if healthy and never
	# auto-restore: print the exact restore command for the pre-reset set.
	log "FAILED: $1"
	log "the reset did not complete; the app services were NOT verified healthy"
	log "to restore the pre-reset state once the cause is fixed (a deliberate human step, never automatic):"
	if [ -n "$BACKUP_FILES_FILE" ]; then
		log "  $(dirname "$0")/restore.sh \"$BACKUP_FILE\" --files \"$BACKUP_FILES_FILE\" --yes"
	else
		log "  $(dirname "$0")/restore.sh \"$BACKUP_FILE\" --yes"
	fi
	log "to inspect instead: compose ps, then 'compose logs --tail 100 <service>' from the deploy checkout"
	exit 1
}

# --- step 2: stop the five app services -------------------------------------
log "stopping the app services (mongo, redis and caddy stay up)"
compose stop "${APP_SERVICES[@]}" || fail "could not stop the app services; aborting before the wipe"

# --- step 3: wipe ------------------------------------------------------------
wipe_db() {
	# wipe_db DB: drop every collection except system.* and the kept list,
	# printing counts before and after (counts only, never documents).
	local db="$1" out line name count
	local -A BEFORE=() AFTER=()
	local -a DROP=()
	out="$(filtered_counts "$db")" || return 1
	while IFS='|' read -r name count; do
		[ -n "$name" ] || continue
		if is_kept "$db" "$name"; then
			continue
		fi
		BEFORE["$name"]="$count"
		DROP+=("$name")
	done <<<"$out"
	if [ "${#DROP[@]}" -gt 0 ]; then
		mongo_drop "$db" "${DROP[@]}" || return 1
	else
		log "$db: nothing to drop"
	fi
	out="$(filtered_counts "$db")" || return 1
	while IFS='|' read -r name count; do
		[ -n "$name" ] || continue
		AFTER["$name"]="$count"
	done <<<"$out"
	for name in "${!BEFORE[@]}"; do
		if [ -z "${AFTER[$name]:-}" ]; then
			log "$db.$name: ${BEFORE[$name]} -> 0 (dropped)"
		else
			log "$db.$name: ${BEFORE[$name]} -> ${AFTER[$name]} (still present)"
			return 1
		fi
	done
	if [ "$db" = "academy_db" ] && [ -n "${AFTER[levels]:-}" ]; then
		log "academy_db.levels: ${AFTER[levels]} (kept)"
	fi
}

wipe_files() {
	# Empty the STORAGE_DIR contents; keep the directory itself at mode 700.
	STORAGE_DIR="${STORAGE_DIR:-$(read_var "$ENV_FILE" STORAGE_DIR 2>/dev/null || true)}"
	if [ -z "$STORAGE_DIR" ]; then
		log "no STORAGE_DIR in env (files feature not deployed); skipping the files wipe"
		return 0
	fi
	if [ ! -d "$STORAGE_DIR" ]; then
		log "STORAGE_DIR $STORAGE_DIR does not exist; skipping the files wipe"
		return 0
	fi
	find "$STORAGE_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + || return 1
	chmod 700 "$STORAGE_DIR" || return 1
	log "storage wiped: $STORAGE_DIR is empty (directory kept, mode 700)"
}

flush_redis() {
	# FLUSHALL with the requirepass from secrets/redis.conf. The password is
	# read into a variable, piped into the container over stdin and expanded
	# only INSIDE the container shell, so it never lands on an argument
	# vector and is never printed.
	local conf="${WAEL_HOME}/secrets/redis.conf" pw rcid
	if [ ! -s "$conf" ]; then
		log "secrets/redis.conf is missing or empty; cannot authenticate the redis flush"
		return 1
	fi
	pw="$(sed -n 's/^requirepass //p' "$conf" | head -n 1)"
	if [ -z "$pw" ]; then
		log "secrets/redis.conf has no requirepass line; cannot authenticate the redis flush"
		return 1
	fi
	rcid="${REDIS_CONTAINER:-$(compose ps -q redis 2>/dev/null | head -n 1 || true)}"
	[ -n "$rcid" ] || rcid="wael-redis-1"
	printf '%s\n' "$pw" | docker exec -i "$rcid" sh -c 'read -r P && export REDISCLI_AUTH="$P" && exec redis-cli FLUSHALL' >/dev/null
	log "redis flushed: FLUSHALL on $rcid (password never printed)"
}

if [ "$SCOPE_AUTH" -eq 1 ]; then
	wipe_db auth_db || recovery "the auth_db wipe failed"
fi
if [ "$SCOPE_ACADEMY" -eq 1 ]; then
	wipe_db academy_db || recovery "the academy_db wipe failed"
fi
if [ "$SCOPE_NOTIFICATIONS" -eq 1 ]; then
	wipe_db notification_db || recovery "the notification_db wipe failed"
fi
if [ "$SCOPE_FILES" -eq 1 ]; then
	wipe_files || recovery "emptying STORAGE_DIR failed"
fi
if [ "$SCOPE_REDIS" -eq 1 ]; then
	flush_redis || recovery "the redis FLUSHALL failed"
fi

# --- step 4 + 5: start again and wait for health (same checks as deploy.sh) -
log "starting the app services again"
compose up -d --remove-orphans --wait --wait-timeout 180 "${APP_SERVICES[@]}" \
	|| recovery "the app services did not become healthy after the reset"
deployed=0
for _ in $(seq 1 12); do
	if external_health; then
		deployed=1
		break
	fi
	sleep 5
done
[ "$deployed" -eq 1 ] || recovery "the public health check through Caddy failed"

# --- step 6: the next owner steps -------------------------------------------
log "reset complete and healthy; next owner steps:"
log "  1. mint a fresh admin token (all admin tokens were wiped): sudo docker exec wael-auth-service-1 /bin/onboard-admin --name \"<name>\" --ttl 2160h  (SERVER-MANUAL §9)"
log "  2. re-enter the console Settings: show_prices, the WhatsApp number and the Academy info (the tab now says \"Academy\", formerly \"center\")"
log "  3. recreate the catalog in the \"دوراتي / Courses\" tab (the seeded levels are kept; subjects and videos are gone)"
log "  4. re-upload the PDFs in the Files tab (STORAGE_DIR was emptied)"
log "  5. keep FEATURES_FILES as decided (the env file was not touched)"
log "  6. recreate the store-review demo account with one active subject (Apple and Google need one for every future review)"
log "full checklist: SERVER-MANUAL.md §10 'Resetting production data (one-time, before launch)'"
