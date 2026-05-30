#!/usr/bin/env bash
# build.sh — builds sd2snes-covers and produces the artifacts in dist/.
#
# Usage:
#   ./build.sh                # everything the host can build (mac/win/linux)
#   ./build.sh mac win        # only the listed targets
#   SKIP_TESTS=1 ./build.sh   # skip go vet + go test
#
# Targets: mac (.app, macOS only) | win (.exe, mingw cross) | linux (.AppImage, via Docker)
set -euo pipefail

cd "$(dirname "$0")"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m[ok]\033[0m %s\n' "$*"; }

command -v go >/dev/null 2>&1 || {
	warn "Go not found — run ./prepare.sh"
	exit 1
}
export PATH="$PATH:$(go env GOPATH)/bin"

OS="$(uname -s)"
DIST="dist"
APPNAME="sd2snes Covers" # name in FyneApp.toml (used for the generated files)
mkdir -p "$DIST"

# targets from the arguments (default: all)
targets=("$@")
[ ${#targets[@]} -eq 0 ] && targets=(mac win linux)
want() {
	for t in "${targets[@]}"; do [ "$t" = "$1" ] && return 0; done
	return 1
}

have_fyne() {
	command -v fyne >/dev/null 2>&1 || {
		warn "'fyne' command not found — run ./prepare.sh"
		return 1
	}
}

# --- checks (skip with SKIP_TESTS=1) ---
if [ "${SKIP_TESTS:-0}" != 1 ]; then
	info "go vet"
	go vet ./...
	info "go test (short)"
	go test ./... -short
fi

build_mac() {
	if [ "$OS" != Darwin ]; then
		warn "skipping macOS (.app only builds on macOS)"
		return
	fi
	have_fyne || return
	info "macOS .app (universal: arm64 + amd64)"
	bash packaging/macos/build-app.sh
	if [ "${NOTARIZE:-0}" = 1 ]; then
		info "signing + notarizing (Developer ID)"
		bash packaging/macos/notarize.sh "$APPNAME.app"
	fi
	rm -f "$DIST/sd2snes-covers-macos-universal.zip"
	ditto -c -k --sequesterRsrc --keepParent "$APPNAME.app" "$DIST/sd2snes-covers-macos-universal.zip"
	rm -rf "$APPNAME.app"
	ok "$DIST/sd2snes-covers-macos-universal.zip"
}

build_win() {
	command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1 || {
		warn "skipping Windows (mingw-w64 missing — run ./prepare.sh)"
		return
	}
	info "Windows .exe (amd64, mingw cross, with version metadata)"
	bash packaging/windows/build-exe.sh
	mv -f "$APPNAME.exe" "$DIST/sd2snes-covers-windows-amd64.exe"
	ok "$DIST/sd2snes-covers-windows-amd64.exe"
}

build_linux() {
	info "Linux .AppImage (x86_64)"
	if [ "$OS" = Linux ]; then
		bash packaging/linux/build-appimage.sh
	elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
		docker run --rm --platform linux/amd64 \
			-v "$PWD":/src -w /src \
			-v "$HOME/go/pkg/mod":/go/pkg/mod \
			golang:1.24-bookworm bash packaging/linux/build-appimage.sh
	else
		warn "skipping Linux/AppImage (needs Docker running, or run on Linux)"
		return
	fi
	ok "$DIST/sd2snes-covers-x86_64.AppImage"
}

if want mac; then build_mac; fi
if want win; then build_win; fi
if want linux; then build_linux; fi

info "artifacts in $DIST/:"
ls -lh "$DIST"
