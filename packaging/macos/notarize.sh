#!/usr/bin/env bash
# notarize.sh — codesign (hardened runtime), notarize and staple a macOS .app.
#
# Usage: packaging/macos/notarize.sh "sd2snes Covers.app"
#
# Credentials via env (same names as the deck project):
#   APPLE_ID                     Apple ID email
#   APPLE_APP_SPECIFIC_PASSWORD  app-specific password (appleid.apple.com)
#   APPLE_TEAM_ID                Apple Developer Team ID
# Optional:
#   APPLE_SIGN_IDENTITY          override the signing identity (default: first
#                                "Developer ID Application" found in the keychain)
#   NOTARY_PROFILE               use a stored `notarytool store-credentials`
#                                profile instead of APPLE_ID/password/team
set -euo pipefail

APP="${1:?usage: notarize.sh <App.app>}"
[ -d "$APP" ] || {
	echo "not a .app bundle: $APP" >&2
	exit 1
}

# --- signing identity (Developer ID Application) ---
IDENTITY="${APPLE_SIGN_IDENTITY:-}"
if [ -z "$IDENTITY" ]; then
	IDENTITY=$(security find-identity -v -p codesigning |
		sed -n 's/.*"\(Developer ID Application: .*\)"/\1/p' | head -1)
fi
[ -n "$IDENTITY" ] || {
	echo "no 'Developer ID Application' identity in the keychain (set APPLE_SIGN_IDENTITY)" >&2
	exit 1
}
echo "==> signing with: $IDENTITY"
codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP"
codesign --verify --strict --verbose=2 "$APP"

# --- notarize (zip, submit, wait for Apple) ---
ZIP="$(mktemp -d)/notarize.zip"
ditto -c -k --keepParent "$APP" "$ZIP"

echo "==> submitting to the Apple notary service (this waits for the result)..."
if [ -n "${NOTARY_PROFILE:-}" ]; then
	xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
else
	: "${APPLE_ID:?set APPLE_ID}"
	: "${APPLE_APP_SPECIFIC_PASSWORD:?set APPLE_APP_SPECIFIC_PASSWORD}"
	: "${APPLE_TEAM_ID:?set APPLE_TEAM_ID}"
	xcrun notarytool submit "$ZIP" \
		--apple-id "$APPLE_ID" \
		--password "$APPLE_APP_SPECIFIC_PASSWORD" \
		--team-id "$APPLE_TEAM_ID" \
		--wait
fi

# --- staple the ticket into the .app and verify ---
echo "==> stapling notarization ticket"
xcrun stapler staple "$APP"
spctl -a -vvv --type exec "$APP" || true
echo "==> done: $APP is signed, notarized and stapled"
