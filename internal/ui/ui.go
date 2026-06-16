// Package ui implements the Fyne desktop interface for sd2snes-covers.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png" // register the PNG decoder for the .cov preview round-trip
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/ludufre/sd2snes-covers/internal/cheats"
	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/pipeline"
	"github.com/ludufre/sd2snes-covers/internal/scan"
	"github.com/ludufre/sd2snes-covers/internal/system"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

var headers = [6]string{"ROM", "CRC32", "No-Intro Match", "Cover", ".cov", "Cheats"}

// Version is shown in the status bar and the window title.
const Version = "v1.4.0"

// preference keys (persisted via Fyne preferences)
const (
	prefDatURL      = "dat_url"       // SNES DAT URL (kept unsuffixed for backward compat)
	prefBoxartBase  = "boxart_base"   // SNES boxart base (kept unsuffixed for backward compat)
	prefLastFolder  = "last_folder"   // remembered ROM folder (native-dialog start dir)
	prefLastSaveDir = "last_save_dir" // remembered CSV export dir (native-dialog start dir)
	forkURL         = "https://github.com/ludufre/sd2snes"
	releasesURL     = "https://github.com/ludufre/sd2snes-covers/releases"
	updateTOMLURL   = "https://raw.githubusercontent.com/ludufre/sd2snes-covers/refs/heads/main/FyneApp.toml"
)

// datPrefKey / boxPrefKey return the preference keys for a system's DAT URL and
// boxart base. SNES keeps the original unsuffixed keys so values customized by
// existing installs are preserved; every other system is suffixed with its key
// (e.g. "dat_url_gb"). The default-migration rules live with each system in
// internal/system (System.LegacyDat / System.LegacyBox).
func datPrefKey(sysKey string) string {
	if sysKey == system.KeySNES {
		return prefDatURL
	}
	return prefDatURL + "_" + sysKey
}

func boxPrefKey(sysKey string) string {
	if sysKey == system.KeySNES {
		return prefBoxartBase
	}
	return prefBoxartBase + "_" + sysKey
}

// UI holds widget state. All widget access happens on the Fyne main goroutine;
// background work communicates back exclusively through fyne.Do.
type UI struct {
	win fyne.Window

	folder   string
	index    dat.Index            // SNES DAT (also gates "DAT loaded"); GB-family DATs below
	lazyDats map[string]dat.Index // GB/GBC/SGB DATs, loaded on demand (worker goroutine only)
	rows     []pipeline.RowResult

	datURL     map[string]string // system key -> configurable DAT URL
	boxartBase map[string]string // system key -> configurable boxart repository base URL
	datDirty   map[string]bool   // GB-family system keys whose DAT URL changed in Settings (force re-download)

	running bool
	cancel  context.CancelFunc

	folderBtn   *widget.Button
	refreshBtn  *widget.Button
	settingsBtn *widget.Button
	startBtn    *widget.Button
	csvBtn      *widget.Button
	convertBtn  *widget.Button // "just convert to .cov": image folder -> .cov, no DAT
	overwrite   *widget.Check
	renameCheck *widget.Check
	covCheck    *widget.Check
	cheatsCheck *widget.Check
	folderLabel *widget.Label
	progress    *widget.ProgressBar
	status      *widget.Label
	summary     *widget.Label
	updateLink  *widget.Hyperlink // shown in the status bar when a newer release exists
	table       *widget.Table

	previewImg   *canvas.Image // box-art image (original)
	covImg       *canvas.Image // .cov rendering (how it looks on the sd2snes)
	previewLabel *widget.Label // caption / status under the preview
	previewKey   string        // game name currently being previewed (guards async loads)
}

