#!/usr/bin/env bash
# Self-test for infrastructure/certs/generate-certs.sh, in particular the
# --sign-only mode that signs one certificate with an EXISTING CA and must
# leave the CA, its directory and every other certificate untouched.
#
# Every case runs a copy of the real script inside a throwaway directory, so
# the working tree (and any real certificates) are never touched.
#
# Usage: scripts/generate_certs_test.sh
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT_SRC="${SCRIPT_SRC:-$REPO_ROOT/infrastructure/certs/generate-certs.sh}"
WORK="$(mktemp -d)"
trap 'chmod -R u+rwx "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
OUT=""
RC=0

ok() { passed=$((passed + 1)); echo "ok   - $1"; }
bad() { failed=$((failed + 1)); echo "FAIL - $1"; [ -z "${2:-}" ] || printf '%s\n' "$2" | sed 's/^/       | /'; }

# assert <description> <command...>: passes when the command succeeds.
assert() {
	local desc="$1"; shift
	if "$@" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi
}

# new_dir <name>: a fresh directory holding a copy of the script; echoes its path.
new_dir() {
	local d="$WORK/$1"
	mkdir -p "$d"
	cp "$SCRIPT_SRC" "$d/generate-certs.sh"
	echo "$d"
}

# run <dir> <args...>: run the copy in <dir>, capture output and exit code.
run() {
	local d="$1"; shift
	RC=0
	OUT="$(cd "$d" && bash ./generate-certs.sh "$@" 2>&1)" || RC=$?
}

check() { # check <description> <expected-rc> [expected-output-substring]
	local desc="$1" want_rc="$2" want_out="${3:-}"
	if [ "$RC" -eq "$want_rc" ] && { [ -z "$want_out" ] || [[ "$OUT" == *"$want_out"* ]]; }; then
		ok "$desc"
	else
		bad "$desc (exit $RC, wanted $want_rc${want_out:+, output containing \"$want_out\"})" "$OUT"
	fi
}

# snapshot <dir>: names, modes, sizes and sha256 of every file, for before/after comparison.
snapshot() {
	(cd "$1" && find . -type f -printf '%P %m\n' | sort | while read -r f m; do
		printf '%s %s %s\n' "$f" "$m" "$(sha256sum "$f" | cut -d' ' -f1)"
	done)
}

pubkey_of_cert() { openssl x509 -in "$1" -noout -pubkey | openssl sha256; }
pubkey_of_key() { openssl pkey -in "$1" -pubout | openssl sha256; }
san_of() { openssl x509 -in "$1" -noout -ext subjectAltName; }
serial_of() { openssl x509 -in "$1" -noout -serial; }

SERVICES=(api-gateway auth-service notification-service academy-service admin-console)

# ---------------------------------------------------------------------------
# 1. Full run: every service, including admin-console, gets a certificate.
# ---------------------------------------------------------------------------
FULL="$(new_dir full)"
run "$FULL"
check "full run succeeds" 0 "Certificates generated successfully"
for svc in "${SERVICES[@]}"; do
	assert "full run: $svc.crt verifies against ca.crt" openssl verify -CAfile "$FULL/ca.crt" "$FULL/$svc.crt"
	assert "full run: $svc.key matches $svc.crt" test "$(pubkey_of_cert "$FULL/$svc.crt")" = "$(pubkey_of_key "$FULL/$svc.key")"
	assert "full run: $svc.crt names DNS:$svc" bash -c "openssl x509 -in '$FULL/$svc.crt' -noout -ext subjectAltName | grep -q 'DNS:$svc'"
done
assert "full run: external gateway certificate exists" test -s "$FULL/api-gateway-external.crt"

