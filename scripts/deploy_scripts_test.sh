#!/usr/bin/env bash
# Self-test for deploy.sh, rollback.sh, backup.sh, restore.sh and pull-backups.sh:
# 1. Rollback uses state/last-good/docker-compose.yml with -p wael and --remove-orphans
# 2. Failed-releases guard blocks deployment of rolled-back SHAs unless ALLOW_FAILED_RELEASE=1
# 3. Missing state/last-good/ fallback triggers loud warning and uses current compose
# 4. Successful deploy populates state/last-good/ (compose, Caddyfile, last-good.env)
# 5. Rollback records failed SHA in state/failed-releases
# 6. Last-good snapshot completeness: relative paths in compose are captured
# 7. Backup writes a 600 archive via the compose-resolved container, never prints the password
# 8. Backup honors MONGO_CONTAINER and BACKUP_KEEP_DAYS pruning, rejects bad keep values
# 9. Backup fails without the root password file
# 10. Restore refuses to run without --yes and touches nothing
# 11. Restore with --yes stops apps, runs mongorestore --drop, restarts and health-gates
# 12. Restore --skip-restart restores without touching services (rehearsal path)
# 13. pull-backups.sh validates env, pulls via rsync, keeps newest N at 600
# 14. preflight.sh redis memory checks: cap + noeviction passes; either alone fails
# 14b. preflight.sh file storage checks (Phase 5): 64-hex key, WAEL_UID/GID of the
#     running user, STORAGE_DIR absolute, present, mode 700, owned by WAEL_UID,
#     strict FEATURES_FILES; the compose files wire storage, key and caps
# 15. Caddyfile sends the exact HSTS header with includeSubDomains (no preload) on both site blocks
# 16. Deploy takes a verified pre-deploy backup before touching containers; a failed backup
#     aborts the deploy; first deploy (no mongo) and SKIP_PREDEPLOY_BACKUP=1 skip it; a failed
#     deploy's rollback prints the exact restore command for the pre-deploy backup
# 17. Backup keeps nothing that fails gzip -t, records last-backup.env, labels archives,
#     pings BACKUP_PING_URL (/fail on failure), and waits on the lock instead of overlapping
# 18. pull-backups.sh fails on a stale or corrupt newest archive
# 19. Backup archives STORAGE_DIR as files-<label>-<stamp>.archive.gz in one
#     set with the mongo archive (same stamp, own retention); unset or
#     missing STORAGE_DIR skips with a log line and still succeeds
# 20. Restore discovers the sibling files archive (or takes --files), refuses
#     mixed stamps and corrupt archives before stopping anything, extracts
#     into STORAGE_DIR; pull-backups.sh freshness-checks files archives too
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
# shellcheck disable=SC2064
trap 'chmod -R u+rwx "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
OUT=""
RC=0

ok() { passed=$((passed + 1)); echo "ok   - $1"; }
bad() { failed=$((failed + 1)); echo "FAIL - $1"; [ -z "${2:-}" ] || printf '%s\n' "$2" | sed 's/^/       | /'; }

assert() {
	local desc="$1"; shift
	if "$@" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi
}

check() {
	local desc="$1" want_rc="$2" want_out="${3:-}"
	if [ "$RC" -eq "$want_rc" ] && { [ -z "$want_out" ] || [[ "$OUT" == *"$want_out"* ]]; }; then
		ok "$desc"
	else
		bad "$desc (exit $RC, wanted $want_rc${want_out:+, output containing \"$want_out\"})" "$OUT"
	fi
}

# Set up mock sandbox environment
setup_sandbox() {
	local name="$1"
	local d="$WORK/$name"
	mkdir -p "$d/bin" "$d/repo/scripts" "$d/wael/state"

	# Copy real deploy scripts into the test repo
	cp "$REPO_ROOT/infrastructure/deploy/scripts/lib.sh" "$d/repo/scripts/"
	cp "$REPO_ROOT/infrastructure/deploy/scripts/deploy.sh" "$d/repo/scripts/"
	cp "$REPO_ROOT/infrastructure/deploy/scripts/rollback.sh" "$d/repo/scripts/"
	cp "$REPO_ROOT/infrastructure/deploy/scripts/backup.sh" "$d/repo/scripts/"
	cp "$REPO_ROOT/infrastructure/deploy/scripts/restore.sh" "$d/repo/scripts/"
	chmod +x "$d/repo/scripts/"*.sh

	# Minimal docker-compose.yml and Caddyfile in repo
	printf 'name: wael\nservices:\n  app:\n    image: wael-app:tag\n  admin-console:\n    image: wael-admin:tag\n' > "$d/repo/docker-compose.yml"
	printf 'admin.elmetracademy.app {\n  reverse_proxy admin-console:80\n}\n' > "$d/repo/Caddyfile"

	# Mock preflight.sh: always succeeds
	cat > "$d/repo/scripts/preflight.sh" << 'EOF'
#!/usr/bin/env bash
exit 0
EOF
	chmod +x "$d/repo/scripts/preflight.sh"

	# Mock curl: always succeeds; logs its arguments when CURL_LOG is set.
	cat > "$d/bin/curl" << 'EOF'
#!/usr/bin/env bash
[ -z "${CURL_LOG:-}" ] || echo "CURL_CALL: $*" >> "$CURL_LOG"
exit 0
EOF
	chmod +x "$d/bin/curl"

	# Mock docker: logs arguments and simulates compose commands.
	# mongodump emits fake archive bytes on stdout (which the caller
	# redirects into the archive file); mongorestore consumes stdin.
	cat > "$d/bin/docker" << 'EOF'
#!/usr/bin/env bash
echo "DOCKER_CALL: $*" >> "$DOCKER_LOG"
case "$*" in
*"ps -q"*) [ "${MOCK_NO_MONGO:-0}" = 1 ] || echo "mockcid123" ;;
*mongodump*)
	if [ "${MOCK_MONGODUMP_CORRUPT:-0}" = 1 ]; then
		printf 'NOT-A-GZIP-STREAM'
	else
		printf 'FAKE-ARCHIVE-BYTES' | gzip -c
	fi
	;;
*mongorestore*) cat >/dev/null ;;
*" up "*)
	# MOCK_UP_FAIL_ONCE=<file>: the first 'up' fails, later ones (rollback) succeed.
	if [ -n "${MOCK_UP_FAIL_ONCE:-}" ] && [ ! -f "$MOCK_UP_FAIL_ONCE" ]; then
		touch "$MOCK_UP_FAIL_ONCE"
		exit 1
	fi
	;;
esac
exit "${MOCK_DOCKER_EXIT:-0}"
EOF
	chmod +x "$d/bin/docker"

	# Env production
	printf 'API_DOMAIN=api.test.local\n' > "$d/wael/.env.production"

	echo "$d"
}

# give_backup_creds BOX: the root user and password file backup.sh needs
# (deploy.sh now runs backup.sh before touching containers).
give_backup_creds() {
	printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$1/wael/.env.production"
	mkdir -p "$1/wael/secrets"
	printf '%s' "pw-for-$(basename "$1")" > "$1/wael/secrets/mongo_root_password"
}

