package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/snes"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

// pngMagic is the 8-byte PNG file signature.
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// TestRunE2E exercises the full pipeline against the live libretro server:
// scan -> headerless CRC -> DAT lookup -> boxart download -> save next to ROM.
// It uses a synthetic index so it does not depend on the real DAT contents.
// Run with `go test -run TestRunE2E ./internal/pipeline` (skipped under -short).
func TestRunE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("network test; skipped in -short mode")
	}

	dir := t.TempDir()
	t.Setenv("SD2SNES_COVERS_CACHE", t.TempDir()) // isolate the boxart cache

	// A ROM that will resolve to a real boxart, named arbitrarily to prove the
	// cover is saved using the ROM's own basename (not the No-Intro name).
	matchROM := filepath.Join(dir, "my favorite game.sfc")
	if err := os.WriteFile(matchROM, []byte("not a real rom, any bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A ROM whose CRC won't be in the index -> StatusNoMatch, no download.
	noMatchROM := filepath.Join(dir, "unknown.smc")
	if err := os.WriteFile(noMatchROM, []byte("some other bytes entirely here"), 0o644); err != nil {
		t.Fatal(err)
	}

	crc, _, err := snes.CRC32Headerless(matchROM)
	if err != nil {
		t.Fatal(err)
	}
	index := dat.Index{snes.CRCHex(crc): "Super Mario World (USA)"}

	out := make(chan Progress)
	go Run(context.Background(), []string{matchROM, noMatchROM}, index, Options{Overwrite: true}, out)

	rows := map[string]RowResult{}
	for p := range out {
		rows[p.Row.ROMName] = p.Row
	}

	// Matched ROM: name resolved, cover OK, PNG saved next to the ROM.
	got := rows["my favorite game.sfc"]
	if got.Match != "Super Mario World (USA)" {
		t.Errorf("Match = %q, want %q", got.Match, "Super Mario World (USA)")
	}
	if got.Err != nil {
		t.Errorf("unexpected error: %v", got.Err)
	}
	if got.Cover != thumbs.StatusOK {
		t.Errorf("Cover = %v, want OK", got.Cover)
	}
	coverPNG := filepath.Join(dir, "my favorite game.png")
	data, err := os.ReadFile(coverPNG)
	if err != nil {
		t.Fatalf("cover not saved next to ROM: %v", err)
	}
	if len(data) == 0 || !bytes.HasPrefix(data, pngMagic) {
		t.Errorf("saved cover is not a valid PNG (size=%d)", len(data))
	}

	// Unmatched ROM: no match, no file written.
	nm := rows["unknown.smc"]
	if nm.Cover != thumbs.StatusNoMatch {
		t.Errorf("unmatched Cover = %v, want NoMatch", nm.Cover)
	}
	if _, err := os.Stat(filepath.Join(dir, "unknown.png")); !os.IsNotExist(err) {
		t.Errorf("unexpected cover written for unmatched ROM")
	}
}

// TestRunE2ERenameAndCov exercises rename-to-No-Intro + .cov generation against
// the live server: an arbitrarily-named ROM is renamed and a .cov produced under
// the No-Intro basename, while the downloaded PNG stays in a temp dir (not kept
// next to the ROM).
func TestRunE2ERenameAndCov(t *testing.T) {
	if testing.Short() {
		t.Skip("network test; skipped in -short mode")
	}
	dir := t.TempDir()
	t.Setenv("SD2SNES_COVERS_CACHE", t.TempDir()) // isolate the boxart cache
	rom := filepath.Join(dir, "anything.sfc")
	if err := os.WriteFile(rom, []byte("arbitrary rom bytes for e2e"), 0o644); err != nil {
		t.Fatal(err)
	}
	crc, _, err := snes.CRC32Headerless(rom)
	if err != nil {
		t.Fatal(err)
	}
	index := dat.Index{snes.CRCHex(crc): "Super Mario World (USA)"}

	out := make(chan Progress)
	go Run(context.Background(), []string{rom}, index, Options{
		Overwrite: true, Rename: true, MakeCov: true, CovOpts: cov.DefaultOptions(),
	}, out)
	var row RowResult
	for p := range out {
		row = p.Row
	}

	const base = "Super Mario World (USA)"
	if row.NewName != base+".sfc" {
		t.Errorf("NewName = %q, want %q", row.NewName, base+".sfc")
	}
	if row.Cover != thumbs.StatusOK {
		t.Errorf("Cover = %v, want OK", row.Cover)
	}
	if row.Cov != CovOK {
		t.Errorf("Cov = %v, want CovOK", row.Cov)
	}
	for _, ext := range []string{".sfc", ".cov"} {
		if _, err := os.Stat(filepath.Join(dir, base+ext)); err != nil {
			t.Errorf("missing %s next to renamed ROM: %v", ext, err)
		}
	}
	// The boxart PNG is an intermediate kept in a temp dir, so it must NOT be
	// left next to the ROM when a .cov is generated.
	if _, err := os.Stat(filepath.Join(dir, base+".png")); !os.IsNotExist(err) {
		t.Errorf("intermediate .png should not be saved next to the ROM when MakeCov is on")
	}
	covBytes, err := os.ReadFile(filepath.Join(dir, base+".cov"))
	if err != nil {
		t.Fatalf(".cov missing: %v", err)
	}
	if len(covBytes) < 9 || covBytes[0] != 'C' || covBytes[1] != 'V' || covBytes[2] != 4 || covBytes[8] != 4 {
		t.Errorf(".cov v4 header invalid")
	}
	d, err := cov.Decode(covBytes)
	if err != nil {
		t.Fatalf("cov.Decode: %v", err)
	}
	if d.WSpr != 8 || d.HSpr != 6 { // DefaultOptions: fixed 8x6 landscape frame
		t.Errorf("cov = %dx%d sprites, want 8x6", d.WSpr, d.HSpr)
	}
	if d.NPalettes < 1 || d.NPalettes > 8 {
		t.Errorf("npalettes = %d, want 1..8", d.NPalettes)
	}
}
