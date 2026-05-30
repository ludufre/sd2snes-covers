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

	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/pipeline"
	"github.com/ludufre/sd2snes-covers/internal/scan"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

var headers = [5]string{"ROM", "CRC32", "No-Intro Match", "Cover", ".cov"}

// Version is shown in the status bar and the window title.
const Version = "v1.1.0"

// preference keys (persisted via Fyne preferences)
const (
	prefDatURL      = "dat_url"
	prefBoxartBase  = "boxart_base"
	prefLastFolder  = "last_folder"   // remembered ROM folder (native-dialog start dir)
	prefLastSaveDir = "last_save_dir" // remembered CSV export dir (native-dialog start dir)
	forkURL         = "https://github.com/ludufre/sd2snes"
	releasesURL     = "https://github.com/ludufre/sd2snes-covers/releases"
	updateTOMLURL   = "https://raw.githubusercontent.com/ludufre/sd2snes-covers/refs/heads/main/FyneApp.toml"
)

// legacyDatURLs / legacyBoxartBases hold PAST values of the matching defaults
// (seeded with the current ones). A stored value equal to the current default OR
// to any entry here is treated as "not customized", so changing a default below
// propagates to existing installs while a genuine custom URL is preserved.
//
// HOW TO CHANGE A DEFAULT: edit dat.DefaultURL / thumbs.DefaultBoxartBase, then
// make sure the value you are replacing is still listed here (the current literal
// already is, so the next change migrates automatically; for later changes append
// the old value).
var (
	legacyDatURLs = []string{
		"https://raw.githubusercontent.com/libretro/libretro-database/refs/heads/master/metadat/no-intro/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System.dat",
	}
	legacyBoxartBases = []string{
		"https://thumbnails.libretro.com/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/",
	}
)

// UI holds widget state. All widget access happens on the Fyne main goroutine;
// background work communicates back exclusively through fyne.Do.
type UI struct {
	win fyne.Window

	folder string
	index  dat.Index
	rows   []pipeline.RowResult

	datURL     string // configurable DAT URL
	boxartBase string // configurable boxart repository base URL

	running bool
	cancel  context.CancelFunc

	folderBtn   *widget.Button
	refreshBtn  *widget.Button
	settingsBtn *widget.Button
	startBtn    *widget.Button
	csvBtn      *widget.Button
	overwrite   *widget.Check
	renameCheck *widget.Check
	covCheck    *widget.Check
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
	u.datURL = resolvePref(prefs, prefDatURL, dat.DefaultURL, legacyDatURLs)
	u.boxartBase = resolvePref(prefs, prefBoxartBase, thumbs.DefaultBoxartBase, legacyBoxartBases)
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

	u.folderBtn = widget.NewButton("Select ROM folder...", u.onPickFolder)
	u.refreshBtn = widget.NewButton("Refresh DAT", func() { u.loadDAT(true, nil) })
	u.settingsBtn = widget.NewButton("Settings", u.onSettings)
	u.csvBtn = widget.NewButton("Export CSV", u.onExportCSV)
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
		container.NewHBox(u.folderBtn, u.refreshBtn, u.settingsBtn, u.csvBtn),
		u.folderLabel,
		container.NewHBox(u.overwrite, u.renameCheck, u.covCheck, moreInfo, u.startBtn),
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
	name, base := r.Match, u.boxartBase
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
		path := filepath.Join(cacheDir, thumbs.Sanitize(name)+".png")
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
		dialog.ShowInformation("Nothing found", "No .sfc/.smc ROMs in the selected folder.", u.win)
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
		BoxartBase: u.boxartBase,
	}

	out := make(chan pipeline.Progress)
	go pipeline.Run(ctx, roms, u.index, opts, out)
	go u.consume(out)
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
	datURL := u.datURL
	go func() {
		idx, err := dat.Load(context.Background(), datURL, force)
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
		u.overwrite.Disable()
		u.renameCheck.Disable()
		u.covCheck.Disable()
	} else {
		u.startBtn.SetText("Start")
		u.folderBtn.Enable()
		u.refreshBtn.Enable()
		u.settingsBtn.Enable()
		u.csvBtn.Enable()
		u.overwrite.Enable()
		u.renameCheck.Enable()
		u.covCheck.Enable()
		u.cancel = nil
	}
}

// onSettings opens a dialog to edit the DAT URL and the boxart repository URL,
// persisting them via Fyne preferences. Changing the DAT URL reloads the DAT.
func (u *UI) onSettings() {
	datEntry := widget.NewEntry()
	datEntry.SetText(u.datURL)
	covEntry := widget.NewEntry()
	covEntry.SetText(u.boxartBase)

	reset := widget.NewButton("Restore defaults", func() {
		datEntry.SetText(dat.DefaultURL)
		covEntry.SetText(thumbs.DefaultBoxartBase)
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

	items := []*widget.FormItem{
		widget.NewFormItem("DAT URL (No-Intro)", datEntry),
		widget.NewFormItem("Cover repository", covEntry),
		widget.NewFormItem("Downloaded covers", clearCache),
		widget.NewFormItem("", reset),
	}

	d := dialog.NewForm("Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		newDat := strings.TrimSpace(datEntry.Text)
		newCov := strings.TrimSpace(covEntry.Text)
		if newDat == "" {
			newDat = dat.DefaultURL
		}
		if newCov == "" {
			newCov = thumbs.DefaultBoxartBase
		}
		datChanged := newDat != u.datURL
		u.datURL, u.boxartBase = newDat, newCov

		prefs := fyne.CurrentApp().Preferences()
		setPrefOrDefault(prefs, prefDatURL, newDat, dat.DefaultURL)
		setPrefOrDefault(prefs, prefBoxartBase, newCov, thumbs.DefaultBoxartBase)

		if datChanged {
			u.loadDAT(true, nil) // re-fetch with the new DAT URL
		}
	}, u.win)
	d.Resize(fyne.NewSize(640, 220))
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
	var ok, notFound, skip, noMatch, errc, renamed, covOK int
	for _, r := range u.rows {
		if r.NewName != "" {
			renamed++
		}
		if r.Cov == pipeline.CovOK {
			covOK++
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
		"Cover OK: %d   |   Not found: %d   |   Skipped: %d   |   No match: %d   |   Renamed: %d   |   .cov: %d   |   Errors: %d",
		ok, notFound, skip, noMatch, renamed, covOK, errc,
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