// New builds the UI bound to window w.
func New(w fyne.Window) *UI {
	u := &UI{win: w}

	prefs := fyne.CurrentApp().Preferences()
	u.datURL = map[string]string{}
	u.boxartBase = map[string]string{}
	u.lazyDats = map[string]dat.Index{}
	u.datDirty = map[string]bool{}
	for _, s := range system.Systems {
		u.datURL[s.Key] = resolvePref(prefs, datPrefKey(s.Key), s.DefaultDat, s.LegacyDat)
		u.boxartBase[s.Key] = resolvePref(prefs, boxPrefKey(s.Key), s.DefaultBox, s.LegacyBox)
	}
	u.folder = prefs.String(prefLastFolder) // remember the last ROM folder

	u.folderLabel = widget.NewLabel("No folder selected")
	if u.folder != "" {
		u.folderLabel.SetText(u.folder)
	}

	rel, _ := url.Parse(releasesURL)
	u.updateLink = widget.NewHyperlink("", rel)
	u.updateLink.Hide() // revealed by checkUpdate when a newer release exists
	u.status = widget.NewLabel("")
	u.summary = widget.NewLabel("")
	u.progress = widget.NewProgressBar()
	u.overwrite = widget.NewCheck("Overwrite existing", nil)
	u.renameCheck = widget.NewCheck("Rename (No-Intro)", nil)
	u.covCheck = widget.NewCheck("Generate covers (.cov)", nil)
	u.covCheck.SetChecked(true) // .cov generation is the main goal — on by default
	u.cheatsCheck = widget.NewCheck("Download cheats", nil)
	u.cheatsCheck.SetChecked(true) // fetch cheats into <folder>/sd2snes/cheats by default

	u.folderBtn = widget.NewButton("Select ROM folder...", u.onPickFolder)
	u.refreshBtn = widget.NewButton("Refresh DAT", func() { u.loadDAT(true, nil) })
	u.settingsBtn = widget.NewButton("Settings", u.onSettings)
	u.csvBtn = widget.NewButton("Export CSV", u.onExportCSV)
	u.convertBtn = widget.NewButton("Just convert to .cov", u.onJustConvert)
	u.startBtn = widget.NewButton("Start", u.onStartOrStop)

	u.table = u.buildTable()

	u.previewImg = canvas.NewImageFromImage(nil)
	u.previewImg.FillMode = canvas.ImageFillContain
	u.previewImg.SetMinSize(fyne.NewSize(220, 210))
	u.covImg = canvas.NewImageFromImage(nil)
	u.covImg.FillMode = canvas.ImageFillContain
	u.covImg.ScaleMode = canvas.ImageScalePixels // chunky pixels, like on the console
	u.covImg.SetMinSize(fyne.NewSize(220, 165))
	u.previewLabel = widget.NewLabelWithStyle("Select a ROM to preview its cover", fyne.TextAlignCenter, fyne.TextStyle{})
	u.previewLabel.Wrapping = fyne.TextWrapWord

	w.SetOnClosed(func() {
		if u.cancel != nil {
			u.cancel()
		}
	})
	return u
}

// Root returns the root widget tree.
func (u *UI) Root() fyne.CanvasObject {
	fork, _ := url.Parse(forkURL)
	moreInfo := widget.NewHyperlink("[more info]", fork)

	top := container.NewVBox(
		container.NewHBox(u.folderBtn, u.refreshBtn, u.settingsBtn, u.csvBtn, u.convertBtn),
		u.folderLabel,
		container.NewHBox(u.overwrite, u.renameCheck, u.covCheck, u.cheatsCheck, moreInfo, u.startBtn),
		widget.NewSeparator(),
	)

	version := widget.NewLabelWithStyle(Version, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true})
	bottom := container.NewVBox(
		widget.NewSeparator(),
		u.updateLink, // hidden until an update is available
		u.progress,
		u.status,
		container.NewBorder(nil, nil, nil, version, u.summary), // summary left, version right
	)

	// Cover preview panel to the right of the table (resizable split): the box
	// art on top and, below it, how the cover would render as a .cov on the
	// sd2snes — so you can judge the on-device result.
	previewContent := container.NewVBox(
		widget.NewLabelWithStyle("Box art", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		u.previewImg,
		widget.NewLabelWithStyle("As .cov on sd2snes", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		u.covImg,
		u.previewLabel,
	)
	center := container.NewHSplit(u.table, container.NewVScroll(previewContent))
	center.SetOffset(0.70)
	return container.NewBorder(top, bottom, nil, nil, center)
}

// Start kicks off the initial DAT load and an async update check.
func (u *UI) Start() {
	u.loadDAT(false, nil)
	go u.checkUpdate()
}

func (u *UI) buildTable() *widget.Table {
	t := widget.NewTable(
		func() (int, int) { return len(u.rows), len(headers) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis // clip overflow with "…" instead of overlapping
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(cellText(u.rows[id.Row], id.Col))
		},
	)
	// Native header row: shows the column titles and lets the user drag the
	// column separators to resize each column.
	t.ShowHeaderRow = true
	t.CreateHeader = func() fyne.CanvasObject {
		l := widget.NewLabel("")
		l.TextStyle = fyne.TextStyle{Bold: true}
		l.Truncation = fyne.TextTruncateEllipsis
		return l
	}
	t.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		lbl := o.(*widget.Label)
		if id.Row < 0 && id.Col >= 0 && id.Col < len(headers) {
			lbl.SetText(headers[id.Col]) // top header row (data column titles)
		} else {
			lbl.SetText("") // row-header corner, if any
		}
	}
	t.SetColumnWidth(0, 320)
	t.SetColumnWidth(1, 90)
	t.SetColumnWidth(2, 320)
	t.SetColumnWidth(3, 100)
	t.SetColumnWidth(4, 80)
	t.SetColumnWidth(5, 90)

	// Selecting a cell copies its full text to the clipboard — handy since long
	// values are visually truncated with an ellipsis (Fyne tables have no native
	// Ctrl+C on a cell).
	t.OnSelected = func(id widget.TableCellID) {
		if id.Row < 0 || id.Row >= len(u.rows) {
			return
		}
		text := cellText(u.rows[id.Row], id.Col)
		if text == "" {
			return
		}
		fyne.CurrentApp().Clipboard().SetContent(text)
		u.status.SetText("Copied: " + ellipsize(text, 60))
		u.showPreview(u.rows[id.Row])
	}
	return t
}

