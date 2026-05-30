// Package cov converts cover images into the sd2snes ".cov" 8bpp BG cover
// format. The on-disk format (header / BGR555 palette / 8bpp planar tiles /
// CGRAM mapping) is reproduced exactly from cover_conv.py so the firmware reads
// it; the image pipeline (resize + median-cut quantization) is an independent
// Go implementation, so output is format-compatible and visually equivalent but
// not byte-identical to the Python tool.
package cov

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"sort"

	_ "image/jpeg" // decode support
	_ "image/png"  // decode support

	xdraw "golang.org/x/image/draw"
)

const (
	magic0     = 'C'
	magic1     = 'V'
	version    = 3
	bpp        = 8
	headerSize = 12
)

// Options controls a .cov conversion. DefaultOptions mirrors the sd2snes fork.
type Options struct {
	Cols      int  // cover width in 8px tiles (1..32); ignored when AutoWidth
	Rows      int  // cover height in 8px tiles (1..7)
	CGBase    int  // CGRAM index the palette loads at (16..255)
	Colors    int  // palette colours (1..256-CGBase)
	Dither    bool // Floyd-Steinberg dithering
	Fill      bool // crop-to-fill instead of letterbox-fit
	KeepAlpha bool // keep source transparency (index 0); for a logo, not covers
	AutoWidth bool // derive Cols from the source aspect ratio (fills the Rows band)
	MaxCols   int  // cap for AutoWidth (defaults to 32, the full menu width)
}

// DefaultOptions returns the sd2snes fork defaults: the cover fills the 7-row
// (56px) header band and its width follows the source aspect ratio (up to the
// full 32-tile menu width), 128 colours at CGRAM 128, dithered. Cols is a
// fallback used only if AutoWidth can't measure the image.
func DefaultOptions() Options {
	return Options{Cols: 10, Rows: 7, CGBase: 128, Colors: 128, Dither: true, AutoWidth: true, MaxCols: 32}
}

func (o Options) validate() error {
	if o.CGBase < 16 || o.CGBase > 255 {
		return fmt.Errorf("cgbase must be 16..255 (CGRAM 0..15 is reserved), got %d", o.CGBase)
	}
	if o.Colors < 1 || o.CGBase+o.Colors > 256 {
		return fmt.Errorf("colors must be 1..%d for cgbase %d, got %d", 256-o.CGBase, o.CGBase, o.Colors)
	}
	if o.Cols < 1 || o.Cols > 32 {
		return fmt.Errorf("cols must be 1..32, got %d", o.Cols)
	}
	if o.Rows < 1 || o.Rows > 7 {
		return fmt.Errorf("rows must be 1..7 (cover must fit the 56px header band), got %d", o.Rows)
	}
	if o.Cols*o.Rows > 224 {
		return fmt.Errorf("cols*rows must be <= 224 tiles, got %d", o.Cols*o.Rows)
	}
	return nil
}

// autoCols picks a column count that preserves the source aspect ratio while
// the cover fills the fixed `rows` band height — so landscape boxart becomes
// wider instead of being letterboxed into a square. Returns 0 if it can't
// measure the image (caller keeps the fallback Cols).
func autoCols(img image.Image, rows, maxCols int) int {
	if maxCols <= 0 || maxCols > 32 {
		maxCols = 32
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || rows <= 0 {
		return 0
	}
	aspect := float64(b.Dx()) / float64(b.Dy())
	c := int(math.Round(float64(rows) * aspect))
	if c < 1 {
		c = 1
	}
	if c > maxCols {
		c = maxCols
	}
	return c
}

// ConvertFile decodes the image at srcImage and writes a .cov file to dstCov.
func ConvertFile(srcImage, dstCov string, o Options) error {
	f, err := os.Open(srcImage)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", srcImage, err)
	}
	blob, err := Encode(img, o)
	if err != nil {
		return err
	}
	tmp := dstCov + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dstCov)
}

