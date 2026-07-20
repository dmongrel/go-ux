// Package db is the general-purpose persistence layer for go-ux components.
// It owns all SQLite access; callers never talk to SQLite directly.
//
// It exposes two independent domains over one SQLite file:
//   - the settings registry (hierarchical nodes + typed properties, staged writes)
//   - per-component UI state (UUID-keyed opaque blobs, live writes)
package db

import (
	"database/sql"
	"fmt"
	"strings"

	"go-ux/internal/sqlite"
)

// PropertyType identifies how a Property's value should be interpreted and rendered.
type PropertyType string

const (
	PropertyBool   PropertyType = "bool"
	PropertyString PropertyType = "string"
	PropertyInt    PropertyType = "int"
	PropertyEnum   PropertyType = "enum"
)

// Node is one entry in the settings tree.
type Node struct {
	ID          int64
	ParentID    *int64
	Description string
	SortOrder   int
}

// Property is one editable field on a Node's properties page.
type Property struct {
	Key         string
	Label       string
	Type        PropertyType
	Value       string
	EnumOptions []string
}

// DB is a handle to the go-ux persistence store.
type DB struct {
	conn *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path. path may be
// ":memory:" for an in-memory database, e.g. in tests.
func Open(path string) (*DB, error) {
	conn, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{conn: conn}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// ListSettings returns every node in the settings tree. Callers assemble the
// tree themselves using Node.ParentID (nil for root nodes).
func (d *DB) ListSettings() ([]Node, error) {
	rows, err := d.conn.Query(`SELECT id, parent_id, description, sort_order FROM settings_nodes ORDER BY sort_order, id`)
	if err != nil {
		return nil, fmt.Errorf("db: list settings: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var parentID sql.NullInt64
		if err := rows.Scan(&n.ID, &parentID, &n.Description, &n.SortOrder); err != nil {
			return nil, fmt.Errorf("db: list settings: %w", err)
		}
		if parentID.Valid {
			id := parentID.Int64
			n.ParentID = &id
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetProperties returns the properties page contents for the given node.
func (d *DB) GetProperties(nodeID int64) ([]Property, error) {
	rows, err := d.conn.Query(`SELECT key, label, type, value, enum_options FROM settings_properties WHERE node_id = ? ORDER BY id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("db: get properties: %w", err)
	}
	defer rows.Close()

	var props []Property
	for rows.Next() {
		var p Property
		var enumOptions string
		var ptype string
		if err := rows.Scan(&p.Key, &p.Label, &ptype, &p.Value, &enumOptions); err != nil {
			return nil, fmt.Errorf("db: get properties: %w", err)
		}
		p.Type = PropertyType(ptype)
		if enumOptions != "" {
			p.EnumOptions = strings.Split(enumOptions, ",")
		}
		props = append(props, p)
	}
	return props, rows.Err()
}

// SaveProperties writes the given key/value pairs for nodeID. Only keys present
// in values are updated.
func (d *DB) SaveProperties(nodeID int64, values map[string]string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("db: save properties: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE settings_properties SET value = ? WHERE node_id = ? AND key = ?`)
	if err != nil {
		return fmt.Errorf("db: save properties: %w", err)
	}
	defer stmt.Close()

	for key, value := range values {
		if _, err := stmt.Exec(value, nodeID, key); err != nil {
			return fmt.Errorf("db: save properties: %w", err)
		}
	}

	return tx.Commit()
}

// AddNode inserts a settings tree node and returns its ID. parentID is nil for
// a root-level node.
func (d *DB) AddNode(parentID *int64, description string, sortOrder int) (int64, error) {
	res, err := d.conn.Exec(`INSERT INTO settings_nodes (parent_id, description, sort_order) VALUES (?, ?, ?)`, parentID, description, sortOrder)
	if err != nil {
		return 0, fmt.Errorf("db: add node: %w", err)
	}
	return res.LastInsertId()
}

// AddProperty inserts a property on the given node.
func (d *DB) AddProperty(nodeID int64, key, label string, ptype PropertyType, value string, enumOptions []string) error {
	_, err := d.conn.Exec(
		`INSERT INTO settings_properties (node_id, key, label, type, value, enum_options) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID, key, label, string(ptype), value, strings.Join(enumOptions, ","),
	)
	if err != nil {
		return fmt.Errorf("db: add property: %w", err)
	}
	return nil
}

// SaveUIState writes the opaque UI-state blob for the given component, live
// (immediately), overwriting any prior state.
func (d *DB) SaveUIState(componentID string, blob []byte) error {
	_, err := d.conn.Exec(
		`INSERT INTO ui_state (component_id, blob, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(component_id) DO UPDATE SET blob = excluded.blob, updated_at = excluded.updated_at`,
		componentID, blob,
	)
	if err != nil {
		return fmt.Errorf("db: save ui state: %w", err)
	}
	return nil
}

// LoadUIState returns the opaque UI-state blob for the given component, or
// (nil, nil) if no state has been saved yet.
func (d *DB) LoadUIState(componentID string) ([]byte, error) {
	var blob []byte
	err := d.conn.QueryRow(`SELECT blob FROM ui_state WHERE component_id = ?`, componentID).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("db: load ui state: %w", err)
	}
	return blob, nil
}
