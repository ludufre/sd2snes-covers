// Package pipeline orchestrates scanning ROMs, looking up names, downloading
// boxart, optionally renaming ROMs and generating .cov covers — streaming
// progress back to the caller.
package pipeline

import (
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/snes"
	"github.com/ludufre/sd2snes-covers/internal/system"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

const defaultWorkers = 6

// Catalog holds the per-system DAT index and boxart base, keyed by system key
// (see internal/system). process() picks the system(s) by ROM extension.
type Catalog struct {
	Index  map[string]dat.Index // system key -> CRC32 -> No-Intro name
	Boxart map[string]string    // system key -> libretro boxart base URL
}

// Options configures a pipeline run.
type Options struct {
	Overwrite bool
	Workers   int
	Rename    bool        // rename matched ROMs to their No-Intro name
	MakeCov   bool        // generate a .cov cover next to each downloaded boxart
	CovOpts   cov.Options // .cov parameters (used when MakeCov)
}

// CovStatus is the outcome of .cov generation for a ROM.
type CovStatus int

const (
	CovNone  CovStatus = iota // not requested, or no source image
	CovOK                     // .cov generated
	CovSkip                   // .cov already existed (not overwritten)
	CovError                  // generation failed
)

func (c CovStatus) String() string {
	switch c {
	case CovOK:
		return "OK"
	case CovSkip:
		return "Skipped"
	case CovError:
		return "Error"
	default:
		return "-" // ASCII so it stays readable in CSV (Excel ignores the BOM with sep=)
	}
}

// RowResult is the outcome for a single ROM.
type RowResult struct {
	ROMPath    string
	ROMName    string // original basename at scan time
	CRC        string
	Match      string // No-Intro game name, or "" when not found in the DAT
	URL        string // boxart URL for the match ("" when there is no match)
	BoxartBase string // boxart base of the system the ROM matched (for the preview)
	SysKey     string // system key the ROM matched (cache-key prefix for the preview)
	Cover      thumbs.Status
	NewName    string // basename after rename, or "" when not renamed
	Cov        CovStatus
	Err        error
}

// Progress carries one completed ROM and overall counters.
type Progress struct {
	Index int // 1-based number of completed items
	Total int
	Row   RowResult
}

// Run processes roms concurrently, sending one Progress per ROM on out and
// closing out when finished. It honors ctx cancellation. cat supplies the
// per-system DAT index and boxart base; the system is chosen by ROM extension.
func Run(ctx context.Context, roms []string, cat *Catalog, opts Options, out chan<- Progress) {
	defer close(out)

	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	client := thumbs.NewClient()
	total := len(roms)

	// Boxart PNGs are cached between runs (so re-runs and duplicate ROMs don't
	// re-download); the cache persists and is cleared explicitly from Settings.
	// An empty cacheDir means "fall back to downloading next to the ROM".
	cacheDir, cerr := BoxartCacheDir()
	if cerr != nil {
		cacheDir = ""
	}

	jobs := make(chan string)
	results := make(chan RowResult)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for romPath := range jobs {
				results <- process(ctx, client, cat, opts, cacheDir, romPath)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, r := range roms {
			select {
			case <-ctx.Done():
				return
			case jobs <- r:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	count := 0
	for res := range results {
		count++
		out <- Progress{Index: count, Total: total, Row: res}
	}
}

func process(ctx context.Context, client *http.Client, cat *Catalog, opts Options, cacheDir, romPath string) RowResult {
	row := RowResult{ROMPath: romPath, ROMName: filepath.Base(romPath)}

	keys, stripHeader := system.ForExt(filepath.Ext(romPath))
	if len(keys) == 0 {
		row.Cover = thumbs.StatusNoMatch // unknown extension
		return row
	}

	// CRC32: SNES skips the 512-byte copier header; Game Boy hashes the whole file.
	var crc uint32
	var err error
	if stripHeader {
		crc, _, err = snes.CRC32Headerless(romPath)
	} else {
		crc, err = snes.CRC32Plain(romPath)
	}
	if err != nil {
		row.Err = err
		row.Cover = thumbs.StatusError
		return row
	}
	row.CRC = snes.CRCHex(crc)

	// Try each system for this extension in order (e.g. .gb -> Game Boy, then GBC).
	var name, base, sysKey string
	for _, k := range keys {
		if idx := cat.Index[k]; idx != nil {
			if n, ok := idx[row.CRC]; ok {
				name, base, sysKey = n, cat.Boxart[k], k
				break
			}
		}
	}
	if name == "" {
		row.Cover = thumbs.StatusNoMatch
		return row
	}
	row.Match = name
	if base == "" {
		base = thumbs.DefaultBoxartBase
	}
	row.BoxartBase = base
	row.SysKey = sysKey
	row.URL = thumbs.BoxartURLFrom(base, name) // recorded in the CSV (the URL that 404s on "Not found")

	// Optional rename to the No-Intro name (keep the original extension).
	if opts.Rename {
		if newPath, renamed, rerr := renameToNoIntro(romPath, name); rerr != nil {
			row.Err = rerr
		} else if renamed {
			romPath = newPath
			row.ROMPath = newPath
			row.NewName = filepath.Base(newPath)
		}
	}

	finalPNG := CoverPath(romPath) // <rom>.png next to the ROM (written only when not making a .cov)
	covPath := CovPath(romPath)    // <rom>.cov next to the ROM

	// When "Overwrite existing" is off, skip work whose primary output already
	// exists. The primary output is the .cov when MakeCov is on, otherwise the
	// boxart PNG next to the ROM.
	if !opts.Overwrite {
		if opts.MakeCov {
			if fileExists(covPath) {
				row.Cover = thumbs.StatusSkip
				row.Cov = CovSkip
				return row
			}
		} else if fileExists(finalPNG) {
			row.Cover = thumbs.StatusSkip
			return row
		}
	}

	// Obtain the boxart: reuse the persistent cache when present, otherwise
	// download into it. The cache is keyed by the game's boxart filename, so the
	// same game is fetched only once across runs and across duplicate ROMs.
	cachePNG := finalPNG // fallback target when there is no cache dir
	if cacheDir != "" {
		// prefix with the system key so same-named games on different systems
		// (e.g. a SNES and a Game Boy game) don't share one cached PNG.
		cachePNG = filepath.Join(cacheDir, sysKey+"_"+thumbs.Sanitize(name)+".png")
	}
	if cacheDir != "" && fileExists(cachePNG) {
		row.Cover = thumbs.StatusOK // cache hit
	} else {
		status, derr := thumbs.Download(ctx, client, base, name, cachePNG, true)
		row.Cover = status
		if derr != nil && row.Err == nil {
			row.Err = derr
		}
		if status != thumbs.StatusOK {
			return row // 404 or error: nothing to produce
		}
	}

	// Produce the output next to the ROM, keeping the cached PNG intact.
	if opts.MakeCov {
		row.Cov = makeCov(cachePNG, covPath, opts)
	} else if cachePNG != finalPNG {
		// .cov not requested: copy the cached boxart next to the ROM
		if err := copyFile(cachePNG, finalPNG); err != nil && row.Err == nil {
			row.Err = err
			row.Cover = thumbs.StatusError
		}
	}
	return row
}

// makeCov converts pngPath into covPath, honoring the overwrite option.
func makeCov(pngPath, covPath string, opts Options) CovStatus {
	if !opts.Overwrite && fileExists(covPath) {
		return CovSkip
	}
	if err := cov.ConvertFile(pngPath, covPath, opts.CovOpts); err != nil {
		return CovError
	}
	return CovOK
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copyFile copies src to dst atomically (temp file + rename), leaving src in
// place — used to place a cached boxart next to a ROM without consuming the cache.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".cp-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// BoxartCacheDir returns the directory where downloaded boxart PNGs are cached
// between runs, creating it on demand. Override with SD2SNES_COVERS_CACHE.
func BoxartCacheDir() (string, error) {
	dir := os.Getenv("SD2SNES_COVERS_CACHE")
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "sd2snes-covers", "boxart")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ClearBoxartCache deletes every cached boxart PNG, returning how many files
// were removed and how many bytes were freed.
func ClearBoxartCache() (files int, freed int64, err error) {
	dir, derr := BoxartCacheDir()
	if derr != nil {
		return 0, 0, derr
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		return 0, 0, rerr
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, ierr := e.Info(); ierr == nil {
			freed += info.Size()
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			files++
		}
	}
	return files, freed, nil
}

// renameToNoIntro renames romPath to "<fs-safe No-Intro name><ext>" in the same
// directory. It never overwrites a different existing file. Returns the new
// path and whether a rename happened.
func renameToNoIntro(romPath, name string) (string, bool, error) {
	dir := filepath.Dir(romPath)
	ext := filepath.Ext(romPath)
	target := filepath.Join(dir, fsSafeName(name)+ext)
	if target == romPath {
		return romPath, false, nil // already named correctly
	}

	src, _ := os.Stat(romPath)
	if dst, err := os.Stat(target); err == nil {
		if !os.SameFile(src, dst) {
			return romPath, false, nil // a different file is already there: don't clobber
		}
		// same file (e.g. case-only change on a case-insensitive FS): fall through
	}
	if err := os.Rename(romPath, target); err != nil {
		return romPath, false, err
	}

	// Carry existing cover/.cov siblings to the new basename so the old-named
	// files are never left orphaned after a rename.
	oldBase := strings.TrimSuffix(romPath, filepath.Ext(romPath))
	newBase := strings.TrimSuffix(target, filepath.Ext(target))
	for _, ext := range []string{".png", ".cov"} {
		moveSibling(oldBase+ext, newBase+ext)
	}
	return target, true, nil
}

// moveSibling moves old -> new when old exists. If a different file already
// occupies new, the old file is removed instead (so no orphan lingers); a
// case-only difference on a case-insensitive FS is treated as a rename.
func moveSibling(old, new string) {
	if old == new {
		return
	}
	so, err := os.Stat(old)
	if err != nil {
		return // nothing to move
	}
	if sn, statErr := os.Stat(new); statErr == nil {
		if os.SameFile(so, sn) {
			_ = os.Rename(old, new) // case-only rename
			return
		}
		_ = os.Remove(old) // a real file is already at new: drop the orphan
		return
	}
	_ = os.Rename(old, new)
}

// fsReserved replaces characters that are illegal in filenames on Windows (and
// path separators) so the renamed file is valid on every OS.
var fsReserved = strings.NewReplacer(
	"<", "_", ">", "_", ":", "_", "\"", "_",
	"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
)

func fsSafeName(s string) string {
	return strings.TrimRight(fsReserved.Replace(s), " .")
}

// CoverPath returns the .png path next to the ROM, reusing the ROM's basename
// (e.g. /games/smw.sfc -> /games/smw.png).
func CoverPath(romPath string) string {
	ext := filepath.Ext(romPath)
	return strings.TrimSuffix(romPath, ext) + ".png"
}

// CovPath returns the .cov path next to the ROM (e.g. /games/smw.sfc -> /games/smw.cov).
func CovPath(romPath string) string {
	ext := filepath.Ext(romPath)
	return strings.TrimSuffix(romPath, ext) + ".cov"
}

// WriteCSV writes the results as CSV to w. Fields are RFC-4180 quoted by
// encoding/csv (commas, quotes and newlines are escaped). A UTF-8 BOM and an
// Excel "sep=," hint are prepended, with CRLF line endings, so spreadsheets
// open it with the right columns and accents in any locale.
func WriteCSV(w io.Writer, rows []RowResult) error {
	if _, err := io.WriteString(w, "\ufeffsep=,\r\n"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	header := []string{"File", "CRC32", "No-Intro", "Cover", "Renamed to", "cov", "Boxart URL", "Error"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		errMsg := ""
		if r.Err != nil {
			errMsg = r.Err.Error()
		}
		rec := []string{r.ROMName, r.CRC, r.Match, r.Cover.String(), r.NewName, r.Cov.String(), r.URL, errMsg}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
