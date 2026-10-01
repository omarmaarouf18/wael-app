#!/usr/bin/env bash
# Self-test for scripts/frontend_composition_gate.sh.
#
# Each case builds a throwaway repo layout (scripts/ + frontend/lib/screens/)
# under a temp dir and runs a copy of the real gate there, so the working tree
# and the committed baseline are never touched.
#
# Usage: scripts/frontend_gate_test.sh
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE_SRC="${GATE_SRC:-$REPO_ROOT/scripts/frontend_composition_gate.sh}"
WORK="$(mktemp -d)"
trap 'chmod -R u+rwx "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
OUT=""
RC=0

# new_case <name>: fresh layout with one clean screen and an empty baseline.
new_case() {
	CASE="$WORK/$1"
	mkdir -p "$CASE/scripts" "$CASE/frontend/lib/screens"
	cp "$GATE_SRC" "$CASE/scripts/frontend_composition_gate.sh"
	: >"$CASE/scripts/frontend_gate_baseline.txt"
	printf 'class Clean {}\n' >"$CASE/frontend/lib/screens/clean_screen.dart"
}

# run_gate [args]: run the case's gate copy, capture output and exit code.
run_gate() {
	RC=0
	OUT="$(bash "$CASE/scripts/frontend_composition_gate.sh" "$@" 2>&1)" || RC=$?
}

check() { # check <description> <expected-rc> [expected-output-substring]
	local desc="$1" want_rc="$2" want_out="${3:-}"
	if [ "$RC" -eq "$want_rc" ] && { [ -z "$want_out" ] || [[ "$OUT" == *"$want_out"* ]]; }; then
		passed=$((passed + 1))
		echo "ok   - $desc"
	else
		failed=$((failed + 1))
		echo "FAIL - $desc (exit $RC, wanted $want_rc${want_out:+, output containing \"$want_out\"})"
		printf '%s\n' "$OUT" | sed 's/^/       | /'
	fi
}

# 1. Clean tree passes.
new_case clean
run_gate
check "clean tree passes" 0 "passed"

# 2. Every rule detects an injected violation, even when all other rules have
#    zero matches (regression: grep exit 1 used to abort the rule loop).
#    label|line
VIOLATIONS=(
	'scaffold|final w = Scaffold(body: x);'
	'appbar|final w = AppBar(title: x);'
	'box_decoration|final d = BoxDecoration(color: x);'
	'text_style|final s = TextStyle(height: 1);'
	'font_size|final s = foo(fontSize: 12);'
	'color_literal|final c = Color(0xFF112233);'
	'color_literal|final c = Color.fromARGB(255, 1, 2, 3);'
	'color_literal|final c = Color.fromRGBO(1, 2, 3, 1);'
	'material_color|final c = Colors.red;'
	'to_upper_case|final t = name.toUpperCase();'
	'non_directional_insets|final p = EdgeInsets.only(left: 1);'
	'non_directional_insets|final p = EdgeInsets.fromLTRB(1, 2, 3, 4);'
)
i=0
for entry in "${VIOLATIONS[@]}"; do
	i=$((i + 1))
	label="${entry%%|*}"
	line="${entry#*|}"
	new_case "inject_$i"
	printf '%s\n' "$line" >"$CASE/frontend/lib/screens/bad_screen.dart"
	run_gate
	check "injected $label violation is blocked: $line" 1 "BLOCKED: frontend/lib/screens/bad_screen.dart: $label 1"
done

# 3. Allowed patterns stay allowed.
new_case allowed
cat >"$CASE/frontend/lib/screens/ok_screen.dart" <<'EOF'
final a = Colors.transparent;
final b = EdgeInsets.all(8);
final c = EdgeInsets.symmetric(horizontal: 4);
final d = MyColor.fromARGB(1, 2, 3, 4);
EOF
run_gate
check "Colors.transparent, EdgeInsets.all/symmetric and MyColor.fromARGB pass" 0 "passed"

# 4. Ratchet: at baseline passes, above fails, below fails with the update note.
new_case ratchet
printf 'final c = Colors.red;\nfinal d = Colors.blue;\n' >"$CASE/frontend/lib/screens/a_screen.dart"
run_gate --update
check "--update writes the baseline" 0 "baseline written: 1 file/rule entries"
run_gate
check "count equal to baseline passes" 0 "passed"
printf 'final e = Colors.green;\n' >>"$CASE/frontend/lib/screens/a_screen.dart"
run_gate
check "count above baseline is blocked" 1 "material_color 3 (baseline 2)"
printf 'final c = Colors.red;\n' >"$CASE/frontend/lib/screens/a_screen.dart"
run_gate
check "count below baseline fails and asks to lower the baseline" 1 "Lower the baseline"
run_gate --update
run_gate
check "after --update the lowered count passes" 0 "passed"
printf 'final c = Colors.red;\n' >"$CASE/frontend/lib/screens/new_screen.dart"
run_gate
check "violation in a file without a baseline entry is blocked" 1 "new_screen.dart: material_color 1 (baseline 0)"

