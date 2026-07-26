package treestate_test

import (
	"slices"
	"testing"

	"github.com/dmongrel/go-ux/test"
	"github.com/dmongrel/go-ux/treestate"
)

// TestSetAndReloadRoundTrip is the core round-trip: state saved by one
// Tracker (simulating a session) must be readable by a second Tracker
// constructed fresh over the same db (simulating reopening the window).
func TestSetAndReloadRoundTrip(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tracker1 := treestate.New(d, "test.tree")
	tracker1.SetExpanded("a", true)
	tracker1.SetSelected("a1")

	tracker2 := treestate.New(d, "test.tree")
	if !slices.Contains(tracker2.Expanded(), "a") {
		t.Errorf("Expanded() = %v, want to contain \"a\"", tracker2.Expanded())
	}
	if tracker2.Selected() != "a1" {
		t.Errorf("Selected() = %q, want \"a1\"", tracker2.Selected())
	}
}

// TestSetExpandedFalseRemovesFromExpanded proves collapsing persists as a
// removal, not just a no-op — a fresh Tracker over the same db must not
// see it as expanded.
func TestSetExpandedFalseRemovesFromExpanded(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tracker1 := treestate.New(d, "test.tree")
	tracker1.SetExpanded("a", true)
	tracker1.SetExpanded("b", true)
	tracker1.SetExpanded("a", false) // "b" stays expanded, "a" is explicitly re-collapsed

	tracker2 := treestate.New(d, "test.tree")
	if slices.Contains(tracker2.Expanded(), "a") {
		t.Error("\"a\" persisted as expanded, want collapsed (it was explicitly collapsed before the last save)")
	}
	if !slices.Contains(tracker2.Expanded(), "b") {
		t.Error("\"b\" persisted as collapsed, want expanded (never collapsed)")
	}
}

// TestNewTrackerWithNoPersistedStateStartsEmpty proves a Tracker over an id
// with no prior SaveUIState call starts with no expanded nodes and no
// selection, rather than erroring.
func TestNewTrackerWithNoPersistedStateStartsEmpty(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tracker := treestate.New(d, "test.tree.unused")
	if len(tracker.Expanded()) != 0 {
		t.Errorf("Expanded() = %v, want empty", tracker.Expanded())
	}
	if tracker.Selected() != "" {
		t.Errorf("Selected() = %q, want \"\"", tracker.Selected())
	}
}

// TestDifferentIDsAreIndependent proves two Trackers with different ids
// against the same db don't see each other's state.
func TestDifferentIDsAreIndependent(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	a := treestate.New(d, "test.tree.a")
	a.SetExpanded("x", true)
	a.SetSelected("x")

	b := treestate.New(d, "test.tree.b")
	if len(b.Expanded()) != 0 || b.Selected() != "" {
		t.Errorf("tree.b saw tree.a's state: Expanded()=%v Selected()=%q", b.Expanded(), b.Selected())
	}
}