# ---------------------------------------------------------------------------
# 2. --sign-only with a separate output directory leaves the CA directory
#    exactly as it was (production: the CA lives on offline media).
# ---------------------------------------------------------------------------
CA_ONLY="$WORK/offline-ca"
mkdir -p "$CA_ONLY"
cp "$FULL/ca.crt" "$FULL/ca.key" "$CA_ONLY/"
chmod 400 "$CA_ONLY/ca.key"
chmod 500 "$CA_ONLY"   # read-only directory: a write there would fail loudly
BEFORE_CA="$(snapshot "$CA_ONLY")"
SIGN="$(new_dir sign)"
mkdir -p "$WORK/signed-out"
run "$SIGN" --sign-only academy-service --ca-dir "$CA_ONLY" --out-dir "$WORK/signed-out"
check "sign-only succeeds with a read-only CA directory" 0 "Signed academy-service"
[ "$BEFORE_CA" = "$(snapshot "$CA_ONLY")" ] && ok "sign-only: CA directory is byte-identical afterwards" || bad "sign-only: CA directory changed"
assert "sign-only: no serial file was written to the CA directory" test ! -e "$CA_ONLY/ca.srl"
assert "sign-only: ca.key mode unchanged (400)" test "$(stat -c %a "$CA_ONLY/ca.key")" = "400"
assert "sign-only: output holds exactly the new key and certificate" test "$(ls -A "$WORK/signed-out" | sort | tr '\n' ' ')" = "academy-service.crt academy-service.key "
assert "sign-only: certificate verifies against the existing CA" openssl verify -CAfile "$CA_ONLY/ca.crt" "$WORK/signed-out/academy-service.crt"
assert "sign-only: key matches certificate" test "$(pubkey_of_cert "$WORK/signed-out/academy-service.crt")" = "$(pubkey_of_key "$WORK/signed-out/academy-service.key")"
assert "sign-only: certificate names DNS:academy-service" bash -c "openssl x509 -in '$WORK/signed-out/academy-service.crt' -noout -ext subjectAltName | grep -q 'DNS:academy-service'"
assert "sign-only: certificate is not a CA" bash -c "openssl x509 -in '$WORK/signed-out/academy-service.crt' -noout -ext basicConstraints | grep -q 'CA:FALSE'"
assert "sign-only: certificate subject is CN=academy-service" bash -c "openssl x509 -in '$WORK/signed-out/academy-service.crt' -noout -subject | grep -q 'CN *= *academy-service'"
assert "sign-only: serial differs from the earlier certificate" test "$(serial_of "$WORK/signed-out/academy-service.crt")" != "$(serial_of "$FULL/academy-service.crt")"
assert "sign-only: certificate and key are world-readable like the full run (644)" test "$(stat -c %a "$WORK/signed-out/academy-service.crt") $(stat -c %a "$WORK/signed-out/academy-service.key")" = "644 644"
chmod 700 "$CA_ONLY"

# ---------------------------------------------------------------------------
# 3. Production shape: certificates already deployed, admin-console added later.
#    Default output directory (the script's own), CA in a different directory.
# ---------------------------------------------------------------------------
PROD="$(new_dir prod)"
for f in ca.crt "${SERVICES[@]/%/.crt}" "${SERVICES[@]/%/.key}"; do
	case "$f" in admin-console.*) continue ;; esac
	cp "$FULL/$f" "$PROD/$f"
done
BEFORE_PROD="$(snapshot "$PROD")"
run "$PROD" --sign-only admin-console --ca-dir "$CA_ONLY"
check "production shape: admin-console certificate signed into the existing directory" 0 "Signed admin-console"
assert "production shape: admin-console.crt verifies against the existing CA" openssl verify -CAfile "$PROD/ca.crt" "$PROD/admin-console.crt"
AFTER_PROD="$(snapshot "$PROD")"
UNCHANGED="$(comm -12 <(printf '%s\n' "$BEFORE_PROD") <(printf '%s\n' "$AFTER_PROD") | wc -l)"
TOTAL_BEFORE="$(printf '%s\n' "$BEFORE_PROD" | wc -l)"
# the script copy itself is in the directory too; every file that existed before must be identical.
[ "$UNCHANGED" -eq "$TOTAL_BEFORE" ] && ok "production shape: every existing file is byte-identical (CA, other certificates and keys)" || bad "production shape: an existing file changed" "$(diff <(printf '%s\n' "$BEFORE_PROD") <(printf '%s\n' "$AFTER_PROD"))"
assert "production shape: only admin-console.crt and admin-console.key were added" test "$(printf '%s\n' "$AFTER_PROD" | wc -l)" -eq "$((TOTAL_BEFORE + 2))"
assert "production shape: no ca.key was created in the deployment directory" test ! -e "$PROD/ca.key"

# ---------------------------------------------------------------------------
# 4. Refusals leave everything alone.
# ---------------------------------------------------------------------------
REF="$(new_dir refuse)"
OKCA="$WORK/ok-ca"; mkdir -p "$OKCA"; cp "$FULL/ca.crt" "$FULL/ca.key" "$OKCA/"

