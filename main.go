// Command sd2snes-covers downloads SNES boxart from libretro for a folder of
// ROMs, matching each ROM by its headerless CRC32 against the No-Intro DAT.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/ludufre/sd2snes-covers/internal/ui"
)

func main() {
	a := app.NewWithID("com.ludufre.sd2snes-covers")
	w := a.NewWindow("sd2snes Covers " + ui.Version)

	u := ui.New(w)
	w.SetContent(u.Root())
	w.Resize(fyne.NewSize(960, 640))

	u.Start() // begin loading the DAT in the background
	w.ShowAndRun()
}
