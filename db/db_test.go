package db_test

import (
	"slices"
	"testing"

	"go-ux/db"
	"go-ux/test"
)

func TestPropertyFloatRoundTrips(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Float Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "ratio", "Ratio", db.PropertyFloat, "1.5", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("GetProperties: got %d properties, want 1", len(props))
	}
	if props[0].Type != db.PropertyFloat {
		t.Errorf("Type = %q, want %q", props[0].Type, db.PropertyFloat)
	}
	if props[0].Value != "1.5" {
		t.Errorf("Value = %q, want %q", props[0].Value, "1.5")
	}
}

func TestSettingsRegistry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("ListSettings: got %d nodes, want 3 (Terminal, Version Control, Git)", len(nodes))
	}

	var terminalID int64
	var gitParent *int64
	for _, n := range nodes {
		switch n.Description {
		case "Terminal":
			terminalID = n.ID
		case "Git":
			gitParent = n.ParentID
		}
	}
	if terminalID == 0 {
		t.Fatal("Terminal node not found")
	}
	if gitParent == nil {
		t.Fatal("Git node should have a parent (Version Control)")
	}

	props, err := d.GetProperties(terminalID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 3 {
		t.Fatalf("GetProperties(Terminal): got %d properties, want 3", len(props))
	}

	if err := d.SaveProperties(terminalID, map[string]string{"tab_width": "8"}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	props, err = d.GetProperties(terminalID)
	if err != nil {
		t.Fatalf("GetProperties after save: %v", err)
	}
	var gotTabWidth string
	for _, p := range props {
		if p.Key == "tab_width" {
			gotTabWidth = p.Value
		}
	}
	if gotTabWidth != "8" {
		t.Fatalf("tab_width = %q, want %q", gotTabWidth, "8")
	}
}

func TestUpdatePropertyOptionsReplacesOptionsNotValue(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Voice Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "voice", "Voice", db.PropertyEnum, "Alice", []string{"Alice", "Bob"}); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	if err := d.UpdatePropertyOptions(nodeID, "voice", []string{"Alice", "Carol", "Dave"}); err != nil {
		t.Fatalf("UpdatePropertyOptions: %v", err)
	}

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("GetProperties: got %d properties, want 1", len(props))
	}
	got := props[0]
	if got.Value != "Alice" {
		t.Errorf("Value = %q, want %q (UpdatePropertyOptions must not touch it)", got.Value, "Alice")
	}
	wantOptions := []string{"Alice", "Carol", "Dave"}
	if !slices.Equal(got.EnumOptions, wantOptions) {
		t.Errorf("EnumOptions = %v, want %v", got.EnumOptions, wantOptions)
	}
}

func TestUpdatePropertyOptionsDoesNotFireOnPropertiesChanged(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Voice Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "voice", "Voice", db.PropertyEnum, "Alice", []string{"Alice"}); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	fired := false
	unsubscribe := d.OnPropertiesChanged(nodeID, func(map[string]string) { fired = true })
	defer unsubscribe()

	if err := d.UpdatePropertyOptions(nodeID, "voice", []string{"Alice", "Bob"}); err != nil {
		t.Fatalf("UpdatePropertyOptions: %v", err)
	}
	if fired {
		t.Error("OnPropertiesChanged fired for UpdatePropertyOptions — it should only fire for SaveProperties")
	}
}

func TestUIState(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	blob, err := d.LoadUIState("component-a")
	if err != nil {
		t.Fatalf("LoadUIState (unset): %v", err)
	}
	if blob != nil {
		t.Fatalf("LoadUIState (unset) = %v, want nil", blob)
	}

	if err := d.SaveUIState("component-a", []byte(`{"width":800,"height":600}`)); err != nil {
		t.Fatalf("SaveUIState: %v", err)
	}

	blob, err = d.LoadUIState("component-a")
	if err != nil {
		t.Fatalf("LoadUIState: %v", err)
	}
	if string(blob) != `{"width":800,"height":600}` {
		t.Fatalf("LoadUIState = %q, want the saved blob", blob)
	}

	if err := d.SaveUIState("component-a", []byte(`{"width":1024,"height":768}`)); err != nil {
		t.Fatalf("SaveUIState (overwrite): %v", err)
	}
	blob, err = d.LoadUIState("component-a")
	if err != nil {
		t.Fatalf("LoadUIState after overwrite: %v", err)
	}
	if string(blob) != `{"width":1024,"height":768}` {
		t.Fatalf("LoadUIState after overwrite = %q, want the updated blob", blob)
	}
}

func TestOnPropertiesChangedFiresAfterSaveProperties(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Notify Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "size", "Size", db.PropertyInt, "10", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	var got map[string]string
	unsubscribe := d.OnPropertiesChanged(nodeID, func(values map[string]string) {
		got = values
	})
	defer unsubscribe()

	if err := d.SaveProperties(nodeID, map[string]string{"size": "20"}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}
	if got == nil {
		t.Fatal("OnPropertiesChanged callback did not fire")
	}
	if got["size"] != "20" {
		t.Errorf("callback values[\"size\"] = %q, want %q", got["size"], "20")
	}

	unsubscribe()
	got = nil
	if err := d.SaveProperties(nodeID, map[string]string{"size": "30"}); err != nil {
		t.Fatalf("SaveProperties (after unsubscribe): %v", err)
	}
	if got != nil {
		t.Error("callback fired after unsubscribe")
	}
}

func TestOnPropertiesChangedOnlyFiresForItsOwnNode(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeA, err := d.AddNode(nil, "A", 0)
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeB, err := d.AddNode(nil, "B", 0)
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	if err := d.AddProperty(nodeA, "k", "K", db.PropertyString, "v", nil); err != nil {
		t.Fatalf("AddProperty A: %v", err)
	}
	if err := d.AddProperty(nodeB, "k", "K", db.PropertyString, "v", nil); err != nil {
		t.Fatalf("AddProperty B: %v", err)
	}

	fired := false
	unsubscribe := d.OnPropertiesChanged(nodeA, func(map[string]string) { fired = true })
	defer unsubscribe()

	if err := d.SaveProperties(nodeB, map[string]string{"k": "changed"}); err != nil {
		t.Fatalf("SaveProperties(nodeB): %v", err)
	}
	if fired {
		t.Error("nodeA's callback fired for a write to nodeB")
	}
}

