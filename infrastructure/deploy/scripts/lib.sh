#!/usr/bin/env bash
# Shared settings for the deploy scripts. Sourced, never executed directly.

WAEL_HOME="${WAEL_HOME:-/home/deploybot/wael}"
export WAEL_HOME

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${WAEL_HOME}/.env.production"
RELEASE_FILE="${REPO_DIR}/release.env"
STATE_DIR="${WAEL_HOME}/state"
# shellcheck disable=SC2034 # used by the scripts that source this file
LAST_GOOD_FILE="${STATE_DIR}/last-good.env"

# shellcheck disable=SC2034
APP_SERVICES=(api-gateway auth-service notification-service)

log() { printf '[deploy] %s\n' "$*"; }
fail() { printf '[deploy] FAILED: %s\n' "$*" >&2; exit 1; }

# compose runs docker compose with the production env and the release tag.
# A shell-exported IMAGE_TAG overrides release.env (used by rollback).
compose() {
	docker compose \
		--project-directory "${REPO_DIR}" \
		--env-file "${ENV_FILE}" \
		--env-file "${RELEASE_FILE}" \
		-f "${REPO_DIR}/docker-compose.yml" \
		"$@"
}

# read_var FILE NAME prints the value of NAME=... from a dotenv file.
read_var() {
	sed -n "s/^$2=//p" "$1" | tail -n 1
}

# external_health checks the public endpoint through Caddy on this host.
external_health() {
	local domain
	domain="$(read_var "${ENV_FILE}" API_DOMAIN)"
	[ -n "${domain}" ] || return 1
	curl -fsS --max-time 10 --resolve "${domain}:443:127.0.0.1" \
		"https://${domain}/health" >/dev/null
}
