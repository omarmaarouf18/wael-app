#!/bin/bash
# Website gate (web/, ADR-0014): clean install from the lockfile, ESLint with
# zero warnings, TypeScript, unit tests (node --test), and a production
# `next build` that must print no warning and must render the routes as
# decided (static ○/● except the form pages, which are dynamic ƒ). Used by the
# pre-push hook (and so `make ci`) and the CI Gate "Website" job.
#
# One build warning is allowed: Next.js's "No build cache found" line, which
# depends only on whether .next/cache exists (a fresh CI runner has none),
# not on the code. The whole line must match; any other warning still fails.
#
# Route rule (ADR-0014 decision 7, options A + C, W2b): the pages with forms
# (log in, sign up, in both languages; buy joins them in W4) are dynamic (ƒ)
# because they carry the per-request CSP nonce; every other page is static
# (○) or prebuilt from generateStaticParams (●). The gate prints the table and
# fails on any other mix, so a page cannot silently turn dynamic or lose its
# nonce.
#
# Usage: scripts/web_gate.sh
#        scripts/web_gate.sh --check-build-log FILE   (only the log check)
#        scripts/web_gate.sh --check-routes FILE      (only the route check)
#        (both used by scripts/web_gate_test.sh)
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

# The dynamic (ƒ) routes, exactly. Keep in step with the matcher in
# web/src/proxy.ts and STATIC_PAGE_SOURCE in web/next.config.ts.
DYNAMIC_ROUTES="/en/login /en/signup /login /signup"

# check_routes FILE: print the route table of a `next build` log and exit 1
# unless exactly DYNAMIC_ROUTES are ƒ and every other route is ○ or ●.
check_routes() {
  local routes bad="" dyn expected
  # Route lines look like "├ ○ /about", "│ ├ ● /subjects/s-1" or
  # "└ ƒ /terms" (optionally followed by revalidate columns).
  routes="$(sed -n -E 's/^[┌├└│ ]+ ([○●ƒ]) (\/[^ ]*).*$/\1 \2/p' "$1")"
  if [ -z "$routes" ]; then
    echo "BLOCKED: no route table found in the next build log"
    return 1
  fi
  echo "-- web: routes (○ static, ● prebuilt from generateStaticParams, ƒ dynamic) --"
  printf '%s\n' "$routes" | sed 's/^/   /'
  dyn="$(printf '%s\n' "$routes" | awk '$1 == "ƒ" { print $2 }' | sort | tr '\n' ' ' | sed 's/ $//')"
  expected="$(printf '%s\n' $DYNAMIC_ROUTES | sort | tr '\n' ' ' | sed 's/ $//')"
  if [ "$dyn" != "$expected" ]; then
    bad="dynamic routes are [$dyn], want exactly [$expected]"
  fi
  if [ -n "$bad" ]; then
    echo "BLOCKED: $bad (static pages must stay ○/●; only the form pages are ƒ)"
    return 1
  fi
  echo "-- web: route check passed: only the form pages are dynamic --"
}

if [ "${1:-}" = "--check-routes" ]; then
  if [ -z "${2:-}" ] || [ ! -f "$2" ]; then
    echo "usage: $0 --check-routes FILE"
    exit 2
  fi
  check_routes "$2"
  exit 0
fi

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

echo "-- web: unit tests --"
npm test

echo "-- web: next build --"
log="$(mktemp)"
trap 'rm -f "$log"' EXIT
npm run build 2>&1 | tee "$log"
check_build_log "$log"
check_routes "$log"
echo "-- web: gate passed --"
