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

	"go-ux/internal/sqlite"
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

// EditorPane is one node in an editors instance's split-layout tree — a
// leaf (IsPane true, a real editor pane) or an internal split node
// (IsPane false, Axis/SplitOffset meaningful, ParentPane's two children
// distinguished by SortOrder). See go-ux/editors for how this tree maps
// onto Fyne's binary container.Split.
type EditorPane struct {
	ID          int64
	GroupID     string
	ParentPane  *int64
	IsPane      bool
	Axis        string // "h" or "v", meaningful iff !IsPane
	SplitOffset float64
	SortOrder   int
	IsPrimary   bool
}

// EditorTab is one open file in one EditorPane.
type EditorTab struct {
	ID       int64
	GroupID  string
	PaneID   int64
	FilePath string
	TabOrder int
	IsActive bool
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

// SaveEditorLayout replaces the entire persisted layout for groupID with
// panes and tabs, in one transaction (delete-then-reinsert) — matching
// this package's other live-write persistence (SaveUIState) rather than
// incremental row updates: editors instances have at most a handful of
// panes/tabs, so whole-state replacement is simpler and cheap, and a
// pane/tab no longer present simply isn't reinserted (no separate cleanup
// step needed).
//
// panes must be ordered so that any row referencing a ParentPane appears
// after that parent — SaveEditorLayout assigns fresh IDs on insert (SQLite
// AUTOINCREMENT) and rewrites each pane's ParentPane (and each tab's
// PaneID) to the newly assigned ID as it goes, using its position in the
// panes slice to resolve the old EditorPane.ID values referenced by
// ParentPane/PaneID in the input to the corresponding freshly-inserted
// row.
func (d *DB) SaveEditorLayout(groupID string, panes []EditorPane, tabs []EditorTab) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("db: save editor layout: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM editors_tabs WHERE group_id = ?`, groupID); err != nil {
		return fmt.Errorf("db: save editor layout: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM editors_panes WHERE group_id = ?`, groupID); err != nil {
		return fmt.Errorf("db: save editor layout: %w", err)
	}

	// oldToNewPaneID maps each input EditorPane's original ID (as given by
	// the caller, e.g. 0 or an arbitrary placeholder for panes the caller
	// hasn't persisted before) to the ID SQLite just assigned it, so later
	// panes' ParentPane and tabs' PaneID (which reference those same
	// caller-side IDs) can be rewritten to the real new IDs.
	oldToNewPaneID := make(map[int64]int64, len(panes))

	paneStmt, err := tx.Prepare(`INSERT INTO editors_panes (group_id, parent_pane, is_pane, axis, split_offset, sort_order, is_primary) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("db: save editor layout: %w", err)
	}
	defer paneStmt.Close()

	for _, p := range panes {
		var parentPane any
		if p.ParentPane != nil {
			newParent, ok := oldToNewPaneID[*p.ParentPane]
			if !ok {
				return fmt.Errorf("db: save editor layout: pane %d references parent %d that was not inserted first", p.ID, *p.ParentPane)
			}
			parentPane = newParent
		}
		res, err := paneStmt.Exec(groupID, parentPane, p.IsPane, p.Axis, p.SplitOffset, p.SortOrder, p.IsPrimary)
		if err != nil {
			return fmt.Errorf("db: save editor layout: %w", err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("db: save editor layout: %w", err)
		}
		oldToNewPaneID[p.ID] = newID
	}

	tabStmt, err := tx.Prepare(`INSERT INTO editors_tabs (group_id, pane_id, file_path, tab_order, is_active) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("db: save editor layout: %w", err)
	}
	defer tabStmt.Close()

	for _, t := range tabs {
		newPaneID, ok := oldToNewPaneID[t.PaneID]
		if !ok {
			return fmt.Errorf("db: save editor layout: tab references pane %d that was not inserted", t.PaneID)
		}
		if _, err := tabStmt.Exec(groupID, newPaneID, t.FilePath, t.TabOrder, t.IsActive); err != nil {
			return fmt.Errorf("db: save editor layout: %w", err)
		}
	}

	return tx.Commit()
}

// LoadEditorLayout returns the persisted layout for groupID, or (nil, nil,
// nil) if nothing has been saved yet for it.
func (d *DB) LoadEditorLayout(groupID string) (panes []EditorPane, tabs []EditorTab, err error) {
	paneRows, err := d.conn.Query(`SELECT id, parent_pane, is_pane, axis, split_offset, sort_order, is_primary FROM editors_panes WHERE group_id = ? ORDER BY id`, groupID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: load editor layout: %w", err)
	}
	defer paneRows.Close()

	for paneRows.Next() {
		var p EditorPane
		var parentPane sql.NullInt64
		if err := paneRows.Scan(&p.ID, &parentPane, &p.IsPane, &p.Axis, &p.SplitOffset, &p.SortOrder, &p.IsPrimary); err != nil {
			return nil, nil, fmt.Errorf("db: load editor layout: %w", err)
		}
		p.GroupID = groupID
		if parentPane.Valid {
			id := parentPane.Int64
			p.ParentPane = &id
		}
		panes = append(panes, p)
	}
	if err := paneRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("db: load editor layout: %w", err)
	}

	tabRows, err := d.conn.Query(`SELECT id, pane_id, file_path, tab_order, is_active FROM editors_tabs WHERE group_id = ? ORDER BY id`, groupID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: load editor layout: %w", err)
	}
	defer tabRows.Close()

	for tabRows.Next() {
		var t EditorTab
		if err := tabRows.Scan(&t.ID, &t.PaneID, &t.FilePath, &t.TabOrder, &t.IsActive); err != nil {
			return nil, nil, fmt.Errorf("db: load editor layout: %w", err)
		}
		t.GroupID = groupID
		tabs = append(tabs, t)
	}
	return panes, tabs, tabRows.Err()
}
