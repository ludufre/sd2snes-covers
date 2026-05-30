#!/usr/bin/env bash
# prepare.sh — installs the toolchain to build sd2snes-covers and produce the
# release artifacts (.app / .exe / .AppImage).
#
# Usage: ./prepare.sh
set -euo pipefail

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m[ok]\033[0m %s\n' "$*"; }

OS="$(uname -s)"

# --- Go ---
if command -v go >/dev/null 2>&1; then
	ok "Go: $(go version)"
else
	info "installing Go"
	case "$OS" in
	Darwin) brew install go ;;
	Linux) sudo apt-get update && sudo apt-get install -y golang ;;
	*)
		warn "install Go manually: https://go.dev/dl/"
		exit 1
		;;
	esac
fi
export PATH="$PATH:$(go env GOPATH)/bin"

# --- Fyne packaging tool ---
info "installing the fyne tool (fyne.io/tools/cmd/fyne)"
go install fyne.io/tools/cmd/fyne@latest
ok "fyne: $(fyne version 2>/dev/null || echo installed)"

# --- mingw-w64 (cross-compiling the .exe from macOS/Linux) ---
if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
	ok "mingw-w64 present (Windows build)"
else
	info "installing mingw-w64 (Windows cross-compile)"
	case "$OS" in
	Darwin) brew install mingw-w64 ;;
	Linux) sudo apt-get update && sudo apt-get install -y gcc-mingw-w64-x86-64 ;;
	*) warn "install mingw-w64 manually for Windows builds" ;;
	esac
fi

# --- native Fyne libs (only when building on Linux itself) ---
if [ "$OS" = Linux ]; then
	info "installing build libs (OpenGL/X11) + squashfs-tools"
	sudo apt-get update && sudo apt-get install -y libgl1-mesa-dev xorg-dev squashfs-tools
fi

# --- Docker (required to build the .AppImage off Linux) ---
if command -v docker >/dev/null 2>&1; then
	if docker info >/dev/null 2>&1; then
		ok "Docker running (builds the Linux AppImage)"
	else
		warn "Docker installed but stopped — start Docker Desktop to build the .AppImage"
	fi
else
	warn "Docker missing — required to build the .AppImage off Linux"
fi

# PATH hint for the user's shell (fyne lives in GOPATH/bin)
case "${SHELL:-}" in
*/fish) warn "if the 'fyne' command isn't found later, run: fish_add_path (go env GOPATH)/bin" ;;
*) warn "if the 'fyne' command isn't found later, add to PATH: $(go env GOPATH)/bin" ;;
esac

ok "environment ready — now run ./build.sh"
