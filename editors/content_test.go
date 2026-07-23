package editors

import (
	"testing"

	"fyne.io/fyne/v2/widget"
	fynetest "fyne.io/fyne/v2/test"
)

func TestNewPlaceholderContentShowsTabText(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "It was a dark and stormy night.")
	obj := NewPlaceholderContent(tab)

	label, ok := obj.(*widget.Label)
	if !ok {
		t.Fatalf("NewPlaceholderContent returned %T, want *widget.Label", obj)
	}
	if label.Text != tab.Text {
		t.Errorf("label.Text = %q, want %q", label.Text, tab.Text)
	}
}
