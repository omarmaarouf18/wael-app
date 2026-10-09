#!/usr/bin/env bash
# iOS App Store signing material without a Mac (Linux + OpenSSL only).
# Guide: docs/frontend/IOS-RELEASE.md. Everything is written OUTSIDE the
# repository, to $IOS_SIGNING_DIR (default ~/ios-signing, mode 700). Never
# commit or share those files; they become GitHub secrets of wael-app-mobile.
#
#   scripts/ios_signing.sh csr "Your Name" you@example.com
#       private key + certificate signing request (upload the .csr at
#       developer.apple.com > Certificates > + > Apple Distribution)
#   scripts/ios_signing.sh p12 ~/Downloads/distribution.cer
#       the .cer Apple gives back + the private key -> password-protected .p12
#   scripts/ios_signing.sh profile ~/Downloads/<name>.mobileprovision
#   scripts/ios_signing.sh apikey ~/Downloads/AuthKey_<KEYID>.p8
#       base64 copies ready to paste as secrets
#   scripts/ios_signing.sh secrets [owner/wael-app-mobile]
#       sets every secret that exists with the GitHub CLI (gh), or prints
#       which files to paste by hand when gh is not installed
set -euo pipefail

DIR="${IOS_SIGNING_DIR:-$HOME/ios-signing}"
KEY="$DIR/ios_distribution.key"
umask 077
mkdir -p "$DIR"
chmod 700 "$DIR"

die() { echo "ios_signing: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null || die "$1 is required"; }
b64() { base64 -w0 "$1" > "$2"; echo "wrote $2"; }
need openssl
need base64

cmd="${1:-}"
case "$cmd" in
  csr)
    name="${2:-}"; email="${3:-}"
    [ -n "$name" ] && [ -n "$email" ] || die 'usage: csr "Your Name" you@example.com'
    [ -e "$KEY" ] && die "$KEY already exists; move it away first (it may belong to a live certificate)"
    openssl genrsa -out "$KEY" 2048 2>/dev/null
    openssl req -new -key "$KEY" -out "$DIR/ios_distribution.csr" \
      -subj "/emailAddress=${email}/CN=${name}/C=EG"
    echo "wrote $KEY (keep it: the certificate is useless without it)"
    echo "wrote $DIR/ios_distribution.csr -> upload it as an Apple Distribution certificate"
    ;;
  p12)
    cer="${2:-}"
    [ -f "$cer" ] || die "usage: p12 path/to/distribution.cer"
    [ -f "$KEY" ] || die "$KEY not found; the .cer must come from the csr made by this script"
    if ! openssl x509 -inform DER -in "$cer" -out "$DIR/ios_distribution.pem" 2>/dev/null; then
      openssl x509 -in "$cer" -out "$DIR/ios_distribution.pem"
    fi
    # The certificate must belong to our private key.
    [ "$(openssl x509 -in "$DIR/ios_distribution.pem" -noout -pubkey)" = "$(openssl pkey -in "$KEY" -pubout)" ] \
      || die "this certificate was not made from $KEY"
    openssl x509 -in "$DIR/ios_distribution.pem" -noout -subject -enddate
    pass="$(openssl rand -hex 16)"
    # SHA1/3DES PKCS#12: the format macOS 'security import' reads reliably
    # (OpenSSL 3's AES default is refused by some macOS versions).
    p12_args=(-export -inkey "$KEY" -in "$DIR/ios_distribution.pem"
      -name "Apple Distribution" -out "$DIR/ios_distribution.p12"
      -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1
      -passout "pass:$pass")
    # A strict system crypto policy can refuse SHA1/3DES; -legacy allows it.
    openssl pkcs12 "${p12_args[@]}" 2>/dev/null || openssl pkcs12 -legacy "${p12_args[@]}"
    printf '%s' "$pass" > "$DIR/IOS_DIST_CERT_PASSWORD.txt"
    b64 "$DIR/ios_distribution.p12" "$DIR/IOS_DIST_CERT_P12_BASE64.txt"
    echo "wrote $DIR/IOS_DIST_CERT_PASSWORD.txt"
    ;;
  profile)
    f="${2:-}"; [ -f "$f" ] || die "usage: profile path/to/profile.mobileprovision"
    b64 "$f" "$DIR/IOS_PROFILE_BASE64.txt"
    ;;
  apikey)
    f="${2:-}"; [ -f "$f" ] || die "usage: apikey path/to/AuthKey_KEYID.p8"
    base="$(basename "$f")"
    case "$base" in AuthKey_*.p8) id="${base#AuthKey_}"; id="${id%.p8}" ;; *) id="" ;; esac
    b64 "$f" "$DIR/APPSTORE_API_KEY_P8_BASE64.txt"
    if [ -n "$id" ]; then printf '%s' "$id" > "$DIR/APPSTORE_API_KEY_ID.txt"; echo "wrote $DIR/APPSTORE_API_KEY_ID.txt ($id)"; fi
    echo "APPSTORE_API_ISSUER_ID: copy the Issuer ID from App Store Connect > Users and Access > Integrations"
    ;;
  secrets)
    repo="${2:-}"
    names="IOS_DIST_CERT_P12_BASE64 IOS_DIST_CERT_PASSWORD IOS_PROFILE_BASE64 APPSTORE_API_KEY_P8_BASE64 APPSTORE_API_KEY_ID"
    if command -v gh >/dev/null && [ -n "$repo" ]; then
      for n in $names; do
        if [ -f "$DIR/$n.txt" ]; then gh secret set "$n" --repo "$repo" < "$DIR/$n.txt"; echo "set $n"; else echo "skip $n (no $DIR/$n.txt)"; fi
      done
      echo "Set APPSTORE_API_ISSUER_ID yourself: gh secret set APPSTORE_API_ISSUER_ID --repo $repo"
    else
      echo "Paste each file's content as a secret of wael-app-mobile (Settings > Secrets and variables > Actions):"
      for n in $names; do echo "  $n  <-  $DIR/$n.txt"; done
      echo "  APPSTORE_API_ISSUER_ID  <-  App Store Connect > Users and Access > Integrations"
    fi
    ;;
  *)
    sed -n '2,20p' "$0"; exit 2 ;;
esac