// showPreview displays the cover for the selected ROM — the box art and a render
// of how it would look as a .cov on the sd2snes. Box art comes from the
// persistent cache (fetched on demand if missing); all IO and the .cov render
// run off the UI goroutine, guarded so a fast re-selection can't show a stale
// image.
func (u *UI) showPreview(r pipeline.RowResult) {
	if r.Match == "" {
		u.previewKey = ""
		u.clearPreview("No match — no cover")
		return
	}
	u.previewKey = r.Match
	name, base, sysKey := r.Match, r.BoxartBase, r.SysKey
	if base == "" {
		base = thumbs.DefaultBoxartBase
	}
	u.clearPreview("Loading cover…")
	go func() {
		cacheDir, err := pipeline.BoxartCacheDir()
		if err != nil {
			fyne.Do(func() {
				if u.previewKey == name {
					u.clearPreview("Cache unavailable")
				}
			})
			return
		}
		path := filepath.Join(cacheDir, sysKey+"_"+thumbs.Sanitize(name)+".png")
		if fi, serr := os.Stat(path); serr != nil || fi.Size() == 0 {
			client := thumbs.NewClient()
			_, _ = thumbs.Download(context.Background(), client, base, name, path, false)
		}
		// Stage 1: show the box art as soon as it's decoded (fast).
		box := decodeImage(path)
		fyne.Do(func() {
			if u.previewKey != name {
				return // selection changed while loading
			}
			if box == nil {
				u.clearPreview("No cover available")
				return
			}
			u.setBox(box, name)
		})
		if box == nil {
			return
		}
		// Stage 2: render the .cov (slower) and show it when ready — runs in
		// parallel so the box art is never blocked by it.
		covPix := renderCov(box)
		fyne.Do(func() {
			if u.previewKey == name {
				u.setCov(covPix)
			}
		})
	}()
}

// clearPreview blanks both preview images and shows a status caption.
func (u *UI) clearPreview(caption string) { u.setBox(nil, caption) }

// setBox shows the box art (and clears the .cov until it's rendered) plus the caption.
func (u *UI) setBox(box image.Image, caption string) {
	u.previewImg.Image = box
	u.previewImg.Refresh()
	u.covImg.Image = nil
	u.covImg.Refresh()
	u.previewLabel.SetText(caption)
}

// setCov shows the rendered .cov once it's ready.
func (u *UI) setCov(covPix image.Image) {
	u.covImg.Image = covPix
	u.covImg.Refresh()
}

