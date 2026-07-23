package editors

import (
	"testing"

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
