package treestate_test

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"go-ux/test"
	"go-ux/treestate"
)

// fixtureTree builds a small three-level tree ("" -> a,b ; a -> a1,a2) for
// tests, plus its matching Exists predicate. Each call returns a fresh
// *widget.Tree instance (tests that simulate "reopening" a tree build a
// second one over the same shape) but the same shared expected node set.
func fixtureTree() (*widget.Tree, func(uid string) bool) {
	children := map[string][]string{
		"":  {"a", "b"},
		"a": {"a1", "a2"},
	}
	exists := func(uid string) bool {
		if uid == "" {
			return true
		}
		for _, kids := range children {
			for _, k := range kids {
				if k == uid {
					return true
				}
			}
		}
		return false
	}
	tree := widget.NewTree(
		func(uid string) []string { return children[uid] },
		func(uid string) bool { return len(children[uid]) > 0 },
		func(bool) fyne.CanvasObject { return widget.NewLabel("") },
		func(uid string, branch bool, obj fyne.CanvasObject) { obj.(*widget.Label).SetText(uid) },
	)
	return tree, exists
}

// TestTrackPersistsAndRestoreReopensExpandedAndSelected is the core
// round-trip: state saved by one Tracker (simulating a session) must be
// readable and replayable by a second Tracker over a fresh *widget.Tree
// instance (simulating reopening the window) against the same db.
func TestTrackPersistsAndRestoreReopensExpandedAndSelected(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})

	tree1.OpenBranch("a")
	tree1.Select("a1")

	tree2, _ := fixtureTree()
	var gotSelected string
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{
		Exists:     exists,
		OnSelected: func(uid string) { gotSelected = uid },
	})
	tracker2.Restore()

	if !tree2.IsBranchOpen("a") {
		t.Error("branch \"a\" not restored open")
	}
	if gotSelected != "a1" {
		t.Errorf("OnSelected pass-through fired with %q, want \"a1\"", gotSelected)
	}
}

// TestRestoreSkipsStaleUIDs proves a persisted UID no longer present in the
// current tree (per Exists) is silently dropped — no panic, no fallback
// selection.
func TestRestoreSkipsStaleUIDs(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})
	tree1.OpenBranch("a")
	tree1.Select("a1")

	// A second tree/tracker pair whose Exists rejects "a" and "a1" entirely
	// (as if those nodes were deleted since the state above was saved).
	tree2, _ := fixtureTree()
	goneExists := func(uid string) bool { return uid != "a" && uid != "a1" }
	var gotSelected string
	selectedCalled := false
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{
		Exists:     goneExists,
		OnSelected: func(uid string) { gotSelected = uid; selectedCalled = true },
	})
	tracker2.Restore()

	if tree2.IsBranchOpen("a") {
		t.Error("stale branch \"a\" must not be opened")
	}
	if selectedCalled {
		t.Errorf("OnSelected must not fire for a stale selection, got %q", gotSelected)
	}
}

// TestRestoreDoesNotReSave proves Restore's own OpenBranch/Select calls
// don't themselves trigger another persist — the blob written before
// Restore must be byte-identical to the blob after it.
func TestRestoreDoesNotReSave(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})
	tree1.OpenBranch("a")
	tree1.Select("a1")

	before, err := d.LoadUIState("test.tree")
	if err != nil {
		t.Fatalf("LoadUIState (before): %v", err)
	}

	tree2, _ := fixtureTree()
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{Exists: exists})
	tracker2.Restore()

	after, err := d.LoadUIState("test.tree")
	if err != nil {
		t.Fatalf("LoadUIState (after): %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("Restore changed the persisted blob:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestTrackPassesThroughBranchCallbacksAfterPersisting proves
// OnBranchOpened/OnBranchClosed pass-throughs fire on live (non-restore)
// events, in addition to OnSelected's coverage in the round-trip test
// above.
func TestTrackPassesThroughBranchCallbacksAfterPersisting(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree, exists := fixtureTree()
	var opened, closed string
	treestate.Track(d, "test.tree", tree, treestate.Options{
		Exists:         exists,
		OnBranchOpened: func(uid string) { opened = uid },
		OnBranchClosed: func(uid string) { closed = uid },
	})

	tree.OpenBranch("a")
	if opened != "a" {
		t.Errorf("OnBranchOpened pass-through = %q, want \"a\"", opened)
	}
	tree.CloseBranch("a")
	if closed != "a" {
		t.Errorf("OnBranchClosed pass-through = %q, want \"a\"", closed)
	}
}

// TestClosedBranchStaysClosedAcrossRestore covers the persistence side of
// closing, which the round-trip and pass-through tests above don't: open
// "a", persist, then close "a" (persisting the removal), and confirm a
// fresh Tracker/tree pair restores it closed — not just that closing fires
// the right callback, but that the close-set survives a save/load cycle.
func TestClosedBranchStaysClosedAcrossRestore(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})

	tree1.OpenBranch("a")
	tree1.OpenBranch("b")
	tree1.CloseBranch("a") // "b" stays open, "a" is explicitly re-closed

	tree2, _ := fixtureTree()
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{Exists: exists})
	tracker2.Restore()

	if tree2.IsBranchOpen("a") {
		t.Error("branch \"a\" restored open, want closed (it was explicitly closed before the last save)")
	}
	if !tree2.IsBranchOpen("b") {
		t.Error("branch \"b\" restored closed, want open (never closed)")
	}
}

// TestTrackWithNilExistsTreatsEveryUIDAsValid proves Track defaults a nil
// Options.Exists to "always valid" rather than letting Restore panic on a
// nil call — Exists is documented as required, but a second consumer of
// this package following the minimal-usage example and forgetting it
// should get graceful behavior, not a crash.
func TestTrackWithNilExistsTreatsEveryUIDAsValid(t *testing.T) {
	fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, _ := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{}) // no Exists
	tree1.OpenBranch("a")
	tree1.Select("a1")

	tree2, _ := fixtureTree()
	var gotSelected string
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{
		OnSelected: func(uid string) { gotSelected = uid },
	}) // no Exists here either — must not panic
	tracker2.Restore()

	if !tree2.IsBranchOpen("a") {
		t.Error("branch \"a\" not restored open with a nil Exists")
	}
	if gotSelected != "a1" {
		t.Errorf("OnSelected pass-through fired with %q, want \"a1\"", gotSelected)
	}
}