# 5. --update keeps every rule's entries even when an earlier rule has no matches.
new_case update_full
printf 'final c = Colors.red;\nfinal t = a.toUpperCase();\n' >"$CASE/frontend/lib/screens/a_screen.dart"
run_gate --update
check "--update with zero-match early rules still writes later rules" 0 "baseline written: 2 file/rule entries"

# 6. Missing screens directory fails closed (check and update).
new_case missing_dir
printf 'final c = Colors.red;\n' >"$CASE/frontend/lib/screens/a_screen.dart"
run_gate --update
cp "$CASE/scripts/frontend_gate_baseline.txt" "$WORK/baseline_before.txt"
rm -rf "$CASE/frontend/lib/screens"
run_gate
check "missing screens dir exits 2" 2 "GATE ERROR"
run_gate --update
check "missing screens dir exits 2 on --update" 2 "GATE ERROR"
if cmp -s "$CASE/scripts/frontend_gate_baseline.txt" "$WORK/baseline_before.txt"; then
	passed=$((passed + 1))
	echo "ok   - missing screens dir leaves the baseline untouched"
else
	failed=$((failed + 1))
	echo "FAIL - missing screens dir changed the baseline"
fi

# 7. Unreadable file fails closed and leaves the baseline untouched.
new_case unreadable
printf 'final c = Colors.red;\n' >"$CASE/frontend/lib/screens/a_screen.dart"
run_gate --update
cp "$CASE/scripts/frontend_gate_baseline.txt" "$WORK/baseline_before.txt"
: >"$CASE/frontend/lib/screens/locked_screen.dart"
chmod 000 "$CASE/frontend/lib/screens/locked_screen.dart"
if [ -r "$CASE/frontend/lib/screens/locked_screen.dart" ]; then
	echo "skip - unreadable-file cases (permissions are not enforced for this user)"
else
	run_gate
	check "unreadable file exits 2" 2 "GATE ERROR"
	run_gate --update
	check "unreadable file exits 2 on --update" 2 "GATE ERROR"
	if cmp -s "$CASE/scripts/frontend_gate_baseline.txt" "$WORK/baseline_before.txt"; then
		passed=$((passed + 1))
		echo "ok   - unreadable file leaves the baseline untouched"
	else
		failed=$((failed + 1))
		echo "FAIL - unreadable file changed the baseline"
	fi
fi

# 8. Every name the failure message points to must exist in the real tree, so
#    the advice never refers to a widget, token or file that is not there.
MESSAGE="$(sed -n "/<<'EOF'/,/^EOF/p" "$GATE_SRC" | grep -- '->' | sed 's/.*->//')"
check_name() { # check_name <description> <command...>
	local desc="$1"
	shift
	if "$@"; then
		passed=$((passed + 1))
		echo "ok   - $desc"
	else
		failed=$((failed + 1))
		echo "FAIL - $desc"
	fi
}
class_exists() {
	grep -rqE "^(abstract |final |base |sealed )?(class|enum|mixin) $1\b" "$REPO_ROOT/frontend/lib"
}
static_exists() {
	grep -qE "static [A-Za-z<>?, ]+ $1\(" "$REPO_ROOT/frontend/lib/core/theme.dart"
}
names_checked=0
while IFS= read -r name; do
	[ -n "$name" ] || continue
	case "$name" in Arabic | RTL | Colors | EdgeInsetsDirectional) continue ;; esac
	names_checked=$((names_checked + 1))
	check_name "failure message names $name, which exists as a class or enum" class_exists "$name"
done < <(grep -oE '\b[A-Z][A-Za-z0-9]*\b' <<<"$MESSAGE" | sort -u)
while IFS= read -r member; do
	[ -n "$member" ] || continue
	names_checked=$((names_checked + 1))
	check_name "failure message names AppTypography.$member, which exists" static_exists "$member"
done < <(grep -oE 'AppTypography\.[a-z][A-Za-z]*' <<<"$MESSAGE" | sed 's/AppTypography\.//' | sort -u)
while IFS= read -r path; do
	[ -n "$path" ] || continue
	names_checked=$((names_checked + 1))
	check_name "failure message names $path, which exists" test -e "$REPO_ROOT/frontend/$path"
done < <(grep -oE 'lib/[A-Za-z_/]+(\.dart)?' <<<"$MESSAGE" | sort -u)
check_name "failure message names at least 8 things to verify" test "$names_checked" -ge 8

echo "== frontend gate self-test: $passed passed, $failed failed =="
[ "$failed" -eq 0 ]
