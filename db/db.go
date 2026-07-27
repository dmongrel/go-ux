// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

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
	// PropertyReadOnly renders as a plain label:value pair — Label is the
	// identifier, Value is the (non-editable) content. Not stageable.
	PropertyReadOnly PropertyType = "readonly"
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
	// Capability is an optional trailing label rendered after the value
	// control — for informing the user of a constraint on the value (e.g.
	// "min 1, max 72") rather than encoding it in Label or Value. Empty
	// means no trailing label is shown.
	Capability string
	// Slider renders a PropertyInt with a slider spanning SliderMin..
	// SliderMax alongside its usual number input — the user can still type
	// an exact value, and the slider repositions to match. Set via
	// SetPropertySlider; ignored for every PropertyType other than
	// PropertyInt.
	Slider               bool
	SliderMin, SliderMax int
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
	rows, err := d.conn.Query(`SELECT key, label, type, value, enum_options, capability, slider, slider_min, slider_max FROM settings_properties WHERE node_id = ? ORDER BY id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("db: get properties: %w", err)
	}
	defer rows.Close()

	var props []Property
	for rows.Next() {
		var p Property
		var enumOptions string
		var ptype string
		var slider int
		if err := rows.Scan(&p.Key, &p.Label, &ptype, &p.Value, &enumOptions, &p.Capability, &slider, &p.SliderMin, &p.SliderMax); err != nil {
			return nil, fmt.Errorf("db: get properties: %w", err)
		}
		p.Type = PropertyType(ptype)
		p.Slider = slider != 0
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

// RenameNode changes a node's description. The node's ID and its properties
// are untouched, so a label derived from data that can change (a file's own
// metadata, a device name, a project title) can be corrected on a later
// launch without the user losing anything stored under it.
//
// Renaming a node that does not exist is not an error, so this is safe to
// call unconditionally on every launch.
//
// Like RemoveProperty and UpdatePropertyOptions, it does not fire
// OnPropertiesChanged: this is a definitional change (what the tree says),
// not a user-edited value. It also does not repaint a settings window that
// is already open — ListNodes is only re-read on mount.
func (d *DB) RenameNode(nodeID int64, description string) error {
	_, err := d.conn.Exec(
		`UPDATE settings_nodes SET description = ? WHERE id = ?`,
		description, nodeID,
	)
	if err != nil {
		return fmt.Errorf("db: rename node: %w", err)
	}
	return nil
}

// RemoveNode deletes a node, every node beneath it, and all of their
// properties, for a settings group an app has retired. Seeding is one-time
// (callers guard it on the registry being empty), so dropping a group from
// the seed only ever affects fresh databases — every existing one keeps the
// node, and the settings window keeps rendering the group, until something
// deletes it.
//
// The delete is recursive and transactional: a group and its children go
// together or not at all, because a node whose parent has been removed is
// unreachable rather than merely untidy.
//
// Removing a node that isn't there is not an error, so this is safe to call
// unconditionally on every launch.
//
// Like RemoveProperty and UpdatePropertyOptions, it does not fire
// OnPropertiesChanged: this is a definitional change (which settings
// exist), not a user-edited value.
//
// It does not touch ui_state. The settings tree's expand/collapse state is
// stored as one row keyed by the whole tree instance's own ID (see
// settings.md), with individual node IDs living inside that row's JSON
// blob, not as ui_state.component_id values — there is no per-node row to
// delete. A removed node's ID lingering inside that blob is harmless:
// settings.Service already filters expand/selection state against its
// current node list on every read (InitialTreeState), so a stale ID is
// silently dropped rather than surfaced.
func (d *DB) RemoveNode(nodeID int64) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("db: remove node: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		WITH RECURSIVE subtree(id) AS (
			SELECT id FROM settings_nodes WHERE id = ?
			UNION ALL
			SELECT n.id FROM settings_nodes n JOIN subtree s ON n.parent_id = s.id
		)
		SELECT id FROM subtree`, nodeID)
	if err != nil {
		return fmt.Errorf("db: remove node: %w", err)
	}
	var ids []any
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("db: remove node: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("db: remove node: %w", err)
	}
	rows.Close()

	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM settings_properties WHERE node_id IN (%s)`, placeholders), ids...); err != nil {
		return fmt.Errorf("db: remove node: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM settings_nodes WHERE id IN (%s)`, placeholders), ids...); err != nil {
		return fmt.Errorf("db: remove node: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: remove node: %w", err)
	}
	return nil
}

// AddProperty inserts a property on the given node. capability is optional
// (at most one value is used) — see Property.Capability.
func (d *DB) AddProperty(nodeID int64, key, label string, ptype PropertyType, value string, enumOptions []string, capability ...string) error {
	var capValue string
	if len(capability) > 0 {
		capValue = capability[0]
	}
	_, err := d.conn.Exec(
		`INSERT INTO settings_properties (node_id, key, label, type, value, enum_options, capability) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nodeID, key, label, string(ptype), value, strings.Join(enumOptions, ","), capValue,
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

// SetPropertySlider marks an existing PropertyInt property as
// slider-enabled, rendering a slider spanning min..max (inclusive)
// alongside its existing number input — the user can still type an exact
// value, and the slider repositions to match. Pass min == max == 0 to use
// the default 0..100 range. Has no effect on any PropertyType other than
// PropertyInt.
//
// Like UpdatePropertyOptions and RenameNode, it does not fire
// OnPropertiesChanged: this is a definitional change (how the control is
// rendered), not a user-edited value.
func (d *DB) SetPropertySlider(nodeID int64, key string, min, max int) error {
	if min == 0 && max == 0 {
		min, max = 0, 100
	}
	_, err := d.conn.Exec(
		`UPDATE settings_properties SET slider = 1, slider_min = ?, slider_max = ? WHERE node_id = ? AND key = ?`,
		min, max, nodeID, key,
	)
	if err != nil {
		return fmt.Errorf("db: set property slider: %w", err)
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