// Encode renders img into a .cov byte blob.
func Encode(img image.Image, o Options) ([]byte, error) {
	if o.AutoWidth {
		if c := autoCols(img, o.Rows, o.MaxCols); c > 0 {
			o.Cols = c
		}
	}
	if err := o.validate(); err != nil {
		return nil, err
	}
	wpx, hpx := o.Cols*8, o.Rows*8
	rgb, opaque := buildCanvas(img, wpx, hpx, o.Fill, o.KeepAlpha)

	indices, palette := quantise8bpp(rgb, opaque, o.Colors, o.Dither)

	// Map local palette index (1..ncolors) to CGRAM index (cgbase..); 0 stays 0.
	cgidx := make([][]uint8, hpx)
	for y := 0; y < hpx; y++ {
		cgidx[y] = make([]uint8, wpx)
		for x := 0; x < wpx; x++ {
			if v := indices[y][x]; v > 0 {
				cg := int(v) - 1 + o.CGBase
				if cg > 255 {
					return nil, fmt.Errorf("cgbase+ncolors overflow CGRAM (%d)", cg)
				}
				cgidx[y][x] = uint8(cg)
			}
		}
	}

	out := make([]byte, 0, headerSize+o.Colors*2+o.Cols*o.Rows*64)
	flags := byte(0)
	if o.Dither {
		flags = 0x01
	}
	// HEADER (12 bytes), little-endian.
	out = append(out,
		magic0, magic1, version, flags,
		byte(o.Cols), byte(o.Rows), byte(o.CGBase), byte(o.Colors-1), bpp,
		0, 0, 0,
	)
	// PALETTE (ncolors * 2 bytes, BGR555 LE).
	for _, c := range palette {
		w := rgbToBGR555(c[0], c[1], c[2])
		out = append(out, byte(w&0xFF), byte((w>>8)&0xFF))
	}
	// TILES (cols*rows * 64 bytes, 8bpp planar, row-major).
	for ty := 0; ty < o.Rows; ty++ {
		for tx := 0; tx < o.Cols; tx++ {
			out = append(out, encodeTile(cgidx, tx*8, ty*8)...)
		}
	}
	return out, nil
}

// --- colour helpers ---

func rgbToBGR555(r, g, b int) int {
	return ((b >> 3) << 10) | ((g >> 3) << 5) | (r >> 3)
}

func bgr555ToRGB(word int) (r, g, b int) {
	r = (word & 0x1F) << 3
	g = ((word >> 5) & 0x1F) << 3
	b = ((word >> 10) & 0x1F) << 3
	r |= r >> 5
	g |= g >> 5
	b |= b >> 5
	return
}

// snap555 snaps a 0..255 channel onto the SNES 15-bit lattice (matches the
// Python snap555: top 5 bits, low 3 bits replicated from the top of the 5).
func snap555(v int) int {
	hi := v >> 3
	s := (hi << 3) | (hi >> 2)
	if s > 255 {
		s = 255
	}
	return s
}

// --- image -> canvas ---

// buildCanvas letterbox-fits (or crop-fills) img into a wpx*hpx canvas over a
// black background, returning the RGB grid and an opacity mask. With keepAlpha
// false the whole canvas is opaque (letterbox bars become opaque black).
func buildCanvas(src image.Image, wpx, hpx int, fill, keepAlpha bool) (rgb [][][3]int, opaque [][]bool) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw < 1 {
		sw = 1
	}
	if sh < 1 {
		sh = 1
	}

	var scale float64
	if fill {
		scale = math.Max(float64(wpx)/float64(sw), float64(hpx)/float64(sh))
	} else {
		scale = math.Min(float64(wpx)/float64(sw), float64(hpx)/float64(sh))
	}
	nw := int(math.Round(float64(sw) * scale))
	nh := int(math.Round(float64(sh) * scale))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	resized := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), src, b, xdraw.Over, nil)

	rgb = make([][][3]int, hpx)
	opaque = make([][]bool, hpx)
	for y := 0; y < hpx; y++ {
		rgb[y] = make([][3]int, wpx)
		opaque[y] = make([]bool, wpx)
		if !keepAlpha {
			for x := 0; x < wpx; x++ {
				opaque[y][x] = true // letterbox bars baked as opaque black
			}
		}
	}

	offx := (wpx - nw) / 2
	offy := (hpx - nh) / 2
	for y := 0; y < nh; y++ {
		cy := offy + y
		if cy < 0 || cy >= hpx {
			continue
		}
		for x := 0; x < nw; x++ {
			cx := offx + x
			if cx < 0 || cx >= wpx {
				continue
			}
			p := resized.NRGBAAt(x, y)
			// composite straight-alpha pixel over a black background
			a := int(p.A)
			rgb[cy][cx] = [3]int{
				int(p.R) * a / 255,
				int(p.G) * a / 255,
				int(p.B) * a / 255,
			}
			if keepAlpha {
				opaque[cy][cx] = a >= 128
			}
		}
	}
	return rgb, opaque
}

