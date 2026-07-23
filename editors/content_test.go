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

func newDocumentContentParts(t *testing.T, tab *Tab, key any) (entry *widget.Entry, sidebar *gutter, cleanup func()) {
	t.Helper()
	obj, cleanup := newDocumentContent(tab, key)
	stack, ok := obj.(*fyne.Container)
	if !ok {
		t.Fatalf("newDocumentContent returned %T, want *fyne.Container", obj)
	}
	scroll, ok := stack.Objects[1].(*container.Scroll)
	if !ok {
		t.Fatalf("stack.Objects[1] is %T, want *container.Scroll", stack.Objects[1])
	}
	row, ok := scroll.Content.(*fyne.Container)
	if !ok {
		t.Fatalf("scroll content is %T, want *fyne.Container (Border)", scroll.Content)
	}
	entry, ok = row.Objects[0].(*widget.Entry)
	if !ok {
		t.Fatalf("row.Objects[0] is %T, want *widget.Entry", row.Objects[0])
	}
	sidebar, ok = row.Objects[1].(*gutter)
	if !ok {
		t.Fatalf("row.Objects[1] is %T, want *gutter", row.Objects[1])
	}
	return entry, sidebar, cleanup
}

func newDocumentContentEntry(t *testing.T, tab *Tab, key any) (*widget.Entry, func()) {
	t.Helper()
	entry, _, cleanup := newDocumentContentParts(t, tab, key)
	return entry, cleanup
}

func TestNewDocumentContentShowsTabText(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "It was a dark and stormy night.")
	entry, cleanup := newDocumentContentEntry(t, tab, "key1")
	defer cleanup()

	if entry.Text != tab.Text() {
		t.Errorf("entry.Text = %q, want %q", entry.Text, tab.Text())
	}
}

func TestNewDocumentContentEditingUpdatesDocument(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "hello")
	entry, cleanup := newDocumentContentEntry(t, tab, "key1")
	defer cleanup()

	entry.SetText("hello world")

	if tab.Doc.Text() != "hello world" {
		t.Errorf("Doc.Text() = %q, want %q", tab.Doc.Text(), "hello world")
	}
	if !tab.Dirty() {
		t.Errorf("Dirty() = false, want true after an edit")
	}
}

func TestNewDocumentContentReflectsExternalDocumentChanges(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "hello")
	entry, cleanup := newDocumentContentEntry(t, tab, "key1")
	defer cleanup()

	// Simulate a second Pane showing the same Document editing it.
	tab.Doc.SetText("hello from elsewhere")

	if entry.Text != "hello from elsewhere" {
		t.Errorf("entry.Text = %q, want %q", entry.Text, "hello from elsewhere")
	}
}

func TestNewDocumentContentTwoListenersStaySynced(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "hello")
	entryA, cleanupA := newDocumentContentEntry(t, tab, "keyA")
	defer cleanupA()
	entryB, cleanupB := newDocumentContentEntry(t, tab, "keyB")
	defer cleanupB()

	entryA.SetText("typed in A")

	if entryB.Text != "typed in A" {
		t.Errorf("entryB.Text = %q, want %q (should sync from entryA's edit)", entryB.Text, "typed in A")
	}
}

func TestNewDocumentContentCleanupStopsFurtherUpdates(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "hello")
	entry, cleanup := newDocumentContentEntry(t, tab, "key1")
	cleanup()

	tab.Doc.SetText("changed after cleanup")

	if entry.Text == "changed after cleanup" {
		t.Errorf("entry still received an update after cleanup was called")
	}
}

func TestNewDocumentContentGutterShowsLineNumbers(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "line one\nline two\nline three")
	_, sidebar, cleanup := newDocumentContentParts(t, tab, "key1")
	defer cleanup()

	if sidebar.label.Text != "1\n2\n3" {
		t.Errorf("gutter text = %q, want %q", sidebar.label.Text, "1\n2\n3")
	}
}

func TestNewDocumentContentGutterUpdatesAsTextChanges(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "one line")
	entry, sidebar, cleanup := newDocumentContentParts(t, tab, "key1")
	defer cleanup()

	entry.SetText("one\ntwo\nthree\nfour")

	if sidebar.label.Text != "1\n2\n3\n4" {
		t.Errorf("gutter text = %q, want %q", sidebar.label.Text, "1\n2\n3\n4")
	}
}

func TestNewDocumentContentBackgroundIsDarkenedNotBlack(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "text")
	obj, cleanup := newDocumentContent(tab, "key1")
	defer cleanup()

	stack := obj.(*fyne.Container)
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
