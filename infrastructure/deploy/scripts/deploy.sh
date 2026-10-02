#!/usr/bin/env bash
# Deploy: pre-flight -> up with health gate -> external check -> record.
# On any failure after containers were touched, roll back to the last
# release that passed this script (fixes saas-core S-03: no HEAD~1 guess).
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"

"$(dirname "$0")/preflight.sh"

tag="$(read_var "$RELEASE_FILE" IMAGE_TAG)"
log "deploying IMAGE_TAG=$tag"

deployed=0
if compose up -d --remove-orphans --wait --wait-timeout 180; then
	# Health through Caddy with real public TLS; allow time for ACME on first run.
	for _ in $(seq 1 12); do
		if external_health; then deployed=1; break; fi
		sleep 5
	done
	[ "$deployed" -eq 1 ] || log "external health check through Caddy failed"
else
	log "containers did not become healthy within 180s"
fi

if [ "$deployed" -eq 1 ]; then
	printf 'IMAGE_TAG=%s\nDEPLOYED_AT=%s\n' "$tag" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$LAST_GOOD_FILE"
	log "deploy succeeded; recorded $tag as last good release"
	exit 0
fi

compose ps || true
for svc in "${APP_SERVICES[@]}"; do
	log "last log lines: $svc"
	compose logs --no-color --tail 30 "$svc" || true
done
"$(dirname "$0")/rollback.sh" || true
fail "deploy of $tag failed (see rollback output above)"
