package dat

import (
	"strings"
	"testing"
)

const fixture = `clrmamepro (
	name "Nintendo - Super Nintendo Entertainment System"
	description "Nintendo - Super Nintendo Entertainment System"
	version "2026.05.02"
)

game (
	name "Super Mario World (USA)"
	region "USA"
	rom ( name "Super Mario World (USA).sfc" size 524288 crc B19ED489 md5 DEAD sha1 BEEF )
)
game (
	name "Chrono Trigger (USA)"
	region "USA"
	rom ( name "Chrono Trigger (USA).sfc" size 4194304 crc 2D206BF7 md5 DEAD sha1 BEEF )
)
game (
	name "Some Homebrew (World)"
	rom ( name "Some Homebrew (World) (Aftermarket) (Unl).sfc" size 524288 crc 0a1b2c3d md5 DEAD sha1 BEEF )
)
`

func TestParse(t *testing.T) {
	idx, err := Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]string{
		"B19ED489": "Super Mario World (USA)",
		"2D206BF7": "Chrono Trigger (USA)",
		"0A1B2C3D": "Some Homebrew (World)", // lowercase crc normalized to upper; game name (not rom name)
	}
	for crc, name := range want {
		if got := idx[crc]; got != name {
			t.Errorf("idx[%q] = %q, want %q", crc, got, name)
		}
	}

	// The clrmamepro header block must not leak into the index.
	if len(idx) != len(want) {
		t.Errorf("index size = %d, want %d (%v)", len(idx), len(want), idx)
	}
}