run "$REF" --sign-only not-a-service --ca-dir "$OKCA"
check "unknown service is refused" 2 "unknown service"
run "$REF" --sign-only ../etc --ca-dir "$OKCA"
check "path-like service name is refused" 2 "unknown service"
run "$REF" --sign-only admin-console
check "--sign-only without --ca-dir is refused" 2 "requires --ca-dir"
run "$REF" --sign-only
check "--sign-only without a value is refused" 2 "needs a service name"
run "$REF" --sign-only admin-console --ca-dir "$WORK/no-such-dir"
check "missing CA directory is refused" 1 "not found or not readable"
NOKEY="$WORK/no-key"; mkdir -p "$NOKEY"; cp "$FULL/ca.crt" "$NOKEY/"
run "$REF" --sign-only admin-console --ca-dir "$NOKEY"
check "CA directory without ca.key is refused" 1 "ca.key not found"
OTHER="$(new_dir other-full)"; run "$OTHER"
MISMATCH="$WORK/mismatch"; mkdir -p "$MISMATCH"; cp "$FULL/ca.crt" "$MISMATCH/ca.crt"; cp "$OTHER/ca.key" "$MISMATCH/ca.key"
run "$REF" --sign-only admin-console --ca-dir "$MISMATCH"
check "ca.key that does not match ca.crt is refused" 1 "does not match"
run "$REF" --sign-only admin-console --ca-dir "$OKCA" --out-dir "$WORK/no-such-out"
check "missing output directory is refused" 1 "does not exist"
assert "refusals wrote nothing into the working directory" test "$(ls -A "$REF" | tr '\n' ' ')" = "generate-certs.sh "

# An expired CA must not sign anything.
EXP="$WORK/expired-ca"; mkdir -p "$EXP/db/new"
openssl genrsa -out "$EXP/ca.key" 2048 2>/dev/null
openssl req -new -key "$EXP/ca.key" -out "$EXP/ca.csr" -subj "/CN=expired-test-ca" 2>/dev/null
: >"$EXP/db/index.txt"; echo 01 >"$EXP/db/serial"
cat >"$EXP/ca.cnf" <<CNF
[ca]
default_ca = CA_default
[CA_default]
database = $EXP/db/index.txt
new_certs_dir = $EXP/db/new
serial = $EXP/db/serial
default_md = sha256
policy = policy_any
unique_subject = no
copy_extensions = none
[policy_any]
commonName = supplied
[v3_ca]
basicConstraints = critical,CA:TRUE
CNF
if openssl ca -batch -selfsign -config "$EXP/ca.cnf" -keyfile "$EXP/ca.key" -in "$EXP/ca.csr" -out "$EXP/ca.crt" \
	-startdate 20200101000000Z -enddate 20200102000000Z -extensions v3_ca -notext >/dev/null 2>&1; then
	assert "test setup: the expired CA is really expired" bash -c "! openssl x509 -in '$EXP/ca.crt' -noout -checkend 0 >/dev/null"
	run "$REF" --sign-only admin-console --ca-dir "$EXP"
	check "an expired CA is refused by an explicit check" 1 "the CA certificate has expired"
else
	bad "test setup: could not create an expired CA with openssl ca"
fi

# Existing certificate: refused without --force, replaced with it, nothing else touched.
EX="$(new_dir existing)"
cp "$FULL/admin-console.crt" "$FULL/admin-console.key" "$EX/"
HASH_BEFORE="$(sha256sum "$EX/admin-console.crt" | cut -d' ' -f1)"
run "$EX" --sign-only admin-console --ca-dir "$OKCA"
check "an existing certificate is not overwritten without --force" 1 "already exists"
assert "existing certificate left unchanged after the refusal" test "$(sha256sum "$EX/admin-console.crt" | cut -d' ' -f1)" = "$HASH_BEFORE"
run "$EX" --sign-only admin-console --ca-dir "$OKCA" --force
check "--force replaces the existing certificate" 0 "Signed admin-console"
assert "--force produced a different certificate" test "$(sha256sum "$EX/admin-console.crt" | cut -d' ' -f1)" != "$HASH_BEFORE"
assert "--force: new certificate still verifies against the CA" openssl verify -CAfile "$FULL/ca.crt" "$EX/admin-console.crt"

# Options that only make sense with --sign-only must not fall through to a full run.
FRESH="$(new_dir fresh)"
run "$FRESH" --ca-dir "$OKCA"
check "--ca-dir without --sign-only is refused" 2 "only apply with --sign-only"
run "$FRESH" --force
check "--force without --sign-only is refused" 2 "only apply with --sign-only"
run "$FRESH" --out-dir "$WORK"
check "--out-dir without --sign-only is refused" 2 "only apply with --sign-only"
run "$FRESH" --bogus
check "an unknown flag is refused" 2 "unknown argument"
assert "none of these generated a CA" test ! -e "$FRESH/ca.key"
run "$FRESH" --help
check "--help prints usage and exits 0" 0 "--sign-only"

echo
echo "$passed passed, $failed failed"
[ "$failed" -eq 0 ]
