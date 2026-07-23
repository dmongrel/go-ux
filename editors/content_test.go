package editors

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"go-ux/fontsettings"
)

func newTestFonts() *fontsettings.State {
	return fontsettings.NewState(fontsettings.DefaultFontSettings)
}

// unwrapDocumentContent drills through newDocumentContent's
// ThemeOverride(Stack(rect, Scroll(Border(entry, sidebar)))) shape down to
// the individual pieces tests need.
func unwrapDocumentContent(t *testing.T, obj fyne.CanvasObject) *fyne.Container {
	t.Helper()
	override, ok := obj.(*container.ThemeOverride)
	if !ok {
		t.Fatalf("newDocumentContent returned %T, want *container.ThemeOverride", obj)
	}
	stack, ok := override.Content.(*fyne.Container)
	if !ok {
		t.Fatalf("override.Content is %T, want *fyne.Container", override.Content)
	}
	return stack
}

func newDocumentContentParts(t *testing.T, tab *Tab, key any) (entry *editorEntry, sidebar *gutter, cleanup func()) {
	t.Helper()
	obj, cleanup := newDocumentContent(tab, key, newTestFonts(), nil)
	stack := unwrapDocumentContent(t, obj)
	scroll, ok := stack.Objects[1].(*container.Scroll)
	if !ok {
		t.Fatalf("stack.Objects[1] is %T, want *container.Scroll", stack.Objects[1])
	}
	row, ok := scroll.Content.(*fyne.Container)
	if !ok {
		t.Fatalf("scroll content is %T, want *fyne.Container (Border)", scroll.Content)
	}
	entry, ok = row.Objects[0].(*editorEntry)
	if !ok {
		t.Fatalf("row.Objects[0] is %T, want *editorEntry", row.Objects[0])
	}
	sidebar, ok = row.Objects[1].(*gutter)
	if !ok {
		t.Fatalf("row.Objects[1] is %T, want *gutter", row.Objects[1])
	}
	return entry, sidebar, cleanup
}

func newDocumentContentEntry(t *testing.T, tab *Tab, key any) (*editorEntry, func()) {
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

	if len(sidebar.rich.Segments) != 3 {
		t.Fatalf("got %d gutter segments, want 3", len(sidebar.rich.Segments))
	}
	for i, want := range []string{"1", "2", "3"} {
		if got := gutterLineText(t, sidebar, i); got != want {
			t.Errorf("gutter segment %d = %q, want %q", i, got, want)
		}
	}
}

func TestNewDocumentContentGutterUpdatesAsTextChanges(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "one line")
	entry, sidebar, cleanup := newDocumentContentParts(t, tab, "key1")
	defer cleanup()

	entry.SetText("one\ntwo\nthree\nfour")

	if len(sidebar.rich.Segments) != 4 {
		t.Fatalf("got %d gutter segments, want 4", len(sidebar.rich.Segments))
	}
	for i, want := range []string{"1", "2", "3", "4"} {
		if got := gutterLineText(t, sidebar, i); got != want {
			t.Errorf("gutter segment %d = %q, want %q", i, got, want)
		}
	}
}

func TestNewDocumentContentBackgroundIsDarkenedNotBlack(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "text")
	obj, cleanup := newDocumentContent(tab, "key1", newTestFonts(), nil)
	defer cleanup()

	stack := unwrapDocumentContent(t, obj)
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

func TestNewDocumentContentFontSizeChangeResizesText(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "text")
	fonts := newTestFonts()
	obj, cleanup := newDocumentContent(tab, "key1", fonts, nil)
	defer cleanup()

	override, ok := obj.(*container.ThemeOverride)
	if !ok {
		t.Fatalf("newDocumentContent returned %T, want *container.ThemeOverride", obj)
	}

	before := override.Theme.Size(theme.SizeNameText)
	fonts.Set(fontsettings.FontSettings{Size: fonts.Current().Size + 5, LineHeight: 1.0, ColumnWidth: 1.0})
	after := override.Theme.Size(theme.SizeNameText)

	if after == before {
		t.Errorf("theme text size did not change after fonts.Set: before=%v after=%v", before, after)
	}
}
