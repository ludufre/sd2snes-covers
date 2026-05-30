package ui

import "testing"

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
