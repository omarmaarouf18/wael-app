#!/usr/bin/env bash
# Generates local-only mTLS certificates for wael-app compose stack.
# Output lands in this directory and is git-ignored (never committed).
set -euo pipefail

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
cd "$DIR"

echo "Generating local root CA..."
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 825 -out ca.crt -subj "/CN=Wael-App-Local-Root-CA"

SERVICES=("api-gateway" "auth-service" "notification-service" "academy-service")

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
