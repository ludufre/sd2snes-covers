#!/usr/bin/env bash
# build-app.sh — build a universal (arm64 + amd64) "sd2snes Covers.app" in the
# repo root, so a single macOS artifact runs on both Apple Silicon and Intel.
#
# Works on an Apple Silicon or Intel Mac: the Xcode clang/SDK is universal, so
# each arch is cross-compiled with `clang -arch <arch>` and merged with lipo.
set -euo pipefail

cd "$(dirname "$0")/../.." # repo root
ICON="packaging/icons/icon-macos.png"

mkdir -p build
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="clang -arch arm64" CXX="clang++ -arch arm64" \
	go build -o build/app-arm64 .
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="clang -arch x86_64" CXX="clang++ -arch x86_64" \
	go build -o build/app-amd64 .
lipo -create -output build/sd2snes-covers build/app-arm64 build/app-amd64

fyne package -os darwin -icon "$ICON" --executable build/sd2snes-covers
rm -rf build
