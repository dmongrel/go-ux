package db_test

import (
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
