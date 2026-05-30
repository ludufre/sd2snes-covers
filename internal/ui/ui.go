// Package ui implements the Fyne desktop interface for sd2snes-covers.
package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/ludufre/sd2snes-covers/internal/cov"
	"github.com/ludufre/sd2snes-covers/internal/dat"
	"github.com/ludufre/sd2snes-covers/internal/pipeline"
	"github.com/ludufre/sd2snes-covers/internal/scan"
	"github.com/ludufre/sd2snes-covers/internal/thumbs"
)

var headers = [5]string{"ROM", "CRC32", "No-Intro Match", "Cover", ".cov"}

// Version is shown in the status bar and the window title.
const Version = "v1.0.0"

// preference keys (persisted via Fyne preferences)
const (
	prefDatURL     = "dat_url"
	prefBoxartBase = "boxart_base"
	forkURL        = "https://github.com/ludufre/sd2snes"
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
	table       *widget.Table
}

// New builds the UI bound to window w.
func New(w fyne.Window) *UI {
	u := &UI{win: w}

	prefs := fyne.CurrentApp().Preferences()
	u.datURL = prefs.StringWithFallback(prefDatURL, dat.DefaultURL)
	u.boxartBase = prefs.StringWithFallback(prefBoxartBase, thumbs.DefaultBoxartBase)

	u.folderLabel = widget.NewLabel("No folder selected")
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
		u.progress,
		u.status,
		container.NewBorder(nil, nil, nil, version, u.summary), // summary left, version right
	)
	return container.NewBorder(top, bottom, nil, nil, u.table)
}

// Start kicks off the initial DAT load.
func (u *UI) Start() {
	u.loadDAT(false, nil)
}

func (u *UI) buildTable() *widget.Table {
	t := widget.NewTable(
		func() (int, int) { return len(u.rows) + 1, len(headers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(headers[id.Col])
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			r := u.rows[id.Row-1]
			switch id.Col {
			case 0:
				name := r.ROMName
				if r.NewName != "" {
					name = r.NewName // show the renamed file
				}
				lbl.SetText(name)
			case 1:
				lbl.SetText(r.CRC)
			case 2:
				lbl.SetText(r.Match)
			case 3:
				lbl.SetText(displayStatus(r))
			case 4:
				lbl.SetText(r.Cov.String())
			}
		},
	)
	t.SetColumnWidth(0, 300)
	t.SetColumnWidth(1, 90)
	t.SetColumnWidth(2, 300)
	t.SetColumnWidth(3, 100)
	t.SetColumnWidth(4, 80)
	return t
}

func (u *UI) onPickFolder() {
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

	items := []*widget.FormItem{
		widget.NewFormItem("DAT URL (No-Intro)", datEntry),
		widget.NewFormItem("Cover repository", covEntry),
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
		prefs.SetString(prefDatURL, newDat)
		prefs.SetString(prefBoxartBase, newCov)

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
		"Cover OK: %d   |   404: %d   |   Skipped: %d   |   No match: %d   |   Renamed: %d   |   .cov: %d   |   Errors: %d",
		ok, notFound, skip, noMatch, renamed, covOK, errc,
	))
}

func displayStatus(r pipeline.RowResult) string {
	if r.Err != nil {
		return "Error"
	}
	return r.Cover.String()
}