// decodeImage decodes the image file at path (nil on failure).
func decodeImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// renderCov renders how src would look as a .cov on the sd2snes
// (encode → decode → rasterize). Returns nil on failure.
func renderCov(src image.Image) image.Image {
	blob, err := cov.Encode(src, cov.DefaultOptions())
	if err != nil {
		return nil
	}
	d, err := cov.Decode(blob)
	if err != nil {
		return nil
	}
	return d.Image()
}

func (u *UI) onPickFolder() {
	start := u.folder // captured on the UI goroutine; used as the dialog's start dir
	go func() {
		opts := []zenity.Option{zenity.Directory(), zenity.Title("Select ROM folder")}
		if start != "" {
			opts = append(opts, zenity.Filename(start))
		}
		path, err := zenity.SelectFile(opts...) // OS-native folder picker
		if err != nil {
			if errors.Is(err, zenity.ErrCanceled) {
				return // user cancelled
			}
			fyne.Do(u.pickFolderFyne) // native dialog unavailable: fall back to Fyne's
			return
		}
		fyne.Do(func() {
			u.folder = path
			u.folderLabel.SetText(path)
			fyne.CurrentApp().Preferences().SetString(prefLastFolder, path)
		})
	}()
}

// pickFolderFyne is the in-app folder picker, used when the OS-native dialog is
// unavailable (e.g. a Linux box without zenity/kdialog installed).
func (u *UI) pickFolderFyne() {
	dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil || lu == nil {
			return
		}
		u.folder = lu.Path()
		u.folderLabel.SetText(u.folder)
	}, u.win)
}

// onJustConvert picks a folder of images (.png/.jpg/.bmp) and converts each one
// straight to a .cov next to it — no CRC32, no DAT, no boxart download. This is
// the quick path for art you already have on disk.
func (u *UI) onJustConvert() {
	start := u.folder // captured on the UI goroutine; used as the dialog's start dir
	go func() {
		opts := []zenity.Option{zenity.Directory(), zenity.Title("Select folder with images (PNG/JPG/BMP)")}
		if start != "" {
			opts = append(opts, zenity.Filename(start))
		}
		path, err := zenity.SelectFile(opts...) // OS-native folder picker
		if err != nil {
			if errors.Is(err, zenity.ErrCanceled) {
				return // user cancelled
			}
			fyne.Do(u.justConvertFyne) // native dialog unavailable: fall back to Fyne's
			return
		}
		fyne.Do(func() { u.runConvert(path) })
	}()
}

// justConvertFyne is the in-app folder picker for the convert flow, used when the
// OS-native dialog is unavailable.
func (u *UI) justConvertFyne() {
	dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil || lu == nil {
			return
		}
		u.runConvert(lu.Path())
	}, u.win)
}

// runConvert turns every supported image under folder into a .cov next to it
// (e.g. cover.png -> cover.cov), honoring the "Overwrite existing" checkbox.
// Progress streams to the status bar; the table stays empty since there is no
// ROM/DAT context here.
func (u *UI) runConvert(folder string) {
	imgs, err := scan.FindImages(folder)
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if len(imgs) == 0 {
		dialog.ShowInformation("Nothing found", "No .png/.jpg/.bmp images in the selected folder.", u.win)
		return
	}

	u.rows = u.rows[:0]
	u.table.Refresh()
	u.summary.SetText("")
	u.progress.SetValue(0)
	u.progress.Max = float64(len(imgs))
	u.previewKey = ""
	u.clearPreview("Select a ROM to preview its cover")
	u.setRunning(true)

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	overwrite := u.overwrite.Checked
	covOpts := cov.DefaultOptions()
	total := len(imgs)

	go func() {
		var ok, skip, errc int
	Loop:
		for i, img := range imgs {
			select {
			case <-ctx.Done():
				break Loop
			default:
			}
			covPath := pipeline.CovPath(img) // <name>.<ext> -> <name>.cov
			if _, serr := os.Stat(covPath); serr == nil && !overwrite {
				skip++
			} else if cerr := cov.ConvertFile(img, covPath, covOpts); cerr != nil {
				errc++
			} else {
				ok++
			}
			// snapshot the running counts for the async UI update
			idx, nOK, nSkip, nErr := i+1, ok, skip, errc
			fyne.Do(func() {
				u.progress.SetValue(float64(idx))
				u.status.SetText(fmt.Sprintf("%d / %d", idx, total))
				u.summary.SetText(fmt.Sprintf("Converted: %d   |   Skipped: %d   |   Errors: %d", nOK, nSkip, nErr))
			})
		}
		fyne.Do(func() {
			u.setRunning(false)
			u.status.SetText(fmt.Sprintf("Converted %d / %d to .cov — done", ok, total))
			dialog.ShowInformation("Done",
				fmt.Sprintf("Converted: %d\nSkipped (already exist): %d\nErrors: %d", ok, skip, errc), u.win)
		})
	}()
}

