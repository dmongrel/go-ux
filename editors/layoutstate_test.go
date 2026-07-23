package editors

import (
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

func TestNotifyChangedIsNoOpWithoutDatabase(t *testing.T) {
	app := fynetest.NewApp()

	g := NewGroup(app)
	g.AddTab(NewTab("t1", "T1", "/a.txt", "hello"))
	g.SplitRight(g.primary)

	var count int
	walkPanes(g.root, func(*Pane) { count++ })
	if count != 2 {
		t.Fatalf("expected 2 panes after split, got %d", count)
	}
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
