package ui

import "testing"

func TestTomlVersion(t *testing.T) {
	body := "[Details]\nName = \"sd2snes Covers\"\nVersion = \"1.2.3\"\nBuild = 9\n"
	if got := tomlVersion(body); got != "1.2.3" {
		t.Errorf("tomlVersion = %q, want 1.2.3", got)
	}
	if got := tomlVersion(`Version = "v2.0.0"`); got != "2.0.0" {
		t.Errorf("tomlVersion (v-prefix) = %q, want 2.0.0", got)
	}
	if got := tomlVersion("Name = \"x\"\nBuild = 1"); got != "" {
		t.Errorf("tomlVersion(no version) = %q, want empty", got)
	}
}

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.2", "1.0.3", true},
		{"1.0.2", "1.0.2", false},
		{"1.0.3", "1.0.2", false},
		{"1.0.2", "1.1.0", true},
		{"1.0", "1.0.1", true},
		{"2.0.0", "1.9.9", false},
		{"1.0.10", "1.0.9", false}, // numeric, not lexicographic
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