func (u *UI) onStartOrStop() {
	if u.running {
		if u.cancel != nil {
			u.cancel()
		}
		return
	}
	u.onStart()
}

func (u *UI) onStart() {
	if u.folder == "" {
		dialog.ShowInformation("Warning", "Select a ROM folder first.", u.win)
		return
	}
	if u.index == nil {
		// DAT not ready yet: load it, then start.
		u.loadDAT(false, u.onStart)
		return
	}

	roms, err := scan.FindROMs(u.folder)
	if err != nil {
		dialog.ShowError(err, u.win)
		return
	}
	if len(roms) == 0 {
		dialog.ShowInformation("Nothing found", "No ROMs (.sfc/.smc/.gb/.gbc/.sgb) in the selected folder.", u.win)
		return
	}

	u.rows = u.rows[:0]
	u.table.Refresh()
	u.summary.SetText("")
	u.progress.SetValue(0)
	u.previewKey = ""
	u.clearPreview("Select a ROM to preview its cover")
	u.setRunning(true)

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	opts := pipeline.Options{
		Overwrite:  u.overwrite.Checked,
		Rename:     u.renameCheck.Checked,
		MakeCov:    u.covCheck.Checked,
		CovOpts:    cov.DefaultOptions(),
		MakeCheats: u.cheatsCheck.Checked,
		CheatsDir:  filepath.Join(u.folder, "sd2snes", "cheats"),
	}

	needed := neededKeys(roms)
	out := make(chan pipeline.Progress)
	go func() {
		cat, err := u.buildCatalog(ctx, needed)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, u.win)
				u.status.SetText("Error loading DAT")
			})
			close(out) // let consume() finish and reset the UI
			return
		}
		pipeline.Run(ctx, roms, cat, opts, out)
	}()
	go u.consume(out)
}

// neededKeys returns the set of system keys required to cover all of roms,
// expanding fallbacks (e.g. a .gb ROM needs both the Game Boy and Game Boy Color
// DATs so the GBC fallback can match).
func neededKeys(roms []string) map[string]bool {
	m := map[string]bool{}
	for _, r := range roms {
		keys, _ := system.ForExt(filepath.Ext(r))
		for _, k := range keys {
			m[k] = true
		}
	}
	return m
}

// buildCatalog loads the DAT index and boxart base for every needed system, all
// from the (possibly customized) Settings URLs. SNES reuses the index already
// loaded at startup; the GB-family systems (Game Boy / Game Boy Color / Super
// Game Boy) are loaded and cached on first use, force-re-downloading any whose
// DAT URL was changed in Settings (datDirty). Loading happens off the main
// goroutine (called from onStart's worker) and is never concurrent with itself
// or with Settings edits (the Start/Settings buttons are mutually disabled while
// running), so the lazy maps need no extra locking. Status updates go via fyne.Do.
func (u *UI) buildCatalog(ctx context.Context, needed map[string]bool) (*pipeline.Catalog, error) {
	cat := &pipeline.Catalog{
		Index:  map[string]dat.Index{},
		Boxart: map[string]string{},
	}
	if needed[system.KeySNES] && u.index != nil {
		cat.Index[system.KeySNES] = u.index
		cat.Boxart[system.KeySNES] = u.boxartBase[system.KeySNES]
	}
	for _, s := range system.Systems {
		if s.Key == system.KeySNES || !needed[s.Key] {
			continue // SNES is handled above; skip systems not present in this run
		}
		idx := u.lazyDats[s.Key]
		if idx == nil || u.datDirty[s.Key] {
			label := s.Name
			fyne.Do(func() { u.status.SetText("Loading " + label + " DAT...") })
			loaded, err := dat.Load(ctx, s.Key, u.datURL[s.Key], u.datDirty[s.Key])
			if err != nil {
				return nil, fmt.Errorf("%s DAT: %w", label, err)
			}
			idx = loaded
			u.lazyDats[s.Key] = loaded
			delete(u.datDirty, s.Key)
		}
		cat.Index[s.Key] = idx
		cat.Boxart[s.Key] = u.boxartBase[s.Key]
	}
	return cat, nil
}