# ---------------------------------------------------------------------------
# Test 1: Deploy populates state/last-good/ on success
# ---------------------------------------------------------------------------
BOX1="$(setup_sandbox test1)"
give_backup_creds "$BOX1"
TAG1="1111111111111111111111111111111111111111"
printf 'IMAGE_TAG=%s\n' "$TAG1" > "$BOX1/repo/release.env"
export DOCKER_LOG="$BOX1/docker.log"
RC=0
OUT="$(PATH="$BOX1/bin:$PATH" WAEL_HOME="$BOX1/wael" bash "$BOX1/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "deploy succeeds with mock compose and curl" 0 "deploy succeeded; recorded $TAG1"
assert "last-good/docker-compose.yml was created" test -f "$BOX1/wael/state/last-good/docker-compose.yml"
assert "last-good/Caddyfile was created" test -f "$BOX1/wael/state/last-good/Caddyfile"
assert "last-good/last-good.env has correct IMAGE_TAG" grep -qF "IMAGE_TAG=$TAG1" "$BOX1/wael/state/last-good/last-good.env"
assert "legacy last-good.env has correct IMAGE_TAG" grep -qF "IMAGE_TAG=$TAG1" "$BOX1/wael/state/last-good.env"
assert "deploy wrote a predeploy archive" bash -c "ls \"$BOX1\"/wael/backups/mongo-predeploy-*.archive.gz"
assert "the pre-deploy backup ran before any 'up'" bash -c "grep -nE 'mongodump| up ' \"$BOX1/docker.log\" | head -n 1 | grep -qF mongodump"

# ---------------------------------------------------------------------------
# Test 2: Failed-releases guard blocks deployment of rolled-back SHAs
# ---------------------------------------------------------------------------
BOX2="$(setup_sandbox test2)"
give_backup_creds "$BOX2"
BAD_TAG="2222222222222222222222222222222222222222"
printf 'IMAGE_TAG=%s\n' "$BAD_TAG" > "$BOX2/repo/release.env"
printf '%s\n' "$BAD_TAG" > "$BOX2/wael/state/failed-releases"
export DOCKER_LOG="$BOX2/docker.log"

# Attempt deploy without override -> must fail
RC=0
OUT="$(PATH="$BOX2/bin:$PATH" WAEL_HOME="$BOX2/wael" bash "$BOX2/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "failed-releases guard blocks deploy" 1 "Fix forward with a new commit on main, or set ALLOW_FAILED_RELEASE=1 to force"

# Attempt deploy with ALLOW_FAILED_RELEASE=1 -> proceeds
RC=0
OUT="$(PATH="$BOX2/bin:$PATH" WAEL_HOME="$BOX2/wael" ALLOW_FAILED_RELEASE=1 bash "$BOX2/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "failed-releases guard respects ALLOW_FAILED_RELEASE=1" 0 "ALLOW_FAILED_RELEASE=1 is set; proceeding"

# ---------------------------------------------------------------------------
# Test 3: Rollback uses state/last-good/docker-compose.yml and removes orphans
# ---------------------------------------------------------------------------
BOX3="$(setup_sandbox test3)"
OLD_GOOD="3333333333333333333333333333333333333333"
NEW_BAD="4444444444444444444444444444444444444444"

# Set up last-good state with a compose that lacks admin-console (pre-admin release)
mkdir -p "$BOX3/wael/state/last-good"
printf 'name: wael\nservices:\n  app:\n    image: wael-app:tag\n' > "$BOX3/wael/state/last-good/docker-compose.yml"
printf 'api.elmetracademy.app {\n  reverse_proxy app:80\n}\n' > "$BOX3/wael/state/last-good/Caddyfile"
printf 'IMAGE_TAG=%s\n' "$OLD_GOOD" > "$BOX3/wael/state/last-good/last-good.env"

# Current repo has new bad release with admin-console
printf 'IMAGE_TAG=%s\n' "$NEW_BAD" > "$BOX3/repo/release.env"

export DOCKER_LOG="$BOX3/docker.log"
RC=0
OUT="$(PATH="$BOX3/bin:$PATH" WAEL_HOME="$BOX3/wael" bash "$BOX3/repo/scripts/rollback.sh" "$NEW_BAD" 2>&1)" || RC=$?
check "rollback succeeds" 0 "rollback to $OLD_GOOD is healthy"
assert "rollback recorded bad release in failed-releases" grep -qxF "$NEW_BAD" "$BOX3/wael/state/failed-releases"

# Verify docker was called with last-good compose, -p wael, and --remove-orphans
LAST_DOCKER_CALL="$(tail -n 1 "$BOX3/docker.log")"
assert "rollback used -p wael" grep -qF -- "-p wael" <<< "$LAST_DOCKER_CALL"
assert "rollback used last-good compose file" grep -qF -- "-f $BOX3/wael/state/last-good/docker-compose.yml" <<< "$LAST_DOCKER_CALL"
assert "rollback used --remove-orphans" grep -qF -- "--remove-orphans" <<< "$LAST_DOCKER_CALL"
assert "rollback did NOT use repo compose file" bash -c "! grep -qF -- \"-f $BOX3/repo/docker-compose.yml\" <<< \"$LAST_DOCKER_CALL\""

# ---------------------------------------------------------------------------
# Test 4: Missing state/last-good/ falls back to current compose with loud warning
# ---------------------------------------------------------------------------
BOX4="$(setup_sandbox test4)"
LEGACY_GOOD="5555555555555555555555555555555555555555"
printf 'IMAGE_TAG=%s\n' "$LEGACY_GOOD" > "$BOX4/wael/state/last-good.env"
# Ensure state/last-good directory does NOT exist
rm -rf "$BOX4/wael/state/last-good"

export DOCKER_LOG="$BOX4/docker.log"
RC=0
OUT="$(PATH="$BOX4/bin:$PATH" WAEL_HOME="$BOX4/wael" bash "$BOX4/repo/scripts/rollback.sh" 2>&1)" || RC=$?
check "fallback rollback prints loud warning" 0 "WARNING: last-good compose file"
check "fallback rollback reports healthy" 0 "rollback to $LEGACY_GOOD is healthy"

LAST_DOCKER_CALL4="$(tail -n 1 "$BOX4/docker.log")"
assert "fallback used current compose file" grep -qF -- "-f $BOX4/repo/docker-compose.yml" <<< "$LAST_DOCKER_CALL4"
assert "fallback used -p wael" grep -qF -- "-p wael" <<< "$LAST_DOCKER_CALL4"
assert "fallback used --remove-orphans" grep -qF -- "--remove-orphans" <<< "$LAST_DOCKER_CALL4"

# ---------------------------------------------------------------------------
# Test 5: Rollback does NOT record failed_tag if failed_tag == last-good IMAGE_TAG
# ---------------------------------------------------------------------------
BOX5="$(setup_sandbox test5)"
SAME_TAG="6666666666666666666666666666666666666666"
mkdir -p "$BOX5/wael/state/last-good"
printf 'IMAGE_TAG=%s\n' "$SAME_TAG" > "$BOX5/wael/state/last-good/last-good.env"
cp "$BOX5/repo/docker-compose.yml" "$BOX5/wael/state/last-good/docker-compose.yml"
printf 'IMAGE_TAG=%s\n' "$SAME_TAG" > "$BOX5/repo/release.env"

