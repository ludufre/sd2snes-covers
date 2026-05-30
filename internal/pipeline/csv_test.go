package pipeline

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

func TestWriteCSV(t *testing.T) {
	var b bytes.Buffer
	rows := []RowResult{{
		ROMName: "a,b.sfc",
		CRC:     "DEADBEEF",
		Match:   "Super Metroid (Japan, USA) (En,Ja)",
		URL:     "https://thumbnails.libretro.com/x/Super%20Metroid.png",
		Cover:   thumbs.StatusNotFound,
		Err:     errors.New("oops, with comma"),
	}}
	if err := WriteCSV(&b, rows); err != nil {
		t.Fatal(err)
	}
	out := b.String()

	if !strings.HasPrefix(out, "\ufeffsep=,\r\n") {
		t.Errorf("missing UTF-8 BOM + sep hint, got prefix %q", out[:12])
	}
	if !strings.Contains(out, `"a,b.sfc"`) {
		t.Errorf("field with a comma was not quoted:\n%s", out)
	}
	if !strings.Contains(out, `"Super Metroid (Japan, USA) (En,Ja)"`) {
		t.Errorf("match with commas was not quoted:\n%s", out)
	}
	if !strings.Contains(out, `"oops, with comma"`) {
		t.Errorf("error with a comma was not quoted:\n%s", out)
	}
	if !strings.Contains(out, "Boxart URL") {
		t.Errorf("CSV is missing the Boxart URL column:\n%s", out)
	}
	if !strings.Contains(out, "https://thumbnails.libretro.com/x/Super%20Metroid.png") {
		t.Errorf("CSV is missing the boxart URL value:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\r\n"), "\"oops, with comma\"") {
		// last field should be the (quoted) error; sanity that columns line up
		t.Logf("CSV:\n%s", out)
	}
}
