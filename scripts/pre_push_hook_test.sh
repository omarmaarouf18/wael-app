#!/usr/bin/env bash
# Self-test for .githooks/pre-push ref handling (C1):
# 1. an untracked failing marker in the pushing tree does NOT block a clean commit
# 2. a committed failing marker DOES block the push
# 3. the temporary worktree is removed after both a pass and a fail
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOK="$REPO_ROOT/.githooks/pre-push"
WORK="$(mktemp -d)"
# shellcheck disable=SC2064
trap 'chmod -R u+rwx "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
OUT=""
RC=0

ok() { passed=$((passed + 1)); echo "ok   - $1"; }
bad() { failed=$((failed + 1)); echo "FAIL - $1"; [ -z "${2:-}" ] || printf '%s\n' "$2" | sed 's/^/       | /'; }

check() {
	local desc="$1" want_rc="$2" want_out="${3:-}"
	if [ "$RC" -eq "$want_rc" ] && { [ -z "$want_out" ] || [[ "$OUT" == *"$want_out"* ]]; }; then
		ok "$desc"
	else
		bad "$desc (exit $RC, wanted $want_rc${want_out:+, output containing \"$want_out\"})" "$OUT"
	fi
}

# Source the hook without running it (PRE_PUSH_SOURCED skips main), then
# neutralize the hook's `set -e` so a failing main() can be asserted on.
PRE_PUSH_SOURCED=1
# shellcheck disable=SC1091
source "$HOOK"
set +e

CHECK_DIRS="$WORK/check-dirs"
: > "$CHECK_DIRS"

# Stub the real gate: record the directory it ran in, and fail only when the
# marker file is present there (i.e. committed, since the temporary worktree
# holds exactly the pushed commit and never the pushing tree's untracked files).
run_gate_checks() {
	pwd >> "$CHECK_DIRS"
	if [ -e "HOOK_SHOULD_FAIL" ]; then
		echo "stub gate: marker present, failing"
		return 1
	fi
	echo "stub gate: passing"
	return 0
}

# Fixture repo with one clean commit.
FIX="$WORK/fixture"
git init -q -b main "$FIX"
git -C "$FIX" config user.email "hook-test@example.com"
git -C "$FIX" config user.name "hook-test"
echo clean > "$FIX/file.txt"
git -C "$FIX" add -A
git -C "$FIX" commit -qm "clean commit"
CLEAN_SHA="$(git -C "$FIX" rev-parse HEAD)"
ZERO="0000000000000000000000000000000000000000"

# Run main() in a subshell from the fixture (the subshell keeps the hook's
# EXIT trap away from this test's own trap).
run_main() {
	( cd "$FIX" && main <<<"refs/heads/main $1 refs/heads/main $ZERO" >"$WORK/hook-out.txt" 2>&1 )
	RC=$?
	OUT="$(cat "$WORK/hook-out.txt")"
}

worktrees_clean() {
	[ "$(git -C "$FIX" worktree list 2>/dev/null | wc -l)" -eq 1 ]
}

# Test 1: untracked marker must not block the clean commit.
touch "$FIX/HOOK_SHOULD_FAIL"
run_main "$CLEAN_SHA"
check "untracked failing marker does not block a clean commit" 0 ""
if [ "$(wc -l < "$CHECK_DIRS")" -eq 1 ] && [ "$(cat "$CHECK_DIRS")" != "$FIX" ]; then
	ok "gate ran in a temporary worktree, not in the pushing tree"
else
	bad "gate ran in a temporary worktree, not in the pushing tree" "$(cat "$CHECK_DIRS")"
fi

# Test 2: committed marker must block the push.
git -C "$FIX" add HOOK_SHOULD_FAIL
git -C "$FIX" commit -qm "bad commit"
BAD_SHA="$(git -C "$FIX" rev-parse HEAD)"
run_main "$BAD_SHA"
check "committed failing marker blocks the push" 1 "failed the gate"

# Test 3: the temporary worktree is gone after the pass and after the fail.
if [ "$(wc -l < "$CHECK_DIRS")" -eq 2 ]; then
	ok "gate ran once per pushed commit"
else
	bad "gate ran once per pushed commit" "$(cat "$CHECK_DIRS")"
fi
LEFTOVER=0
while IFS= read -r d || [ -n "$d" ]; do
	[ -e "$d" ] && LEFTOVER=1
done < "$CHECK_DIRS"
if [ "$LEFTOVER" -eq 0 ] && worktrees_clean; then
	ok "temporary worktree removed after both pass and fail"
else
	bad "temporary worktree removed after both pass and fail" "$(cat "$CHECK_DIRS")$(git -C "$FIX" worktree list)"
fi

echo ""
echo "Pre-push hook test results: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
