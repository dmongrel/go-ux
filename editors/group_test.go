package editors

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

func TestNewGroupStartsWithSinglePrimaryPane(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 1 || visited[0] != g.primary {
		t.Fatalf("expected [g.primary], got %v", visited)
	}
}

func TestSplitRightCreatesTwoPaneLayout(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	tab := NewTab("t1", "chapter1.md", "", "")
	g.AddTab(tab)

	g.SplitRight(g.primary)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(visited))
	}
	if g.root.isLeaf() {
		t.Fatalf("expected root to be a split, not a leaf")
	}
	if g.root.axis != axisHorizontal {
		t.Fatalf("axis = %v, want axisHorizontal", g.root.axis)
	}
}

// TestSplitCopiesActiveTabIntoNewPane is a regression test for a real bug
// found via manual testing: SplitRight/SplitDown created the new pane but
// left it with zero tabs, contradicting the design's split semantics
// ("the new pane shows the SAME underlying document") — the new pane
// should immediately show the source pane's active tab, not start empty.
func TestSplitCopiesActiveTabIntoNewPane(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	tab := NewTab("t1", "chapter1.md", "", "")
	g.AddTab(tab)

	newPaneObj := g.splitPane(g.primary, axisHorizontal)
	if newPaneObj == nil {
		t.Fatal("splitPane returned nil, want the new Pane")
	}

	if len(newPaneObj.tabs) != 1 || newPaneObj.tabs[0] != tab {
		t.Fatalf("new pane's tabs = %v, want [tab] (the same *Tab, shared with the source pane)", newPaneObj.tabs)
	}
	if newPaneObj.active != tab {
		t.Fatalf("new pane's active = %v, want tab", newPaneObj.active)
	}
	// The source pane must still have it too — split shares, it doesn't
	// move (that's what distinguishes it from Move).
	if len(g.primary.tabs) != 1 || g.primary.tabs[0] != tab {
		t.Fatalf("source pane's tabs = %v, want [tab] (split must not remove it from the source)", g.primary.tabs)
	}
}

func TestSplitDownOnAlreadySplitPaneDoesNotExceedOneLevel(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	tab := NewTab("t1", "chapter1.md", "", "")
	g.AddTab(tab)

	g.SplitRight(g.primary)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected 2 panes after SplitRight, got %d", len(visited))
	}
	secondary := visited[1]

	g.SplitDown(secondary)

	visited = nil
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 3 {
		t.Fatalf("expected 3 panes after SplitDown, got %d", len(visited))
	}
	nested := visited[2]

	// nested is now at depth 2; splitting it again should fail (canSplit's
	// depth-2 rule) and leave the tree unchanged.
	g.SplitRight(nested)

	visited = nil
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 3 {
		t.Fatalf("expected still 3 panes after failed split, got %d", len(visited))
	}
}

func TestMoveRightAutoCreatesSplitWhenNoTargetExists(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	tab := NewTab("t1", "chapter1.md", "", "")
	g.AddTab(tab)

	g.MoveRight(g.primary, tab)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(visited))
	}

	var target *Pane
	for _, p := range visited {
		if p != g.primary {
			target = p
		}
	}
	if target == nil {
		t.Fatalf("expected a non-primary pane to exist")
	}
	if len(target.tabs) != 1 || target.tabs[0] != tab {
		t.Fatalf("expected tab to be in the new pane, got %v", target.tabs)
	}
	if len(g.primary.tabs) != 0 {
		t.Fatalf("expected primary to have no tabs left, got %v", g.primary.tabs)
	}
}

func TestMoveRightToExistingAdjacentPane(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	seed := NewTab("seed", "seed.md", "", "")
	g.AddTab(seed)
	g.SplitRight(g.primary)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected 2 panes after SplitRight, got %d", len(visited))
	}
	var existing *Pane
	for _, p := range visited {
		if p != g.primary {
			existing = p
		}
	}

	moved := NewTab("m1", "moved.md", "", "")
	g.primary.AddTab(moved)

	g.MoveRight(g.primary, moved)

	visited = nil
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected still 2 panes (no 3rd pane created), got %d", len(visited))
	}

	found := false
	for _, t2 := range existing.tabs {
		if t2 == moved {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected moved tab in the pre-existing adjacent pane")
	}
}

func TestClosingLastTabInNonPrimaryPaneCollapsesSplit(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	seed := NewTab("seed", "seed.md", "", "")
	g.AddTab(seed)
	g.SplitRight(g.primary)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	var secondary *Pane
	for _, p := range visited {
		if p != g.primary {
			secondary = p
		}
	}

	// Split copies the source's active tab into the new pane (see
	// TestSplitCopiesActiveTabIntoNewPane) — secondary already has `seed`
	// as its only tab at this point. Closing it is what should empty and
	// collapse the pane; no need to add a second tab first.
	if len(secondary.tabs) != 1 || secondary.tabs[0] != seed {
		t.Fatalf("precondition failed: secondary.tabs = %v, want [seed]", secondary.tabs)
	}

	secondary.closeTabRequested(seed)

	visited = nil
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 1 || visited[0] != g.primary {
		t.Fatalf("expected [g.primary], got %v", visited)
	}
}

func TestClosingLastTabInPrimaryPaneDoesNotRemoveIt(t *testing.T) {
	fynetest.NewApp()

	g := NewGroup(nil)
	tab := NewTab("t1", "chapter1.md", "", "")
	g.AddTab(tab)

	g.primary.closeTabRequested(tab)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 1 || visited[0] != g.primary {
		t.Fatalf("expected [g.primary], got %v", visited)
	}
	if g.primary.active != nil {
		t.Fatalf("active = %v, want nil", g.primary.active)
	}
	if len(g.primary.tabs) != 0 {
		t.Fatalf("tabs has %d entries, want 0", len(g.primary.tabs))
	}
}