export DOCKER_LOG="$BOX5/docker.log"
RC=0
OUT="$(PATH="$BOX5/bin:$PATH" WAEL_HOME="$BOX5/wael" bash "$BOX5/repo/scripts/rollback.sh" "$SAME_TAG" 2>&1)" || RC=$?
check "rollback with same tag succeeds" 0 "rollback to $SAME_TAG is healthy"
assert "same tag was NOT recorded in failed-releases" test ! -f "$BOX5/wael/state/failed-releases"

# ---------------------------------------------------------------------------
# Test 6: Last-good snapshot completeness: every relative path in deploy compose
#         must be explicitly captured in state/last-good/
# ---------------------------------------------------------------------------
COMPOSE_FILE="$REPO_ROOT/infrastructure/deploy/docker-compose.yml"
# List of relative paths known and captured by deploy.sh into state/last-good/
KNOWN_SNAPSHOT_PATHS=("./Caddyfile")

# Extract any relative path references (e.g. ./path) from the deploy compose file
mapfile -t FOUND_RELATIVE_PATHS < <(grep -oE '\./[^ :"]+' "$COMPOSE_FILE" | sort -u)

unhandled_rel_paths=0
for p in "${FOUND_RELATIVE_PATHS[@]}"; do
	found=0
	for known in "${KNOWN_SNAPSHOT_PATHS[@]}"; do
		if [ "$p" = "$known" ]; then found=1; break; fi
	done
	if [ "$found" -eq 0 ]; then
		bad "deploy docker-compose.yml contains relative path '$p' not covered by last-good snapshot"
		unhandled_rel_paths=$((unhandled_rel_paths + 1))
	fi
done
[ "$unhandled_rel_paths" -eq 0 ] && ok "last-good snapshot completeness: all relative paths in compose are captured"
# shellcheck disable=SC2016
assert "deploy.sh copies Caddyfile into last-good" grep -qF 'cp "${REPO_DIR}/Caddyfile" "${LAST_GOOD_DIR}/Caddyfile"' "$REPO_ROOT/infrastructure/deploy/scripts/deploy.sh"
# shellcheck disable=SC2016
assert "deploy.sh copies docker-compose.yml into last-good" grep -qF 'cp "${REPO_DIR}/docker-compose.yml" "${LAST_GOOD_DIR}/docker-compose.yml"' "$REPO_ROOT/infrastructure/deploy/scripts/deploy.sh"

# ---------------------------------------------------------------------------
# Test 7: Backup writes a 600 archive via the compose-resolved container
# ---------------------------------------------------------------------------
BOX7="$(setup_sandbox test7)"
TAG7="7777777777777777777777777777777777777777"
printf 'IMAGE_TAG=%s\n' "$TAG7" > "$BOX7/repo/release.env"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX7/wael/.env.production"
mkdir -p "$BOX7/wael/secrets"
printf '%s' "pw-for-test7-never-logged" > "$BOX7/wael/secrets/mongo_root_password"
export DOCKER_LOG="$BOX7/docker.log"
RC=0
OUT="$(PATH="$BOX7/bin:$PATH" WAEL_HOME="$BOX7/wael" bash "$BOX7/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup succeeds" 0 "backup written and verified:"
ARCHIVE7="$(echo "$BOX7"/wael/backups/mongo-*.archive.gz)"
assert "backup archive exists and is non-empty" test -s "$ARCHIVE7"
assert "backup archive mode is 600" test "$(stat -c %a "$ARCHIVE7")" = 600
assert "backup resolved the container via compose ps" grep -qF "ps -q mongo" "$BOX7/docker.log"
assert "backup ran mongodump --archive --gzip" grep -qF "mongodump" "$BOX7/docker.log"
if grep -qF "pw-for-test7-never-logged" <<<"$OUT"; then
	bad "backup printed the root password"
else
	ok "backup never printed the root password"
fi

# ---------------------------------------------------------------------------
# Test 8: Backup honors MONGO_CONTAINER and BACKUP_KEEP_DAYS pruning
# ---------------------------------------------------------------------------
BOX8="$(setup_sandbox test8)"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX8/wael/.env.production"
mkdir -p "$BOX8/wael/secrets" "$BOX8/wael/backups"
printf '%s' "pw-for-test8" > "$BOX8/wael/secrets/mongo_root_password"
touch -d '10 days ago' "$BOX8/wael/backups/mongo-2000-01-01T000000Z.archive.gz"
export DOCKER_LOG="$BOX8/docker.log"
RC=0
OUT="$(PATH="$BOX8/bin:$PATH" WAEL_HOME="$BOX8/wael" MONGO_CONTAINER=mockcid BACKUP_KEEP_DAYS=7 bash "$BOX8/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup with MONGO_CONTAINER succeeds" 0 "pruned 1 archive(s)"
assert "10-day-old archive was pruned" test ! -f "$BOX8/wael/backups/mongo-2000-01-01T000000Z.archive.gz"
REMAINING8="$(echo "$BOX8"/wael/backups/mongo-*.archive.gz)"
assert "fresh archive was kept" test -s "$REMAINING8"
RC=0
OUT="$(PATH="$BOX8/bin:$PATH" WAEL_HOME="$BOX8/wael" BACKUP_KEEP_DAYS=0 bash "$BOX8/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup rejects BACKUP_KEEP_DAYS=0" 1 "must be a positive integer"
RC=0
OUT="$(PATH="$BOX8/bin:$PATH" WAEL_HOME="$BOX8/wael" BACKUP_KEEP_DAYS=soon bash "$BOX8/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup rejects BACKUP_KEEP_DAYS=soon" 1 "must be a positive integer"

# ---------------------------------------------------------------------------
# Test 9: Backup fails without the root password file
# ---------------------------------------------------------------------------
BOX9="$(setup_sandbox test9)"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX9/wael/.env.production"
export DOCKER_LOG="$BOX9/docker.log"
RC=0
OUT="$(PATH="$BOX9/bin:$PATH" WAEL_HOME="$BOX9/wael" bash "$BOX9/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup fails without the password file" 1 "password file is missing or empty"

# ---------------------------------------------------------------------------
# Test 10: Restore refuses to run without --yes and touches nothing
# ---------------------------------------------------------------------------
BOX10="$(setup_sandbox test10)"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX10/wael/.env.production"
mkdir -p "$BOX10/wael/secrets"
printf '%s' "pw-for-test10" > "$BOX10/wael/secrets/mongo_root_password"
printf 'FAKE-ARCHIVE' > "$BOX10/dummy.archive.gz"
export DOCKER_LOG="$BOX10/docker.log"
RC=0
OUT="$(PATH="$BOX10/bin:$PATH" WAEL_HOME="$BOX10/wael" bash "$BOX10/repo/scripts/restore.sh" "$BOX10/dummy.archive.gz" 2>&1)" || RC=$?
check "restore refuses without --yes" 1 "without --yes"
assert "restore without --yes called no docker command" test ! -f "$BOX10/docker.log"

