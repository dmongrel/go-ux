package editors

import (
	"testing"

	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
)

// newPlaceholderPane builds a bare, group-less Pane for split.go's
// pure-tree-logic tests — these only need distinct, comparable
// fyne.CanvasObjects (see split.go's node doc comment on pointer-identity
// comparisons), not a fully wired-up Pane. A nil group is fine here: none
// of these tests call methods that touch p.group.
func newPlaceholderPane(name string) *Pane {
	return newPane(nil, name, false)
}

func TestSplitCreatesTwoPaneTree(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	root := leaf(paneA)

	newRoot, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("split returned ok=false")
	}
	if newRoot.isLeaf() {
		t.Fatalf("expected new root to not be a leaf")
	}
	if newRoot.axis != axisHorizontal {
		t.Fatalf("expected axisHorizontal, got %v", newRoot.axis)
	}

	var visited []*Pane
	walkPanes(newRoot, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 || visited[0] != paneA || visited[1] != paneB {
		t.Fatalf("expected [a, b], got %v", visited)
	}
}

func TestSplitFailsOnAlreadyNestedPane(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	paneC := newPlaceholderPane("c")
	paneD := newPlaceholderPane("d")

	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("first split failed")
	}
	root, ok = split(root, paneB, axisVertical, paneC)
	if !ok {
		t.Fatalf("second split failed")
	}

	// paneB and paneC are now nested (depth 2) inside the inner split.
	before := root
	newRoot, ok := split(root, paneC, axisHorizontal, paneD)
	if ok {
		t.Fatalf("expected split on depth-2 pane to fail")
	}
	if newRoot != before {
		t.Fatalf("expected unchanged root to be returned")
	}

	var visited []*Pane
	walkPanes(newRoot, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 3 {
		t.Fatalf("expected 3 panes still present, got %d", len(visited))
	}
}

func TestRemovePaneReplacesWithSibling(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("split failed")
	}

	newRoot, ok := removePane(root, paneB, paneA)
	if !ok {
		t.Fatalf("removePane returned ok=false")
	}
	if !newRoot.isLeaf() {
		t.Fatalf("expected new root to be a leaf")
	}
	if newRoot.pane != paneA {
		t.Fatalf("expected surviving root to be paneA")
	}

	var visited []*Pane
	walkPanes(newRoot, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 1 || visited[0] != paneA {
		t.Fatalf("expected only paneA, got %v", visited)
	}
}

func TestRemovePanePromotesInnerSplitSibling(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	paneC := newPlaceholderPane("c")

	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("outer split failed")
	}
	// Split the right side (paneB) into top/bottom with paneC.
	root, ok = split(root, paneB, axisVertical, paneC)
	if !ok {
		t.Fatalf("inner split failed")
	}

	newRoot, ok := removePane(root, paneC, paneA)
	if !ok {
		t.Fatalf("removePane returned ok=false")
	}
	if newRoot.isLeaf() {
		t.Fatalf("expected new root to still be a split")
	}
	if newRoot.a.pane != paneA {
		t.Fatalf("expected left side untouched (paneA)")
	}
	if !newRoot.b.isLeaf() || newRoot.b.pane != paneB {
		t.Fatalf("expected right side to be a bare leaf paneB, got %+v", newRoot.b)
	}
}

func TestRemovePaneNoOpOnPrimary(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("split failed")
	}

	before := root
	newRoot, ok := removePane(root, paneA, paneA)
	if ok {
		t.Fatalf("expected removePane on primary to fail")
	}
	if newRoot != before {
		t.Fatalf("expected unchanged root")
	}
}

func TestAdjacentPaneFindsExistingSibling(t *testing.T) {
	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("split failed")
	}

	target, ok := adjacentPane(root, paneA, axisHorizontal)
	if !ok {
		t.Fatalf("expected to find adjacent pane")
	}
	if target != paneB {
		t.Fatalf("expected paneB, got %v", target)
	}
}

func TestAdjacentPaneMissingReturnsFalse(t *testing.T) {
	paneA := newPlaceholderPane("a")
	root := leaf(paneA)

	_, ok := adjacentPane(root, paneA, axisHorizontal)
	if ok {
		t.Fatalf("expected no adjacent pane on single-pane tree")
	}
}

func TestRebuildProducesCanvasObjectMatchingShape(t *testing.T) {
	fynetest.NewApp()

	paneA := newPlaceholderPane("a")
	paneB := newPlaceholderPane("b")
	root := leaf(paneA)
	root, ok := split(root, paneA, axisHorizontal, paneB)
	if !ok {
		t.Fatalf("split failed")
	}

	obj := rebuild(root)
	s, ok := obj.(*container.Split)
	if !ok {
		t.Fatalf("expected *container.Split, got %T", obj)
	}
	if s.Leading != paneA {
		t.Fatalf("expected Leading to be paneA")
	}
	if s.Trailing != paneB {
		t.Fatalf("expected Trailing to be paneB")
	}

	singleRoot := leaf(paneA)
	singleObj := rebuild(singleRoot)
	if singleObj != paneA {
		t.Fatalf("expected bare paneA for single-pane tree, got %T", singleObj)
	}
}
