package cov

import (
	"image"
	"image/color"
	"testing"
)

// decodeTileRef mirrors the decode side of the 8bpp planar layout for tests.
func decodeTileRef(tile []byte) [8][8]int {
	var out [8][8]int
	for row := 0; row < 8; row++ {
		p := [8]byte{
			tile[0+row*2], tile[0+row*2+1],
			tile[16+row*2], tile[16+row*2+1],
			tile[32+row*2], tile[32+row*2+1],
			tile[48+row*2], tile[48+row*2+1],
		}
		for col := 0; col < 8; col++ {
			bit := uint(7 - col)
			v := 0
			for pl := 0; pl < 8; pl++ {
				v |= int((p[pl]>>bit)&1) << pl
			}
			out[row][col] = v
		}
	}
	return out
}

func TestEncodeTilePlanarRoundTrip(t *testing.T) {
	// An 8x8 block covering a spread of 8-bit CGRAM values.
	cg := make([][]uint8, 8)
	var want [8][8]int
	for y := 0; y < 8; y++ {
		cg[y] = make([]uint8, 8)
		for x := 0; x < 8; x++ {
			v := (y*8 + x) * 4 // 0,4,...,252
			cg[y][x] = uint8(v)
			want[y][x] = v
		}
	}
	tile := encodeTile(cg, 0, 0)
	if len(tile) != 64 {
		t.Fatalf("tile len = %d, want 64", len(tile))
	}
	got := decodeTileRef(tile)
	if got != want {
		t.Errorf("planar round-trip mismatch\n got=%v\nwant=%v", got, want)
	}
}

func TestEncodeHeaderAndSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 16), 64, 255})
		}
	}
	o := Options{Cols: 4, Rows: 3, CGBase: 128, Colors: 32, Dither: false}
	blob, err := Encode(img, o)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if blob[0] != 'C' || blob[1] != 'V' {
		t.Errorf("magic = %q%q, want CV", blob[0], blob[1])
	}
	checks := map[string][2]int{
		"version":    {2, version},
		"flags":      {3, 0}, // dither off
		"cols":       {4, 4},
		"rows":       {5, 3},
		"cgbase":     {6, 128},
		"ncolors-1":  {7, 31},
		"bpp":        {8, 8},
		"reserved":   {9, 0},
		"reserved16": {10, 0},
	}
	for name, c := range checks {
		if int(blob[c[0]]) != c[1] {
			t.Errorf("header %s @%d = %d, want %d", name, c[0], blob[c[0]], c[1])
		}
	}

	wantLen := headerSize + o.Colors*2 + o.Cols*o.Rows*64
	if len(blob) != wantLen {
		t.Errorf("blob len = %d, want %d", len(blob), wantLen)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// A colourful gradient so quantisation produces a varied palette.
	img := image.NewRGBA(image.Rect(0, 0, 120, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), uint8((x + y)), 255})
		}
	}
	o := DefaultOptions()
	blob, err := Encode(img, o)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	d, err := Decode(blob)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if d.Cols != 9 || d.Rows != 7 { // 120x90 aspect -> AutoWidth picks 9 cols
		t.Errorf("dims = %dx%d tiles, want 9x7", d.Cols, d.Rows)
	}
	if d.CGBase != 128 || d.Colors != 128 || !d.Dithered {
		t.Errorf("hdr cgbase=%d colors=%d dither=%v, want 128/128/true", d.CGBase, d.Colors, d.Dithered)
	}
	if len(d.Palette) != 128 {
		t.Errorf("palette len = %d, want 128", len(d.Palette))
	}
	if len(d.Pixels) != 56 || len(d.Pixels[0]) != 72 {
		t.Fatalf("pixel dims = %dx%d, want 72x56", len(d.Pixels[0]), len(d.Pixels))
	}
	// keepAlpha=false => every pixel is opaque => CGRAM index in [cgbase, cgbase+colors-1].
	for y := range d.Pixels {
		for x, v := range d.Pixels[y] {
			if int(v) < d.CGBase || int(v) > d.CGBase+d.Colors-1 {
				t.Fatalf("pixel (%d,%d) CGRAM index %d out of [%d,%d]", x, y, v, d.CGBase, d.CGBase+d.Colors-1)
			}
		}
	}
}

func TestAutoWidth(t *testing.T) {
	cases := []struct{ w, h, wantCols int }{
		{200, 100, 14},  // landscape 2.0 -> 7*2 = 14
		{100, 100, 7},   // square -> 7
		{50, 100, 4},    // portrait 0.5 -> round(3.5) = 4
		{4000, 100, 32}, // very wide -> capped at MaxCols (32)
	}
	for _, c := range cases {
		img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
		for y := 0; y < c.h; y++ {
			for x := 0; x < c.w; x++ {
				img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
			}
		}
		blob, err := Encode(img, DefaultOptions())
		if err != nil {
			t.Fatalf("%dx%d: Encode: %v", c.w, c.h, err)
		}
		d, err := Decode(blob)
		if err != nil {
			t.Fatalf("%dx%d: Decode: %v", c.w, c.h, err)
		}
		if d.Cols != c.wantCols || d.Rows != 7 {
			t.Errorf("%dx%d -> %dx%d, want %dx7", c.w, c.h, d.Cols, d.Rows, c.wantCols)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := []Options{
		{Cols: 7, Rows: 7, CGBase: 8, Colors: 128},   // cgbase < 16
		{Cols: 7, Rows: 7, CGBase: 200, Colors: 128}, // cgbase+colors > 256
		{Cols: 0, Rows: 7, CGBase: 128, Colors: 128}, // cols < 1
		{Cols: 7, Rows: 8, CGBase: 128, Colors: 128}, // rows > 7
	}
	for i, o := range bad {
		if err := o.validate(); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
	if err := DefaultOptions().validate(); err != nil {
		t.Errorf("DefaultOptions invalid: %v", err)
	}
}
