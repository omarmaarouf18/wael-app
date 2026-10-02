#!/usr/bin/env bash
# Generates mTLS certificates for the wael-app services.
# Output lands in this directory (or --out-dir) and is git-ignored (never committed).
#
# Full run (default):
#   generate-certs.sh
#   Creates a NEW root CA and a certificate for every service. Existing files
#   with the same names are REPLACED. Local development and CI only.
#
# Sign one certificate with an existing CA:
#   generate-certs.sh --sign-only <service> --ca-dir <dir> [--out-dir <dir>] [--force]
#   Signs a certificate for <service> with the CA in <dir> (ca.crt and ca.key).
#   Nothing else is generated or replaced, and <dir> is only read: the CA files
#   are not modified and no serial file is written there. Use this for
#   production, where the CA key stays offline and a full run would replace
#   every certificate. Refuses to overwrite an existing <service>.crt/.key in
#   the output directory unless --force is given.
set -euo pipefail

SERVICES=("api-gateway" "auth-service" "notification-service" "academy-service" "admin-console")

usage() {
  awk '/^set -euo/ { exit } NR > 1 { sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}" >&2
}

is_service() {
  local candidate="$1" s
  for s in "${SERVICES[@]}"; do
    [ "$s" = "$candidate" ] && return 0
  done
  return 1
}

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

SIGN_ONLY=""
CA_DIR=""
OUT_DIR="$DIR"
OUT_DIR_GIVEN=0
FORCE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --sign-only) [ $# -ge 2 ] || { echo "error: --sign-only needs a service name" >&2; usage; exit 2; }
                 SIGN_ONLY="$2"; shift 2 ;;
    --ca-dir)    [ $# -ge 2 ] || { echo "error: --ca-dir needs a directory" >&2; usage; exit 2; }
                 CA_DIR="$2"; shift 2 ;;
    --out-dir)   [ $# -ge 2 ] || { echo "error: --out-dir needs a directory" >&2; usage; exit 2; }
                 OUT_DIR="$2"; OUT_DIR_GIVEN=1; shift 2 ;;
    --force)     FORCE=1; shift ;;
    -h|--help)   usage; exit 0 ;;
    *)           echo "error: unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

# --- sign-only mode ---------------------------------------------------------
if [ -n "$SIGN_ONLY" ]; then
  is_service "$SIGN_ONLY" || { echo "error: unknown service '$SIGN_ONLY' (expected one of: ${SERVICES[*]})" >&2; exit 2; }
  [ -n "$CA_DIR" ] || { echo "error: --sign-only requires --ca-dir (the directory holding ca.crt and ca.key)" >&2; exit 2; }
  [ -r "$CA_DIR/ca.crt" ] || { echo "error: $CA_DIR/ca.crt not found or not readable" >&2; exit 1; }
  [ -r "$CA_DIR/ca.key" ] || { echo "error: $CA_DIR/ca.key not found or not readable" >&2; exit 1; }
  [ -d "$OUT_DIR" ] || { echo "error: output directory $OUT_DIR does not exist" >&2; exit 1; }

  openssl x509 -in "$CA_DIR/ca.crt" -noout 2>/dev/null \
    || { echo "error: $CA_DIR/ca.crt is not a valid certificate" >&2; exit 1; }
  openssl x509 -in "$CA_DIR/ca.crt" -noout -checkend 0 >/dev/null \
    || { echo "error: the CA certificate has expired" >&2; exit 1; }

  # The key must belong to the certificate, or every signature would be invalid.
  ca_cert_pub="$(openssl x509 -in "$CA_DIR/ca.crt" -noout -pubkey | openssl sha256)"
  ca_key_pub="$(openssl pkey -in "$CA_DIR/ca.key" -pubout 2>/dev/null | openssl sha256)" \
    || { echo "error: $CA_DIR/ca.key is not a usable private key" >&2; exit 1; }
  [ "$ca_cert_pub" = "$ca_key_pub" ] \
    || { echo "error: $CA_DIR/ca.key does not match $CA_DIR/ca.crt" >&2; exit 1; }

  if [ "$FORCE" -ne 1 ]; then
    for f in "$OUT_DIR/$SIGN_ONLY.crt" "$OUT_DIR/$SIGN_ONLY.key"; do
      [ ! -e "$f" ] || { echo "error: $f already exists; pass --force to replace it" >&2; exit 1; }
    done
  fi

  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT

  openssl genrsa -out "$work/$SIGN_ONLY.key" 2048 2>/dev/null
  openssl req -new -key "$work/$SIGN_ONLY.key" -out "$work/$SIGN_ONLY.csr" \
    -subj "/CN=${SIGN_ONLY}"
  cat <<EOF > "$work/$SIGN_ONLY.ext"
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = DNS:${SIGN_ONLY}, DNS:localhost, IP:127.0.0.1
EOF
  # A random serial instead of -CAcreateserial: the CA directory stays untouched.
  openssl x509 -req -in "$work/$SIGN_ONLY.csr" \
    -CA "$CA_DIR/ca.crt" -CAkey "$CA_DIR/ca.key" -set_serial "0x$(openssl rand -hex 15)" \
    -out "$work/$SIGN_ONLY.crt" -days 825 -sha256 -extfile "$work/$SIGN_ONLY.ext" 2>/dev/null

  openssl verify -CAfile "$CA_DIR/ca.crt" "$work/$SIGN_ONLY.crt" >/dev/null \
    || { echo "error: the new certificate does not verify against the CA" >&2; exit 1; }

  # Same permissions as the full run: containers read the key (see RUNBOOK, W-10).
  install -m 644 "$work/$SIGN_ONLY.key" "$OUT_DIR/$SIGN_ONLY.key"
  install -m 644 "$work/$SIGN_ONLY.crt" "$OUT_DIR/$SIGN_ONLY.crt"

  echo "Signed $SIGN_ONLY with the CA in $CA_DIR."
  echo "Wrote $OUT_DIR/$SIGN_ONLY.crt and $OUT_DIR/$SIGN_ONLY.key (CA and other certificates untouched)."
  exit 0
fi

if [ -n "$CA_DIR" ] || [ "$OUT_DIR_GIVEN" -eq 1 ] || [ "$FORCE" -eq 1 ]; then
  echo "error: --ca-dir, --out-dir and --force only apply with --sign-only" >&2
  usage
  exit 2
fi

# --- full run -------------------------------------------------------------
cd "$DIR"

echo "Generating local root CA..."
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 825 -out ca.crt -subj "/CN=Wael-App-Local-Root-CA"

for service in "${SERVICES[@]}"; do
  echo "Generating certificate for service: $service..."
  openssl genrsa -out "${service}.key" 2048

  openssl req -new -key "${service}.key" -out "${service}.csr" \
    -subj "/CN=${service}" \
    -addext "subjectAltName = DNS:${service}, DNS:localhost, IP:127.0.0.1"

  cat <<EOF > "${service}.ext"
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = DNS:${service}, DNS:localhost, IP:127.0.0.1
EOF

  openssl x509 -req -in "${service}.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "${service}.crt" -days 825 -sha256 -extfile "${service}.ext"

  rm -f "${service}.csr" "${service}.ext"
done

echo "Generating public/external gateway certificate (localhost only, no domain yet)..."
openssl req -x509 -newkey rsa:2048 -nodes -keyout api-gateway-external.key -out api-gateway-external.crt -days 825 -subj "/CN=localhost" -addext "subjectAltName = DNS:localhost, IP:127.0.0.1"

chmod 644 *.key
chmod 644 *.crt

echo "Certificates generated successfully in $DIR (untracked, do not commit)."
