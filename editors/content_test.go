package editors

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	fynetest "fyne.io/fyne/v2/test"
)

func TestNewPlaceholderContentShowsTabText(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "It was a dark and stormy night.")
	obj := NewPlaceholderContent(tab)

	stack, ok := obj.(*fyne.Container)
	if !ok {
		t.Fatalf("NewPlaceholderContent returned %T, want *fyne.Container", obj)
	}
	scroll, ok := stack.Objects[1].(*container.Scroll)
	if !ok {
		t.Fatalf("stack.Objects[1] is %T, want *container.Scroll", stack.Objects[1])
	}
	label, ok := scroll.Content.(*widget.Label)
	if !ok {
		t.Fatalf("scroll content is %T, want *widget.Label", scroll.Content)
	}
	if label.Text != tab.Text {
		t.Errorf("label.Text = %q, want %q", label.Text, tab.Text)
	}
}

func TestNewPlaceholderContentBackgroundIsDarkenedNotBlack(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "text")
	stack := NewPlaceholderContent(tab).(*fyne.Container)
	rect, ok := stack.Objects[0].(*canvas.Rectangle)
	if !ok {
		t.Fatalf("stack.Objects[0] is %T, want *canvas.Rectangle", stack.Objects[0])
	}

	got, ok := rect.FillColor.(color.NRGBA)
	if !ok {
		t.Fatalf("rect.FillColor is %T, want color.NRGBA", rect.FillColor)
	}
	if got.R == 0 && got.G == 0 && got.B == 0 {
		t.Fatalf("background is fully black: %+v", got)
	}
	if got.A == 0 {
		t.Fatalf("background is fully transparent: %+v", got)
	}
}
