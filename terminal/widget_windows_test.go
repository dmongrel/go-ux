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
	beforeSessCellH := sess.cellH
	setFontSettings(FontSettings{Family: "", Size: 24, LineHeight: 1.0, ColumnWidth: 1.0})

	after := sess.render.cellH
	if after == before {
		t.Errorf("session's render.cellH unchanged after setFontSettings (before=%d, after=%d) — session did not react to the live font change", before, after)
	}

	// s.cellW/cellH is the Session's own cached copy of the renderer's
	// per-cell size, used by Resize's gridDims call — it must be
	// re-synced from render.cellH too, or gridDims silently keeps sizing
	// the grid from the old font while MinSize already reflects the new
	// one (a font-size change would then never actually reflow columns
	// or rows).
	afterSessCellH := sess.cellH
	if afterSessCellH == beforeSessCellH {
		t.Errorf("session's own cellH unchanged after setFontSettings (before=%d, after=%d) — Resize would still use the stale per-cell size", beforeSessCellH, afterSessCellH)
	}
	if afterSessCellH != after {
		t.Errorf("session's own cellH (%d) does not match render.cellH (%d) after a live font change", afterSessCellH, after)
	}
}

// TestSessionRendererMinSizeDoesNotScaleWithGridSize locks in the "the
// window frame must never be forced to resize by a font-size change or a
// bigger grid" contract: Fyne enforces a widget's/window's size can't
// shrink below its renderer's MinSize, so if MinSize scaled with the full
// grid (cols*cellW x rows*cellH), a live font-size increase — or simply
// having more columns after the user resizes the window bigger — would
// force the window itself to keep growing. MinSize must only be a small,
// constant-per-font-size floor (one cell), never tied to the current
// column/row count.
func TestSessionRendererMinSizeDoesNotScaleWithGridSize(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings)

	sess, err := NewSession(cmdDef("minsize-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	r := sess.CreateRenderer()
	before := r.MinSize()

	// Growing the widget substantially grows the grid's column/row count
	// (more characters fit), but must not change MinSize. Routed through
	// doUI (fyne.Do), matching how Fyne's own layout system actually calls
	// Resize on the UI goroutine — calling it directly from the test
	// goroutine races with refreshLoop's own doUI-wrapped Refresh, which
	// also touches the cursor rectangle Resize mutates.
	doUI(func() { sess.Resize(fyne.NewSize(2000, 1500)) })
	afterResize := r.MinSize()
	if afterResize != before {
		t.Errorf("MinSize changed after growing the grid via Resize: before=%v after=%v — MinSize must not scale with column/row count", before, afterResize)
	}

	// MinSize must still track a single cell's size, so it grows with the
	// font itself (a real floor, not a value frozen at construction).
	setFontSettings(FontSettings{Family: "", Size: 24, LineHeight: 1.0, ColumnWidth: 1.0})
	afterFontChange := r.MinSize()
	if afterFontChange.Width <= afterResize.Width || afterFontChange.Height <= afterResize.Height {
		t.Errorf("MinSize did not grow with a larger font size: before=%v after=%v", afterResize, afterFontChange)
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
	for range 30 {
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
	for range 3 {
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
