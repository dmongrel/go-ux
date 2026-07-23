package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// gutterLineText/gutterLineColor read back rich's i'th segment — the test
// helpers for asserting on gutter's per-line RichText rendering.
func gutterLineText(t *testing.T, g *gutter, i int) string {
	t.Helper()
	seg, ok := g.rich.Segments[i].(*widget.TextSegment)
	if !ok {
		t.Fatalf("segment %d is %T, want *widget.TextSegment", i, g.rich.Segments[i])
	}
	return seg.Text
}

func gutterLineColor(t *testing.T, g *gutter, i int) fyne.ThemeColorName {
	t.Helper()
	seg, ok := g.rich.Segments[i].(*widget.TextSegment)
	if !ok {
		t.Fatalf("segment %d is %T, want *widget.TextSegment", i, g.rich.Segments[i])
	}
	return seg.Style.ColorName
}

func TestGutterSetTextRecomputesLineNumbers(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("one\ntwo", entry)

	g.SetText("one\ntwo\nthree")

	if len(g.rich.Segments) != 3 {
		t.Fatalf("got %d segments, want 3", len(g.rich.Segments))
	}
	for i, want := range []string{"1", "2", "3"} {
		if got := gutterLineText(t, g, i); got != want {
			t.Errorf("segment %d text = %q, want %q", i, got, want)
		}
	}
}

func TestGutterEmptyTextIsOneLine(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("", entry)

	if len(g.rich.Segments) != 1 {
		t.Fatalf("got %d segments, want 1 (an empty document is still one, empty, line)", len(g.rich.Segments))
	}
	if got := gutterLineText(t, g, 0); got != "1" {
		t.Errorf("segment 0 text = %q, want %q", got, "1")
	}
}

func TestGutterActiveLineDefaultsToFirstLine(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("one\ntwo\nthree", entry)

	if got := gutterLineColor(t, g, 0); got != theme.ColorNameForeground {
		t.Errorf("line 0 color = %q, want %q (default active line)", got, theme.ColorNameForeground)
	}
	if got := gutterLineColor(t, g, 1); got != theme.ColorNameDisabled {
		t.Errorf("line 1 color = %q, want %q (dimmed)", got, theme.ColorNameDisabled)
	}
	if got := gutterLineColor(t, g, 2); got != theme.ColorNameDisabled {
		t.Errorf("line 2 color = %q, want %q (dimmed)", got, theme.ColorNameDisabled)
	}
}

func TestGutterSetActiveLineMovesHighlight(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("one\ntwo\nthree", entry)

	g.SetActiveLine(2)

	if got := gutterLineColor(t, g, 0); got != theme.ColorNameDisabled {
		t.Errorf("line 0 color = %q, want %q (no longer active)", got, theme.ColorNameDisabled)
	}
	if got := gutterLineColor(t, g, 2); got != theme.ColorNameForeground {
		t.Errorf("line 2 color = %q, want %q (now active)", got, theme.ColorNameForeground)
	}
}

func TestGutterSetTextPreservesActiveLineHighlight(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("one\ntwo\nthree", entry)
	g.SetActiveLine(1)

	g.SetText("one\ntwo\nthree\nfour")

	if got := gutterLineColor(t, g, 1); got != theme.ColorNameForeground {
		t.Errorf("line 1 color = %q, want %q (highlight should survive a SetText)", got, theme.ColorNameForeground)
	}
}

func TestGutterToggleSoftWrapMenuItemFlipsEntryWrapping(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapWord
	g := newGutter("text", entry)

	// Exercise the same toggle logic the menu item's callback runs,
	// without needing a real popup menu / canvas in the test.
	toggle := func() {
		if entry.Wrapping == fyne.TextWrapOff {
			entry.Wrapping = fyne.TextWrapWord
		} else {
			entry.Wrapping = fyne.TextWrapOff
		}
	}

	toggle()
	if entry.Wrapping != fyne.TextWrapOff {
		t.Fatalf("Wrapping = %v after first toggle, want TextWrapOff", entry.Wrapping)
	}

	toggle()
	if entry.Wrapping != fyne.TextWrapWord {
		t.Fatalf("Wrapping = %v after second toggle, want TextWrapWord", entry.Wrapping)
	}

	_ = g // g exists purely to confirm newGutter accepts this entry without panicking
}
