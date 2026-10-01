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
	"color_literal|Color\\(0x[0-9A-Fa-f]{8}\\)"
	"material_color|(?<![A-Za-z0-9_])Colors\\.(?!transparent\\b)[a-z]"
	"to_upper_case|\\.toUpperCase\\(\\)"
	"non_directional_insets|EdgeInsets\\.(only|fromLTRB)\\("
)

current() {
	local rule label regex
	for rule in "${RULES[@]}"; do
		label="${rule%%|*}"
		regex="${rule#*|}"
		grep -rcP "$regex" "$SCREENS_DIR" --include='*.dart' 2>/dev/null |
			awk -F: -v l="$label" -v root="$REPO_ROOT/" '$2 > 0 { sub(root, "", $1); print $1 "|" l "|" $2 }'
	done | sort
}

if [ ! -d "$SCREENS_DIR" ]; then
	echo "GATE SKIP: $SCREENS_DIR not found"
	exit 0
fi

if [ "${1:-}" = "--update" ]; then
	current >"$BASELINE"
	echo "baseline written: $(wc -l <"$BASELINE") file/rule entries"
	exit 0
fi

touch "$BASELINE"
failed=0
improved=0
while IFS='|' read -r file label count; do
	base="$(awk -F'|' -v f="$file" -v l="$label" '$1 == f && $2 == l { print $3 }' "$BASELINE")"
	base="${base:-0}"
	if [ "$count" -gt "$base" ]; then
		echo "BLOCKED: $file: $label $count (baseline $base)"
		failed=1
	fi
done < <(current)

while IFS='|' read -r file label base; do
	[ -n "$file" ] || continue
	now="$(current | awk -F'|' -v f="$file" -v l="$label" '$1 == f && $2 == l { print $3 }')"
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