# ---------------------------------------------------------------------------
# Test 11: Restore with --yes stops apps, restores, restarts, health-gates
# ---------------------------------------------------------------------------
BOX11="$(setup_sandbox test11)"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX11/wael/.env.production"
mkdir -p "$BOX11/wael/secrets"
printf '%s' "pw-for-test11" > "$BOX11/wael/secrets/mongo_root_password"
printf 'FAKE' | gzip -c > "$BOX11/dummy.archive.gz"
export DOCKER_LOG="$BOX11/docker.log"
RC=0
OUT="$(PATH="$BOX11/bin:$PATH" WAEL_HOME="$BOX11/wael" MONGO_CONTAINER=mockcid bash "$BOX11/repo/scripts/restore.sh" "$BOX11/dummy.archive.gz" --yes 2>&1)" || RC=$?
check "restore with --yes succeeds" 0 "restore of $BOX11/dummy.archive.gz complete and healthy"
assert "restore stopped the five app services" grep -qF "stop api-gateway auth-service notification-service academy-service admin-console" "$BOX11/docker.log"
assert "restore ran mongorestore --archive --gzip --drop" grep -qF "mongorestore" "$BOX11/docker.log"
assert "restore restarted with --wait" grep -qF -- "--wait" "$BOX11/docker.log"

# ---------------------------------------------------------------------------
# Test 12: Restore --skip-restart restores without touching services
# ---------------------------------------------------------------------------
BOX12="$(setup_sandbox test12)"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX12/wael/.env.production"
mkdir -p "$BOX12/wael/secrets"
printf '%s' "pw-for-test12" > "$BOX12/wael/secrets/mongo_root_password"
printf 'FAKE' | gzip -c > "$BOX12/dummy.archive.gz"
export DOCKER_LOG="$BOX12/docker.log"
RC=0
OUT="$(PATH="$BOX12/bin:$PATH" WAEL_HOME="$BOX12/wael" MONGO_CONTAINER=mockcid bash "$BOX12/repo/scripts/restore.sh" "$BOX12/dummy.archive.gz" --yes --skip-restart 2>&1)" || RC=$?
check "restore --skip-restart succeeds" 0 "restore of $BOX12/dummy.archive.gz complete and healthy"
assert "skip-restart still ran mongorestore" grep -qF "mongorestore" "$BOX12/docker.log"
assert "skip-restart stopped nothing" bash -c "! grep -qF ' stop ' \"$BOX12/docker.log\""
assert "skip-restart started nothing" bash -c "! grep -qF ' up ' \"$BOX12/docker.log\""

