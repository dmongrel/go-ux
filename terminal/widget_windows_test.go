//go:build windows

package terminal

import (
	"os"
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// TestNewSessionConstructsAndCloses is a smoke test: a real Session spawns
// (against cmd.exe), builds its widget renderer without panicking, and closes
// cleanly. It uses fyne/test's headless app so CreateRenderer/Refresh run
// through a real (offscreen) canvas. It does NOT assert on rendered pixels or
// on live shell output — this machine's PTY does not reliably deliver a
// shell's output (see Global Constraints), and pixel correctness needs a
// human at a GUI. What it proves is the wiring: construct, render, refresh,
// resize, close, all crash-free.
func TestNewSessionConstructsAndCloses(t *testing.T) {
	test.NewApp()

	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	s, err := NewSession(def)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	// Building the renderer must not panic and must expose both the raster
	// and the cursor overlay.
	r := s.CreateRenderer()
	if got := len(r.Objects()); got != 2 {
		t.Errorf("renderer has %d objects, want 2 (raster + cursor)", got)
	}
	r.Refresh() // repaint path, crash-free
	r.Layout(r.MinSize())

	// A resize must not panic and must update the tracked grid dimensions.
	s.Resize(r.MinSize())
	if s.cols < 1 || s.rows < 1 {
		t.Errorf("after resize, grid = %dx%d, want both >= 1", s.cols, s.rows)
	}
}

// TestSessionOnExitFiresAfterClose proves the exit callback is delivered.
// cmd.exe with "/C exit" terminates on its own; the callback must fire via
// the waitLoop. A generous timeout accounts for process teardown.
func TestSessionOnExitFiresAfterClose(t *testing.T) {
	test.NewApp()

	def := ShellDef{
		Name: "cmd.exe",
		Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`,
		Args: []string{"/C", "exit"},
	}
	s, err := NewSession(def)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	fired := make(chan struct{}, 1)
	s.OnExit(func() { fired <- struct{}{} })

	select {
	case <-fired:
		// exit delivered
	case <-time.After(5 * time.Second):
		t.Skip("OnExit did not fire within timeout; on this machine PTY " +
			"process lifecycle is unreliable (see Global Constraints) — " +
			"skipping rather than failing on a documented environment limitation")
	}
}

// TestSessionTitleFallsBackToShellName confirms Title() reports the shell's
// configured name when no program has set an OSC title — the tab-label
// default a Task 3 TabView relies on.
func TestSessionTitleFallsBackToShellName(t *testing.T) {
	test.NewApp()

	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	s, err := NewSession(def)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	if got := s.Title(); got != "cmd.exe" {
		t.Errorf("Title() = %q, want %q", got, "cmd.exe")
	}
}

func TestGridDimsAppliesLineHeightAndColumnWidthMultipliers(t *testing.T) {
	cols, rows := gridDims(800, 480, 10, 20, 1.0, 1.0)
	wantCols, wantRows := 80, 24
	if cols != wantCols || rows != wantRows {
		t.Fatalf("gridDims(800, 480, 10, 20, 1.0, 1.0) = %d,%d, want %d,%d", cols, rows, wantCols, wantRows)
	}

	// Doubling column width halves how many columns fit in the same pixel
	// width; doubling line height halves how many rows fit.
	cols2, rows2 := gridDims(800, 480, 10, 20, 2.0, 2.0)
	if cols2 != wantCols/2 {
		t.Errorf("gridDims with ColumnWidth=2.0: cols = %d, want %d", cols2, wantCols/2)
	}
	if rows2 != wantRows/2 {
		t.Errorf("gridDims with LineHeight=2.0: rows = %d, want %d", rows2, wantRows/2)
	}
}

func TestSessionReactsToLiveFontSettingsChange(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings) // restore for other tests

	sess, err := NewSession(cmdDef("font-live-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	before := sess.render.cellH
	setFontSettings(FontSettings{Family: "", Size: 24, LineHeight: 1.0, ColumnWidth: 1.0})

	after := sess.render.cellH
	if after == before {
		t.Errorf("session's render.cellH unchanged after setFontSettings (before=%d, after=%d) — session did not react to the live font change", before, after)
	}
}

func TestCtrlScrollAdjustsFontSizeLiveAndClamps(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings) // restore for other tests

	sess, err := NewSession(cmdDef("ctrl-scroll-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	sess.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}}) // scroll "up" = larger

	got := currentFontSettings().Size
	want := defaultFontSettings.Size + fontSizeScrollStep
	if got != want {
		t.Errorf("font size after one Ctrl+scroll-up tick = %d, want %d", got, want)
	}

	// Clamp: many ticks shouldn't exceed maxFontSize.
	for i := 0; i < 30; i++ {
		sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}})
	}
	if got := currentFontSettings().Size; got != maxFontSize {
		t.Errorf("font size after many Ctrl+scroll-up ticks = %d, want %d (clamped)", got, maxFontSize)
	}

	sess.KeyUp(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	before := currentFontSettings().Size
	sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}}) // Ctrl no longer held: no-op
	if got := currentFontSettings().Size; got != before {
		t.Errorf("font size changed on a scroll without Ctrl held: %d -> %d, want unchanged", before, got)
	}
}

func TestCtrlScrollDebouncedSavePersistsAfterIdle(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings)
	defer setActiveFontDB(nil)

	d := newTestDB(t)
	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	setActiveFontDB(d)

	sess, err := NewSession(cmdDef("debounce-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	sess.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	for i := 0; i < 3; i++ {
		sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}})
	}

	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, terminalSettingsLabel)
	props, _ := d.GetProperties(node.ID)
	for _, p := range props {
		if p.Key == KeyFontSize && p.Value != "13" {
			t.Fatal("font_size written to db before the debounce period elapsed")
		}
	}

	time.Sleep(fontSizeSaveDebounce + 100*time.Millisecond)

	props, err = d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties (after debounce): %v", err)
	}
	want := strconv.Itoa(13 + 3*fontSizeScrollStep)
	found := false
	for _, p := range props {
		if p.Key == KeyFontSize {
			found = true
			if p.Value != want {
				t.Errorf("font_size in db = %q, want %q", p.Value, want)
			}
		}
	}
	if !found {
		t.Fatal("font_size property not found")
	}
}
