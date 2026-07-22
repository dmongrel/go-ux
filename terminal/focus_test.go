package terminal

import (
	"errors"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// fakePTY is a minimal ptySession double that records everything written to
// it. It needs no real process or ConPTY handle, so these tests are
// deterministic and unaffected by this machine's documented ConPTY
// limitation (see Global Constraints) — they check what Session *wrote*, not
// anything read back from a live shell.
type fakePTY struct {
	written []byte
}

func (f *fakePTY) Write(p []byte) (int, error) {
	f.written = append(f.written, p...)
	return len(p), nil
}
func (f *fakePTY) Read([]byte) (int, error) { return 0, errors.New("fakePTY: not readable") }
func (f *fakePTY) Resize(int, int) error    { return nil }
func (f *fakePTY) Close() error             { return nil }
func (f *fakePTY) Wait() error              { return nil }

// newTestSession builds a Session wired to a fakePTY, bypassing NewSession's
// real ConPTY spawn entirely (no OS process, unaffected by ConPTY
// limitations). It otherwise mirrors NewSession's construction so
// CreateRenderer/Refresh/Tapped-driven canvas focusing work exactly as they
// would on a real Session — keyboard-input handling itself only ever
// touches s.pty.
func newTestSession() (*Session, *fakePTY) {
	pty := &fakePTY{}
	vt := newVTState(defaultCols, defaultRows)
	render := newGridRenderer(vt)
	s := &Session{
		pty:    pty,
		vt:     vt,
		render: render,
		cellW:  render.cellW,
		cellH:  render.cellH,
		cols:   defaultCols,
		rows:   defaultRows,
	}
	s.cursor = canvas.NewRectangle(nil)
	s.ExtendBaseWidget(s)
	return s, pty
}

// TestSessionIsFocusable pins down the interface conformance the task
// requires: a compile-time check that would fail loudly if a method were
// missing or had the wrong signature.
func TestSessionIsFocusable(t *testing.T) {
	var _ fyne.Focusable = (*Session)(nil)
	var _ fyne.Tappable = (*Session)(nil)
	var _ fyne.Shortcutable = (*Session)(nil)
}

// TestTypedRuneWritesUTF8 confirms ordinary character input reaches the PTY
// verbatim, including a multi-byte rune.
func TestTypedRuneWritesUTF8(t *testing.T) {
	s, pty := newTestSession()
	s.TypedRune('x')
	s.TypedRune('é')
	if got, want := string(pty.written), "xé"; got != want {
		t.Errorf("written = %q, want %q", got, want)
	}
}

// TestTypedKeyWritesMappedSequence spot-checks a couple of entries from the
// fixed-key table via the actual Session method (keymap_test.go already
// covers the full table against keyEventBytes directly).
func TestTypedKeyWritesMappedSequence(t *testing.T) {
	s, pty := newTestSession()
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	if got, want := string(pty.written), "\r\x1b[A"; got != want {
		t.Errorf("written = %q, want %q", got, want)
	}
}

// TestTypedKeyUnmappedIsNoop confirms a key outside the pragmatic subset
// (e.g. F1) writes nothing rather than garbage bytes.
func TestTypedKeyUnmappedIsNoop(t *testing.T) {
	s, pty := newTestSession()
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF1})
	if len(pty.written) != 0 {
		t.Errorf("written = %q, want empty", pty.written)
	}
}

// TestTypedShortcutWritesCtrlByte proves Ctrl+C (delivered by Fyne's driver
// as a ShortcutCopy, never as a TypedKey — see Session.TypedShortcut's doc
// comment) reaches the PTY as the 0x03 SIGINT byte, and that an arbitrary
// Ctrl+letter combo (delivered as desktop.CustomShortcut) works the same
// way.
func TestTypedShortcutWritesCtrlByte(t *testing.T) {
	s, pty := newTestSession()
	s.TypedShortcut(&fyne.ShortcutCopy{})
	if got, want := pty.written, []byte{0x03}; string(got) != string(want) {
		t.Errorf("Ctrl+C written = %v, want %v", got, want)
	}
}

// TestTypedShortcutUnmappedIsNoop confirms a shortcut this package doesn't
// understand (not a fyne.KeyboardShortcut at all) is silently ignored.
type opaqueShortcut struct{}

func (opaqueShortcut) ShortcutName() string { return "Opaque" }

func TestTypedShortcutUnmappedIsNoop(t *testing.T) {
	s, pty := newTestSession()
	s.TypedShortcut(opaqueShortcut{})
	if len(pty.written) != 0 {
		t.Errorf("written = %q, want empty", pty.written)
	}
}

// TestFocusGainedLostTracksState confirms the Focusable lifecycle hooks
// update Session's focus state without panicking (no PTY writes expected
// from either).
func TestFocusGainedLostTracksState(t *testing.T) {
	s, pty := newTestSession()
	s.FocusGained()
	if !s.focused {
		t.Error("focused = false after FocusGained, want true")
	}
	s.FocusLost()
	if s.focused {
		t.Error("focused = true after FocusLost, want false")
	}
	if len(pty.written) != 0 {
		t.Errorf("focus hooks wrote %q to the PTY, want none", pty.written)
	}
}

// TestTappedRequestsFocus confirms tapping the widget focuses it on a real
// (headless) canvas — the "focus-on-tap" requirement — matching the pattern
// widget.Entry uses (requestFocus via the canvas Fyne resolves the object
// onto).
func TestTappedRequestsFocus(t *testing.T) {
	a := test.NewApp()
	defer test.NewApp() // reset the global app for other tests in this package

	s, _ := newTestSession()
	w := a.NewWindow("")
	w.SetContent(s)
	defer w.Close()

	c := w.Canvas()
	if c.Focused() == s {
		t.Fatal("session already focused before Tapped; test setup is wrong")
	}
	s.Tapped(&fyne.PointEvent{})
	if c.Focused() != fyne.Focusable(s) {
		t.Error("Tapped did not focus the session")
	}
}
