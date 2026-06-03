// Package system describes the console systems the tool can generate covers
// for, and maps a ROM file extension to the system(s) to try. Each system has
// its own No-Intro DAT (CRC32 -> name) and libretro boxart repository, and each
// is overridable in the app's Settings (see Systems).
//
// Multi-system is automatic by extension:
//   - .sfc/.smc -> Super Nintendo (CRC32 skips the 512-byte SMC copier header)
//   - .gb       -> Game Boy, then Game Boy Color as a fallback
//   - .gbc      -> Game Boy Color, then Game Boy as a fallback
//   - .sgb      -> Super Game Boy, then Game Boy Color as a fallback. .sgb files
//     are Game Boy ROMs (the extension only signals SGB enhancement),
//     so the Super Game Boy system has no separate libretro DAT/boxart
//     and simply defaults to the Game Boy sources — but it is its own
//     configurable entry in Settings so it can be pointed elsewhere.
//
// Game Boy family ROMs never carry a copier header, so their CRC32 is computed
// over the whole file (stripHeader=false).
package system

import (
	"strings"

	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

// System keys (also used as the DAT disk-cache filenames and the Settings
// preference-key suffixes).
const (
	KeySNES = "snes"
	KeyGB   = "gb"
	KeyGBC  = "gbc"
	KeySGB  = "sgb"
)

// Default Game Boy-family DAT URLs and libretro boxart bases. Super Game Boy has
// no separate libretro sources, so it reuses the Game Boy ones as its defaults.
const (
	GBDatURL  = "https://raw.githubusercontent.com/libretro/libretro-database/refs/heads/master/metadat/no-intro/Nintendo%20-%20Game%20Boy.dat"
	GBCDatURL = "https://raw.githubusercontent.com/libretro/libretro-database/refs/heads/master/metadat/no-intro/Nintendo%20-%20Game%20Boy%20Color.dat"

	GBBoxart  = "https://thumbnails.libretro.com/Nintendo%20-%20Game%20Boy/Named_Boxarts/"
	GBCBoxart = "https://thumbnails.libretro.com/Nintendo%20-%20Game%20Boy%20Color/Named_Boxarts/"
)

// System describes one configurable console: its disk-cache/preference key, a
// display name and the extensions it covers, and the default DAT URL + libretro
// boxart base. The defaults are overridable per system in the app's Settings.
//
// LegacyDat / LegacyBox hold PAST values of the matching defaults. A stored
// preference equal to the current default OR to any legacy entry is treated as
// "not customized", so changing a default below propagates to existing installs
// while a genuine custom URL is preserved.
//
// HOW TO CHANGE A DEFAULT: edit DefaultDat/DefaultBox (for SNES that means
// dat.DefaultURL / thumbs.DefaultBoxartBase; for the GB family the constants
// above), then append the value you are replacing to that system's LegacyDat /
// LegacyBox so the change migrates automatically for non-customized installs.
type System struct {
	Key        string   // disk-cache key + preference-key suffix
	Name       string   // human name, e.g. "Game Boy"
	Exts       string   // extensions shown in Settings, e.g. ".gb"
	DefaultDat string   // default No-Intro DAT URL
	DefaultBox string   // default libretro boxart base URL
	LegacyDat  []string // past DAT defaults, migrated to the current one
	LegacyBox  []string // past boxart defaults, migrated to the current one
}

// Label is the "<Name> (<Exts>)" caption used in the Settings dialog.
func (s System) Label() string { return s.Name + " (" + s.Exts + ")" }

// Systems lists every configurable system, in Settings display order.
var Systems = []System{
	{
		Key:        KeySNES,
		Name:       "Super Nintendo",
		Exts:       ".sfc/.smc",
		DefaultDat: dat.DefaultURL,
		DefaultBox: thumbs.DefaultBoxartBase,
		LegacyDat:  []string{"https://raw.githubusercontent.com/libretro/libretro-database/refs/heads/master/metadat/no-intro/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System.dat"},
		LegacyBox:  []string{"https://thumbnails.libretro.com/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/"},
	},
	{Key: KeyGB, Name: "Game Boy", Exts: ".gb", DefaultDat: GBDatURL, DefaultBox: GBBoxart},
	{Key: KeyGBC, Name: "Game Boy Color", Exts: ".gbc", DefaultDat: GBCDatURL, DefaultBox: GBCBoxart},
	{Key: KeySGB, Name: "Super Game Boy", Exts: ".sgb", DefaultDat: GBDatURL, DefaultBox: GBBoxart},
}

// Get returns the System for a key and whether it exists.
func Get(key string) (System, bool) {
	for _, s := range Systems {
		if s.Key == key {
			return s, true
		}
	}
	return System{}, false
}

// ForExt returns the ordered list of system keys to try for a ROM extension and
// whether the CRC32 must skip the SNES copier header. An unknown extension
// returns (nil, false).
func ForExt(ext string) (keys []string, stripHeader bool) {
	switch strings.ToLower(ext) {
	case ".sfc", ".smc":
		return []string{KeySNES}, true
	case ".gb":
		return []string{KeyGB, KeyGBC}, false
	case ".gbc":
		return []string{KeyGBC, KeyGB}, false
	case ".sgb":
		return []string{KeySGB, KeyGBC}, false
	}
	return nil, false
}
