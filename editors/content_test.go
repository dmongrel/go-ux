package editors

import (
	"testing"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	fynetest "fyne.io/fyne/v2/test"
)

func TestNewPlaceholderContentShowsTabText(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "It was a dark and stormy night.")
	obj := NewPlaceholderContent(tab)

	scroll, ok := obj.(*container.Scroll)
	if !ok {
		t.Fatalf("NewPlaceholderContent returned %T, want *container.Scroll", obj)
	}
	label, ok := scroll.Content.(*widget.Label)
	if !ok {
		t.Fatalf("scroll content is %T, want *widget.Label", scroll.Content)
	}
	if label.Text != tab.Text {
		t.Errorf("label.Text = %q, want %q", label.Text, tab.Text)
	}
}
