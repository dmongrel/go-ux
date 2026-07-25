// Package sqlite provides the pure-Go SQLite connection and schema used by the db package.
package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

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
	UNIQUE(node_id, key)
);

CREATE TABLE IF NOT EXISTS ui_state (
	component_id TEXT PRIMARY KEY,
	blob         BLOB NOT NULL,
	updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS editors_panes (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id     TEXT NOT NULL,
	parent_pane  INTEGER NULL REFERENCES editors_panes(id),
	is_pane      INTEGER NOT NULL,
	axis         TEXT NOT NULL DEFAULT '',
	split_offset REAL NOT NULL DEFAULT 0.5,
	sort_order   INTEGER NOT NULL DEFAULT 0,
	is_primary   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS editors_tabs (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id   TEXT NOT NULL,
	pane_id    INTEGER NOT NULL REFERENCES editors_panes(id),
	file_path  TEXT NOT NULL,
	tab_order  INTEGER NOT NULL DEFAULT 0,
	is_active  INTEGER NOT NULL DEFAULT 0
);
`

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

	return conn, nil
}
