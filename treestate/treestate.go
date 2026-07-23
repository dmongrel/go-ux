// Package treestate persists a *widget.Tree's expand/collapse state and
// selected node, live, reusable across any tree-based UI backed by a
// go-ux/db.DB. It is the Fyne-widget-wiring layer on top of db's already-
// generic UI-state blob store (db.SaveUIState/LoadUIState); db itself
// gains no new API and stays free of any Fyne dependency.
package treestate

import (
	"encoding/json"
	"log"
	"sync"

	"fyne.io/fyne/v2/widget"

	"go-ux/db"
)

// Options configures Track.
type Options struct {
	// Exists reports whether uid is still valid in the tree's current
	// data. Required — Restore uses it to filter stale persisted
	// references (a node that existed in a previous session but was since
	// removed) out of the branches it opens and the node it selects.
	Exists func(uid string) bool

	// OnSelected/OnBranchOpened/OnBranchClosed are optional pass-throughs:
	// Track takes over the tree's corresponding callback field, persists
	// first, then calls these (if non-nil) so the caller's own reaction
	// (e.g. rendering a properties pane) still runs — including during
	// Restore's replay, so a caller like settings.Window still renders
	// the restored selection's properties page.
	OnSelected     func(uid string)
	OnBranchOpened func(uid string)
	OnBranchClosed func(uid string)
}

// Tracker is the handle Track returns.
type Tracker struct {
	database *db.DB
	id       string
	tree     *widget.Tree
	opts     Options

	mu        sync.Mutex
	expanded  map[string]bool
	selected  string
	restoring bool
}

// state is the persisted shape, marshaled as JSON into the db's opaque
// UI-state blob — an implementation detail, never exposed.
type state struct {
	Expanded []string
	Selected string
}

// Track wires database-backed persistence for tree's expand/collapse state
// and selection, keyed by id (caller-chosen, unique per tree instance —
// e.g. "settings.tree"). It takes over tree.OnSelected/OnBranchOpened/
// OnBranchClosed. Every toggle/selection writes the entire current state
// to database immediately — no explicit Save call, no debounce.
//
// Call Track once, after tree's data-provider callbacks are set but
// before it's shown; call the returned Tracker's Restore once the tree is
// attached to its window/container.
func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker {
	if opts.Exists == nil {
		// Exists is documented as required, but a nil check here turns a
		// caller's mistake into "treat every persisted UID as valid"
		// (Restore's stale-skip simply never triggers) instead of a
		// nil-pointer panic the first time Restore runs — friendlier for a
		// second, unfamiliar consumer of this package than a crash deep
		// inside Restore's replay loop.
		opts.Exists = func(string) bool { return true }
	}
	t := &Tracker{
		database: database,
		id:       id,
		tree:     tree,
		opts:     opts,
		expanded: make(map[string]bool),
	}

	tree.OnBranchOpened = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		t.expanded[uid] = true
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnBranchOpened != nil {
			opts.OnBranchOpened(uid)
		}
	}
	tree.OnBranchClosed = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		delete(t.expanded, uid)
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnBranchClosed != nil {
			opts.OnBranchClosed(uid)
		}
	}
	tree.OnSelected = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		t.selected = uid
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnSelected != nil {
			opts.OnSelected(uid)
		}
	}

	return t
}

// save writes the tracker's current in-memory state to database under id.
// Errors are logged and swallowed — a UI-state persistence failure must
// never block the tree from working, matching settings.saveUIState's
// existing error-handling convention.
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

// Restore loads the persisted state (if any) and opens every persisted
// branch / selects the persisted node, each filtered through
// Options.Exists — a stale UID is silently skipped, with no fallback
// selection. Call once, after tree is populated and visible.
//
// The replay (OpenBranch/Select) does not itself trigger another persist
// — restoring is held true for its duration, checked by the wrapped
// callbacks above, so this is idempotent to call against an unmodified
// tree.
func (t *Tracker) Restore() {
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
	t.restoring = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.restoring = false
		t.mu.Unlock()
	}()

	for _, uid := range s.Expanded {
		if t.opts.Exists(uid) {
			t.tree.OpenBranch(uid)
		}
	}
	if s.Selected != "" && t.opts.Exists(s.Selected) {
		t.tree.Select(s.Selected)
	}
}
