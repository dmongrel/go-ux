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
	"sync"

	"github.com/dmongrel/go-ux/internal/sqlite"
)

// PropertyType identifies how a Property's value should be interpreted and rendered.
type PropertyType string

const (
	PropertyBool   PropertyType = "bool"
	PropertyString PropertyType = "string"
	PropertyInt    PropertyType = "int"
	PropertyEnum   PropertyType = "enum"
	PropertyFloat  PropertyType = "float"
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

	mu             sync.Mutex
	nextListenerID int
	// listeners is keyed by nodeID, then by a per-subscription id (assigned
	// by OnPropertiesChanged) — a map rather than a slice-with-holes so
	// Unsubscribe actually reclaims memory instead of leaving a permanent
	// nil hole behind for every unsubscribed listener.
	listeners map[int64]map[int]func(values map[string]string)
}

// Open opens (creating if necessary) the SQLite database at path. path may be
// ":memory:" for an in-memory database, e.g. in tests.
func Open(path string) (*DB, error) {
	conn, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{conn: conn, listeners: make(map[int64]map[int]func(values map[string]string))}, nil
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

	if err := tx.Commit(); err != nil {
		return err
	}

	d.notifyPropertiesChanged(nodeID, values)
	return nil
}

// OnPropertiesChanged registers fn to be called, synchronously and on
// whatever goroutine calls it, after every successful SaveProperties(nodeID,
// ...) — with the same values map that was passed to it. Returns an
// unsubscribe function; safe to call OnPropertiesChanged and the returned
// function from any goroutine.
//
// This exists so a UI displaying nodeID's properties (go-ux/settings.Window)
// can react to a write it didn't itself make — e.g. go-ux/terminal writing a
// live Ctrl+scroll font-size change directly to the db while a Settings
// window happens to be open on the same node.
func (d *DB) OnPropertiesChanged(nodeID int64, fn func(values map[string]string)) (unsubscribe func()) {
	d.mu.Lock()
	id := d.nextListenerID
	d.nextListenerID++
	if d.listeners[nodeID] == nil {
		d.listeners[nodeID] = make(map[int]func(values map[string]string))
	}
	d.listeners[nodeID][id] = fn
	d.mu.Unlock()

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.listeners[nodeID], id)
		if len(d.listeners[nodeID]) == 0 {
			delete(d.listeners, nodeID)
		}
	}
}

func (d *DB) notifyPropertiesChanged(nodeID int64, values map[string]string) {
	d.mu.Lock()
	fns := make([]func(map[string]string), 0, len(d.listeners[nodeID]))
	for _, fn := range d.listeners[nodeID] {
		fns = append(fns, fn)
	}
	d.mu.Unlock()

	for _, fn := range fns {
		fn(values)
	}
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

// RemoveProperty deletes a property from a node, for a setting an app has
// retired. Seeding is one-time (callers guard it on the registry being
// empty), so dropping a property from the seed only ever affects fresh
// databases — every existing one keeps the row, and the settings window keeps
// rendering the control, until something deletes it. Removing a key that
// isn't there is not an error, so this is safe to call unconditionally on
// every launch.
//
// Like UpdatePropertyOptions, it does not fire OnPropertiesChanged: this is a
// definitional change (which settings exist), not a user-edited value.
func (d *DB) RemoveProperty(nodeID int64, key string) error {
	_, err := d.conn.Exec(
		`DELETE FROM settings_properties WHERE node_id = ? AND key = ?`,
		nodeID, key,
	)
	if err != nil {
		return fmt.Errorf("db: remove property: %w", err)
	}
	return nil
}

// UpdatePropertyOptions replaces a property's EnumOptions without touching
// its stored value — for properties whose valid choices can change between
// launches (e.g. an OS voice/font list), unlike SaveProperties which only
// ever updates value. Unlike SaveProperties, this does not fire
// OnPropertiesChanged: it's a definitional change (what choices exist), not
// a user-edited value.
func (d *DB) UpdatePropertyOptions(nodeID int64, key string, enumOptions []string) error {
	_, err := d.conn.Exec(
		`UPDATE settings_properties SET enum_options = ? WHERE node_id = ? AND key = ?`,
		strings.Join(enumOptions, ","), nodeID, key,
	)
	if err != nil {
		return fmt.Errorf("db: update property options: %w", err)
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