# ---------------------------------------------------------------------------
# Test 13: pull-backups.sh validates env, pulls, keeps newest N at 600
# ---------------------------------------------------------------------------
BOX13="$WORK/test13"
mkdir -p "$BOX13/bin" "$BOX13/fixture" "$BOX13/local"
cat >"$BOX13/bin/ssh" << 'EOF'
#!/usr/bin/env bash
case "$*" in
*"sudo -n true"*) exit "${MOCK_SSH_EXIT:-0}" ;;
*) exit 0 ;;
esac
EOF
chmod +x "$BOX13/bin/ssh"
cat >"$BOX13/bin/rsync" << 'EOF'
#!/usr/bin/env bash
# Faithful enough: copy files only, like 'rsync -a src/ dst/' (which, unlike
# 'cp -a src/. dst/', never retargets the destination dir mode).
dest="${@: -1}"
mkdir -p "$dest"
for f in "$FIXTURE_SRC"/*; do cp -a "$f" "$dest/"; done
EOF
chmod +x "$BOX13/bin/rsync"
touch -d '40 days ago' "$BOX13/fixture/mongo-a.archive.gz"
touch -d '20 days ago' "$BOX13/fixture/mongo-b.archive.gz"
touch -d '10 days ago' "$BOX13/fixture/mongo-c.archive.gz"
printf 'FAKE' | gzip -c > "$BOX13/fixture/mongo-d.archive.gz"
touch -d '1 day ago' "$BOX13/fixture/mongo-d.archive.gz"
RC=0
OUT="$(PATH="$BOX13/bin:$PATH" FIXTURE_SRC="$BOX13/fixture" WAEL_HOST=server.test BACKUP_KEEP_LOCAL=2 LOCAL_BACKUP_DIR="$BOX13/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups keeps newest 2" 0 "2 archive(s)"
assert "newest archive kept" test -f "$BOX13/local/mongo-d.archive.gz"
assert "second newest kept" test -f "$BOX13/local/mongo-c.archive.gz"
assert "older archives pruned" test ! -f "$BOX13/local/mongo-b.archive.gz"
assert "pulled archives are mode 600" test "$(stat -c %a "$BOX13/local/mongo-d.archive.gz")" = 600
assert "local dir is mode 700" test "$(stat -c %a "$BOX13/local")" = 700
RC=0
OUT="$(PATH="$BOX13/bin:$PATH" env -u WAEL_HOST bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups requires WAEL_HOST" 1 "WAEL_HOST"
RC=0
OUT="$(PATH="$BOX13/bin:$PATH" MOCK_SSH_EXIT=1 WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX13/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups explains the sudoers rule" 1 "NOPASSWD"

# ---------------------------------------------------------------------------
# Test 14: preflight redis memory checks (real preflight.sh, mock docker)
# ---------------------------------------------------------------------------
# The rendered compose carries the redis command as a YAML list, exactly as
# `docker compose config` prints it, so the fixtures use that shape.
render_redis() { # render_redis [--maxmemory VALUE] [--policy VALUE] -> fixture text
	local cap="" policy=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--maxmemory) cap="$2" ;;
		--policy) policy="$2" ;;
		esac
		shift 2
	done
	printf 'services:\n  redis:\n    command:\n      - redis-server\n      - /run/secrets/redis_conf\n'
	[ -z "$cap" ] || printf '      - --maxmemory\n      - %s\n' "$cap"
	[ -z "$policy" ] || printf '      - --maxmemory-policy\n      - %s\n' "$policy"
	printf '    image: redis:7-alpine\n'
}

# run_preflight NAME FIXTURE_TEXT: builds a sandbox that passes every check
# before 5b, then runs the real preflight.sh with the fixture as the output of
# `docker compose config`. Sets OUT and RC.
run_preflight() {
	local name="$1" fixture="$2" box c
	box="$(setup_sandbox "$name")"
	cp "$REPO_ROOT/infrastructure/deploy/scripts/preflight.sh" "$box/repo/scripts/preflight.sh"
	printf 'IMAGE_TAG=%s\n' "1414141414141414141414141414141414141414" > "$box/repo/release.env"
	# Built at runtime: a literal long token trips the gitleaks generic rule.
	local secret
	secret="$(printf 's%.0s' {1..40})"
	{
		for v in API_DOMAIN ADMIN_DOMAIN ACME_EMAIL ALLOWED_ORIGIN AUTH_MONGO_URI NOTIFICATION_MONGO_URI \
			ACADEMY_MONGO_URI REDIS_URI RESEND_API_KEY RESEND_FROM_EMAIL SUPPORT_WHATSAPP MONGO_ROOT_USERNAME; do
			printf '%s=value-for-%s\n' "$v" "$v"
		done
		for v in JWT_SECRET GATEWAY_SECRET INTERNAL_SERVICE_TOKEN BLOCKLIST_HMAC_KEY; do
			printf '%s=%s\n' "$v" "$secret"
		done
		# Phase 5 files: a 64-hex key built at runtime, this user's ids and a
		# mode-700 storage dir. PF_EXTRA_ENV lines come last and win (read_var
		# takes the last match).
		printf 'DOCUMENT_ENCRYPTION_KEY=%s\n' "$(printf 'ab%.0s' {1..32})"
		printf 'WAEL_UID=%s\nWAEL_GID=%s\n' "$(id -u)" "$(id -g)"
		printf 'STORAGE_DIR=%s\n' "$box/wael/storage"
		printf 'FEATURES_FILES=false\n'
		[ -z "${PF_EXTRA_ENV:-}" ] || printf '%s\n' "$PF_EXTRA_ENV"
	} > "$box/wael/.env.production"
	mkdir -p "$box/wael/storage"
	chmod "${PF_STORAGE_MODE:-700}" "$box/wael/storage"
	chmod 600 "$box/wael/.env.production"
	mkdir -p "$box/wael/secrets" "$box/wael/certs"
	chmod 700 "$box/wael/secrets" "$box/wael/certs"
	printf 'pw' > "$box/wael/secrets/mongo_root_password"
	printf 'requirepass pw-for-preflight-test\n' > "$box/wael/secrets/redis.conf"
	openssl req -x509 -newkey rsa:2048 -nodes -days 60 -subj /CN=test \
		-keyout "$box/key.pem" -out "$box/crt.pem" >/dev/null 2>&1
	for c in ca api-gateway auth-service notification-service academy-service admin-console; do
		cp "$box/crt.pem" "$box/wael/certs/$c.crt"
	done
	for c in api-gateway auth-service notification-service academy-service admin-console; do
		cp "$box/key.pem" "$box/wael/certs/$c.key"
	done
	printf '%s' "$fixture" > "$box/rendered.yml"
	# Mock docker: `compose config --quiet` renders fine, plain `compose
	# config` prints the fixture, everything else (pull, run --check-env) is ok.
	cat > "$box/bin/docker" << 'EOF'
#!/usr/bin/env bash
case "$*" in
*"config --quiet"*) exit 0 ;;
*"config --images"*) exit 0 ;;
*" config"*) cat "$RENDERED_FIXTURE"; exit 0 ;;
esac
exit 0
EOF
	chmod +x "$box/bin/docker"
	RC=0
	OUT="$(PATH="$box/bin:$PATH" WAEL_HOME="$box/wael" RENDERED_FIXTURE="$box/rendered.yml" \
		bash "$box/repo/scripts/preflight.sh" 2>&1)" || RC=$?
}

run_preflight pf-ok "$(render_redis --maxmemory 96mb --policy noeviction)"
check "preflight passes with maxmemory cap and noeviction" 0 "pre-flight passed"
check "preflight reports the cap as ok" 0 "ok: redis maxmemory is capped"

run_preflight pf-nocap "$(render_redis --policy noeviction)"
check "preflight fails with policy only (no maxmemory cap)" 1 "FAIL: redis maxmemory is capped"
assert "policy-only run still sees noeviction as ok" grep -qF "ok: redis maxmemory-policy is noeviction" <<<"$OUT"

run_preflight pf-nopolicy "$(render_redis --maxmemory 96mb)"
check "preflight fails with cap only (no noeviction policy)" 1 "FAIL: redis maxmemory-policy is noeviction"
assert "cap-only run sees the cap as ok" grep -qF "ok: redis maxmemory is capped" <<<"$OUT"

run_preflight pf-zero "$(render_redis --maxmemory 0 --policy noeviction)"
check "preflight fails when maxmemory is 0 (unlimited)" 1 "FAIL: redis maxmemory is capped"

run_preflight pf-evict "$(render_redis --maxmemory 96mb --policy allkeys-lru)"
check "preflight fails when an eviction policy is set" 1 "must not use an eviction policy"

# ---------------------------------------------------------------------------
# Test 14b: preflight file storage checks (Phase 5) and compose wiring
# ---------------------------------------------------------------------------
GOOD_REDIS="$(render_redis --maxmemory 96mb --policy noeviction)"
run_preflight pf-files-ok "$GOOD_REDIS"
check "preflight passes with a valid key, ids and storage dir" 0 "pre-flight passed"
for line in "ok: DOCUMENT_ENCRYPTION_KEY is 64 hex characters" "ok: STORAGE_DIR mode is 700" \
	"ok: STORAGE_DIR is owned by WAEL_UID" "ok: WAEL_UID is this user's uid"; do
	assert "preflight reports: $line" grep -qF "$line" <<<"$OUT"
done

PF_EXTRA_ENV="DOCUMENT_ENCRYPTION_KEY=$(printf 'ab%.0s' {1..31})" run_preflight pf-key-short "$GOOD_REDIS"
check "preflight fails on a 62-character key" 1 "FAIL: DOCUMENT_ENCRYPTION_KEY is 64 hex characters"
PF_EXTRA_ENV="DOCUMENT_ENCRYPTION_KEY=$(printf 'zz%.0s' {1..32})" run_preflight pf-key-nonhex "$GOOD_REDIS"
check "preflight fails on a non-hex key" 1 "FAIL: DOCUMENT_ENCRYPTION_KEY is 64 hex characters"
PF_EXTRA_ENV="DOCUMENT_ENCRYPTION_KEY=PASTE_64_HEX" run_preflight pf-key-placeholder "$GOOD_REDIS"
check "preflight fails on the example placeholder key" 1 "DOCUMENT_ENCRYPTION_KEY still holds a placeholder"
PF_EXTRA_ENV="DOCUMENT_ENCRYPTION_KEY=" run_preflight pf-key-empty "$GOOD_REDIS"
check "preflight fails on an empty key" 1 "FAIL: DOCUMENT_ENCRYPTION_KEY is set"
PF_EXTRA_ENV="WAEL_UID=$(( $(id -u) + 1 ))" run_preflight pf-uid "$GOOD_REDIS"
check "preflight fails when WAEL_UID is not the running user" 1 "FAIL: WAEL_UID is this user's uid"
PF_EXTRA_ENV="WAEL_GID=PASTE_GID" run_preflight pf-gid "$GOOD_REDIS"
check "preflight fails on a placeholder WAEL_GID" 1 "FAIL: WAEL_GID is this user's gid"
PF_EXTRA_ENV="STORAGE_DIR=$WORK/no-such-storage" run_preflight pf-nodir "$GOOD_REDIS"
check "preflight fails when STORAGE_DIR does not exist" 1 "FAIL: STORAGE_DIR exists and is a directory"
PF_EXTRA_ENV="STORAGE_DIR=wael/storage" run_preflight pf-reldir "$GOOD_REDIS"
check "preflight fails on a relative STORAGE_DIR" 1 "FAIL: STORAGE_DIR is an absolute path"
PF_STORAGE_MODE=755 run_preflight pf-mode "$GOOD_REDIS"
check "preflight fails when STORAGE_DIR is not mode 700" 1 "FAIL: STORAGE_DIR mode is 700"
PF_EXTRA_ENV="FEATURES_FILES=1" run_preflight pf-features "$GOOD_REDIS"
check "preflight fails on a loose FEATURES_FILES" 1 "FAIL: FEATURES_FILES is empty, true or false"

DEPLOY_COMPOSE="$REPO_ROOT/infrastructure/deploy/docker-compose.yml"
DEV_COMPOSE="$REPO_ROOT/infrastructure/docker-compose.yml"
# shellcheck disable=SC2016 # literal ${...}: the text the compose file must contain
for needle in 'user: "${WAEL_UID:?WAEL_UID is required}:${WAEL_GID:?WAEL_GID is required}"' \
	'STORAGE_DIR: /data/files' \
	'DOCUMENT_ENCRYPTION_KEY: ${DOCUMENT_ENCRYPTION_KEY:?DOCUMENT_ENCRYPTION_KEY is required}' \
	'- ${STORAGE_DIR:?STORAGE_DIR is required}:/data/files' \
	'FEATURES_FILES: ${FEATURES_FILES:-false}' \
	'MAX_CONCURRENT_DOWNLOADS: ${MAX_CONCURRENT_DOWNLOADS:-}' \
	'DOWNLOAD_STALL_TIMEOUT: ${DOWNLOAD_STALL_TIMEOUT:-}'; do
	assert "deploy compose has: $needle" grep -qF -- "$needle" "$DEPLOY_COMPOSE"
done
# shellcheck disable=SC2016 # literal ${...}: the text the compose file must contain
assert "deploy compose passes MAX_PDF_BYTES to academy and console" \
	test "$(grep -cF 'MAX_PDF_BYTES: ${MAX_PDF_BYTES:-}' "$DEPLOY_COMPOSE")" -eq 2
assert "dev compose keeps files in the academy_files volume" grep -qF -- '- academy_files:/data/files' "$DEV_COMPOSE"
# shellcheck disable=SC2016 # literal ${...}: the text the compose file must contain
assert "dev compose passes MAX_PDF_BYTES to academy and console" \
	test "$(grep -cF 'MAX_PDF_BYTES: ${MAX_PDF_BYTES:-}' "$DEV_COMPOSE")" -eq 2
for v in STORAGE_DIR DOCUMENT_ENCRYPTION_KEY WAEL_UID WAEL_GID FEATURES_FILES; do
	assert "env.production.example documents $v" grep -qE "^$v=" "$REPO_ROOT/infrastructure/deploy/env.production.example"
done
# shellcheck disable=SC2016 # $1 expands in the inner bash
assert "Caddyfile sets no request body limit on the admin host (the console caps uploads)" \
	bash -c '! grep -q "max_size\|request_body" "$1"' _ "$REPO_ROOT/infrastructure/deploy/Caddyfile"

# ---------------------------------------------------------------------------
# Test 15: Caddyfile HSTS header (owner decision 2026-10-05: includeSubDomains
# on both site blocks, no preload)
# ---------------------------------------------------------------------------
HSTS_FILE="$REPO_ROOT/infrastructure/deploy/Caddyfile"
HSTS_WANT='Strict-Transport-Security "max-age=31536000; includeSubDomains"'
export HSTS_FILE HSTS_WANT
assert "Caddyfile has the exact HSTS header twice (API and admin blocks)" \
	bash -c '[ "$(grep -cF -- "$HSTS_WANT" "$HSTS_FILE")" -eq 2 ]'
assert "each site block carries the HSTS header exactly once" \
	bash -c 'awk -v want="$HSTS_WANT" "{ if (index(\$0, \"{\$API_DOMAIN} {\")) b=\"api\"; if (index(\$0, \"{\$ADMIN_DOMAIN} {\")) b=\"admin\"; if (index(\$0, want)) n[b]++ } END { exit !(n[\"api\"]==1 && n[\"admin\"]==1) }" "$HSTS_FILE"'
assert "no HSTS max-age without includeSubDomains remains" \
	bash -c '! grep -E "Strict-Transport-Security \"max-age=[0-9]+\"" "$HSTS_FILE"'
assert "HSTS has no preload directive" \
	bash -c '! grep -qi "preload" "$HSTS_FILE"'

# ---------------------------------------------------------------------------
# Test 16: pre-deploy backup (full review 2026-10-06, infra H1)
# ---------------------------------------------------------------------------
TAG16="1616161616161616161616161616161616161616"

# 16a: a failed pre-deploy backup aborts before any container is touched.
BOX16A="$(setup_sandbox test16a)"
printf 'IMAGE_TAG=%s\n' "$TAG16" > "$BOX16A/repo/release.env"
printf 'MONGO_ROOT_USERNAME=wael_root\n' >> "$BOX16A/wael/.env.production" # no password file
export DOCKER_LOG="$BOX16A/docker.log"
RC=0
OUT="$(PATH="$BOX16A/bin:$PATH" WAEL_HOME="$BOX16A/wael" bash "$BOX16A/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "deploy aborts when the pre-deploy backup fails" 1 "pre-deploy backup failed; nothing was deployed"
assert "aborted deploy ran no 'up'" bash -c "! grep -qF ' up ' \"$BOX16A/docker.log\""
assert "aborted deploy recorded no last-good release" test ! -f "$BOX16A/wael/state/last-good/last-good.env"

# 16b: a corrupt dump also aborts the deploy.
BOX16B="$(setup_sandbox test16b)"
give_backup_creds "$BOX16B"
printf 'IMAGE_TAG=%s\n' "$TAG16" > "$BOX16B/repo/release.env"
export DOCKER_LOG="$BOX16B/docker.log"
RC=0
OUT="$(PATH="$BOX16B/bin:$PATH" WAEL_HOME="$BOX16B/wael" MOCK_MONGODUMP_CORRUPT=1 bash "$BOX16B/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "deploy aborts when the pre-deploy archive is corrupt" 1 "gzip integrity check"
assert "corrupt-dump deploy ran no 'up'" bash -c "! grep -qF ' up ' \"$BOX16B/docker.log\""

# 16c: first deploy (no mongo running yet) skips the backup and deploys.
BOX16C="$(setup_sandbox test16c)"
printf 'IMAGE_TAG=%s\n' "$TAG16" > "$BOX16C/repo/release.env"
export DOCKER_LOG="$BOX16C/docker.log"
RC=0
OUT="$(PATH="$BOX16C/bin:$PATH" WAEL_HOME="$BOX16C/wael" MOCK_NO_MONGO=1 bash "$BOX16C/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "first deploy without mongo skips the backup and succeeds" 0 "skipping the pre-deploy backup"
assert "first deploy ran no mongodump" bash -c "! grep -qF mongodump \"$BOX16C/docker.log\""

# 16d: SKIP_PREDEPLOY_BACKUP=1 is honoured and logged.
BOX16D="$(setup_sandbox test16d)"
printf 'IMAGE_TAG=%s\n' "$TAG16" > "$BOX16D/repo/release.env"
export DOCKER_LOG="$BOX16D/docker.log"
RC=0
OUT="$(PATH="$BOX16D/bin:$PATH" WAEL_HOME="$BOX16D/wael" SKIP_PREDEPLOY_BACKUP=1 bash "$BOX16D/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "SKIP_PREDEPLOY_BACKUP=1 deploys with a warning" 0 "deploying without a pre-deploy backup"
assert "skipped backup ran no mongodump" bash -c "! grep -qF mongodump \"$BOX16D/docker.log\""

# 16e: a failed deploy rolls back and prints the restore command.
BOX16E="$(setup_sandbox test16e)"
give_backup_creds "$BOX16E"
GOOD16="1515151515151515151515151515151515151515"
printf 'IMAGE_TAG=%s\n' "$TAG16" > "$BOX16E/repo/release.env"
mkdir -p "$BOX16E/wael/state/last-good"
printf 'IMAGE_TAG=%s\n' "$GOOD16" > "$BOX16E/wael/state/last-good/last-good.env"
cp "$BOX16E/repo/docker-compose.yml" "$BOX16E/wael/state/last-good/docker-compose.yml"
export DOCKER_LOG="$BOX16E/docker.log"
RC=0
OUT="$(PATH="$BOX16E/bin:$PATH" WAEL_HOME="$BOX16E/wael" MOCK_UP_FAIL_ONCE="$BOX16E/up-failed" bash "$BOX16E/repo/scripts/deploy.sh" 2>&1)" || RC=$?
check "failed deploy rolls back and fails" 1 "rollback to $GOOD16 is healthy"
check "rollback prints the restore command for the pre-deploy backup" 1 "restore.sh $BOX16E/wael/backups/mongo-predeploy-"
check "rollback says the data was not rolled back" 1 "DATA NOT ROLLED BACK"

# ---------------------------------------------------------------------------
# Test 17: backup verification, state, label, ping and lock (infra H2)
# ---------------------------------------------------------------------------
BOX17="$(setup_sandbox test17)"
give_backup_creds "$BOX17"
export DOCKER_LOG="$BOX17/docker.log" CURL_LOG="$BOX17/curl.log"
PING17="https://hc.test/ping/abc"

RC=0
OUT="$(PATH="$BOX17/bin:$PATH" WAEL_HOME="$BOX17/wael" BACKUP_PING_URL="$PING17" bash "$BOX17/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup verifies the archive" 0 "backup written and verified"
LAST17="$(sed -n 's/^BACKUP_FILE=//p' "$BOX17/wael/state/last-backup.env" 2>/dev/null)"
assert "last-backup.env names an existing archive" test -s "$LAST17"
assert "the recorded archive passes gzip -t" gzip -t "$LAST17"
assert "success pinged BACKUP_PING_URL" grep -qF "$PING17" "$BOX17/curl.log"
assert "success did not ping /fail" bash -c "! grep -qF '$PING17/fail' \"$BOX17/curl.log\""

rm -f "$BOX17/curl.log"
COUNT17="$(find "$BOX17/wael/backups" -name 'mongo-*.archive.gz' | wc -l)"
RC=0
OUT="$(PATH="$BOX17/bin:$PATH" WAEL_HOME="$BOX17/wael" BACKUP_PING_URL="$PING17" MOCK_MONGODUMP_CORRUPT=1 bash "$BOX17/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "a corrupt dump fails the backup" 1 "gzip integrity check; nothing was kept"
assert "a corrupt dump left no archive behind" test "$(find "$BOX17/wael/backups" -name 'mongo-*.archive.gz' | wc -l)" -eq "$COUNT17"
assert "a corrupt dump left no temp file behind" bash -c "! ls \"$BOX17\"/wael/backups/*.tmp.* 2>/dev/null"
assert "failure pinged BACKUP_PING_URL/fail" grep -qF "$PING17/fail" "$BOX17/curl.log"
assert "last-backup.env still names the good archive" grep -qF "BACKUP_FILE=$LAST17" "$BOX17/wael/state/last-backup.env"

RC=0
OUT="$(PATH="$BOX17/bin:$PATH" WAEL_HOME="$BOX17/wael" BACKUP_LABEL=predeploy bash "$BOX17/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "a labelled backup succeeds" 0 "mongo-predeploy-"
RC=0
OUT="$(PATH="$BOX17/bin:$PATH" WAEL_HOME="$BOX17/wael" BACKUP_LABEL='../x' bash "$BOX17/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "a path-like BACKUP_LABEL is refused" 1 "BACKUP_LABEL must be"

mkdir -p "$BOX17/wael/state"
flock "$BOX17/wael/state/backup.lock" sleep 5 &
LOCKPID=$!
sleep 1
RC=0
OUT="$(PATH="$BOX17/bin:$PATH" WAEL_HOME="$BOX17/wael" BACKUP_LOCK_WAIT=1 bash "$BOX17/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "a second backup does not overlap a running one" 1 "another backup is still running"
wait "$LOCKPID" 2>/dev/null || true
unset CURL_LOG

# ---------------------------------------------------------------------------
# Test 18: pull-backups.sh freshness and integrity (infra H2)
# ---------------------------------------------------------------------------
BOX18="$WORK/test18"
mkdir -p "$BOX18/fixture" "$BOX18/local"
cp -a "$BOX13/bin" "$BOX18/bin"
printf 'FAKE' | gzip -c > "$BOX18/fixture/mongo-old.archive.gz"
touch -d '3 days ago' "$BOX18/fixture/mongo-old.archive.gz"
RC=0
OUT="$(PATH="$BOX18/bin:$PATH" FIXTURE_SRC="$BOX18/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX18/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups fails when the newest archive is stale" 1 "STALE BACKUPS"
printf 'NOT-GZIP' > "$BOX18/fixture/mongo-new.archive.gz"
RC=0
OUT="$(PATH="$BOX18/bin:$PATH" FIXTURE_SRC="$BOX18/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX18/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups fails when the newest archive is corrupt" 1 "CORRUPT BACKUP"
RC=0
OUT="$(PATH="$BOX18/bin:$PATH" FIXTURE_SRC="$BOX18/fixture" WAEL_HOST=server.test BACKUP_MAX_AGE_HOURS=0 LOCAL_BACKUP_DIR="$BOX18/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups rejects BACKUP_MAX_AGE_HOURS=0" 1 "must be a positive integer"

# ---------------------------------------------------------------------------
# Test 19: backup.sh archives STORAGE_DIR as one set with mongo (I1)
# ---------------------------------------------------------------------------
BOX19="$(setup_sandbox test19)"
give_backup_creds "$BOX19"
mkdir -p "$BOX19/storage/uploads/nested" "$BOX19/wael/backups"
printf 'object-bytes-1' > "$BOX19/storage/obj1"
printf 'object-bytes-2' > "$BOX19/storage/uploads/nested/obj2"
touch -d '10 days ago' "$BOX19/wael/backups/files-2000-01-01T000000Z.archive.gz"
export DOCKER_LOG="$BOX19/docker.log"
RC=0
OUT="$(PATH="$BOX19/bin:$PATH" WAEL_HOME="$BOX19/wael" STORAGE_DIR="$BOX19/storage" bash "$BOX19/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup with STORAGE_DIR writes the files archive" 0 "files archive written and verified"
FILES19="$(echo "$BOX19"/wael/backups/files-2*.archive.gz)"
assert "files archive exists and is mode 600" test "$(stat -c %a "$FILES19")" = 600
assert "files archive passes gzip -t" gzip -t "$FILES19"
assert "files archive holds the stored objects" bash -c "tar -tzf \"$FILES19\" | grep -q obj1"
assert "last-backup.env names the files archive" grep -qF "BACKUP_FILES_FILE=$FILES19" "$BOX19/wael/state/last-backup.env"
MONGO19="$(echo "$BOX19"/wael/backups/mongo-2*.archive.gz)"
assert "mongo and files archives share one stamp" test "$(basename "$MONGO19" | sed 's/^mongo-//')" = "$(basename "$FILES19" | sed 's/^files-//')"
assert "old files archive was pruned" test ! -f "$BOX19/wael/backups/files-2000-01-01T000000Z.archive.gz"
assert "just-written files archive was kept" test -s "$FILES19"
RC=0
OUT="$(PATH="$BOX19/bin:$PATH" WAEL_HOME="$BOX19/wael" bash "$BOX19/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup without STORAGE_DIR still succeeds" 0 "skipping the files archive"
assert "last-backup.env records an empty files file" grep -qxF "BACKUP_FILES_FILE=" "$BOX19/wael/state/last-backup.env"
RC=0
OUT="$(PATH="$BOX19/bin:$PATH" WAEL_HOME="$BOX19/wael" STORAGE_DIR="$BOX19/does-not-exist" bash "$BOX19/repo/scripts/backup.sh" 2>&1)" || RC=$?
check "backup with a missing STORAGE_DIR skips cleanly" 0 "does not exist"

# ---------------------------------------------------------------------------
# Test 20: restore.sh handles the files archive of a set; pull-backups.sh
# checks files archives too (I1)
# ---------------------------------------------------------------------------
BOX20="$(setup_sandbox test20)"
give_backup_creds "$BOX20"
mkdir -p "$BOX20/wael/backups"
STAMP20="20200101T000000Z"
printf 'FAKE' | gzip -c > "$BOX20/wael/backups/mongo-manual-$STAMP20.archive.gz"
mkdir -p "$BOX20/restore-src/objects"
printf 'restored-bytes' > "$BOX20/restore-src/objects/o1"
tar -czf "$BOX20/wael/backups/files-manual-$STAMP20.archive.gz" -C "$BOX20/restore-src" .
export DOCKER_LOG="$BOX20/docker.log"
RC=0
OUT="$(PATH="$BOX20/bin:$PATH" WAEL_HOME="$BOX20/wael" MONGO_CONTAINER=mockcid STORAGE_DIR="$BOX20/restored" bash "$BOX20/repo/scripts/restore.sh" "$BOX20/wael/backups/mongo-manual-$STAMP20.archive.gz" --yes --skip-restart 2>&1)" || RC=$?
check "restore discovers the sibling files archive" 0 "found the matching files archive"
assert "files were extracted into STORAGE_DIR" grep -qF "restored-bytes" "$BOX20/restored/objects/o1"
check "restore reports both archives of the set" 0 "and $BOX20/wael/backups/files-manual-$STAMP20.archive.gz complete and healthy"

printf 'NOT-GZIP' > "$BOX20/wael/backups/files-manual-$STAMP20.archive.gz"
RC=0
OUT="$(PATH="$BOX20/bin:$PATH" WAEL_HOME="$BOX20/wael" MONGO_CONTAINER=mockcid STORAGE_DIR="$BOX20/restored" bash "$BOX20/repo/scripts/restore.sh" "$BOX20/wael/backups/mongo-manual-$STAMP20.archive.gz" --yes 2>&1)" || RC=$?
check "restore refuses a corrupt sibling before stopping anything" 1 "fails the gzip integrity check"
assert "corrupt sibling stopped no service" bash -c "! grep -qF ' stop ' \"$BOX20/docker.log\""

printf 'FAKE' | gzip -c > "$BOX20/mongo-setA.archive.gz"
printf 'FAKE' | gzip -c > "$BOX20/files-setB.archive.gz"
RC=0
OUT="$(PATH="$BOX20/bin:$PATH" WAEL_HOME="$BOX20/wael" MONGO_CONTAINER=mockcid STORAGE_DIR="$BOX20/restored" bash "$BOX20/repo/scripts/restore.sh" "$BOX20/mongo-setA.archive.gz" --files "$BOX20/files-setB.archive.gz" --yes --skip-restart 2>&1)" || RC=$?
check "restore refuses an explicit --files from another set" 1 "refusing to mix backup sets"
assert "mixed sets stopped no service" bash -c "! grep -qF ' stop ' \"$BOX20/docker.log\""

BOX20P="$WORK/test20pull"
mkdir -p "$BOX20P/fixture" "$BOX20P/local"
cp -a "$BOX13/bin" "$BOX20P/bin"
printf 'FAKE' | gzip -c > "$BOX20P/fixture/mongo-new.archive.gz"
printf 'FAKE' | gzip -c > "$BOX20P/fixture/files-new.archive.gz"
RC=0
OUT="$(PATH="$BOX20P/bin:$PATH" FIXTURE_SRC="$BOX20P/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX20P/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups checks the files archive too" 0 "newest files archive is"
assert "files archive was pulled at 600" test "$(stat -c %a "$BOX20P/local/files-new.archive.gz")" = 600
touch -d '40 days ago' "$BOX20P/fixture/files-new.archive.gz"
RC=0
OUT="$(PATH="$BOX20P/bin:$PATH" FIXTURE_SRC="$BOX20P/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX20P/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups fails when the files archive is stale" 1 "newest files archive"
printf 'NOT-GZIP' > "$BOX20P/fixture/files-new.archive.gz"
RC=0
OUT="$(PATH="$BOX20P/bin:$PATH" FIXTURE_SRC="$BOX20P/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX20P/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups fails when the files archive is corrupt" 1 "newest files archive fails"
rm -f "$BOX20P/fixture/files-new.archive.gz"
rm -f "$BOX20P/local/files-new.archive.gz"
RC=0
OUT="$(PATH="$BOX20P/bin:$PATH" FIXTURE_SRC="$BOX20P/fixture" WAEL_HOST=server.test LOCAL_BACKUP_DIR="$BOX20P/local" bash "$REPO_ROOT/infrastructure/deploy/scripts/pull-backups.sh" 2>&1)" || RC=$?
check "pull-backups skips files checks when none were pulled" 0 "skipping files checks"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Deploy scripts test results: $passed passed, $failed failed"
[ "$failed" -eq 0 ] || exit 1
