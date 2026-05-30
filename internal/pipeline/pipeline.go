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
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

const defaultWorkers = 6

// Options configures a pipeline run.
type Options struct {
	Overwrite  bool
	Workers    int
	Rename     bool        // rename matched ROMs to their No-Intro name
	MakeCov    bool        // generate a .cov cover next to each downloaded boxart
	CovOpts    cov.Options // .cov parameters (used when MakeCov)
	BoxartBase string      // boxart repository base URL ("" = default)
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
		return "—"
	}
}

// RowResult is the outcome for a single ROM.
type RowResult struct {
	ROMPath string
	ROMName string // original basename at scan time
	CRC     string
	Match   string // No-Intro game name, or "" when not found in the DAT
	Cover   thumbs.Status
	NewName string // basename after rename, or "" when not renamed
	Cov     CovStatus
	Err     error
}

// Progress carries one completed ROM and overall counters.
type Progress struct {
	Index int // 1-based number of completed items
	Total int
	Row   RowResult
}

// Run processes roms concurrently, sending one Progress per ROM on out and
// closing out when finished. It honors ctx cancellation.
func Run(ctx context.Context, roms []string, index dat.Index, opts Options, out chan<- Progress) {
	defer close(out)

	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	client := thumbs.NewClient()
	total := len(roms)

	jobs := make(chan string)
	results := make(chan RowResult)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for romPath := range jobs {
				results <- process(ctx, client, index, opts, romPath)
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

func process(ctx context.Context, client *http.Client, index dat.Index, opts Options, romPath string) RowResult {
	row := RowResult{ROMPath: romPath, ROMName: filepath.Base(romPath)}

	crc, _, err := snes.CRC32Headerless(romPath)
	if err != nil {
		row.Err = err
		row.Cover = thumbs.StatusError
		return row
	}
	row.CRC = snes.CRCHex(crc)

	name, ok := index[row.CRC]
	if !ok {
		row.Cover = thumbs.StatusNoMatch
		return row
	}
	row.Match = name

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

	coverPath := CoverPath(romPath)
	status, derr := thumbs.Download(ctx, client, opts.BoxartBase, name, coverPath, opts.Overwrite)
	row.Cover = status
	if derr != nil && row.Err == nil {
		row.Err = derr
	}

	// Optional .cov generation from the cover image next to the ROM.
	if opts.MakeCov {
		row.Cov = makeCov(coverPath, romPath, opts)
	}
	return row
}

// makeCov converts the cover PNG next to the ROM into a .cov file.
func makeCov(coverPath, romPath string, opts Options) CovStatus {
	if _, err := os.Stat(coverPath); err != nil {
		return CovNone // no source image to convert
	}
	covPath := strings.TrimSuffix(romPath, filepath.Ext(romPath)) + ".cov"
	if !opts.Overwrite {
		if _, err := os.Stat(covPath); err == nil {
			return CovSkip
		}
	}
	if err := cov.ConvertFile(coverPath, covPath, opts.CovOpts); err != nil {
		return CovError
	}
	return CovOK
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

// WriteCSV writes the results as CSV (UTF-8) to w.
func WriteCSV(w io.Writer, rows []RowResult) error {
	cw := csv.NewWriter(w)
	header := []string{"File", "CRC32", "No-Intro", "Cover", "Renamed to", "cov", "Error"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		errMsg := ""
		if r.Err != nil {
			errMsg = r.Err.Error()
		}
		rec := []string{r.ROMName, r.CRC, r.Match, r.Cover.String(), r.NewName, r.Cov.String(), errMsg}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
