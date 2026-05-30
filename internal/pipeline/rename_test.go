package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRenameToNoIntro_MovesSiblings(t *testing.T) {
	dir := t.TempDir()
	rom := filepath.Join(dir, "old.sfc")
	write(t, rom, "rom")
	write(t, filepath.Join(dir, "old.png"), "png")
	write(t, filepath.Join(dir, "old.cov"), "cov")

	newPath, renamed, err := renameToNoIntro(rom, "Super Mario World (USA)")
	if err != nil || !renamed {
		t.Fatalf("rename failed: renamed=%v err=%v", renamed, err)
	}
	const base = "Super Mario World (USA)"
	if filepath.Base(newPath) != base+".sfc" {
		t.Errorf("newPath = %q, want %q", filepath.Base(newPath), base+".sfc")
	}
	// new files present, old basenames gone
	for _, ext := range []string{".sfc", ".png", ".cov"} {
		if !exists(filepath.Join(dir, base+ext)) {
			t.Errorf("missing new %s", ext)
		}
		if exists(filepath.Join(dir, "old"+ext)) {
			t.Errorf("orphan old%s left behind", ext)
		}
	}
}

func TestRenameToNoIntro_SiblingDestExists(t *testing.T) {
	dir := t.TempDir()
	rom := filepath.Join(dir, "old.sfc")
	write(t, rom, "rom")
	write(t, filepath.Join(dir, "old.png"), "old-cover")
	const base = "Chrono Trigger (USA)"
	write(t, filepath.Join(dir, base+".png"), "existing-cover") // dest already there

	_, renamed, err := renameToNoIntro(rom, base)
	if err != nil || !renamed {
		t.Fatalf("rename failed: renamed=%v err=%v", renamed, err)
	}
	if exists(filepath.Join(dir, "old.png")) {
		t.Errorf("orphan old.png should have been removed")
	}
	// existing destination cover must be preserved (not clobbered by the old one)
	got, _ := os.ReadFile(filepath.Join(dir, base+".png"))
	if string(got) != "existing-cover" {
		t.Errorf("dest cover = %q, want %q (must not be clobbered)", got, "existing-cover")
	}
}

func TestRenameToNoIntro_ConflictKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	rom := filepath.Join(dir, "old.sfc")
	write(t, rom, "rom-A")
	const base = "Super Metroid (Japan, USA) (En,Ja)"
	write(t, filepath.Join(dir, base+".sfc"), "rom-B") // different file already at target

	newPath, renamed, err := renameToNoIntro(rom, base)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if renamed {
		t.Errorf("should not rename onto a different existing file")
	}
	if newPath != rom {
		t.Errorf("newPath = %q, want original %q", newPath, rom)
	}
	if !exists(rom) {
		t.Errorf("original ROM should still exist")
	}
}
