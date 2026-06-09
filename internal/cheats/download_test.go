package cheats

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestURL(t *testing.T) {
	if got, want := URL("A1B2C3D4"), "https://sd2snes.ludufre.com/cheats/A1B2C3D4.yml"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.yml":
			_, _ = w.Write([]byte("cheats: []\n"))
		case "/missing.yml":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	ctx := context.Background()

	data, st, err := fetch(ctx, client, srv.URL+"/ok.yml")
	if err != nil || st != StatusOK || string(data) != "cheats: []\n" {
		t.Errorf("200: data=%q st=%v err=%v", data, st, err)
	}
	if _, st, err := fetch(ctx, client, srv.URL+"/missing.yml"); err != nil || st != StatusNotFound {
		t.Errorf("404: st=%v err=%v, want StatusNotFound", st, err)
	}
	if _, st, _ := fetch(ctx, client, srv.URL+"/boom.yml"); st != StatusError {
		t.Errorf("500: st=%v, want StatusError", st)
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusNone:      "-",
		StatusOK:        "OK",
		StatusNotFound:  "Not found",
		StatusSkip:      "Skipped",
		StatusCollision: "Collision",
		StatusError:     "Error",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}
