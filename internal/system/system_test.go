package system

import "testing"

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestForExt(t *testing.T) {
	cases := []struct {
		ext       string
		wantKeys  []string
		wantStrip bool
	}{
		{".sfc", []string{KeySNES}, true},
		{".smc", []string{KeySNES}, true},
		{".SFC", []string{KeySNES}, true}, // case-insensitive
		{".gb", []string{KeyGB, KeyGBC}, false},
		{".gbc", []string{KeyGBC, KeyGB}, false},
		{".sgb", []string{KeySGB, KeyGBC}, false}, // Super Game Boy first, GBC fallback
		{".zip", nil, false},
	}
	for _, c := range cases {
		keys, strip := ForExt(c.ext)
		if strip != c.wantStrip || !equalKeys(keys, c.wantKeys) {
			t.Errorf("ForExt(%q) = (%v, %v); want (%v, %v)", c.ext, keys, strip, c.wantKeys, c.wantStrip)
		}
	}
}

func TestSystemsAndGet(t *testing.T) {
	wantKeys := []string{KeySNES, KeyGB, KeyGBC, KeySGB}
	if len(Systems) != len(wantKeys) {
		t.Fatalf("len(Systems) = %d, want %d", len(Systems), len(wantKeys))
	}
	for i, key := range wantKeys {
		s := Systems[i]
		if s.Key != key {
			t.Errorf("Systems[%d].Key = %q, want %q", i, s.Key, key)
		}
		if s.DefaultDat == "" || s.DefaultBox == "" {
			t.Errorf("system %q has empty default DAT/boxart", s.Key)
		}
		got, ok := Get(key)
		if !ok || got.Key != key {
			t.Errorf("Get(%q) = (%+v, %v); want the matching system", key, got, ok)
		}
	}
	if _, ok := Get("nope"); ok {
		t.Errorf("Get(unknown) returned ok=true")
	}
}

// Super Game Boy has no separate libretro sources, so it defaults to Game Boy's.
func TestSGBDefaultsToGameBoy(t *testing.T) {
	sgb, _ := Get(KeySGB)
	gb, _ := Get(KeyGB)
	if sgb.DefaultDat != gb.DefaultDat || sgb.DefaultBox != gb.DefaultBox {
		t.Errorf("Super Game Boy defaults should mirror Game Boy (got dat=%q box=%q)", sgb.DefaultDat, sgb.DefaultBox)
	}
}

func TestLabel(t *testing.T) {
	s := System{Name: "Game Boy", Exts: ".gb"}
	if got := s.Label(); got != "Game Boy (.gb)" {
		t.Errorf("Label() = %q, want %q", got, "Game Boy (.gb)")
	}
}
