// Package treestate persists a tree UI's expand/collapse state and
// selected node, live, reusable across any tree-based component backed by
// a go-ux/db.DB. It is the persistence-wiring layer on top of db's already-
// generic UI-state blob store (db.SaveUIState/LoadUIState); db itself
// gains no new API and stays free of any UI-toolkit dependency.
//
// Unlike the original Fyne version (which took over a live *widget.Tree's
// callback fields), a Wails tree lives entirely in the frontend — there is
// no live Go-side widget to hang callbacks off. Tracker is instead a plain
// request/response state store: the frontend calls SetExpanded/SetSelected
// on every toggle/selection, and reads Expanded/Selected back (typically
// once, when a tree first mounts) to replay the persisted state itself.
// Filtering out UIDs that no longer exist in the tree's current data is
// the caller's responsibility (e.g. go-ux/settings.Service, which already
// has the current node list) — Tracker has no notion of tree shape.
package treestate

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/dmongrel/go-ux/db"
)

// Tracker is a database-backed store for one tree instance's expand/
// collapse state and selection, keyed by id (caller-chosen, unique per
// tree instance — e.g. "settings.tree"). Every SetExpanded/SetSelected
// call writes the entire current state immediately — no explicit Save
// call, no debounce.
type Tracker struct {
	database *db.DB
	id       string

	mu       sync.Mutex
	expanded map[string]bool
	selected string
}

// state is the persisted shape, marshaled as JSON into the db's opaque
// UI-state blob — an implementation detail, never exposed.
type state struct {
	Expanded []string
	Selected string
}

// New returns a Tracker for id, loading any previously persisted state.
func New(database *db.DB, id string) *Tracker {
	t := &Tracker{database: database, id: id, expanded: make(map[string]bool)}
	t.load()
	return t
}

// Expanded returns every currently expanded node UID, in no particular
// order.
func (t *Tracker) Expanded() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.expanded))
	for uid := range t.expanded {
		out = append(out, uid)
	}
	return out
}

// Selected returns the currently selected node UID, or "" if none.
func (t *Tracker) Selected() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.selected
}

// SetExpanded records uid's expand/collapse state and persists it.
func (t *Tracker) SetExpanded(uid string, expanded bool) {
	t.mu.Lock()
	if expanded {
		t.expanded[uid] = true
	} else {
		delete(t.expanded, uid)
	}
	t.mu.Unlock()
	t.save()
}

// SetSelected records the selected node and persists it.
func (t *Tracker) SetSelected(uid string) {
	t.mu.Lock()
	t.selected = uid
	t.mu.Unlock()
	t.save()
}

// save writes the tracker's current in-memory state to database under id.
// Errors are logged and swallowed — a UI-state persistence failure must
// never block the tree from working, matching this package's original
// error-handling convention.
func (t *Tracker) save() {
	t.mu.Lock()
	expanded := make([]string, 0, len(t.expanded))
	for uid := range t.expanded {
		expanded = append(expanded, uid)
	}
	selected := t.selected
	t.mu.Unlock()

	blob, err := json.Marshal(state{Expanded: expanded, Selected: selected})
	if err != nil {
		log.Printf("treestate: marshal state: %v", err)
		return
	}
	if err := t.database.SaveUIState(t.id, blob); err != nil {
		log.Printf("treestate: save state: %v", err)
	}
}

// load reads any previously persisted state into memory. Errors are
// logged and swallowed, leaving the Tracker at its zero state — matching
// save's own best-effort persistence convention.
func (t *Tracker) load() {
	blob, err := t.database.LoadUIState(t.id)
	if err != nil {
		log.Printf("treestate: load state: %v", err)
		return
	}
	if blob == nil {
		return
	}

	var s state
	if err := json.Unmarshal(blob, &s); err != nil {
		log.Printf("treestate: unmarshal state: %v", err)
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, uid := range s.Expanded {
		t.expanded[uid] = true
	}
	t.selected = s.Selected
}