// consume drains progress events and applies UI updates on the main goroutine.
func (u *UI) consume(out <-chan pipeline.Progress) {
	for p := range out {
		p := p
		fyne.Do(func() {
			u.rows = append(u.rows, p.Row)
			u.progress.Max = float64(p.Total)
			u.progress.SetValue(float64(p.Index))
			u.status.SetText(fmt.Sprintf("%d / %d", p.Index, p.Total))
			u.table.Refresh()
			u.updateSummary()
		})
	}
	fyne.Do(func() {
		u.setRunning(false)
		if u.status.Text != "" {
			u.status.SetText(u.status.Text + " — done")
		}
	})
}

func (u *UI) loadDAT(force bool, then func()) {
	u.refreshBtn.Disable()
	u.startBtn.Disable()
	u.status.SetText("Loading DAT...")
	datURL := u.datURL[system.KeySNES]
	go func() {
		idx, err := dat.Load(context.Background(), system.KeySNES, datURL, force)
		fyne.Do(func() {
			u.refreshBtn.Enable()
			u.startBtn.Enable()
			if err != nil {
				u.status.SetText("Error loading DAT")
				dialog.ShowError(err, u.win)
				return
			}
			u.index = idx
			u.status.SetText(fmt.Sprintf("DAT loaded: %d games", len(idx)))
			if then != nil {
				then()
			}
		})
	}()
}

func (u *UI) setRunning(running bool) {
	u.running = running
	if running {
		u.startBtn.SetText("Stop")
		u.folderBtn.Disable()
		u.refreshBtn.Disable()
		u.settingsBtn.Disable()
		u.csvBtn.Disable()
		u.convertBtn.Disable()
		u.overwrite.Disable()
		u.renameCheck.Disable()
		u.covCheck.Disable()
		u.cheatsCheck.Disable()
	} else {
		u.startBtn.SetText("Start")
		u.folderBtn.Enable()
		u.refreshBtn.Enable()
		u.settingsBtn.Enable()
		u.csvBtn.Enable()
		u.convertBtn.Enable()
		u.overwrite.Enable()
		u.renameCheck.Enable()
		u.covCheck.Enable()
		u.cheatsCheck.Enable()
		u.cancel = nil
	}
}

