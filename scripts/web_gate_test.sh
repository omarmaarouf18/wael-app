#!/usr/bin/env bash
# Self-test for the build log check in scripts/web_gate.sh.
#
# Each case writes a fake `next build` log to a temp file and runs the real
# gate's `--check-build-log` mode on it, so no npm or Next.js is needed.
#
# Usage: scripts/web_gate_test.sh
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="${GATE:-$REPO_ROOT/scripts/web_gate.sh}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

NO_CACHE='⚠ No build cache found. Please configure build caching for faster rebuilds. Read more: https://nextjs.org/docs/messages/no-cache'

passed=0
failed=0
n=0

# check_log <description> <expected-rc> <log text>
check_log() {
	local desc="$1" want_rc="$2" log out rc=0
	n=$((n + 1))
	log="$WORK/build_$n.log"
	printf '%s\n' "$3" >"$log"
	out="$(bash "$GATE" --check-build-log "$log" 2>&1)" || rc=$?
	if [ "$rc" -eq "$want_rc" ]; then
		passed=$((passed + 1))
		echo "ok   - $desc"
	else
		failed=$((failed + 1))
		echo "FAIL - $desc (exit $rc, wanted $want_rc)"
		printf '%s\n' "$out" | sed 's/^/       | /'
	fi
}

BUILD_OK='▲ Next.js 16.4.2
  Creating an optimized production build ...
✓ Compiled successfully in 3.1s
ƒ  (Dynamic)  server-rendered on demand'

check_log "clean build log passes" 0 "$BUILD_OK"
check_log "only the no-cache line passes" 0 "$NO_CACHE"
check_log "no-cache line inside a normal build log passes" 0 "$BUILD_OK
$NO_CACHE"
check_log "no-cache line with a CRLF ending passes" 0 "$NO_CACHE"$'\r'

check_log "another ⚠ line fails" 1 "⚠ Unsupported metadata viewport is configured in metadata export"
check_log "another ⚠ line next to the no-cache line fails" 1 "$NO_CACHE
⚠ Unsupported metadata viewport is configured in metadata export"
check_log "a 'warn' line without ⚠ fails" 1 "$BUILD_OK
warn - something changed"
check_log "the no-cache text with extra words fails" 1 "$NO_CACHE and also something else"
check_log "the no-cache text indented fails" 1 "  $NO_CACHE"
check_log "a different no-cache link fails" 1 "⚠ No build cache found. Please configure build caching for faster rebuilds. Read more: https://example.com/no-cache"

# check_routes_log <description> <expected-rc> <log text>: the route check
# (--check-routes) on a fake route table.
check_routes_log() {
	local desc="$1" want_rc="$2" log out rc=0
	n=$((n + 1))
	log="$WORK/routes_$n.log"
	printf '%s\n' "$3" >"$log"
	out="$(bash "$GATE" --check-routes "$log" 2>&1)" || rc=$?
	if [ "$rc" -eq "$want_rc" ]; then
		passed=$((passed + 1))
		echo "ok   - $desc"
	else
		failed=$((failed + 1))
		echo "FAIL - $desc (exit $rc, wanted $want_rc)"
		printf '%s\n' "$out" | sed 's/^/       | /'
	fi
}

ROUTES_OK='Route (app)                   Revalidate  Expire
┌ ○ /
├ ○ /_not-found
├ ƒ /account
├ ○ /about
├ ○ /en
├ ƒ /en/account
├ ƒ /en/forgot-password
├ ƒ /en/login
├ ƒ /en/signup
├ ○ /en/subjects                      5m      1y
├   /en/subjects/[id]
│ ├ ● /en/subjects/s-1                5m      1y
│ └ ● /en/subjects/s-2                5m      1y
├ ƒ /forgot-password
├ ƒ /login
├ ƒ /signup
├ ○ /subjects                         5m      1y
├ ● /subjects/[id]
└ ○ /terms

ƒ Proxy (Middleware)

○  (Static)   prerendered as static content
ƒ  (Dynamic)  server-rendered on demand'

check_routes_log "decided route mix passes" 0 "$ROUTES_OK"
check_routes_log "a static page turned dynamic fails" 1 "${ROUTES_OK/├ ○ \/about/├ ƒ /about}"
check_routes_log "a subject page turned dynamic fails" 1 "${ROUTES_OK/│ ├ ● \/en\/subjects\/s-1/│ ├ ƒ /en/subjects/s-1}"
check_routes_log "a form page turned static fails" 1 "${ROUTES_OK/├ ƒ \/login/├ ○ /login}"
check_routes_log "a missing form page fails" 1 "$(printf '%s\n' "$ROUTES_OK" | grep -v ' /en/signup')"
check_routes_log "an extra dynamic page fails" 1 "$ROUTES_OK
├ ƒ /en/buy"
check_routes_log "a log without a route table fails" 1 "$BUILD_OK"

# Usage errors are not passes.
rc=0
bash "$GATE" --check-build-log "$WORK/missing.log" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
	passed=$((passed + 1))
	echo "ok   - missing log file is a usage error"
else
	failed=$((failed + 1))
	echo "FAIL - missing log file is a usage error (exit $rc, wanted 2)"
fi

rc=0
bash "$GATE" --check-routes "$WORK/missing.log" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
	passed=$((passed + 1))
	echo "ok   - missing route log file is a usage error"
else
	failed=$((failed + 1))
	echo "FAIL - missing route log file is a usage error (exit $rc, wanted 2)"
fi

echo "web gate log check: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
