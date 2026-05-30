package thumbs

import "testing"

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Super Mario World (USA)": "Super Mario World (USA)",
		"Q*bert's Qubes (USA)":    "Q_bert's Qubes (USA)",
		`A:B/C\D|E?F<G>H*I&J"K`:   "A_B_C_D_E_F_G_H_I_J_K",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBoxartURL(t *testing.T) {
	// url.URL.String() percent-encodes spaces (%20) and parentheses (%28/%29).
	// The libretro server decodes both, so these URLs resolve (verified: HTTP 200).
	cases := map[string]string{
		"Chrono Trigger (USA)":    "https://thumbnails.libretro.com/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/Chrono%20Trigger%20%28USA%29.png",
		"Super Mario World (USA)": "https://thumbnails.libretro.com/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/Super%20Mario%20World%20%28USA%29.png",
	}
	for in, want := range cases {
		if got := BoxartURL(in); got != want {
			t.Errorf("BoxartURL(%q) =\n  %q\nwant\n  %q", in, got, want)
		}
	}
}
