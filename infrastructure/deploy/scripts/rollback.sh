#!/usr/bin/env bash
# Rollback to the last release recorded by a successful deploy.sh run.
# Limits: images only. Database changes are NOT rolled back (no migration
# framework yet; see RUNBOOK.md "Known gaps").
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

[ -f "$LAST_GOOD_FILE" ] || fail "no last-good release recorded ($LAST_GOOD_FILE); manual recovery needed"
good="$(read_var "$LAST_GOOD_FILE" IMAGE_TAG)"
grep -qE '^[0-9a-f]{40}$' <<<"$good" || fail "last-good IMAGE_TAG is invalid: '$good'"

log "rolling back to IMAGE_TAG=$good"
export IMAGE_TAG="$good"
compose up -d --remove-orphans --wait --wait-timeout 180 \
	|| fail "rollback containers did not become healthy; manual recovery needed"
external_health || fail "rollback is up but the public health check fails; manual recovery needed"
log "rollback to $good is healthy"
