#!/bin/bash
# Website gate (web/, ADR-0014): clean install from the lockfile, ESLint with
# zero warnings, TypeScript, and a production `next build` that must print no
# warning. Used by the pre-push hook (and so `make ci`) and the CI Gate
# "Website" job.
#
# One build warning is allowed: Next.js's "No build cache found" line, which
# depends only on whether .next/cache exists (a fresh CI runner has none),
# not on the code. The whole line must match; any other warning still fails.
#
# Usage: scripts/web_gate.sh
#        scripts/web_gate.sh --check-build-log FILE   (only the log check;
#        used by scripts/web_gate_test.sh)
set -euo pipefail

# The one allowed warning, as an exact whole line (optional trailing CR).
ALLOWED_WARNING_RE='^⚠ No build cache found\. Please configure build caching for faster rebuilds\. Read more: https://nextjs\.org/docs/messages/no-cache'$'\r''?$'

# check_build_log FILE: exit 1 if FILE has a warning other than the allowed one.
check_build_log() {
  local hits
  hits="$(grep -v -E "$ALLOWED_WARNING_RE" "$1" | grep -i -E '(^|[^a-z])warn(ing)?s?([^a-z]|$)|⚠' || true)"
  if [ -n "$hits" ]; then
    printf '%s\n' "$hits"
    echo "BLOCKED: next build printed a warning (warnings are errors for web/)"
    return 1
  fi
  if grep -q -E "$ALLOWED_WARNING_RE" "$1"; then
    echo "-- web: allowed: next build has no .next/cache yet (\"No build cache found\") --"
  fi
}

if [ "${1:-}" = "--check-build-log" ]; then
  if [ -z "${2:-}" ] || [ ! -f "$2" ]; then
    echo "usage: $0 --check-build-log FILE"
    exit 2
  fi
  check_build_log "$2"
  echo "-- web: build log check passed --"
  exit 0
fi

cd "$(dirname "$0")/../web"
export NEXT_TELEMETRY_DISABLED=1

if ! command -v node >/dev/null 2>&1; then
  echo "BLOCKED: node is required for web/ (Node 24, see web/package.json engines)"
  exit 1
fi

echo "-- web: npm ci --"
npm ci --no-audit --no-fund

echo "-- web: lint --"
npm run lint

echo "-- web: typecheck --"
npm run typecheck

echo "-- web: next build --"
log="$(mktemp)"
trap 'rm -f "$log"' EXIT
npm run build 2>&1 | tee "$log"
check_build_log "$log"
echo "-- web: gate passed --"