// --- quantisation ---

// quantise8bpp returns the per-pixel palette indices (1..ncolors for opaque
// pixels, 0 elsewhere) and the palette of exactly ncolors (r,g,b) entries.
func quantise8bpp(rgb [][][3]int, opaque [][]bool, ncolors int, dither bool) (indices [][]uint8, palette [][3]int) {
	h := len(rgb)
	w := 0
	if h > 0 {
		w = len(rgb[0])
	}

	var opx [][3]int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if opaque[y][x] {
				p := rgb[y][x]
				opx = append(opx, [3]int{snap555(p[0]), snap555(p[1]), snap555(p[2])})
			}
		}
	}
	palette = medianCut(opx, ncolors)

	indices = make([][]uint8, h)
	for y := 0; y < h; y++ {
		indices[y] = make([]uint8, w)
	}
	if len(opx) == 0 {
		return indices, palette
	}

	if !dither {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if opaque[y][x] {
					indices[y][x] = uint8(nearest(rgb[y][x], palette) + 1)
				}
			}
		}
		return indices, palette
	}

	// Floyd-Steinberg over the opaque region (errors diffuse to opaque pixels
	// only), matching cover_conv.py exactly.
	work := make([][][3]float64, h)
	for y := 0; y < h; y++ {
		work[y] = make([][3]float64, w)
		for x := 0; x < w; x++ {
			work[y][x] = [3]float64{float64(rgb[y][x][0]), float64(rgb[y][x][1]), float64(rgb[y][x][2])}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !opaque[y][x] {
				continue
			}
			old := work[y][x]
			k := nearestF(old, palette)
			newc := palette[k]
			indices[y][x] = uint8(k + 1)
			err := [3]float64{old[0] - float64(newc[0]), old[1] - float64(newc[1]), old[2] - float64(newc[2])}
			diffuse := func(yy, xx int, f float64) {
				if yy < 0 || yy >= h || xx < 0 || xx >= w || !opaque[yy][xx] {
					return
				}
				work[yy][xx][0] += err[0] * f
				work[yy][xx][1] += err[1] * f
				work[yy][xx][2] += err[2] * f
			}
			diffuse(y, x+1, 7.0/16)
			diffuse(y+1, x-1, 3.0/16)
			diffuse(y+1, x, 5.0/16)
			diffuse(y+1, x+1, 1.0/16)
		}
	}
	return indices, palette
}

