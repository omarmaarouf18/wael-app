#!/usr/bin/env bash
# Frontend composition gate (ported from saas-core, with a ratchet).
#
# Screens (frontend/lib/screens/**) must compose UI from the shared widget
# layer (frontend/lib/widgets/**) and the tokens in frontend/lib/core/theme.dart.
# wael-app starts with existing violations, so instead of failing on every
# match this gate compares per-file counts against a committed baseline:
#   - a count ABOVE the baseline (or a new file with violations) fails;
#   - a count BELOW the baseline passes and asks you to lower the baseline
#     in the same commit (./scripts/frontend_composition_gate.sh --update).
# The baseline only ever goes down. When it is empty, the gate is strict.
#
# Usage: scripts/frontend_composition_gate.sh [--update]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCREENS_DIR="$REPO_ROOT/frontend/lib/screens"
BASELINE="$REPO_ROOT/scripts/frontend_gate_baseline.txt"

# label|PCRE. Each rule names the shared replacement in the error text below.
RULES=(
	"scaffold|(?<![A-Za-z0-9_])Scaffold\\("
	"appbar|(?<![A-Za-z0-9_])AppBar\\("
	"box_decoration|(?<![A-Za-z0-9_])BoxDecoration\\("
	"text_style|(?<![A-Za-z0-9_])TextStyle\\("
	"font_size|(?<![A-Za-z0-9_])fontSize:"
	"color_literal|Color\\(0x[0-9A-Fa-f]{8}\\)|(?<![A-Za-z0-9_])Color\\.from(ARGB|RGBO)\\("
	"material_color|(?<![A-Za-z0-9_])Colors\\.(?!transparent\\b)[a-z]"
	"to_upper_case|\\.toUpperCase\\(\\)"
	"non_directional_insets|EdgeInsets\\.(only|fromLTRB)\\("
)

# Prints "file|rule|count" for every file/rule with at least one match.
# grep -c exits 1 when a rule has no match anywhere, which is the goal state, so
# only exit codes above 1 (bad regex, unreadable file) are errors. Errors must
# stop the gate: a partial result would silently skip every later rule.
current() {
	local rule label regex out rc
	for rule in "${RULES[@]}"; do
		label="${rule%%|*}"
		regex="${rule#*|}"
		rc=0
		out="$(grep -rcP "$regex" "$SCREENS_DIR" --include='*.dart')" || rc=$?
		if [ "$rc" -gt 1 ]; then
			echo "GATE ERROR: grep failed with exit $rc for rule $label" >&2
			return 2
		fi
		printf '%s\n' "$out" |
			awk -F: -v l="$label" -v root="$REPO_ROOT/" '$2 > 0 { sub(root, "", $1); print $1 "|" l "|" $2 }'
	done | sort
}

# Fail closed: a renamed or moved screens directory must not turn the gate off.
if [ ! -d "$SCREENS_DIR" ]; then
	echo "GATE ERROR: $SCREENS_DIR not found" >&2
	exit 2
fi

# Evaluate once, before touching the baseline. A failure here aborts the script
# (set -e) instead of leaving a truncated baseline or a half-checked tree.
CURRENT="$(current)"

if [ "${1:-}" = "--update" ]; then
	if [ -n "$CURRENT" ]; then
		printf '%s\n' "$CURRENT" >"$BASELINE"
	else
		: >"$BASELINE"
	fi
	echo "baseline written: $(wc -l <"$BASELINE") file/rule entries"
	exit 0
fi

touch "$BASELINE"
failed=0
improved=0
while IFS='|' read -r file label count; do
	[ -n "$file" ] || continue
	base="$(awk -F'|' -v f="$file" -v l="$label" '$1 == f && $2 == l { print $3 }' "$BASELINE")"
	base="${base:-0}"
	if [ "$count" -gt "$base" ]; then
		echo "BLOCKED: $file: $label $count (baseline $base)"
		failed=1
	fi
done <<<"$CURRENT"

while IFS='|' read -r file label base; do
	[ -n "$file" ] || continue
	now="$(awk -F'|' -v f="$file" -v l="$label" '$1 == f && $2 == l { print $3 }' <<<"$CURRENT")"
	now="${now:-0}"
	if [ "$now" -lt "$base" ]; then improved=1; fi
done <"$BASELINE"

if [ "$failed" -eq 1 ]; then
	cat <<'EOF'

New composition violations in frontend/lib/screens/. Use instead:
  Scaffold/AppBar          -> AppShell / screen templates (lib/widgets/)
  BoxDecoration            -> ThemedCard / ThemedPanel (lib/widgets/)
  TextStyle / fontSize     -> AppTypography.* (lib/core/theme.dart)
  Color(0x..) / Colors.x   -> AppColors.* (Colors.transparent is allowed)
  .toUpperCase()           -> AppTypography.uppercaseLabel(...)
  EdgeInsets.only/fromLTRB -> EdgeInsetsDirectional (Arabic is RTL)
EOF
	exit 1
fi

if [ "$improved" -eq 1 ]; then
	echo "NOTE: violations went down. Lower the baseline in this commit:"
	echo "  scripts/frontend_composition_gate.sh --update"
	exit 1
fi

echo "== frontend composition gate: passed ($(wc -l <"$BASELINE") baseline entries left) =="
