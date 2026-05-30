#!/usr/bin/env bash
# build-softgl-zip.sh — assemble the "software OpenGL" Windows variant.
#
# Usage: build-softgl-zip.sh <path-to-built-exe> <output-zip>
set -euo pipefail

cd "$(dirname "$0")/../.." # repo root

EXE="${1:?usage: build-softgl-zip.sh <exe> <output.zip>}"
OUT="${2:?usage: build-softgl-zip.sh <exe> <output.zip>}"
ARCHIVE="packaging/windows/mesa-opengl32-softgl.7z"

# make OUT absolute (we cd into the stage dir before zipping)
OUT="$(cd "$(dirname "$OUT")" && pwd)/$(basename "$OUT")"

# pick a 7-Zip CLI: 7z on Windows/CI, 7zz from Homebrew on macOS.
SEVENZIP=""
for c in 7z 7zz 7za; do
	if command -v "$c" >/dev/null 2>&1; then SEVENZIP="$c"; break; fi
done
[ -n "$SEVENZIP" ] || {
	echo "need 7-Zip (7z / 7zz) to extract $ARCHIVE" >&2
	exit 1
}

STAGE="$(dirname "$OUT")/.softgl-stage"
rm -rf "$STAGE"
mkdir -p "$STAGE"

# extract only the software opengl32.dll from the vendored archive
"$SEVENZIP" e -y -o"$STAGE" "$ARCHIVE" opengl32.dll >/dev/null
test -f "$STAGE/opengl32.dll"

cp "$EXE" "$STAGE/sd2snes Covers.exe"
cp packaging/windows/SOFTWARE-OPENGL-README.txt "$STAGE/README.txt"

rm -f "$OUT"
(cd "$STAGE" && "$SEVENZIP" a -tzip "$OUT" "./*" >/dev/null)
rm -rf "$STAGE"
echo "built: $OUT"
