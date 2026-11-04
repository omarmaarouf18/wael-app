#!/bin/bash
# Website gate (web/, ADR-0014): clean install from the lockfile, ESLint with
# zero warnings, TypeScript, and a production `next build` that must print no
# warning. Used by the pre-push hook (and so `make ci`) and the CI Gate
# "Website" job.
set -euo pipefail

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
if grep -n -i -E '(^|[^a-z])warn(ing)?s?([^a-z]|$)|⚠' "$log"; then
  echo "BLOCKED: next build printed a warning (warnings are errors for web/)"
  exit 1
fi
echo "-- web: gate passed --"
