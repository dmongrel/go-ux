package terminal

import (
	"fyne.io/fyne/v2"

	"go-ux/db"
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
// NewWindow takes shells directly (no *db.DB parameter) and remains the
// base constructor. NewWindowFromSettings (settings_schema.go) is a second,
// additional constructor that reads default_shell/close_on_exit from a
// settings registry the way settings.Window reads its own — go-ux/dialog
// already establishes the precedent of several named constructors
// (NewInfo/NewError/NewCustom) for different construction needs rather than
// threading optional parameters through one, so this package follows that
// rather than inventing a third convention.
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
	return newWindow(app, shells, false)
}

// NewWindowFromSettings builds a terminal window like NewWindow, but sources
// its default shell and close-on-exit behavior from database's settings
// registry (as seeded by RegisterSettings) instead of a fixed []ShellDef.
// It still calls DetectShells() itself for the actual list of runnable
// shells on this machine — the registry only picks which of those is the
// default (matched by name, for the "+" button and the initial tab order)
// and whether a tab auto-closes when its shell process exits.
//
// If database has no Terminal node yet (RegisterSettings was never called),
// this falls back to DetectShells()'s own ordering and close-on-exit off,
// rather than failing — a caller that forgets to call RegisterSettings
// first still gets a working window.
func NewWindowFromSettings(app fyne.App, database *db.DB) (*Window, error) {
	shells := DetectShells()

	defaultShell, closeOnExit, found, err := readTerminalSettings(database)
	if err != nil {
		return nil, err
	}
	if found {
		shells = withDefaultFirst(shells, defaultShell)
	}

	return newWindow(app, shells, found && closeOnExit)
}

// withDefaultFirst reorders shells so the entry named name is first (which
// becomes TabView's default for its "+" button and the initially-selected
// tab), leaving the relative order of the rest unchanged. Returns shells
// unmodified if no entry matches name (e.g. the configured default_shell is
// no longer detected on this machine).
func withDefaultFirst(shells []ShellDef, name string) []ShellDef {
	for i, s := range shells {
		if s.Name != name {
			continue
		}
		if i == 0 {
			return shells
		}
		reordered := make([]ShellDef, 0, len(shells))
		reordered = append(reordered, s)
		reordered = append(reordered, shells[:i]...)
		reordered = append(reordered, shells[i+1:]...)
		return reordered
	}
	return shells
}

// newWindow is NewWindow plus the closeOnExit flag threaded down to
// TabView — unexported since only NewWindow (closeOnExit always false) and
// NewWindowFromSettings (closeOnExit from the registry) need to set it.
func newWindow(app fyne.App, shells []ShellDef, closeOnExit bool) (*Window, error) {
	tv := newTabView(shells, closeOnExit)

	win := app.NewWindow("Terminal")
	// Guarded by uiMu: by the time newTabView returns, any tabs it created
	// already have background loops running (see uiMu's doc comment in
	// widget.go), which would otherwise race these calls on Fyne's shared
	// widget-render cache.
	uiMu.Lock()
	win.Resize(fyne.NewSize(defaultWindowWidth, defaultWindowHeight))
	win.SetContent(tv.tabs)
	uiMu.Unlock()

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
		uiMu.Lock()
		w.win.Resize(fyne.NewSize(width, height))
		uiMu.Unlock()
	}
	return w
}

// Show displays the terminal window. Non-blocking, like settings.Window's
// Show — unlike go-ux/dialog's Show, which blocks the calling goroutine
// until the dialog closes.
func (w *Window) Show() {
	uiMu.Lock()
	w.win.Show()
	uiMu.Unlock()
}
