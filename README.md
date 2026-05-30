<div align="center">

<img src="Icon.png" width="120" alt="sd2snes Covers">

# sd2snes Covers

**Desktop app (Windows · macOS · Linux) that generates `.cov` cover files for [the sd2snes firmware fork](https://github.com/ludufre/sd2snes).**

Point it at a folder of Super Nintendo ROMs: it identifies each game by its CRC32, downloads the official box art from libretro, and converts it into the `.cov` format the firmware shows in its menu — all in one click. It can also rename ROMs to their No-Intro name and export a CSV.

</div>

---

> **What is this for?** The [ludufre/sd2snes](https://github.com/ludufre/sd2snes) firmware fork can display a game's cover in the file browser. This tool prepares those covers (`<rom>.cov`, next to each ROM) so you just copy them to the SD card. Downloading the box art and matching by CRC32 are the means; **the `.cov` files are the goal.**

<img src="https://raw.githubusercontent.com/ludufre/sd2snes/refs/heads/master/gfx/showcase.gif">

## ✨ Features

| Feature | Description |
|---|---|
| 🧩 **Generate `.cov`** ⭐ | Converts each cover into the firmware's `.cov` (8bpp BG cover) format, saved as `<rom>.cov` next to the ROM — **the main goal.** |
| 🔎 **Recursive scan** | Finds every `.sfc` / `.smc` in a folder and its subfolders. |
| 🧮 **Match by CRC32** | Identifies each game by its *headerless* CRC32 against the libretro No-Intro DAT (4256 games). |
| 🖼️ **Box art download** | Fetches the official `.png` from `thumbnails.libretro.com` (the source image for the `.cov`), saved next to the ROM. |
| ✏️ **Rename (No-Intro)** | Renames the ROM to its canonical No-Intro name (carrying the cover/`.cov` along). |
| 📄 **Export CSV** | A spreadsheet with the result for each ROM. |
| ⚙️ **Cross-platform** | Single binary per OS (Go + [Fyne](https://fyne.io)), no external runtime. |

<img src="packaging/screenshot.png" width="600" alt="Screenshot of the app running on macOS">

## 🚀 Usage

1. **Select ROM folder** — pick the folder (subfolders are scanned too).
2. Toggle the options you want: **Overwrite existing**, **Rename (No-Intro)**, **Generate .cov** (on by default).
3. **Start** — watch the progress bar and the per-ROM status in the table.
4. (Optional) **Export CSV** with the results.

The No-Intro DAT is downloaded once and cached; **Refresh DAT** forces a re-download. Use **Settings** to change the No-Intro DAT URL or the cover repository URL (saved across runs). A link to the [firmware fork](https://github.com/ludufre/sd2snes) sits at the bottom of the window.

Per-ROM status: **OK** · **404** (in the DAT, but no cover on the server) · **Skipped** (file already exists) · **No match** (CRC not in the DAT) · **Error** (network/IO).

## 📦 Install

Download the artifact for your OS from the [Releases](../../releases) page:

- **Windows** — `sd2snes-covers-windows-amd64.exe` (if it doesn't open, see [the note below](#windows--app-doesnt-open-no-window))
- **Windows (software OpenGL)** — `sd2snes-covers-windows-amd64-softgl.zip`, for machines with no GPU OpenGL driver (see [the note below](#windows--app-doesnt-open-no-window))
- **macOS** — `sd2snes-covers-macos-universal.zip` (Apple Silicon + Intel; see the Gatekeeper note below)
- **Linux** — `sd2snes-covers-x86_64.AppImage` (`chmod +x` and run)

### macOS — Gatekeeper

Unsigned binaries trigger Gatekeeper on first launch:

- Right-click the app → **Open** → **Open**; **or**
- `xattr -dr com.apple.quarantine "sd2snes Covers.app"`

To ship without that prompt, sign + notarize with an Apple Developer ID:

```sh
export APPLE_ID="you@example.com"
export APPLE_APP_SPECIFIC_PASSWORD="xxxx-xxxx-xxxx-xxxx"  # appleid.apple.com
export APPLE_TEAM_ID="XXXXXXXXXX"
NOTARIZE=1 ./build.sh mac        # fyne package → codesign → notarytool → staple
```

Requires a *Developer ID Application* certificate in your keychain; the steps live in `packaging/macos/notarize.sh`.

### Windows — app doesn't open (no window)

If the `.exe` shows up in Task Manager but **no window ever appears** — or the window **flashes and closes immediately** — your machine can't use a hardware OpenGL driver (Fyne needs OpenGL 2.1+). This happens when the display adapter is **"Microsoft Basic Render Driver"** (no GPU driver installed), and on **Remote Desktop**, **virtual machines** without 3D acceleration, and **old/driverless GPUs**.

Fix: download **`sd2snes-covers-windows-amd64-softgl.zip`** from [Releases](../../releases), extract both files (`sd2snes Covers.exe` + `opengl32.dll`) into the same folder, and just run the `.exe`. That bundled `opengl32.dll` is a software OpenGL renderer ([Mesa3D / llvmpipe](https://fdossena.com/?p=mesa/index.frag)) that Windows loads automatically because it sits next to the program — no launcher, no settings. Keep the two files together. If your machine actually has a GPU, the cleaner fix is to install its driver and use the normal `.exe` instead.

> To capture the exact error, a maintainer can build a console variant with `DEBUG_CONSOLE=1 bash packaging/windows/build-exe.sh` and run it from `cmd.exe`.

## 🛠️ Build from source

Requires [Go 1.24+](https://go.dev/dl/). Fyne uses CGO + OpenGL, so each OS needs its own C compiler.

### Run in development

```sh
go run .
```

### Produce the artifacts (quick way, from macOS)

```sh
./prepare.sh      # installs Go, fyne, mingw-w64 and checks Docker (once)
./build.sh        # builds .app + .exe + .AppImage into dist/
```

`./build.sh` accepts targets (`./build.sh mac win linux`) and `SKIP_TESTS=1` to skip `go vet`/`go test`.

### The manual steps behind the build

```sh
go install fyne.io/tools/cmd/fyne@latest

# macOS (.app, native)
fyne package -os darwin

# Windows (.exe, cross-compiled with mingw-w64)
env CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc fyne package -os windows

# Linux (.AppImage) — on Linux: run the script directly; on another OS, via Docker:
docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src \
  golang:1.24-bookworm bash packaging/linux/build-appimage.sh
```

### Release via CI

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which builds all three on native runners (the AppImage on Linux, no emulation) and publishes a GitHub Release:

```sh
git tag v1.0.0 && git push origin v1.0.0
```

## 🔬 How it works (technical notes)

- **Headerless CRC32** — SNES ROMs may carry a 512-byte copier header. It is detected by size (`size % 1024 == 512`) and skipped before the CRC32 (IEEE polynomial), because No-Intro checksums are computed without the header.
- **Box art name** — uses the No-Intro game name, replacing `` & * / : ` < > ? \ | " `` with `_` (libretro's filename rule) and then URL-encoding it.
- **Rename without leftovers** — when a ROM is renamed, its old-named `.png`/`.cov` siblings are carried to the new name; it never overwrites a different existing file.
- **`.cov` format** — a 12-byte header + BGR555 palette + 8bpp planar tiles (plane pairs 0&1/2&3/4&5/6&7), mapped into CGRAM. The cover fills the menu's header band (7 tiles = 56px) and its **width follows the source aspect ratio** (up to 32 tiles), with 128 colours @ CGRAM 128 and Floyd-Steinberg dithering.
  > The on-disk format is identical to `cover_conv.py` (validated by decoding it with the fork's `--verify`). The resize and quantization are an independent Go implementation (Catmull-Rom + median-cut), so the `.cov` is **firmware-compatible** but **not byte-for-byte identical** to the Python output.

## 🗂️ Project layout

```
main.go                       Entry point (Fyne app + window)
internal/snes/                Headerless CRC32
internal/scan/                Recursive .sfc/.smc discovery
internal/dat/                 DAT download/cache + clrmamepro parser
internal/thumbs/              Box art URL + download
internal/cov/                 .cov encoder/decoder
internal/pipeline/            Worker pool, rename, .cov, CSV, progress
internal/ui/                  Fyne interface
packaging/linux/              AppImage build (script + AppRun + .desktop)
packaging/icons/              Per-platform icons (square/macOS .png, .ico, .icns)
prepare.sh · build.sh         Environment setup and artifact builds
```

## ✅ Tests

```sh
go test ./...            # unit (offline)
go test ./... -run E2E   # includes end-to-end tests that download for real
```

They cover: header/CRC32 detection, the DAT parser, box art sanitization/URL, `.cov` encode/decode (planar round-trip, header, AutoWidth) and the full pipeline (match → cover → rename → `.cov`).

## 🙏 Credits

- Database and thumbnails: [libretro-database](https://github.com/libretro/libretro-database) / [No-Intro](https://no-intro.org/).
- GUI: [Fyne](https://fyne.io/).
- `.cov` format & target firmware: the [ludufre/sd2snes](https://github.com/ludufre/sd2snes) fork (`utils/cover_conv.py` / `src/cover.c`).
