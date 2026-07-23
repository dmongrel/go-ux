package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"go-ux/fontsettings"
)

// TestEditorEntrySelectAllShortcutSelectsEverything checks that Ctrl+A
// selects the entry's full text. widget.Entry already registers
// fyne.ShortcutSelectAll internally (registerShortcut, called from its
// own ExtendBaseWidget override, which editorEntry's embedding promotes
// unchanged) — this just confirms that plumbing survives editorEntry's
// wrapping (and the requestFocus fix above) rather than assuming it.
func TestEditorEntrySelectAllShortcutSelectsEverything(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	entry := newEditorEntry(fonts, nil)
	entry.SetText("hello world")

	entry.TypedShortcut(&fyne.ShortcutSelectAll{})

	if got := entry.SelectedText(); got != "hello world" {
		t.Errorf("SelectedText() = %q, want %q", got, "hello world")
	}
}

// TestEditorEntryMouseDownAcquiresRealFocus is a regression test for a
// real user-reported bug: typing and pasting into the content area did
// nothing at all. Root cause: newEditorEntry built its *widget.Entry via
// widget.NewMultiLineEntry(), which itself calls
// entry.ExtendBaseWidget(entry) — and BaseWidget.ExtendBaseWidget is a
// one-shot that silently no-ops once already set, so the later
// e.ExtendBaseWidget(e) call left the widget's internal "impl" pointed at
// the bare inner *widget.Entry (never actually placed in any canvas)
// instead of e (the *editorEntry actually placed in the canvas tree).
// widget.Entry.requestFocus() — called from MouseDown, the real desktop
// left-click focus trigger (Entry.Tapped itself does nothing; fynetest.Tap
// only simulates Tapped, not MouseDown, which is why this test calls
// MouseDown directly rather than using fynetest.Tap) — looks up the
// canvas FOR THAT IMPL value; since the impl was never in the tree, the
// lookup returned nil and focus was silently never acquired. Every other
// test in this file drives editorEntry via SetText/OnChanged directly,
// never through this real click-to-focus path, so none of them caught
// this.
func TestEditorEntryMouseDownAcquiresRealFocus(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	entry := newEditorEntry(fonts, nil)
	win := fynetest.NewWindow(entry)
	defer win.Close()
	win.Resize(fyne.NewSize(200, 200))

	entry.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(5, 5)}, Button: desktop.MouseButtonPrimary})

	if win.Canvas().Focused() != fyne.Focusable(entry) {
		t.Fatalf("Canvas().Focused() = %v, want the clicked entry itself — clicking the content area does not acquire keyboard focus, so typing/pasting silently does nothing", win.Canvas().Focused())
	}
}

func TestEditorEntryPlainScrollForwardsToAncestor(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	var forwarded *fyne.ScrollEvent
	entry := newEditorEntry(fonts, func(ev *fyne.ScrollEvent) { forwarded = ev })

	ev := &fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}}
	entry.Scrolled(ev)

	if forwarded != ev {
		t.Errorf("plain scroll was not forwarded to the ancestor")
	}
	if fonts.Current().Size != fontsettings.DefaultFontSettings.Size {
		t.Errorf("font size changed on a plain (non-Ctrl) scroll: %d, want unchanged %d", fonts.Current().Size, fontsettings.DefaultFontSettings.Size)
	}
}

func TestEditorEntryCtrlScrollAdjustsFontSizeInsteadOfForwarding(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	forwarded := false
	entry := newEditorEntry(fonts, func(*fyne.ScrollEvent) { forwarded = true })

	entry.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	before := fonts.Current().Size
	entry.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}})

	if forwarded {
		t.Errorf("Ctrl+scroll was forwarded to the ancestor instead of being consumed for font sizing")
	}
	if fonts.Current().Size != before+fontSizeScrollStep {
		t.Errorf("Size = %d, want %d (before %d + step %d)", fonts.Current().Size, before+fontSizeScrollStep, before, fontSizeScrollStep)
	}
}

func TestEditorEntryCtrlScrollDownShrinksFontSize(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	entry := newEditorEntry(fonts, nil)

	entry.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlRight})
	before := fonts.Current().Size
	entry.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: -10}})

	if fonts.Current().Size != before-fontSizeScrollStep {
		t.Errorf("Size = %d, want %d", fonts.Current().Size, before-fontSizeScrollStep)
	}
}

func TestEditorEntryKeyUpEndsCtrlScrollMode(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	forwarded := false
	entry := newEditorEntry(fonts, func(*fyne.ScrollEvent) { forwarded = true })

	entry.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	entry.KeyUp(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	entry.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}})

	if !forwarded {
		t.Errorf("scroll after KeyUp was not forwarded — ctrlHeld should have been cleared")
	}
}

func TestEditorEntryFocusLostClearsCtrlHeld(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	forwarded := false
	entry := newEditorEntry(fonts, func(*fyne.ScrollEvent) { forwarded = true })

	entry.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	entry.FocusLost() // simulates Alt-Tab away without a matching KeyUp
	entry.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 10}})

	if !forwarded {
		t.Errorf("scroll after FocusLost was not forwarded — ctrlHeld should have been cleared")
	}
}

func TestSizeOverrideThemeUsesFontsCurrentSizeForText(t *testing.T) {
	base := fynetest.NewApp().Settings().Theme()
	fonts := newTestFonts()
	th := &sizeOverrideTheme{base: base, fonts: fonts}

	fonts.Set(fontsettings.FontSettings{Size: 24, LineHeight: 1.0, ColumnWidth: 1.0})

	if got := th.Size(theme.SizeNameText); got != 24 {
		t.Errorf("Size(SizeNameText) = %v, want 24", got)
	}
}

func TestSizeOverrideThemeDelegatesOtherSizesToBase(t *testing.T) {
	base := fynetest.NewApp().Settings().Theme()
	fonts := newTestFonts()
	th := &sizeOverrideTheme{base: base, fonts: fonts}

	if got, want := th.Size(theme.SizeNamePadding), base.Size(theme.SizeNamePadding); got != want {
		t.Errorf("Size(SizeNamePadding) = %v, want %v (delegated to base)", got, want)
	}
}

func TestNewContentThemeOverrideRefreshesOnFontChange(t *testing.T) {
	fynetest.NewApp()

	fonts := newTestFonts()
	label := newGutter("x", newEditorEntry(fonts, nil).Entry)
	_, cleanup := newContentThemeOverride(label, fonts, "key1")
	defer cleanup()

	// Just confirm registering/setting doesn't panic and the listener is
	// reachable — newDocumentContentFontSizeChangeResizesText (content_test.go)
	// covers the actual theme.Size() propagation end-to-end.
	fonts.Set(fontsettings.FontSettings{Size: 20, LineHeight: 1.0, ColumnWidth: 1.0})
}
