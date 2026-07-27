// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

// Package sqlite provides the pure-Go SQLite connection and schema used by the db package.
package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// schema is applied via CREATE TABLE IF NOT EXISTS, which is a no-op against
// a database that already has the table — a column added here after a
// database was first created is invisible to it. See additive below, which
// must be kept in sync with every column added to an existing table.
const schema = `
CREATE TABLE IF NOT EXISTS settings_nodes (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	parent_id   INTEGER NULL REFERENCES settings_nodes(id),
	description TEXT NOT NULL,
	sort_order  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS settings_properties (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id      INTEGER NOT NULL REFERENCES settings_nodes(id),
	key          TEXT NOT NULL,
	label        TEXT NOT NULL,
	type         TEXT NOT NULL,
	value        TEXT NOT NULL DEFAULT '',
	enum_options TEXT NOT NULL DEFAULT '',
	capability   TEXT NOT NULL DEFAULT '',
	slider       INTEGER NOT NULL DEFAULT 0,
	slider_min   INTEGER NOT NULL DEFAULT 0,
	slider_max   INTEGER NOT NULL DEFAULT 0,
	UNIQUE(node_id, key)
);

CREATE TABLE IF NOT EXISTS ui_state (
	component_id TEXT PRIMARY KEY,
	blob         BLOB NOT NULL,
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// additive lists every column added to a table after that table first
// shipped, in the order they were introduced. This schema has no version
// table to hang a migration chain off, and every change so far has been
// additive (a new NOT NULL DEFAULT column), so applyAdditive brings an
// existing database up to the current shape directly via ALTER TABLE ADD
// COLUMN rather than tracking a schema version. A real change (a type
// change, a dropped/renamed column) is not additive and does not belong in
// this list — that needs a rebuild-into-a-new-table migration and a
// schema_version table, not an improvised ALTER TABLE here.
var additive = []struct{ table, column, ddl string }{
	{"settings_properties", "enum_options", "TEXT NOT NULL DEFAULT ''"},
	{"settings_properties", "capability", "TEXT NOT NULL DEFAULT ''"},
	{"settings_properties", "slider", "INTEGER NOT NULL DEFAULT 0"},
	{"settings_properties", "slider_min", "INTEGER NOT NULL DEFAULT 0"},
	{"settings_properties", "slider_max", "INTEGER NOT NULL DEFAULT 0"},
}

// applyAdditive brings every table in additive up to its current column set.
// Each column is added only if PRAGMA table_info reports it missing —
// ALTER TABLE ADD COLUMN on a column that already exists is itself an error,
// so this guard is load-bearing, not defensive.
func applyAdditive(conn *sql.DB) error {
	for _, col := range additive {
		rows, err := conn.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, col.table))
		if err != nil {
			return fmt.Errorf("sqlite: inspect %s: %w", col.table, err)
		}
		found := false
		for rows.Next() {
			var (
				cid        int
				name, ctyp string
				notnull    int
				dflt       sql.NullString
				pk         int
			)
			if err := rows.Scan(&cid, &name, &ctyp, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return fmt.Errorf("sqlite: inspect %s: %w", col.table, err)
			}
			if name == col.column {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("sqlite: inspect %s: %w", col.table, err)
		}
		rows.Close()

		if found {
			continue
		}
		stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, col.table, col.column, col.ddl)
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("sqlite: add column %s.%s: %w", col.table, col.column, err)
		}
	}
	return nil
}

// Open opens (creating if necessary) the SQLite database at path and applies the schema.
// path may be ":memory:" for an in-memory database.
func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}

	// database/sql pools connections and can open more than one physical
	// connection to the same DSN. For a ":memory:" database that's fatal —
	// each new connection gets its own separate, empty in-memory database
	// (no shared cache), so the schema created via the Exec below on
	// connection #1 is invisible to a later query that happens to land on
	// connection #2, surfacing as "no such table" — Wails dispatches
	// Service method calls across goroutines, unlike the single
	// UI-goroutine access pattern the original Fyne version mostly saw, so
	// this now actually triggers. Pinning the pool to one connection
	// (also correct for a real file: SQLite itself only supports one
	// writer at a time) makes every query and the migration above share
	// the exact same connection.
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	if err := applyAdditive(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}
