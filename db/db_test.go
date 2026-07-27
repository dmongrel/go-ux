// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package db_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/dmongrel/go-ux/db"
	"github.com/dmongrel/go-ux/test"
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

func TestPropertyReadOnlyRoundTrips(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "ReadOnly Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "build_id", "Build ID", db.PropertyReadOnly, "abc123", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("GetProperties: got %d properties, want 1", len(props))
	}
	if props[0].Type != db.PropertyReadOnly {
		t.Errorf("Type = %q, want %q", props[0].Type, db.PropertyReadOnly)
	}
	if props[0].Value != "abc123" {
		t.Errorf("Value = %q, want %q", props[0].Value, "abc123")
	}
}

func TestPropertyCapabilityRoundTrips(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Capability Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "port", "Port", db.PropertyInt, "8080", nil, "min 1, max 65535"); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}
	if err := d.AddProperty(nodeID, "name", "Name", db.PropertyString, "svc", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("GetProperties: got %d properties, want 2", len(props))
	}
	if props[0].Capability != "min 1, max 65535" {
		t.Errorf("Capability = %q, want %q", props[0].Capability, "min 1, max 65535")
	}
	if props[1].Capability != "" {
		t.Errorf("Capability = %q, want empty", props[1].Capability)
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

func TestRenameNodeChangesDescriptionOnly(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Old Label", 3)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	if err := d.RenameNode(nodeID, "New Label"); err != nil {
		t.Fatalf("RenameNode: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var got *db.Node
	for i := range nodes {
		if nodes[i].ID == nodeID {
			got = &nodes[i]
		}
	}
	if got == nil {
		t.Fatal("renamed node not found in ListSettings")
	}
	if got.Description != "New Label" {
		t.Errorf("Description = %q, want %q", got.Description, "New Label")
	}
	if got.ID != nodeID {
		t.Errorf("ID = %d, want %d", got.ID, nodeID)
	}
	if got.ParentID != nil {
		t.Errorf("ParentID = %v, want nil", got.ParentID)
	}
	if got.SortOrder != 3 {
		t.Errorf("SortOrder = %d, want 3", got.SortOrder)
	}
}

func TestRenameNodePreservesProperties(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Model", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "info_id", "Info ID", db.PropertyReadOnly, "abc-123", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}
	if err := d.AddProperty(nodeID, "quant", "Quantisation", db.PropertyEnum, "Q4_K_S", []string{"Q4_K_S", "Q8_0"}, "affects file size"); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	before, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties before rename: %v", err)
	}

	if err := d.RenameNode(nodeID, "Model (renamed)"); err != nil {
		t.Fatalf("RenameNode: %v", err)
	}

	after, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties after rename: %v", err)
	}

	if !reflect.DeepEqual(before, after) {
		t.Errorf("properties changed by rename:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

func TestRenameNodeDoesNotAffectSiblingsOrChildren(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	parentID, err := d.AddNode(nil, "Parent", 0)
	if err != nil {
		t.Fatalf("AddNode(parent): %v", err)
	}
	childID, err := d.AddNode(&parentID, "Child", 0)
	if err != nil {
		t.Fatalf("AddNode(child): %v", err)
	}
	siblingID, err := d.AddNode(nil, "Sibling", 1)
	if err != nil {
		t.Fatalf("AddNode(sibling): %v", err)
	}

	if err := d.RenameNode(childID, "Child (renamed)"); err != nil {
		t.Fatalf("RenameNode: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	byID := make(map[int64]db.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if byID[parentID].Description != "Parent" {
		t.Errorf("parent Description = %q, want %q", byID[parentID].Description, "Parent")
	}
	if byID[siblingID].Description != "Sibling" {
		t.Errorf("sibling Description = %q, want %q", byID[siblingID].Description, "Sibling")
	}
	if byID[childID].Description != "Child (renamed)" {
		t.Errorf("child Description = %q, want %q", byID[childID].Description, "Child (renamed)")
	}
}

func TestRenameNodeNonexistentIDIsNotAnError(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := d.RenameNode(999999, "Doesn't Matter"); err != nil {
		t.Errorf("RenameNode on nonexistent ID: got %v, want nil", err)
	}
}

func TestRenameNodeAllowsDuplicateDescription(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeA, err := d.AddNode(nil, "Q4_K_S", 0)
	if err != nil {
		t.Fatalf("AddNode(A): %v", err)
	}
	nodeB, err := d.AddNode(nil, "Q8_0", 1)
	if err != nil {
		t.Fatalf("AddNode(B): %v", err)
	}

	if err := d.RenameNode(nodeB, "Q4_K_S"); err != nil {
		t.Fatalf("RenameNode: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var count int
	for _, n := range nodes {
		if n.Description == "Q4_K_S" && (n.ID == nodeA || n.ID == nodeB) {
			count++
		}
	}
	if count != 2 {
		t.Errorf("got %d nodes named %q, want 2", count, "Q4_K_S")
	}
}

// TestGetPropertiesSucceedsOnLegacyDatabaseMissingCapabilityColumn reproduces
// the reported failure: a database file created before the `capability`
// column existed used to make every GetProperties call fail with "no such
// column: capability", because CREATE TABLE IF NOT EXISTS is a no-op against
// a table that already exists. db.Open (via internal/sqlite) must bring such
// a file up to the current schema before returning.
func TestGetPropertiesSucceedsOnLegacyDatabaseMissingCapabilityColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE settings_nodes (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id   INTEGER NULL REFERENCES settings_nodes(id),
			description TEXT NOT NULL,
			sort_order  INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE settings_properties (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id      INTEGER NOT NULL REFERENCES settings_nodes(id),
			key          TEXT NOT NULL,
			label        TEXT NOT NULL,
			type         TEXT NOT NULL,
			value        TEXT NOT NULL DEFAULT '',
			enum_options TEXT NOT NULL DEFAULT '',
			UNIQUE(node_id, key)
		);
	`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO settings_nodes (id, parent_id, description, sort_order) VALUES (1, NULL, 'Server', 0)`); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO settings_properties (node_id, key, label, type, value, enum_options) VALUES (1, 'port', 'Port', 'int', '8080', '')`); err != nil {
		t.Fatalf("seed property: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer d.Close()

	props, err := d.GetProperties(1)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("GetProperties: got %d properties, want 1", len(props))
	}
	if props[0].Key != "port" || props[0].Label != "Port" || props[0].Type != db.PropertyInt || props[0].Value != "8080" {
		t.Errorf("got %+v, want key=port label=Port type=int value=8080", props[0])
	}
	if props[0].Capability != "" {
		t.Errorf("Capability = %q, want empty", props[0].Capability)
	}
}


