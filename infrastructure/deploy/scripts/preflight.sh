#!/usr/bin/env bash
# Pre-flight: validates everything a deploy needs WITHOUT touching running
# containers (fixes saas-core S-02). Exit 0 only when every check passes.
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

errors=0
check() { # check "description" command...
	local desc="$1"; shift
	if "$@"; then log "ok: ${desc}"; else log "FAIL: ${desc}"; errors=$((errors + 1)); fi
}
mode_is() { [ "$(stat -c %a "$1" 2>/dev/null)" = "$2" ]; }

# 1. Files and permissions
check "env file exists ($ENV_FILE)" test -f "$ENV_FILE"
check "env file mode is 600" mode_is "$ENV_FILE" 600
check "secrets dir mode is 700" mode_is "$WAEL_HOME/secrets" 700
check "certs dir mode is 700" mode_is "$WAEL_HOME/certs" 700
check "mongo root password file exists" test -s "$WAEL_HOME/secrets/mongo_root_password"
check "redis.conf has requirepass" grep -q '^requirepass .\+' "$WAEL_HOME/secrets/redis.conf"
[ "$errors" -eq 0 ] || fail "$errors file check(s) failed; nothing was changed"

# 2. Release tag must be an immutable 40-char commit sha (no :latest)
tag="$(read_var "$RELEASE_FILE" IMAGE_TAG)"
check "IMAGE_TAG is a full commit sha" grep -qE '^[0-9a-f]{40}$' <<<"$tag"

# 3. Required values present and not placeholders or dev defaults
required=(API_DOMAIN ADMIN_DOMAIN ACME_EMAIL ALLOWED_ORIGIN JWT_SECRET GATEWAY_SECRET
	INTERNAL_SERVICE_TOKEN MONGO_ROOT_USERNAME AUTH_MONGO_URI
	NOTIFICATION_MONGO_URI ACADEMY_MONGO_URI REDIS_URI RESEND_API_KEY
	RESEND_FROM_EMAIL BLOCKLIST_HMAC_KEY SUPPORT_WHATSAPP)
for name in "${required[@]}"; do
	value="$(read_var "$ENV_FILE" "$name")"
	check "$name is set" test -n "$value"
	if grep -qiE 'PASTE_|CHANGE_ME|devpassword123' <<<"$value"; then
		log "FAIL: $name still holds a placeholder or dev default"
		errors=$((errors + 1))
	fi
done
for name in JWT_SECRET GATEWAY_SECRET INTERNAL_SERVICE_TOKEN BLOCKLIST_HMAC_KEY; do
	value="$(read_var "$ENV_FILE" "$name")"
	check "$name is at least 32 characters" test "${#value}" -ge 32
done
if grep -q '^APP_ENV=' "$ENV_FILE"; then
	log "FAIL: APP_ENV must not be set in the env file (compose pins production)"
	errors=$((errors + 1))
fi

# 4. Certificates: present and not expiring within 14 days
for crt in ca api-gateway auth-service notification-service academy-service admin-console; do
	check "cert $crt.crt valid for 14+ days" \
		openssl x509 -checkend $((14 * 86400)) -noout -in "$WAEL_HOME/certs/$crt.crt"
done
for key in api-gateway auth-service notification-service academy-service admin-console; do
	check "key $key.key exists" test -s "$WAEL_HOME/certs/$key.key"
done
[ "$errors" -eq 0 ] || fail "$errors check(s) failed; nothing was changed"

# 5. Compose renders with this env
check "compose config renders" compose config --quiet
[ "$errors" -eq 0 ] || fail "compose config failed; nothing was changed"

# 6. Get the new images (does not affect running containers).
#    SKIP_PULL=1 is for a manual trial deploy where the app images were
#    loaded with `docker load` instead of coming from GHCR (RUNBOOK.md,
#    "Manual trial deploy"). It skips the pull of the app images only:
#    they must already be present locally under this exact tag. mongo, redis
#    and caddy are still pulled from Docker Hub. Shell only: the deploy
#    workflow never sets it, so the normal path always pulls.
image_loaded() { docker image inspect "$1" >/dev/null 2>&1; }
if [ "${SKIP_PULL:-0}" = 1 ]; then
	log "SKIP_PULL=1: not pulling app images for $tag; checking they are loaded locally"
	while read -r image; do
		check "image $image is loaded locally" image_loaded "$image"
	done < <(compose config --images | grep '/wael-app-')
	[ "$errors" -eq 0 ] || fail "app images are not loaded locally; nothing was changed"
	compose pull --quiet mongo redis caddy \
		|| fail "base image pull failed; nothing was changed"
else
	log "pulling images for $tag"
	compose pull --quiet "${APP_SERVICES[@]}" mongo redis caddy \
		|| fail "image pull failed; nothing was changed"
fi

# 7. Each new image validates its own config with --check-env (W-04).
#    One-off containers only; running services are untouched.
for svc in "${APP_SERVICES[@]}"; do
	check "$svc --check-env" compose run --rm --no-deps -T "$svc" --check-env
done
[ "$errors" -eq 0 ] || fail "$errors check(s) failed; nothing was changed"

log "pre-flight passed for IMAGE_TAG=$tag"
