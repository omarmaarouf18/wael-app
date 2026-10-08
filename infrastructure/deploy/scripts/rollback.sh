#!/usr/bin/env bash
# Rollback to the last release recorded by a successful deploy.sh run.
# Limits: images only. Database changes are NOT rolled back automatically (no
# migration framework yet; see RUNBOOK.md "Known gaps"). When deploy.sh took a
# pre-deploy backup it exports PREDEPLOY_BACKUP, and this script prints the
# exact restore command; restoring stays a deliberate human decision because
# it discards every write made since the backup.
set -euo pipefail
# shellcheck disable=SC1091
source "$(dirname "$0")/lib.sh"

# Locate last-good release info
last_good_env="$LAST_GOOD_FILE"
if [ ! -f "$last_good_env" ] && [ -f "$LEGACY_LAST_GOOD_FILE" ]; then
	last_good_env="$LEGACY_LAST_GOOD_FILE"
fi

[ -f "$last_good_env" ] || fail "no last-good release recorded ($LAST_GOOD_FILE or $LEGACY_LAST_GOOD_FILE); manual recovery needed"
good="$(read_var "$last_good_env" IMAGE_TAG)"
grep -qE '^[0-9a-f]{40}$' <<<"$good" || fail "last-good IMAGE_TAG is invalid: '$good'"

# Record the failed release in failed-releases so it cannot be redeployed accidentally.
# Do NOT record if failed_tag == good (re-deploy of the running release that hit a transient
# failure, or a manual rollback with nothing newer deployed).
failed_tag="${1:-}"
if [ -z "$failed_tag" ] && [ -f "$RELEASE_FILE" ]; then
	failed_tag="$(read_var "$RELEASE_FILE" IMAGE_TAG || true)"
fi
if [ -n "$failed_tag" ] && [ "$failed_tag" != "$good" ] && grep -qE '^[0-9a-f]{40}$' <<<"$failed_tag"; then
	mkdir -p "$STATE_DIR"
	if [ ! -f "$FAILED_RELEASES_FILE" ] || ! grep -qxF "$failed_tag" "$FAILED_RELEASES_FILE"; then
		printf '%s\n' "$failed_tag" >> "$FAILED_RELEASES_FILE"
		log "recorded failed release $failed_tag in $FAILED_RELEASES_FILE"
	fi
fi

log "rolling back to IMAGE_TAG=$good"
export IMAGE_TAG="$good"

# Rollback must use the last-good compose file if available (P2),
# with fallback and loud warning if state/last-good/ is missing.
last_good_compose="${LAST_GOOD_DIR}/docker-compose.yml"
if [ -f "$last_good_compose" ]; then
	compose_rollback() {
		docker compose \
			-p wael \
			--project-directory "$LAST_GOOD_DIR" \
			--env-file "${ENV_FILE}" \
			--env-file "$last_good_env" \
			-f "$last_good_compose" \
			"$@"
	}
else
	log "WARNING: last-good compose file ($last_good_compose) not found; falling back to current compose file (${REPO_DIR}/docker-compose.yml)"
	compose_rollback() {
		docker compose \
			-p wael \
			--project-directory "${REPO_DIR}" \
			--env-file "${ENV_FILE}" \
			--env-file "$last_good_env" \
			-f "${REPO_DIR}/docker-compose.yml" \
			"$@"
	}
fi

compose_rollback up -d --remove-orphans --wait --wait-timeout 180 \
	|| fail "rollback containers did not become healthy; manual recovery needed"
external_health || fail "rollback is up but the public health check fails; manual recovery needed"
log "rollback to $good is healthy"
if [ -n "${PREDEPLOY_BACKUP:-}" ]; then
	log "DATA NOT ROLLED BACK. If the failed release changed data, restore the pre-deploy backup"
	log "(this discards every write made since it was taken):"
	log "  $(dirname "$0")/restore.sh $PREDEPLOY_BACKUP --yes"
fi
