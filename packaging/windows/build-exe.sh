#!/usr/bin/env bash
# build-exe.sh — build the Windows .exe with a full VERSIONINFO resource
# (CompanyName, FileVersion, LegalCopyright, OriginalFilename, ...) + an
# application manifest + the icon. Proper metadata makes the binary look like
# real software and reduces heuristic/ML antivirus false positives. (Code
# signing is still the real fix for SmartScreen — this just hardens the unsigned
# binary.)
#
# The resource is compiled with mingw's windres (so the COFF object links with
# the same toolchain). Works cross-compiled (macOS/Linux) and native on Windows.
set -euo pipefail

cd "$(dirname "$0")/../.." # repo root

if [ "$(go env GOHOSTOS)" = windows ]; then
	WINDRES="${WINDRES:-windres}"
	BUILD=(env CGO_ENABLED=1 GOOS=windows GOARCH=amd64)
else
	WINDRES="${WINDRES:-x86_64-w64-mingw32-windres}"
	BUILD=(env CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++)
fi

command -v "$WINDRES" >/dev/null 2>&1 || {
	echo "windres not found ($WINDRES) — install mingw-w64" >&2
	exit 1
}

# Compile the resource script to a COFF object; Go links any *_windows_amd64.syso
# in the main package automatically when building for windows/amd64.
"$WINDRES" -O coff -i packaging/windows/resource.rc -o resource_windows_amd64.syso

LDFLAGS="-s -w -H windowsgui"
OUT="sd2snes Covers.exe"
if [ "${DEBUG_CONSOLE:-0}" = 1 ]; then
	LDFLAGS=""
	OUT="sd2snes Covers (debug).exe"
fi

"${BUILD[@]}" go build -ldflags "$LDFLAGS" -o "$OUT" .

rm -f resource_windows_amd64.syso
echo "built: $OUT"