func nearest(p [3]int, pal [][3]int) int {
	best, bestD := 0, math.MaxInt64
	for i, c := range pal {
		dr, dg, db := p[0]-c[0], p[1]-c[1], p[2]-c[2]
		d := dr*dr + dg*dg + db*db
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

func nearestF(p [3]float64, pal [][3]int) int {
	best := 0
	bestD := math.MaxFloat64
	for i, c := range pal {
		dr, dg, db := p[0]-float64(c[0]), p[1]-float64(c[1]), p[2]-float64(c[2])
		d := dr*dr + dg*dg + db*db
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// medianCut reduces pixels to exactly ncolors representative colours (padded
// with black when there are too few distinct colours).
func medianCut(pixels [][3]int, ncolors int) [][3]int {
	out := make([][3]int, ncolors)
	if len(pixels) == 0 {
		return out
	}
	type box struct{ px [][3]int }
	boxes := []box{{px: append([][3]int(nil), pixels...)}}

	for len(boxes) < ncolors {
		bi, bestRange, bestCh := -1, 0, 0
		for i := range boxes {
			if len(boxes[i].px) < 2 {
				continue
			}
			for ch := 0; ch < 3; ch++ {
				mn, mx := 255, 0
				for _, p := range boxes[i].px {
					if p[ch] < mn {
						mn = p[ch]
					}
					if p[ch] > mx {
						mx = p[ch]
					}
				}
				if mx-mn > bestRange {
					bestRange, bi, bestCh = mx-mn, i, ch
				}
			}
		}
		if bi < 0 || bestRange <= 0 {
			break // no box can be split further
		}
		px := boxes[bi].px
		sort.Slice(px, func(a, b int) bool { return px[a][bestCh] < px[b][bestCh] })
		mid := len(px) / 2
		nb := make([]box, 0, len(boxes)+1)
		nb = append(nb, boxes[:bi]...)
		nb = append(nb, box{px: px[:mid]}, box{px: px[mid:]})
		nb = append(nb, boxes[bi+1:]...)
		boxes = nb
	}

	for i := 0; i < ncolors; i++ {
		if i < len(boxes) && len(boxes[i].px) > 0 {
			var sr, sg, sb int
			for _, p := range boxes[i].px {
				sr += p[0]
				sg += p[1]
				sb += p[2]
			}
			n := len(boxes[i].px)
			out[i] = [3]int{(sr + n/2) / n, (sg + n/2) / n, (sb + n/2) / n}
		}
	}
	return out
}

// --- tile encoding ---

// encodeTile encodes the 8x8 block at (x0,y0) of cgidx into 64 bytes of SNES
// 8bpp planar tile data (plane-pairs 0&1,2&3,4&5,6&7 in 16-byte groups).
func encodeTile(cgidx [][]uint8, x0, y0 int) []byte {
	out := make([]byte, 64)
	for row := 0; row < 8; row++ {
		var planes [8]int
		for col := 0; col < 8; col++ {
			v := int(cgidx[y0+row][x0+col])
			bit := 7 - col
			for p := 0; p < 8; p++ {
				planes[p] |= ((v >> p) & 1) << bit
			}
		}
		for pair := 0; pair < 4; pair++ {
			out[pair*16+row*2] = byte(planes[pair*2])
			out[pair*16+row*2+1] = byte(planes[pair*2+1])
		}
	}
	return out
}

// --- decoder (for round-trip tests / QA) ---

// Decoded is the result of decoding a .cov blob.
type Decoded struct {
	Cols, Rows int
	CGBase     int
	Colors     int
	Dithered   bool
	Palette    [][3]int // ncolors RGB (expanded from BGR555)
	// Pixels holds CGRAM indices, Rows*8 high by Cols*8 wide.
	Pixels [][]uint8
}

// Decode parses a .cov blob (mirrors cover_conv.py verify_cov).
func Decode(blob []byte) (*Decoded, error) {
	if len(blob) < headerSize || blob[0] != magic0 || blob[1] != magic1 || blob[2] != version {
		return nil, fmt.Errorf("not a .cov file")
	}
	d := &Decoded{
		Dithered: blob[3]&0x01 != 0,
		Cols:     int(blob[4]),
		Rows:     int(blob[5]),
		CGBase:   int(blob[6]),
		Colors:   int(blob[7]) + 1,
	}
	off := headerSize
	if len(blob) < off+d.Colors*2+d.Cols*d.Rows*64 {
		return nil, fmt.Errorf("truncated .cov file")
	}
	d.Palette = make([][3]int, d.Colors)
	for i := 0; i < d.Colors; i++ {
		w := int(blob[off]) | int(blob[off+1])<<8
		r, g, b := bgr555ToRGB(w)
		d.Palette[i] = [3]int{r, g, b}
		off += 2
	}
	hpx, wpx := d.Rows*8, d.Cols*8
	d.Pixels = make([][]uint8, hpx)
	for y := 0; y < hpx; y++ {
		d.Pixels[y] = make([]uint8, wpx)
	}
	for ty := 0; ty < d.Rows; ty++ {
		for tx := 0; tx < d.Cols; tx++ {
			tile := blob[off : off+64]
			off += 64
			for row := 0; row < 8; row++ {
				p := [8]byte{
					tile[0+row*2], tile[0+row*2+1],
					tile[16+row*2], tile[16+row*2+1],
					tile[32+row*2], tile[32+row*2+1],
					tile[48+row*2], tile[48+row*2+1],
				}
				for col := 0; col < 8; col++ {
					bit := uint(7 - col)
					var v int
					for pl := 0; pl < 8; pl++ {
						v |= int((p[pl]>>bit)&1) << pl
					}
					d.Pixels[ty*8+row][tx*8+col] = uint8(v)
				}
			}
		}
	}
	return d, nil
}

// Image renders a Decoded cover to an RGBA image (index 0 = transparent).
func (d *Decoded) Image() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, d.Cols*8, d.Rows*8))
	draw.Draw(img, img.Bounds(), image.Transparent, image.Point{}, draw.Src)
	for y := range d.Pixels {
		for x, v := range d.Pixels[y] {
			if int(v) < d.CGBase {
				continue // transparent / below palette
			}
			idx := int(v) - d.CGBase
			if idx < 0 || idx >= len(d.Palette) {
				img.Set(x, y, color.RGBA{255, 0, 255, 255})
				continue
			}
			c := d.Palette[idx]
			img.Set(x, y, color.RGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), 255})
		}
	}
	return img
}
