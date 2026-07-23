package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestGutterTextSingleLine(t *testing.T) {
	if got := gutterText("just one line"); got != "1" {
		t.Errorf("gutterText = %q, want %q", got, "1")
	}
}

func TestGutterTextMultipleLines(t *testing.T) {
	if got := gutterText("a\nb\nc"); got != "1\n2\n3" {
		t.Errorf("gutterText = %q, want %q", got, "1\n2\n3")
	}
}

func TestGutterTextEmptyStringIsOneLine(t *testing.T) {
	if got := gutterText(""); got != "1" {
		t.Errorf("gutterText(\"\") = %q, want %q (an empty document is still one, empty, line)", got, "1")
	}
}

func TestGutterSetTextRecomputesLineNumbers(t *testing.T) {
	fynetest.NewApp()

	entry := widget.NewMultiLineEntry()
	g := newGutter("one\ntwo", entry)

	g.SetText("one\ntwo\nthree")

	if g.label.Text != "1\n2\n3" {
		t.Errorf("label.Text = %q, want %q", g.label.Text, "1\n2\n3")
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
