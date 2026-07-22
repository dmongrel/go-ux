//go:build windows

package terminal

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// TestNewWindowWithNoShellsBuildsEmptyUsableWindow confirms NewWindow doesn't
// require a non-empty shells list to construct successfully (no spawn is
// attempted, so this needs no real process and runs regardless of this
// machine's PTY limitations).
func TestNewWindowWithNoShellsBuildsEmptyUsableWindow(t *testing.T) {
	a := test.NewApp()

	w, err := NewWindow(a, nil)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if len(w.tv.tabs.Items) != 0 {
		t.Errorf("len(tv.tabs.Items) = %d, want 0", len(w.tv.tabs.Items))
	}
	w.Show()
	uiMu.Lock()
	w.win.Close()
	uiMu.Unlock()
}

// TestWindowSetSizeIsChainableAndGuarded mirrors dialog.Dialog.SetSize's and
// settings.Window.SetSize's own tests: both dimensions must be positive for
// the resize to take effect, and the call returns *Window for chaining.
func TestWindowSetSizeIsChainableAndGuarded(t *testing.T) {
	a := test.NewApp()

	w, err := NewWindow(a, nil)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	defer func() {
		uiMu.Lock()
		w.win.Close()
		uiMu.Unlock()
	}()

	if got := w.SetSize(640, 480); got != w {
		t.Error("SetSize did not return the same *Window (not chainable)")
	}
	if got := w.win.Canvas().Size(); got.Width != 640 || got.Height != 480 {
		t.Errorf("size after SetSize(640, 480) = %v, want 640x480", got)
	}

	before := w.win.Canvas().Size()
	w.SetSize(0, 200) // guarded: width<=0 must be a no-op
	if got := w.win.Canvas().Size(); got != before {
		t.Errorf("SetSize(0, 200) changed size to %v, want unchanged %v", got, before)
	}
	w.SetSize(200, 0) // guarded: height<=0 must be a no-op
	if got := w.win.Canvas().Size(); got != before {
		t.Errorf("SetSize(200, 0) changed size to %v, want unchanged %v", got, before)
	}
}

// TestNewWindowContentIsTabViewWidget confirms NewWindow actually wires
// TabView's DocTabs into the window's content (rather than, say, building a
// TabView that's never shown) — the structural half of the wiring that the
// plan's Global Constraints say is the automated bar for a GUI window (full
// visual correctness needs a human at a GUI).
func TestNewWindowContentIsTabViewWidget(t *testing.T) {
	a := test.NewApp()

	w, err := NewWindow(a, []ShellDef{cmdDef("cmd-1")})
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	defer func() {
		w.tv.closeAll()
		uiMu.Lock()
		w.win.Close()
		uiMu.Unlock()
	}()

	if got := w.win.Content(); got != fyne.CanvasObject(w.tv.tabs) {
		t.Errorf("window content = %T, want the TabView's DocTabs", got)
	}
}
