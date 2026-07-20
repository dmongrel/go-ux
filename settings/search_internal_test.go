package settings

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"

	"go-ux/test"
)

func newTestWindow(t *testing.T) *Window {
	t.Helper()

	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	t.Cleanup(app.Quit)

	w, err := NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	return w
}

func uidByDescription(w *Window, description string) string {
	for uid, n := range w.byID {
		if n.Description == description {
			return uid
		}
	}
	return ""
}

func TestApplySearchMatchesNodeDescription(t *testing.T) {
	w := newTestWindow(t)

	gitUID := uidByDescription(w, "Git")
	vcsUID := uidByDescription(w, "Version Control")
	terminalUID := uidByDescription(w, "Terminal")
	if gitUID == "" || vcsUID == "" || terminalUID == "" {
		t.Fatal("expected Terminal, Version Control, and Git nodes from seed data")
	}

	w.applySearch("git")

	if !w.descMatch[gitUID] {
		t.Errorf("descMatch: expected Git to match description search %q", "git")
	}
	if !w.visible[gitUID] {
		t.Error("visible: expected Git node to be visible")
	}
	if !w.visible[vcsUID] {
		t.Error("visible: expected Git's ancestor Version Control to be visible so it stays reachable in the tree")
	}
	if w.visible[terminalUID] {
		t.Error("visible: Terminal should not match search \"git\"")
	}
}

func TestApplySearchMatchesPropertyLabel(t *testing.T) {
	w := newTestWindow(t)

	vcsUID := uidByDescription(w, "Version Control")
	terminalUID := uidByDescription(w, "Terminal")

	w.applySearch("confirm")

	if w.descMatch[vcsUID] {
		t.Error("descMatch: Version Control's own description should not match \"confirm\"")
	}
	if w.propMatch[vcsUID] == nil || !w.propMatch[vcsUID]["confirm_add"] {
		t.Error("propMatch: expected confirm_add property under Version Control to match")
	}
	if !w.visible[vcsUID] {
		t.Error("visible: expected Version Control to be shown because one of its property labels matched")
	}
	if w.visible[terminalUID] {
		t.Error("visible: Terminal has no matching property and should not be shown")
	}
}

func TestApplySearchEmptyClearsFilter(t *testing.T) {
	w := newTestWindow(t)

	w.applySearch("git")
	if w.visible == nil {
		t.Fatal("expected a filter to be active after a non-empty search")
	}

	w.applySearch("")
	if w.visible != nil {
		t.Error("expected clearing the search to remove the filter (visible == nil means show everything)")
	}
}
