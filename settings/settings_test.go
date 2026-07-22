package settings_test

import (
	"strconv"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"go-ux/db"
	"go-ux/settings"
	"go-ux/test"
)

func TestNewWindowBuildsTreeFromRegistry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	win, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if win == nil {
		t.Fatal("NewWindow returned nil window")
	}
}

func TestSetSizeIsChainableAndReturnsWindow(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	app := fynetest.NewApp()
	defer app.Quit()

	win, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	if got := win.SetSize(640, 480); got != win {
		t.Errorf("SetSize returned %#v, want the same *Window for chaining", got)
	}
	// Non-positive values must not panic and must remain no-ops.
	win.SetSize(0, 480).SetSize(640, 0).SetSize(-1, -1)
}

func TestNewWindowWithEmptyRegistry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	app := fynetest.NewApp()
	defer app.Quit()

	if _, err := settings.NewWindow(app, d); err != nil {
		t.Fatalf("NewWindow with empty registry: %v", err)
	}
}

func TestTreeStateRestoresExpandedAndSelectedNodeAcrossReopen(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w1, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var vcsID, gitID int64
	for _, n := range nodes {
		switch n.Description {
		case "Version Control":
			vcsID = n.ID
		case "Git":
			gitID = n.ID
		}
	}
	if vcsID == 0 || gitID == 0 {
		t.Fatal("expected \"Version Control\" and \"Git\" nodes from SeedExample")
	}
	vcsUID := strconv.FormatInt(vcsID, 10)
	gitUID := strconv.FormatInt(gitID, 10)

	w1.TreeForTest().OpenBranch(vcsUID)
	w1.TreeForTest().Select(gitUID)

	if got := w1.SelectedNodeForTest(); got != gitUID {
		t.Fatalf("after selecting Git, SelectedNodeForTest() = %q, want %q", got, gitUID)
	}

	// A fresh Window against the same db must restore both the expanded
	// branch and the selection on its own, with no help from this test.
	w2, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow (reopen): %v", err)
	}
	if !w2.TreeForTest().IsBranchOpen(vcsUID) {
		t.Error("\"Version Control\" branch not restored open on reopen")
	}
	if got := w2.SelectedNodeForTest(); got != gitUID {
		t.Errorf("SelectedNodeForTest() after reopen = %q, want %q (Git)", got, gitUID)
	}
}

func TestTreeStateStaleReferenceSkippedOnRestore(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	// Simulate a previous session that selected/expanded a node which no
	// longer exists (e.g. removed by an app upgrade) by writing a
	// tree-state blob directly, referencing a UID no current node has.
	// settings has no node-deletion API to exercise this more directly.
	if err := d.SaveUIState(settings.TreeComponentIDForTest(), []byte(`{"Expanded":["999999"],"Selected":"999999"}`)); err != nil {
		t.Fatalf("SaveUIState: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w, err := settings.NewWindow(app, d) // must not panic
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if got := w.SelectedNodeForTest(); got != "" {
		t.Errorf("SelectedNodeForTest() = %q, want empty (stale UID must be skipped, no fallback)", got)
	}
	if w.TreeForTest().IsBranchOpen("999999") {
		t.Error("stale branch UID must not be opened")
	}
}

// TestSearchExpandedBranchesArePersisted covers the applySearch fix this
// task makes: search-driven branch opens (via the per-branch OpenBranch
// loop replacing OpenAllBranches) must persist just like a manual click,
// per the design's "persist whatever's expanded, regardless of cause"
// decision.
func TestSearchExpandedBranchesArePersisted(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w1, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var vcsID int64
	for _, n := range nodes {
		if n.Description == "Version Control" {
			vcsID = n.ID
		}
	}
	if vcsID == 0 {
		t.Fatal("expected a \"Version Control\" node from SeedExample")
	}
	vcsUID := strconv.FormatInt(vcsID, 10)

	// "auto" matches Git's "Auto-update on branch switch" property label,
	// which should reveal and auto-expand its parent, "Version Control".
	w1.ApplySearchForTest("auto")
	if !w1.TreeForTest().IsBranchOpen(vcsUID) {
		t.Fatal("search for \"auto\" did not open \"Version Control\" branch — precondition for this test failed")
	}

	w2, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow (reopen): %v", err)
	}
	if !w2.TreeForTest().IsBranchOpen(vcsUID) {
		t.Error("search-driven branch expansion was not persisted across reopen")
	}
}

func TestPropertyFloatRendersValidatedEntry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Float Node", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "ratio", "Ratio", db.PropertyFloat, "1.2", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	// settings.Window has no public Close method and no other test in this
	// file closes its Window either — no cleanup call needed here.

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	obj := w.PropertyWidgetForTest(nodeID, props[0])
	entry, ok := obj.(*widget.Entry)
	if !ok {
		t.Fatalf("propertyWidget(PropertyFloat) = %T, want *widget.Entry", obj)
	}
	if entry.Text != "1.2" {
		t.Errorf("entry.Text = %q, want %q", entry.Text, "1.2")
	}
	if entry.Validator == nil {
		t.Fatal("entry.Validator is nil, want a float validator")
	}
	if err := entry.Validator("not-a-number"); err == nil {
		t.Error("Validator(\"not-a-number\") = nil, want an error")
	}
	if err := entry.Validator("2.5"); err != nil {
		t.Errorf("Validator(\"2.5\") = %v, want nil", err)
	}
}
