#!/usr/bin/env bash
# Self-test for deploy.sh and rollback.sh:
# 1. Rollback uses state/last-good/docker-compose.yml with -p wael and --remove-orphans
# 2. Failed-releases guard blocks deployment of rolled-back SHAs unless ALLOW_FAILED_RELEASE=1
# 3. Missing state/last-good/ fallback triggers loud warning and uses current compose
# 4. Successful deploy populates state/last-good/ (compose, Caddyfile, last-good.env)
# 5. Rollback records failed SHA in state/failed-releases
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

	# Mock curl: always succeeds
	cat > "$d/bin/curl" << 'EOF'
#!/usr/bin/env bash
exit 0
EOF
	chmod +x "$d/bin/curl"

	# Mock docker: logs arguments and simulates compose commands
	cat > "$d/bin/docker" << 'EOF'
#!/usr/bin/env bash
echo "DOCKER_CALL: $*" >> "$DOCKER_LOG"
exit "${MOCK_DOCKER_EXIT:-0}"
EOF
	chmod +x "$d/bin/docker"

	# Env production
	printf 'API_DOMAIN=api.test.local\n' > "$d/wael/.env.production"

	echo "$d"
}

# ---------------------------------------------------------------------------
# Test 1: Deploy populates state/last-good/ on success
# ---------------------------------------------------------------------------
BOX1="$(setup_sandbox test1)"
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

# ---------------------------------------------------------------------------
# Test 2: Failed-releases guard blocks deployment of rolled-back SHAs
# ---------------------------------------------------------------------------
BOX2="$(setup_sandbox test2)"
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
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Deploy scripts test results: $passed passed, $failed failed"
[ "$failed" -eq 0 ] || exit 1