// onSettings opens a dialog to edit, per system, the No-Intro DAT URL and the
// libretro boxart repository, persisting them via Fyne preferences. Each system
// (Super Nintendo, Game Boy, Game Boy Color, Super Game Boy) gets its own
// collapsible section. Changing a system's DAT URL forces that DAT to be
// re-fetched: SNES reloads immediately; the GB-family DATs are re-downloaded the
// next time a matching ROM is scanned (see buildCatalog / datDirty).
func (u *UI) onSettings() {
	prefs := fyne.CurrentApp().Preferences()

	// One accordion section per system, each with its DAT URL and cover entries.
	type sysRow struct {
		sys system.System
		dat *widget.Entry
		box *widget.Entry
	}
	var rows []sysRow
	acc := widget.NewAccordion()
	acc.MultiOpen = true // let several systems stay expanded at once
	for _, s := range system.Systems {
		datEntry := widget.NewEntry()
		datEntry.SetText(u.datURL[s.Key])
		boxEntry := widget.NewEntry()
		boxEntry.SetText(u.boxartBase[s.Key])
		form := widget.NewForm(
			widget.NewFormItem("DAT URL (No-Intro)", datEntry),
			widget.NewFormItem("Cover repository", boxEntry),
		)
		acc.Append(widget.NewAccordionItem(s.Label(), form))
		rows = append(rows, sysRow{s, datEntry, boxEntry})
	}
	acc.Open(0) // expand Super Nintendo by default

	reset := widget.NewButton("Restore defaults", func() {
		for _, r := range rows {
			r.dat.SetText(r.sys.DefaultDat)
			r.box.SetText(r.sys.DefaultBox)
		}
	})

	clearCache := widget.NewButton("Clear cover cache", func() {
		n, freed, err := pipeline.ClearBoxartCache()
		if err != nil {
			dialog.ShowError(err, u.win)
			return
		}
		dialog.ShowInformation("Cover cache cleared",
			fmt.Sprintf("Removed %d cached cover(s), freed %s.", n, humanBytes(freed)), u.win)
	})

	content := container.NewVBox(
		acc,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel("Downloaded covers"), nil, clearCache),
		reset,
	)

	d := dialog.NewCustomConfirm("Settings", "Save", "Cancel", container.NewVScroll(content), func(ok bool) {
		if !ok {
			return
		}
		snesChanged := false
		for _, r := range rows {
			newDat := strings.TrimSpace(r.dat.Text)
			newBox := strings.TrimSpace(r.box.Text)
			if newDat == "" {
				newDat = r.sys.DefaultDat
			}
			if newBox == "" {
				newBox = r.sys.DefaultBox
			}
			if newDat != u.datURL[r.sys.Key] {
				if r.sys.Key == system.KeySNES {
					snesChanged = true // SNES is eager: reload below
				} else {
					u.datDirty[r.sys.Key] = true  // GB-family: force re-download on next use
					delete(u.lazyDats, r.sys.Key) // drop the stale in-memory index
				}
			}
			u.datURL[r.sys.Key] = newDat
			u.boxartBase[r.sys.Key] = newBox
			setPrefOrDefault(prefs, datPrefKey(r.sys.Key), newDat, r.sys.DefaultDat)
			setPrefOrDefault(prefs, boxPrefKey(r.sys.Key), newBox, r.sys.DefaultBox)
		}
		if snesChanged {
			u.loadDAT(true, nil) // re-fetch SNES with the new DAT URL
		}
	}, u.win)
	d.Resize(fyne.NewSize(700, 520))
	d.Show()
}

// onExportCSV writes the current results to a CSV chosen via a save dialog.
func (u *UI) onExportCSV() {
	if len(u.rows) == 0 {
		dialog.ShowInformation("No data", "Run a scan before exporting.", u.win)
		return
	}
	rows := append([]pipeline.RowResult(nil), u.rows...) // snapshot for the goroutine
	prefs := fyne.CurrentApp().Preferences()
	start := prefs.String(prefLastSaveDir)
	go func() {
		def := "sd2snes-covers.csv"
		if start != "" {
			def = filepath.Join(start, def)
		}
		path, err := zenity.SelectFileSave( // OS-native save dialog
			zenity.Title("Export CSV"),
			zenity.ConfirmOverwrite(),
			zenity.Filename(def),
			zenity.FileFilters{{Name: "CSV files", Patterns: []string{"*.csv"}, CaseFold: true}},
		)
		if err != nil {
			if errors.Is(err, zenity.ErrCanceled) {
				return // user cancelled
			}
			fyne.Do(func() { u.exportCSVFyne(rows) }) // native dialog unavailable: fall back
			return
		}
		if filepath.Ext(path) == "" {
			path += ".csv"
		}
		werr := writeCSVFile(path, rows)
		fyne.Do(func() {
			if werr != nil {
				dialog.ShowError(werr, u.win)
				return
			}
			prefs.SetString(prefLastSaveDir, filepath.Dir(path)) // remember for next time
			u.status.SetText("CSV exported: " + ellipsize(path, 60))
		})
	}()
}

// exportCSVFyne is the in-app save dialog, used when the OS-native one isn't available.
func (u *UI) exportCSVFyne(rows []pipeline.RowResult) {
	save := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		defer wc.Close()
		if werr := pipeline.WriteCSV(wc, rows); werr != nil {
			dialog.ShowError(werr, u.win)
		}
	}, u.win)
	save.SetFileName("sd2snes-covers.csv")
	save.Show()
}

