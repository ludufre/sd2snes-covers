// Command covgen is a thin CLI over internal/cov: convert one image to a .cov
// v4 using the exact same code path as the GUI app. Used to cross-check the Go
// encoder against the Python cover_conv.py byte-for-byte, and (optionally) as the
// single source of truth for batch cover generation in the firmware repo.
package main

import (
	"flag"
	"fmt"
	"os"

	// register decoders for the formats libretro boxart ships in
	_ "image/jpeg"
	_ "image/png"

	"github.com/ludufre/sd2snes-covers/internal/cov"
)

func main() {
	out := flag.String("o", "", "output .cov path (default: <src>.cov)")
	wspr := flag.Int("wspr", 0, "force width in 16x16 sprites (disables auto-size)")
	hspr := flag.Int("hspr", 0, "force height in 16x16 sprites (disables auto-size)")
	npal := flag.Int("palettes", 8, "OBJ palettes (1..8)")
	nodither := flag.Bool("no-dither", false, "disable Floyd-Steinberg dithering")
	fill := flag.Bool("fill", false, "crop-to-fill instead of letterbox")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: covgen [flags] <image>")
		os.Exit(2)
	}
	src := flag.Arg(0)
	dst := *out
	if dst == "" {
		dst = src + ".cov"
	}

	o := cov.DefaultOptions()
	o.NPalettes = *npal
	o.Dither = !*nodither
	o.Fill = *fill
	if *wspr > 0 && *hspr > 0 { // explicit dims override auto-size
		o.AutoSize = false
		o.WSpr, o.HSpr = *wspr, *hspr
	}

	if err := cov.ConvertFile(src, dst, o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fi, _ := os.Stat(dst)
	fmt.Printf("%s -> %s (%d bytes)\n", src, dst, fi.Size())
}
