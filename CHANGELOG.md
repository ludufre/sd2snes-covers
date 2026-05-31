# Changelog

All notable changes to **sd2snes Covers** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [1.2.0] — 2026-05-31

### Added

- **"Just convert to .cov"** button: pick a folder of images and convert each one
  straight to a `.cov` next to it (e.g. `cover.png` → `cover.cov`) — no CRC32, no
  DAT, no box-art download. For art you already have on disk. Accepts **PNG, JPG
  and BMP**, scans subfolders, and honors **Overwrite existing**.

## [1.1.0] — 2026-05-30

### ⚠️ Breaking

- **New `.cov` (OBJ-sprite) format.** Covers are now drawn as 16×16 OBJ
  sprites floating over the file list (fixed 8×6 grid, 128×96 px, 4bpp, up to 8
  palettes). This **requires the sd2snes firmware `v1.11.2-br-2.1` or newer** and
  is **not compatible** with tile covers from earlier versions — existing
  `.cov` files must be regenerated.

### Added

- **Cover preview** panel: the box art and a render of how it will look as a
  `.cov` on the sd2snes, loaded in parallel (the box art shows immediately).
- **Persistent box-art cache** between runs (re-runs and duplicate ROMs no longer
  re-download); **Settings → Clear cover cache** empties it.
- **CSV**: a **Boxart URL** column (handy for *Not found* games) and
  Excel/LibreOffice-friendly output (UTF-8 BOM + `sep=,` hint + CRLF).
- **Native OS file dialogs** for the ROM folder and the CSV save, each
  remembering the last used directory.
- **In-app update checker**: shows a download link in the status bar when a newer
  release is published.
- **Windows software-OpenGL build** (`sd2snes-covers-windows-amd64-softgl.zip`)
  for machines with no GPU OpenGL driver ("Microsoft Basic Render Driver", RDP,
  VMs).
- Click any table cell to **copy its full text** to the clipboard.

### Changed

- **"Overwrite existing"** now considers the `.cov`: a ROM is skipped when its
  `.cov` already exists (previously only the `.png` was checked).
- Box-art **PNGs no longer clutter ROM folders** — they live in the cache and are
  only copied next to the ROM when `.cov` generation is off.
- **Table** columns are resizable, with ellipsis truncation (no more overlapping
  text).
- **Default DAT / cover URLs migrate**: changing a default now reaches existing
  installs that never customized it, while genuine custom URLs are preserved.
- macOS releases are **signed + notarized** in CI when the Apple secrets are set.

### Fixed

- Cover preview not rendering due to a layout-sizing issue.
- CSV fields containing commas/accents now open with the right columns in
  spreadsheets.

## [1.0.1] — 2026-05-30

### Changed

- **Windows**: full version metadata (CompanyName, FileVersion, …) + an
  application manifest, to reduce antivirus / SmartScreen false positives on the
  unsigned `.exe`.

## [1.0.0] — 2026-05-29

### Added

- First release. Scan a folder (and subfolders) for SNES ROMs, identify each game
  by its headerless CRC32 against the libretro No-Intro DAT, download the official
  libretro box art, and convert it to `.cov`. Optional rename to the No-Intro name
  and CSV export. Single binary for Windows, macOS and Linux (Go + Fyne).

[1.2.0]: https://github.com/ludufre/sd2snes-covers/releases/tag/v1.2.0
[1.1.0]: https://github.com/ludufre/sd2snes-covers/releases/tag/v1.1.0
[1.0.1]: https://github.com/ludufre/sd2snes-covers/releases/tag/v1.0.1
[1.0.0]: https://github.com/ludufre/sd2snes-covers/releases/tag/v1.0.0
