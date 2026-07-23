package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	fynetest "fyne.io/fyne/v2/test"
)

// chipObjects returns the i'th chip's (title, close) tappable objects,
// exactly as CreateRenderer/rebuild constructed them — used by tests to
// drive taps through the real widget tree rather than calling TabBar's
// unexported handlers directly.
func chipObjects(t *testing.T, bar *TabBar, i int) (title *chipTitle, closeGlyph *chipClose) {
	t.Helper()
	renderer := bar.CreateRenderer()
	root := renderer.Objects()[0].(*fyne.Container) // Border: [strip, previewBtn]
	strip := root.Objects[0].(*fyne.Container)
	chip := strip.Objects[i].(*fyne.Container)         // Stack: [rect, titleAndClose]
	titleAndClose := chip.Objects[1].(*fyne.Container) // zero-gap HBox: [title, closeGlyph]
	return titleAndClose.Objects[0].(*chipTitle), titleAndClose.Objects[1].(*chipClose)
}

func TestTabBarOnSelectedFiresOnChipTap(t *testing.T) {
	fynetest.NewApp()

	tabA := NewTab("a", "chapter1.md", "", "")
	tabB := NewTab("b", "chapter2.md", "", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tabA, tabB}

	var selected *Tab
	bar.OnSelected = func(tab *Tab) { selected = tab }

	title, _ := chipObjects(t, bar, 1)
	fynetest.Tap(title)

	if selected != tabB {
		t.Fatalf("OnSelected fired with %v, want tabB", selected)
	}
	if bar.Active != tabB {
		t.Fatalf("Active = %v, want tabB", bar.Active)
	}
}

func TestTabBarOnClosedFiresOnCloseGlyphTap(t *testing.T) {
	fynetest.NewApp()

	tabA := NewTab("a", "chapter1.md", "", "")
	tabB := NewTab("b", "chapter2.md", "", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tabA, tabB}

	var closed *Tab
	selectedCalled := false
	bar.OnClosed = func(tab *Tab) { closed = tab }
	bar.OnSelected = func(*Tab) { selectedCalled = true }

	_, closeGlyph := chipObjects(t, bar, 1)
	fynetest.Tap(closeGlyph)

	if closed != tabB {
		t.Fatalf("OnClosed fired with %v, want tabB", closed)
	}
	if selectedCalled {
		t.Fatalf("OnSelected fired on a close-glyph tap, want no call")
	}
	if bar.Active != nil {
		t.Fatalf("Active = %v, want nil (unchanged) after closing without prior selection", bar.Active)
	}
}

func TestTabBarPreviewButtonHiddenForNonMarkdownActiveTab(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("a", "notes.txt", "notes.txt", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tab}
	bar.Active = tab

	bar.CreateRenderer()

	if bar.previewBtn.Visible() {
		t.Errorf("preview button visible for a non-Markdown active tab")
	}
}

func TestTabBarPreviewButtonVisibleForMarkdownActiveTab(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("a", "chapter1.md", "chapter1.md", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tab}
	bar.Active = tab

	bar.CreateRenderer()

	if !bar.previewBtn.Visible() {
		t.Errorf("preview button not visible for a Markdown active tab")
	}
	if bar.previewBtn.Text != "Preview" {
		t.Errorf("preview button text = %q, want %q", bar.previewBtn.Text, "Preview")
	}
}

func TestTabBarPreviewButtonLabelReflectsPreviewMode(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("a", "chapter1.md", "chapter1.md", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tab}
	bar.Active = tab
	bar.PreviewMode = true

	bar.CreateRenderer()

	if bar.previewBtn.Text != "Edit" {
		t.Errorf("preview button text = %q, want %q", bar.previewBtn.Text, "Edit")
	}
}

func TestTabBarPreviewButtonTapFiresOnTogglePreview(t *testing.T) {
	fynetest.NewApp()

	tab := NewTab("a", "chapter1.md", "chapter1.md", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tab}
	bar.Active = tab

	called := false
	bar.OnTogglePreview = func() { called = true }
	bar.CreateRenderer()

	fynetest.Tap(bar.previewBtn)

	if !called {
		t.Errorf("OnTogglePreview did not fire on preview button tap")
	}
}

func TestTabBarActiveTabIsVisuallyDistinguished(t *testing.T) {
	fynetest.NewApp()

	tabA := NewTab("a", "chapter1.md", "", "")
	tabB := NewTab("b", "chapter2.md", "", "")
	bar := NewTabBar()
	bar.Tabs = []*Tab{tabA, tabB}
	bar.Active = tabA

	renderer := bar.CreateRenderer()
	bar.Refresh()

	root := renderer.Objects()[0].(*fyne.Container) // Border: [strip, previewBtn]
	strip := root.Objects[0].(*fyne.Container)
	activeRect := strip.Objects[0].(*fyne.Container).Objects[0].(*canvas.Rectangle)
	inactiveRect := strip.Objects[1].(*fyne.Container).Objects[0].(*canvas.Rectangle)

	if activeRect.FillColor != chipActiveColor {
		t.Errorf("active chip rect FillColor = %v, want %v", activeRect.FillColor, chipActiveColor)
	}
	if inactiveRect.FillColor == chipActiveColor {
		t.Errorf("inactive chip rect FillColor unexpectedly matches chipActiveColor")
	}
}