// writeCSVFile writes the results CSV to path.
func writeCSVFile(path string, rows []pipeline.RowResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pipeline.WriteCSV(f, rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (u *UI) updateSummary() {
	var ok, notFound, skip, noMatch, errc, renamed, covOK, cheatsOK int
	for _, r := range u.rows {
		if r.NewName != "" {
			renamed++
		}
		if r.Cov == pipeline.CovOK {
			covOK++
		}
		if r.Cheat == cheats.StatusOK {
			cheatsOK++
		}
		if r.Err != nil {
			errc++
			continue
		}
		switch r.Cover {
		case thumbs.StatusOK:
			ok++
		case thumbs.StatusNotFound:
			notFound++
		case thumbs.StatusSkip:
			skip++
		case thumbs.StatusNoMatch:
			noMatch++
		case thumbs.StatusError:
			errc++
		}
	}
	u.summary.SetText(fmt.Sprintf(
		"Cover OK: %d   |   Not found: %d   |   Skipped: %d   |   No match: %d   |   Renamed: %d   |   .cov: %d   |   Cheats: %d   |   Errors: %d",
		ok, notFound, skip, noMatch, renamed, covOK, cheatsOK, errc,
	))
}

func displayStatus(r pipeline.RowResult) string {
	if r.Err != nil {
		return "Error"
	}
	return r.Cover.String()
}

// cellText returns the text shown (and copied on selection) for a table cell.
func cellText(r pipeline.RowResult, col int) string {
	switch col {
	case 0:
		if r.NewName != "" {
			return r.NewName // show the renamed file
		}
		return r.ROMName
	case 1:
		return r.CRC
	case 2:
		return r.Match
	case 3:
		return displayStatus(r)
	case 4:
		return r.Cov.String()
	case 5:
		return r.Cheat.String()
	}
	return ""
}

// ellipsize shortens s to at most max runes, adding an ellipsis when truncated.
func ellipsize(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// humanBytes formats a byte count as a short human-readable size.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// checkUpdate fetches the published FyneApp.toml and, if its version is newer
// than this build, reveals a download link in the status bar. Failures are silent.
func (u *UI) checkUpdate() {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(updateTOMLURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return
	}
	remote := tomlVersion(string(body))
	if remote == "" || !versionLess(strings.TrimPrefix(Version, "v"), remote) {
		return // unknown, same, or older than this build
	}
	fyne.Do(func() {
		u.updateLink.SetText(fmt.Sprintf("Update available: v%s — download", remote))
		u.updateLink.Show()
	})
}

// tomlVersion extracts the Version value from a FyneApp.toml body ("" if absent).
func tomlVersion(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Version") {
			continue
		}
		if i := strings.IndexByte(line, '='); i >= 0 {
			return strings.TrimPrefix(strings.Trim(strings.TrimSpace(line[i+1:]), `"'`), "v")
		}
	}
	return ""
}

// versionLess reports whether dotted-numeric version a is older than b.
func versionLess(a, b string) bool {
	pa, pb := parseVer(a), parseVer(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func parseVer(s string) []int {
	fields := strings.Split(s, ".")
	out := make([]int, len(fields))
	for i, f := range fields {
		out[i], _ = strconv.Atoi(strings.TrimFunc(f, func(r rune) bool { return r < '0' || r > '9' }))
	}
	return out
}

// pickPref decides the effective value for a stored preference and whether the
// stored key should be cleared because it only holds a (current or old) default.
// A value that is neither empty nor a known default is a genuine customization.
func pickPref(stored, def string, legacy []string) (value string, clear bool) {
	if stored == "" || stored == def {
		return def, false
	}
	for _, old := range legacy {
		if stored == old {
			return def, true // stored an old default → migrate to the new one
		}
	}
	return stored, false
}

// resolvePref returns the effective value for key, migrating old defaults to the
// current one (so a changed default reaches existing installs) while preserving
// genuine custom values.
func resolvePref(prefs fyne.Preferences, key, def string, legacy []string) string {
	value, clear := pickPref(prefs.String(key), def, legacy)
	if clear {
		prefs.RemoveValue(key)
	}
	return value
}

// setPrefOrDefault stores value under key, or removes the key when value equals
// the default — defaults are never persisted, so future default changes apply
// automatically to non-customized installs.
func setPrefOrDefault(prefs fyne.Preferences, key, value, def string) {
	if value == def {
		prefs.RemoveValue(key)
		return
	}
	prefs.SetString(key, value)
}
