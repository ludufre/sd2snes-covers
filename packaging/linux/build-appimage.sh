#!/usr/bin/env bash
# Builds a Linux AppImage. Designed to run inside a Linux container (or a real
# Linux machine / CI runner) with the repo mounted/checked out at the CWD.
# Output: dist/sd2snes-covers-<arch>.AppImage
set -euo pipefail

ARCH="$(uname -m)" # x86_64 | aarch64

SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo"; fi

if command -v apt-get >/dev/null 2>&1; then
	$SUDO apt-get update -qq
	$SUDO apt-get install -y -qq libgl1-mesa-dev xorg-dev wget file ca-certificates squashfs-tools >/dev/null
fi

CGO_ENABLED=1 go build -o /tmp/sd2snes-covers .

APPDIR=/tmp/AppDir
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin"
cp /tmp/sd2snes-covers "$APPDIR/usr/bin/sd2snes-covers"
cp Icon.png "$APPDIR/sd2snes-covers.png"
cp "$APPDIR/sd2snes-covers.png" "$APPDIR/.DirIcon"
cp packaging/linux/sd2snes-covers.desktop "$APPDIR/"
cp packaging/linux/AppRun "$APPDIR/AppRun"
chmod +x "$APPDIR/AppRun"

case "$ARCH" in
	x86_64) RT_ARCH=x86_64 ;;
	aarch64) RT_ARCH=aarch64 ;;
	*) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

# Assemble the AppImage WITHOUT FUSE/appimagetool: an AppImage is just the
# official runtime followed by a squashfs image. This is what appimagetool does
# internally, but it works in containers and CI runners that lack FUSE.
wget -q "https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-${RT_ARCH}" -O /tmp/runtime
mksquashfs "$APPDIR" /tmp/app.squashfs -root-owned -noappend -comp zstd -mkfs-time 0

mkdir -p dist
cat /tmp/runtime /tmp/app.squashfs > "dist/sd2snes-covers-${ARCH}.AppImage"
chmod +x "dist/sd2snes-covers-${ARCH}.AppImage"
echo "AppImage written: dist/sd2snes-covers-${ARCH}.AppImage"
