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
