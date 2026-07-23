package editors

import (
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fynetest "fyne.io/fyne/v2/test"
)

func TestPaneAddTabSetsActiveAndUpdatesContent(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tab := NewTab("t1", "chapter1.md", "", "hello")

	p.AddTab(tab)

	if p.active != tab {
		t.Fatalf("active = %v, want tab", p.active)
	}
	if len(p.tabs) != 1 || p.tabs[0] != tab {
		t.Fatalf("tabs = %v, want [tab]", p.tabs)
	}
	if p.tabBar.Active != tab {
		t.Fatalf("tabBar.Active = %v, want tab", p.tabBar.Active)
	}
	if len(p.center.Objects) != 1 {
		t.Fatalf("center.Objects has %d entries, want 1", len(p.center.Objects))
	}
}

// TestPaneAddTabSyncsTabBarTabs is a regression test for a real bug found
// via manual testing (go run ./editorsdemo showed only a bare label, no
// tab bar at all): AddTab updated p.tabs and p.tabBar.Active but never
// p.tabBar.Tabs, so TabBar always rendered zero chips regardless of how
// many tabs were actually open. Unit tests for Pane and TabBar in
// isolation both missed this because neither one exercises the wiring
// between them that AddTab is responsible for.
func TestPaneAddTabSyncsTabBarTabs(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tabA := NewTab("a", "a.md", "", "")
	tabB := NewTab("b", "b.md", "", "")
	tabC := NewTab("c", "c.md", "", "")

	p.AddTab(tabA)
	p.AddTab(tabB)
	p.AddTab(tabC)

	if len(p.tabBar.Tabs) != 3 {
		t.Fatalf("tabBar.Tabs has %d entries after 3 AddTab calls, want 3 (tab bar would render with no chips otherwise)", len(p.tabBar.Tabs))
	}
	if p.tabBar.Tabs[0] != tabA || p.tabBar.Tabs[1] != tabB || p.tabBar.Tabs[2] != tabC {
		t.Fatalf("tabBar.Tabs = %v, want [tabA, tabB, tabC] in order", p.tabBar.Tabs)
	}
}

func TestPaneCloseTabRemovesAndPicksNeighbor(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tabA := NewTab("a", "a.md", "", "")
	tabB := NewTab("b", "b.md", "", "")
	tabC := NewTab("c", "c.md", "", "")
	p.AddTab(tabA)
	p.AddTab(tabB)
	p.AddTab(tabC)
	p.setActive(tabB) // make the middle tab active before closing it

	p.closeTabRequested(tabB)

	if len(p.tabs) != 2 {
		t.Fatalf("tabs has %d entries, want 2", len(p.tabs))
	}
	if p.active == nil {
		t.Fatalf("active is nil, want a real neighboring tab")
	}
	if p.active != tabA && p.active != tabC {
		t.Fatalf("active = %v, want tabA or tabC", p.active)
	}
}

func TestPaneTogglePreviewSwitchesToRenderedMarkdown(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tab := NewTab("t1", "chapter1.md", "chapter1.md", "hello world")
	p.AddTab(tab)

	p.togglePreview()

	if !p.previewMode {
		t.Fatalf("previewMode = false after togglePreview, want true")
	}
	scroll, ok := p.center.Objects[0].(*container.Scroll)
	if !ok {
		t.Fatalf("center.Objects[0] is %T, want *container.Scroll (preview)", p.center.Objects[0])
	}
	if _, ok := scroll.Content.(*fyne.Container); !ok {
		t.Fatalf("preview scroll content is %T, want *fyne.Container (renderMarkdown's VBox)", scroll.Content)
	}
	if p.contentCleanup != nil {
		t.Errorf("contentCleanup is non-nil while showing a preview snapshot (nothing to clean up)")
	}
	if p.tabBar.PreviewMode != true {
		t.Errorf("tabBar.PreviewMode = false, want true after togglePreview")
	}
}

func TestPaneTogglePreviewTwiceReturnsToEditableContent(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tab := NewTab("t1", "chapter1.md", "chapter1.md", "hello world")
	p.AddTab(tab)

	p.togglePreview()
	p.togglePreview()

	if p.previewMode {
		t.Fatalf("previewMode = true after toggling twice, want false")
	}
	if p.contentCleanup == nil {
		t.Errorf("contentCleanup is nil after returning to editable content, want a real cleanup func")
	}
}

func TestPaneTogglePreviewNoOpForNonMarkdownTab(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tab := NewTab("t1", "notes.txt", "notes.txt", "hello world")
	p.AddTab(tab)

	// TabBar hides the preview button for non-Markdown tabs, so this path
	// isn't reachable via the UI — but rebuildCenterContent's
	// isMarkdownFile guard must still hold even if togglePreview is called
	// directly, so it's still worth asserting content stays editable.
	p.togglePreview()

	if p.contentCleanup == nil {
		t.Errorf("contentCleanup is nil — content did not stay in editable mode for a non-Markdown tab")
	}
}

func TestPaneSetActiveResetsPreviewMode(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tabA := NewTab("a", "a.md", "a.md", "content a")
	tabB := NewTab("b", "b.md", "b.md", "content b")
	p.AddTab(tabA)
	p.AddTab(tabB)

	p.togglePreview() // preview tabB (the active one)
	p.setActive(tabA)

	if p.previewMode {
		t.Errorf("previewMode = true after setActive, want false (switching tabs exits preview)")
	}
}

func TestPaneContentOnSaveWritesTabToDisk(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close() // stop the real watcher before the write below — see watch_test.go's package doc comment

	tab.Doc.SetText("edited via the content area")

	// g.primary's center content holds the editorEntry whose onSave was
	// wired by rebuildCenterContent (pane.go) to call g.SaveTab — reach
	// through the same Stack(rect, Scroll(Border(entry, sidebar))) shape
	// content_test.go's unwrapDocumentContent documents, but starting
	// from the already-built center content rather than calling
	// newDocumentContent directly, so this exercises the real wiring.
	override, ok := g.primary.center.Objects[0].(*container.ThemeOverride)
	if !ok {
		t.Fatalf("center.Objects[0] is %T, want *container.ThemeOverride", g.primary.center.Objects[0])
	}
	stack := override.Content.(*fyne.Container)
	scroll := stack.Objects[1].(*container.Scroll)
	row := scroll.Content.(*fyne.Container)
	entry := row.Objects[0].(*editorEntry)

	entry.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: fyne.KeyModifierControl})

	if tab.Dirty() {
		t.Errorf("Dirty() = true after Ctrl+S, want false")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if string(onDisk) != "edited via the content area" {
		t.Errorf("file on disk = %q, want %q", string(onDisk), "edited via the content area")
	}
}

func TestPaneCloseLastTabOnPrimaryLeavesItEmpty(t *testing.T) {
	fynetest.NewApp()

	p := newPane(nil, "p", true)
	tab := NewTab("t1", "chapter1.md", "", "")
	p.AddTab(tab)

	p.closeTabRequested(tab)

	if len(p.tabs) != 0 {
		t.Fatalf("tabs has %d entries, want 0", len(p.tabs))
	}
	if p.active != nil {
		t.Fatalf("active = %v, want nil", p.active)
	}
	if len(p.center.Objects) != 0 {
		t.Fatalf("center.Objects has %d entries, want 0", len(p.center.Objects))
	}
}
