// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func columnNames(t *testing.T, conn *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name, ctyp string
			notnull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	return cols
}

func TestOpenFreshDatabaseHasFullSchema(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "fresh.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	cols := columnNames(t, conn, "settings_properties")
	for _, want := range []string{"enum_options", "capability", "slider", "slider_min", "slider_max"} {
		if !cols[want] {
			t.Errorf("settings_properties missing column %q", want)
		}
	}
}

func TestOpenTwiceOnCurrentDatabaseIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.sqlite")

	conn1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := conn1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// ALTER TABLE ADD COLUMN on a column that already exists is itself a
	// SQLite error, so a second Open succeeding proves the PRAGMA
	// table_info guard actually works, not just that the happy path does.
	conn2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer conn2.Close()

	cols := columnNames(t, conn2, "settings_properties")
	if !cols["capability"] {
		t.Error(`settings_properties missing column "capability" after reopen`)
	}
}

func TestOpenAddsMissingColumnToLegacyDatabase(t *testing.T) {
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
	if _, err := raw.Exec(`INSERT INTO settings_properties (node_id, key, label, type, value, enum_options) VALUES (1, 'port', 'Port', 'int', '8080', '')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer conn.Close()

	cols := columnNames(t, conn, "settings_properties")
	if !cols["capability"] {
		t.Fatal(`settings_properties missing column "capability" after migration`)
	}

	var key, label, ptype, value, capability string
	if err := conn.QueryRow(`SELECT key, label, type, value, capability FROM settings_properties WHERE node_id = 1`).
		Scan(&key, &label, &ptype, &value, &capability); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if key != "port" || label != "Port" || ptype != "int" || value != "8080" {
		t.Errorf("row = %q/%q/%q/%q, want %q/%q/%q/%q", key, label, ptype, value, "port", "Port", "int", "8080")
	}
	if capability != "" {
		t.Errorf("capability = %q, want empty", capability)
	}
}

func TestOpenAddsSliderColumnsToDatabasePredatingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presliders.sqlite")

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
			capability   TEXT NOT NULL DEFAULT '',
			UNIQUE(node_id, key)
		);
	`); err != nil {
		t.Fatalf("create v0.1.5-shaped schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO settings_properties (node_id, key, label, type, value, enum_options, capability) VALUES (1, 'port', 'Port', 'int', '8080', '', '')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	cols := columnNames(t, conn, "settings_properties")
	for _, want := range []string{"slider", "slider_min", "slider_max"} {
		if !cols[want] {
			t.Fatalf("settings_properties missing column %q after migration", want)
		}
	}

	var value string
	var slider, sliderMin, sliderMax int
	if err := conn.QueryRow(`SELECT value, slider, slider_min, slider_max FROM settings_properties WHERE node_id = 1`).
		Scan(&value, &slider, &sliderMin, &sliderMax); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if value != "8080" {
		t.Errorf("value = %q, want %q", value, "8080")
	}
	if slider != 0 || sliderMin != 0 || sliderMax != 0 {
		t.Errorf("slider/slider_min/slider_max = %d/%d/%d, want 0/0/0", slider, sliderMin, sliderMax)
	}
}
