package terminal

import (
	"testing"

	"go-ux/db"
)

// newTestDB opens an in-memory db.DB for this file's tests. terminal can't
// import go-ux/test (that package exists for the repo's own top-level tests
// and importing it here would be a cross-package dependency in the wrong
// direction), so it opens its own in-memory database directly the same way
// test.NewDB does.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestRegisterSettingsCreatesTerminalNodeWithProperties confirms
// RegisterSettings seeds exactly the two-property minimal slice this task
// calls for: a root "Terminal" node with default_shell (enum) and
// close_on_exit (bool, default "true").
func TestRegisterSettingsCreatesTerminalNodeWithProperties(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodes, "Terminal")
	if !ok {
		t.Fatalf("no root Terminal node found in %+v", nodes)
	}

	props, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("len(props) = %d, want 2 (%+v)", len(props), props)
	}

	byKey := make(map[string]db.Property, len(props))
	for _, p := range props {
		byKey[p.Key] = p
	}

	shellProp, ok := byKey[KeyDefaultShell]
	if !ok {
		t.Fatal("no default_shell property")
	}
	if shellProp.Type != db.PropertyEnum {
		t.Errorf("default_shell.Type = %v, want PropertyEnum", shellProp.Type)
	}
	if shellProp.Value == "" {
		t.Error("default_shell.Value is empty, want a non-empty default")
	}
	wantDefault := "PowerShell"
	if shells := DetectShells(); len(shells) > 0 {
		wantDefault = shells[0].Name
	}
	if shellProp.Value != wantDefault {
		t.Errorf("default_shell.Value = %q, want %q (first DetectShells() entry, or PowerShell if none)", shellProp.Value, wantDefault)
	}

	closeProp, ok := byKey[KeyCloseOnExit]
	if !ok {
		t.Fatal("no close_on_exit property")
	}
	if closeProp.Type != db.PropertyBool {
		t.Errorf("close_on_exit.Type = %v, want PropertyBool", closeProp.Type)
	}
	if closeProp.Value != "true" {
		t.Errorf("close_on_exit.Value = %q, want \"true\"", closeProp.Value)
	}
}

// TestRegisterSettingsIsIdempotent confirms a second call doesn't duplicate
// the node or its properties — the brief's explicit done-bar, verified by
// comparing ListSettings()'s contents before and after, not just checking
// for a nil error.
func TestRegisterSettingsIsIdempotent(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings (1st call): %v", err)
	}
	nodesBefore, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodesBefore, "Terminal")
	if !ok {
		t.Fatalf("no root Terminal node found after first call")
	}
	propsBefore, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings (2nd call): %v", err)
	}

	nodesAfter, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if len(nodesAfter) != len(nodesBefore) {
		t.Fatalf("len(nodes) after 2nd call = %d, want unchanged %d", len(nodesAfter), len(nodesBefore))
	}

	propsAfter, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(propsAfter) != len(propsBefore) {
		t.Fatalf("len(props) after 2nd call = %d, want unchanged %d", len(propsAfter), len(propsBefore))
	}
}

// TestWithDefaultFirstReorders is a pure unit test over withDefaultFirst's
// reordering logic (window.go) — no db, no Fyne, no ConPTY involved, so it
// runs on any platform and needs no windows build tag.
func TestWithDefaultFirstReorders(t *testing.T) {
	a := ShellDef{Name: "a"}
	b := ShellDef{Name: "b"}
	c := ShellDef{Name: "c"}

	tests := []struct {
		name  string
		in    []ShellDef
		match string
		want  []ShellDef
	}{
		{"already first", []ShellDef{a, b, c}, "a", []ShellDef{a, b, c}},
		{"middle moves to front", []ShellDef{a, b, c}, "b", []ShellDef{b, a, c}},
		{"last moves to front", []ShellDef{a, b, c}, "c", []ShellDef{c, a, b}},
		{"no match is unchanged", []ShellDef{a, b, c}, "z", []ShellDef{a, b, c}},
		{"empty input", nil, "a", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withDefaultFirst(tt.in, tt.match)
			if len(got) != len(tt.want) {
				t.Fatalf("withDefaultFirst(%v, %q) = %v, want %v", tt.in, tt.match, got, tt.want)
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Errorf("withDefaultFirst(%v, %q)[%d] = %q, want %q", tt.in, tt.match, i, got[i].Name, tt.want[i].Name)
				}
			}
		})
	}
}

// TestRegisterSettingsDoesNotTouchOtherNodes confirms RegisterSettings on a
// db that already has unrelated settings (e.g. a Version Control node from
// test.SeedExample-style seeding) only adds its own Terminal node, leaving
// everything else alone — idempotency should be scoped to "Terminal", not
// "the registry has anything in it at all".
func TestRegisterSettingsDoesNotTouchOtherNodes(t *testing.T) {
	d := newTestDB(t)

	vcsID, err := d.AddNode(nil, "Version Control", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(vcsID, "vcs_type", "VCS", db.PropertyEnum, "Git", []string{"Git", "None"}); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if _, ok := findRootNode(nodes, "Version Control"); !ok {
		t.Error("Version Control node was removed/lost")
	}
	if _, ok := findRootNode(nodes, "Terminal"); !ok {
		t.Error("Terminal node was not created")
	}
	if len(nodes) != 2 {
		t.Errorf("len(nodes) = %d, want 2", len(nodes))
	}
}
