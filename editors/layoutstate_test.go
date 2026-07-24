package editors

import (
	"os"
	"path/filepath"
	"testing"

	fynetest "fyne.io/fyne/v2/test"

	"go-ux/db"
	"go-ux/test"
)

func TestNewGroupFromSettingsWithNoPriorStateFallsBackToDefault(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	g := NewGroupFromSettings(app, d, "g1")

	var count int
	var only *Pane
	walkPanes(g.root, func(p *Pane) {
		count++
		only = p
	})
	if count != 1 {
		t.Fatalf("expected 1 pane, got %d", count)
	}
	if only != g.primary {
		t.Fatalf("expected the single pane to be the primary pane")
	}
}

// TestNewGroupFromSettingsReadsRealFileContentOnRestore is a regression
// test for a real gap: before Group had any real file I/O (OpenFile, a
// later phase than the original layout persistence), a restored tab
// showed a fake "(restored placeholder text for ...)" string instead of
// its actual file content — that stopped being necessary once reading a
// real file from disk was something this package already did elsewhere,
// but layoutstate.go's restore path kept using the fake placeholder
// anyway until this fix.
func TestNewGroupFromSettingsReadsRealFileContentOnRestore(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	path := filepath.Join(t.TempDir(), "chapter1.txt")
	if err := os.WriteFile(path, []byte("real saved content"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	panes := []db.EditorPane{{ID: 1, IsPane: true, IsPrimary: true}}
	tabs := []db.EditorTab{{PaneID: 1, FilePath: path, TabOrder: 0, IsActive: true}}
	if err := d.SaveEditorLayout("g-restore", panes, tabs); err != nil {
		t.Fatalf("SaveEditorLayout: %v", err)
	}

	g := NewGroupFromSettings(app, d, "g-restore")
	g.Close()

	if got := g.primary.active.Doc.Text(); got != "real saved content" {
		t.Errorf("restored tab text = %q, want %q (the real file content, not a placeholder)", got, "real saved content")
	}
}

// TestNewGroupFromSettingsMissingFileShowsClearPlaceholder confirms a
// restored tab whose file no longer exists on disk gets an explicit,
// clearly-labeled placeholder rather than either a crash or a silently
// empty Document.
func TestNewGroupFromSettingsMissingFileShowsClearPlaceholder(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	missingPath := filepath.Join(t.TempDir(), "does-not-exist.txt")
	panes := []db.EditorPane{{ID: 1, IsPane: true, IsPrimary: true}}
	tabs := []db.EditorTab{{PaneID: 1, FilePath: missingPath, TabOrder: 0, IsActive: true}}
	if err := d.SaveEditorLayout("g-restore-missing", panes, tabs); err != nil {
		t.Fatalf("SaveEditorLayout: %v", err)
	}

	g := NewGroupFromSettings(app, d, "g-restore-missing")
	g.Close()

	got := g.primary.active.Doc.Text()
	if got == "" {
		t.Errorf("restored tab text is empty for a missing file, want a clear placeholder message")
	}
	if got == "real saved content" {
		t.Errorf("restored tab text unexpectedly matches real content for a file that shouldn't exist")
	}
}

// TestLoadPrunesEmptyNonPrimaryPaneAndRePersists is a regression test for
// a real report: a pre-bugfix build of splitPane could leave a non-primary
// pane with zero tabs persisted to disk; simply fixing splitPane doesn't
// retroactively clean up state a user already saved with the old, buggy
// code. NewGroupFromSettings must self-heal: prune the empty pane on
// load AND write the healed shape back, so the same stale layout doesn't
// keep reappearing on every subsequent restart.
func TestLoadPrunesEmptyNonPrimaryPaneAndRePersists(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	// Hand-construct exactly the shape a pre-bugfix splitPane would have
	// produced and persisted: a 2-pane horizontal split where the primary
	// (id 2) has one tab and the non-primary pane (id 3) has none.
	panes := []db.EditorPane{
		{ID: 1, IsPane: false, Axis: "h", SplitOffset: 0.5},
		{ID: 2, ParentPane: int64Ptr(1), IsPane: true, SortOrder: 0, IsPrimary: true},
		{ID: 3, ParentPane: int64Ptr(1), IsPane: true, SortOrder: 1, IsPrimary: false},
	}
	tabs := []db.EditorTab{
		{PaneID: 2, FilePath: "chapter1.txt", TabOrder: 0, IsActive: true},
	}
	if err := d.SaveEditorLayout("stale-group", panes, tabs); err != nil {
		t.Fatalf("SaveEditorLayout: %v", err)
	}

	g := NewGroupFromSettings(app, d, "stale-group")

	var count int
	var only *Pane
	walkPanes(g.root, func(p *Pane) {
		count++
		only = p
	})
	if count != 1 {
		t.Fatalf("expected the empty non-primary pane to be pruned, leaving 1 pane, got %d", count)
	}
	if only != g.primary {
		t.Fatal("expected the surviving pane to be the primary pane")
	}

	// Confirm the healed shape was actually written back, not just fixed
	// in memory — a second, independent load against the same groupID
	// must NOT see the stale empty pane reappear.
	g2 := NewGroupFromSettings(app, d, "stale-group")
	var count2 int
	walkPanes(g2.root, func(*Pane) { count2++ })
	if count2 != 1 {
		t.Fatalf("stale empty pane reappeared on a second load — healed layout was not re-persisted; got %d panes", count2)
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestLiveChangesPersistAndReloadRoundTrips(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	g1 := NewGroupFromSettings(app, d, "g2")
	g1.AddTab(NewTab("t1", "T1", "/a.txt", "hello"))
	g1.SplitRight(g1.primary)

	var newPane *Pane
	walkPanes(g1.root, func(p *Pane) {
		if p != g1.primary {
			newPane = p
		}
	})
	if newPane == nil {
		t.Fatalf("SplitRight did not create a second pane")
	}
	newPane.AddTab(NewTab("t2", "T2", "/b.txt", "world"))

	// Simulate an app restart: a second, independent Group loading the
	// same groupID from the same db.
	g2 := NewGroupFromSettings(app, d, "g2")

	var count int
	foundA := false
	walkPanes(g2.root, func(p *Pane) {
		count++
		for _, tab := range p.tabs {
			if tab.FilePath == "/a.txt" {
				foundA = true
			}
		}
	})
	if count != 2 {
		t.Fatalf("expected 2 panes after reload, got %d", count)
	}
	if !foundA {
		t.Fatalf("expected to find a tab with FilePath /a.txt after reload")
	}
}

// TestNotifyChangedIsNoOpWithoutDatabase exercises every call site that
// invokes notifyChanged internally (AddTab, SplitRight, MoveRight,
// closeTabRequested) plus a direct notifyChanged() call, all against a
// Group with a nil database (NewGroup, not NewGroupFromSettings) — the
// actual thing being tested is that none of them panic or otherwise
// misbehave trying to reach a nil *db.DB. That's the whole contract: a
// no-op-by-design function with no return value and no observable side
// effect (by definition — there's no database to have one on) has
// nothing else to assert on, so "doesn't crash across every path that
// calls it" is the meaningful check here, not just "the tree still has
// the right shape after one split" (which doesn't actually exercise
// notifyChanged's nil-database behavior at all on its own).
func TestNotifyChangedIsNoOpWithoutDatabase(t *testing.T) {
	app := fynetest.NewApp()

	g := NewGroup(app)
	if g.database != nil {
		t.Fatalf("precondition: expected g.database == nil for a plain NewGroup Group")
	}

	tab := NewTab("t1", "T1", "/a.txt", "hello")
	g.AddTab(tab)
	g.notifyChanged()
	g.SplitRight(g.primary)

	var visited []*Pane
	walkPanes(g.root, func(p *Pane) { visited = append(visited, p) })
	if len(visited) != 2 {
		t.Fatalf("expected 2 panes after split, got %d", len(visited))
	}
	var secondary *Pane
	for _, p := range visited {
		if p != g.primary {
			secondary = p
		}
	}

	g.MoveRight(g.primary, tab)
	secondary.closeTabRequested(tab)
	g.notifyChanged()
}

func TestMalformedPersistedDataFallsBackToDefault(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	// A split node with no children rows at all — malformed on purpose.
	if err := d.SaveEditorLayout("g3", []db.EditorPane{{ID: 1, IsPane: false, Axis: "h"}}, nil); err != nil {
		t.Fatalf("SaveEditorLayout: %v", err)
	}

	g := NewGroupFromSettings(app, d, "g3")

	var count int
	var only *Pane
	walkPanes(g.root, func(p *Pane) {
		count++
		only = p
	})
	if count != 1 {
		t.Fatalf("expected fallback to 1 pane, got %d", count)
	}
	if only != g.primary {
		t.Fatalf("expected the single pane to be the primary pane")
	}
}
