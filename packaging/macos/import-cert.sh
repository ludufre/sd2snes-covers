#!/usr/bin/env bash
# import-cert.sh — import the "Developer ID Application" certificate into a
# temporary keychain on a CI macOS runner, from the same secrets the deck
# project uses:
#   CSC_LINK          base64 of the exported .p12 certificate
#   CSC_KEY_PASSWORD  password of that .p12
#
# After this runs, `security find-identity` (used by notarize.sh) finds the
# identity and codesign can use it.
set -euo pipefail

: "${CSC_LINK:?set CSC_LINK (base64 of the .p12)}"
: "${CSC_KEY_PASSWORD:?set CSC_KEY_PASSWORD}"

TMP="${RUNNER_TEMP:-/tmp}"
KEYCHAIN="$TMP/signing.keychain-db"
KEYCHAIN_PW="$(openssl rand -hex 20)"
CERT="$TMP/cert.p12"

printf '%s' "$CSC_LINK" | base64 --decode >"$CERT"

security create-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"
security set-keychain-settings -lut 21600 "$KEYCHAIN" # don't auto-lock during the job
security unlock-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"
security import "$CERT" -k "$KEYCHAIN" -P "$CSC_KEY_PASSWORD" -T /usr/bin/codesign -T /usr/bin/security
security set-key-partition-list -S apple-tool:,apple: -k "$KEYCHAIN_PW" "$KEYCHAIN" >/dev/null
# put our keychain on the search list (alongside the login keychain) so codesign finds it
security list-keychains -d user -s "$KEYCHAIN" login.keychain-db
rm -f "$CERT"

echo "Developer ID certificate imported:"
security find-identity -v -p codesigning | grep "Developer ID Application" || true
