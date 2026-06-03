package ui

import (
	"testing"

	"github.com/ludufre/sd2snes-covers/internal/system"
)

// SNES keeps the original unsuffixed preference keys (so values customized by
// existing installs survive the multi-system refactor); every other system is
// suffixed with its key.
func TestSystemPrefKeys(t *testing.T) {
	cases := []struct {
		key     string
		wantDat string
		wantBox string
	}{
		{system.KeySNES, prefDatURL, prefBoxartBase},
		{system.KeyGB, "dat_url_gb", "boxart_base_gb"},
		{system.KeyGBC, "dat_url_gbc", "boxart_base_gbc"},
		{system.KeySGB, "dat_url_sgb", "boxart_base_sgb"},
	}
	for _, c := range cases {
		if got := datPrefKey(c.key); got != c.wantDat {
			t.Errorf("datPrefKey(%q) = %q, want %q", c.key, got, c.wantDat)
		}
		if got := boxPrefKey(c.key); got != c.wantBox {
			t.Errorf("boxPrefKey(%q) = %q, want %q", c.key, got, c.wantBox)
		}
	}
}

func TestPickPref(t *testing.T) {
	const def = "https://new.example/default"
	legacy := []string{"https://old1.example", "https://old2.example"}
	cases := []struct {
		stored    string
		wantValue string
		wantClear bool
	}{
		{"", def, false},                                          // never set → current default
		{def, def, false},                                         // already the current default
		{"https://old1.example", def, true},                       // old default → migrate + clear
		{"https://old2.example", def, true},                       // another old default → migrate
		{"https://my.custom/url", "https://my.custom/url", false}, // genuine custom → keep
	}
	for _, c := range cases {
		v, clr := pickPref(c.stored, def, legacy)
		if v != c.wantValue || clr != c.wantClear {
			t.Errorf("pickPref(%q) = (%q, %v); want (%q, %v)", c.stored, v, clr, c.wantValue, c.wantClear)
		}
	}
}
