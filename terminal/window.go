package terminal

import (
	"fyne.io/fyne/v2"
)

// defaultWindowWidth/Height are Window's starting size before any SetSize
// override, chosen to comfortably fit an 80x24 grid at a normal font size
// plus the DocTabs bar, matching settings.Window's own pattern of a
// generous default rather than trying to compute an exact fit.
const (
	defaultWindowWidth  = 1024
	defaultWindowHeight = 700
)

// Window is a standalone terminal window: a TabView of shell sessions inside
// a plain fyne.Window. It mirrors settings.Window's and dialog.Dialog's
// conventions — chainable SetSize, non-blocking Show — rather than
// introducing a third pattern for a third go-ux window type.
//
// NewWindow takes shells directly (no *db.DB parameter). A db-aware
// constructor that reads default_shell/close_on_exit from a settings
// registry, the way settings.Window reads its own, is a later task's
// addition layered on top of this one, not a replacement for it.
type Window struct {
	win fyne.Window
	tv  *TabView
}

// NewWindow builds a terminal window with one tab per shell in shells (shells
// may be empty; see NewTabView). Call Show to display it. The returned error
// is always nil today — NewSession failures for individual shells are logged
// and skipped by TabView rather than failing the whole window, so a caller
// with a partially-broken shell list still gets a usable window. The error
// return exists to match settings.NewWindow's signature (and to leave room
// for a future failure mode, e.g. an empty shells list with no db fallback)
// without an incompatible signature change later.
func NewWindow(app fyne.App, shells []ShellDef) (*Window, error) {
	tv := NewTabView(shells)

	win := app.NewWindow("Terminal")
	win.Resize(fyne.NewSize(defaultWindowWidth, defaultWindowHeight))
	win.SetContent(tv.tabs)

	win.SetCloseIntercept(func() {
		tv.closeAll()
		win.Close()
	})

	return &Window{win: win, tv: tv}, nil
}

// SetSize overrides the terminal window's default size (1024x700). Both
// width and height must be positive or the call has no effect. Call before
// Show. Chainable — same pattern as dialog.Dialog.SetSize and
// settings.Window.SetSize.
func (w *Window) SetSize(width, height float32) *Window {
	if width > 0 && height > 0 {
		w.win.Resize(fyne.NewSize(width, height))
	}
	return w
}

// Show displays the terminal window. Non-blocking, like settings.Window's
// Show — unlike go-ux/dialog's Show, which blocks the calling goroutine
// until the dialog closes.
func (w *Window) Show() {
	w.win.Show()
}
