// Command covgen converts box art to the .cov v4 format using the exact same
// code path as the GUI app. Two modes:
//
//	covgen [flags] <image>      convert one image to <image>.cov
//	covgen [flags] <directory>  batch: scan the folder for ROMs (.sfc/.smc/.gb/
//	                            .gbc/.sgb), match each by CRC32 against the
//	                            No-Intro DAT for its system, download the libretro
//	                            box art and write <rom>.cov next to each ROM.
//
// The batch mode reuses internal/pipeline (the same scan -> CRC -> DAT -> boxart
// -> .cov path as the GUI), so it is multi-system: SNES uses the headerless CRC
// and the SNES DAT/boxart; the Game Boy family (.gb/.gbc/.sgb) uses the whole-file
// CRC. .sgb is treated as its own Super Game Boy system that defaults to the Game
// Boy sources, then falls back to Game Boy Color. The CLI always uses the built-in
// default DAT/boxart URLs (per-system overrides live only in the GUI's Settings).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	// register decoders for the formats libretro boxart ships in
	_ "image/jpeg"
	_ "image/png"

	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/pipeline"
	"github.com/ludufre/sd2snes-covers/internal/scan"
	"github.com/ludufre/sd2snes-covers/internal/system"
)

func main() {
	out := flag.String("o", "", "single-image mode: output .cov path (default: <src>.cov)")
	wspr := flag.Int("wspr", 0, "force width in 16x16 sprites (disables auto-size)")
	hspr := flag.Int("hspr", 0, "force height in 16x16 sprites (disables auto-size)")
	npal := flag.Int("palettes", 8, "OBJ palettes (1..8)")
	nodither := flag.Bool("no-dither", false, "disable Floyd-Steinberg dithering")
	fill := flag.Bool("fill", false, "crop-to-fill instead of letterbox")
	overwrite := flag.Bool("overwrite", false, "batch mode: regenerate .cov files that already exist")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: covgen [flags] <image|directory>")
		os.Exit(2)
	}
	src := flag.Arg(0)

	o := cov.DefaultOptions()
	o.NPalettes = *npal
	o.Dither = !*nodither
	o.Fill = *fill
	if *wspr > 0 && *hspr > 0 { // explicit dims override auto-size
		o.AutoSize = false
		o.WSpr, o.HSpr = *wspr, *hspr
	}

	// Directory -> batch ROM pipeline; file -> single image conversion.
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		if err := runBatch(src, *overwrite, o); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	dst := *out
	if dst == "" {
		dst = src + ".cov"
	}
	if err := cov.ConvertFile(src, dst, o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fi, _ := os.Stat(dst)
	fmt.Printf("%s -> %s (%d bytes)\n", src, dst, fi.Size())
}

// runBatch scans dir (recursively) for ROMs and writes a .cov next to each one,
// using the multi-system pipeline. It never renames ROMs (safe to point at an SD
// card) and skips ROMs whose .cov already exists unless overwrite is set.
func runBatch(dir string, overwrite bool, covOpts cov.Options) error {
	ctx := context.Background()

	roms, err := scan.FindROMs(dir)
	if err != nil {
		return err
	}
	if len(roms) == 0 {
		return fmt.Errorf("no ROMs (.sfc/.smc/.gb/.gbc/.sgb) under %s", dir)
	}

	// Load only the DATs needed by the ROMs present (expanding fallbacks: a .gb
	// ROM needs both Game Boy and Game Boy Color so the GBC fallback can match).
	needed := map[string]bool{}
	for _, r := range roms {
		keys, _ := system.ForExt(filepath.Ext(r))
		for _, k := range keys {
			needed[k] = true
		}
	}
	cat := &pipeline.Catalog{
		Index:  map[string]dat.Index{},
		Boxart: map[string]string{},
	}
	for _, s := range system.Systems {
		if !needed[s.Key] {
			continue
		}
		fmt.Fprintf(os.Stderr, "loading %s DAT...\n", s.Name)
		idx, err := dat.Load(ctx, s.Key, s.DefaultDat, false)
		if err != nil {
			return fmt.Errorf("%s DAT: %w", s.Name, err)
		}
		cat.Index[s.Key] = idx
		cat.Boxart[s.Key] = s.DefaultBox
		fmt.Fprintf(os.Stderr, "  %s: %d games\n", s.Name, len(idx))
	}

	opts := pipeline.Options{
		Overwrite: overwrite,
		Rename:    false, // never rename ROMs on disk (SD-safe)
		MakeCov:   true,
		CovOpts:   covOpts,
	}

	outc := make(chan pipeline.Progress)
	go pipeline.Run(ctx, roms, cat, opts, outc)

	var ok, skip, nomatch, errc int
	for p := range outc {
		r := p.Row
		switch {
		case r.Err != nil:
			errc++
			fmt.Printf("[%d/%d] ERR  %s: %v\n", p.Index, p.Total, r.ROMName, r.Err)
		case r.Cov == pipeline.CovOK:
			ok++
			fmt.Printf("[%d/%d] cov  %s  (%s)\n", p.Index, p.Total, r.ROMName, r.Match)
		case r.Cov == pipeline.CovSkip:
			skip++
		default: // no DAT match or no box art for the match
			nomatch++
			fmt.Printf("[%d/%d] miss %s\n", p.Index, p.Total, r.ROMName)
		}
	}
	fmt.Printf("\ndone: cov=%d skip=%d miss=%d err=%d  (%d ROMs)\n", ok, skip, nomatch, errc, len(roms))
	return nil
}
