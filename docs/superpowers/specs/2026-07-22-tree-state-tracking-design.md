# Reusable tree-state tracking — design spec

## Scope

Add live, persisted tracking of a `*widget.Tree`'s expand/collapse state and
selected node, reusable across any tree-based UI in this module — not just
`go-ux/settings`'s properties tree. Wire it into `settings.Window` as the
first (and currently only) consumer.

Explicitly out of scope: `terminal.Window` window-size/position persistence
(terminal's primary use case is embedding in a host app window, which owns
sizing — see project memory `project_terminal_embedding`). Also out of
scope: changing `settings.Window`'s existing window-size/splitter
persistence (`uiState`/`saveUIState`/`restoreUIState`) — it stays exactly as
it is today, just no longer also carrying tree state.

## New package: `go-ux/treestate`

A small package, sibling to `db`/`dialog`/`settings`/`terminal`. It depends
on `fyne.io/fyne/v2/widget` (for `*widget.Tree`) and `go-ux/db` (for the
existing generic `SaveUIState`/`LoadUIState` blob store) — nothing else.

`db.SaveUIState(componentID string, blob []byte) error` and
`LoadUIState(componentID string) ([]byte, error)` are already fully generic
(arbitrary opaque blob keyed by an arbitrary string ID) — this package adds
the missing Fyne-widget-wiring layer on top, so a consumer doesn't have to
hand-roll tree-callback-to-blob plumbing itself. `db` itself gains no new
API and no new dependency (it has zero Fyne imports today and should stay
that way).

### Public API

```go
package treestate

// Options configures Track. Exists is required; the three On* fields are
// optional pass-throughs for the caller's own tree-callback logic.
type Options struct {
	// Exists reports whether uid is still valid in the tree's current data.
	// Track uses this to filter stale persisted references (e.g. a node
	// that existed last session but was since deleted) out of Restore.
	Exists func(uid string) bool

	// OnSelected/OnBranchOpened/OnBranchClosed are optional pass-throughs:
	// Track takes over the tree's corresponding callback field, persists
	// first, then calls these (if non-nil) so the caller's own reaction
	// (e.g. rendering a properties pane) still runs.
	OnSelected     func(uid string)
	OnBranchOpened func(uid string)
	OnBranchClosed func(uid string)
}

// Tracker is the handle Track returns; its only method is Restore.
type Tracker struct {
	// unexported
}

// Track wires database-backed persistence for tree's expand/collapse state
// and selection, keyed by id (caller-chosen, unique per tree instance —
// e.g. "settings.tree"). It takes over tree.OnSelected/OnBranchOpened/
// OnBranchClosed (see Options). Every toggle/selection writes immediately
// — no explicit Save call, no debounce.
//
// Call Track once, after the tree is built (its data-provider callbacks
// set) but before the tree is shown, then call Restore once the tree is
// attached to its window/container.
func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker

// Restore opens every persisted branch and selects the persisted node,
// each filtered through Options.Exists (stale UIDs are silently skipped —
// no fallback selection). Call once, after the tree is populated and
// visible. Calls made here do not themselves trigger further persistence
// (see "Restore does not re-save" below).
func (t *Tracker) Restore()
```

### Persisted shape (internal, not exported)

```go
type treeState struct {
	Expanded []string // node UIDs currently open, from tree.OnBranchOpened/Closed
	Selected string   // last-selected node UID, from tree.OnSelected
}
```

Marshaled as JSON, same pattern `settings.uiState` already uses. `Expanded`
tracks the *open* set (not collapsed) since `widget.Tree` defaults to fully
collapsed — the persisted list is empty on a tree's first-ever use, and only
grows as the user actually opens branches.

### Behavior details

- **Live persistence, whole-state writes.** Every `OnBranchOpened`/
  `OnBranchClosed`/`OnSelected` event calls `database.SaveUIState(id, ...)`
  with the *entire* current `treeState` (expanded set + selection), not an
  incremental delta — matches how the blob store works (one write replaces
  the whole blob) and matches `settings.saveUIState`'s existing all-at-once
  pattern.
- **Search-driven expansion counts.** `settings.Window`'s search feature
  already calls `tree.OpenAllBranches()` to reveal matches; since `Track`
  owns `OnBranchOpened`, that also persists (per your "persist whatever's
  expanded, regardless of cause" answer) — no special-casing needed.
- **Restore does not re-save.** `Restore` calls `tree.OpenBranch`/
  `tree.Select` internally, which would otherwise re-trigger `Track`'s own
  `OnBranchOpened`/`OnSelected` handlers and immediately write back the
  state that was just loaded. `Tracker` holds an internal `restoring bool`
  guard: true for the duration of `Restore`, checked at the top of each
  wrapped callback, so these calls are no-ops for persistence purposes
  (the caller's own `Options.OnSelected` etc. still fire, though — e.g.
  `settings.selectNode` still needs to run on restore to render the
  properties pane for the last-selected node).
- **Stale references skipped silently.** `Restore` checks `Options.Exists`
  before calling `OpenBranch`/`Select` for each persisted UID; anything
  not currently present is dropped with no error and no fallback
  selection — matches your answer (properties pane just starts empty if
  the last-selected node is gone).
- **No new `db` API.** Everything above is built entirely on the existing
  `SaveUIState`/`LoadUIState`; `db/db.go` is untouched.

## `settings.Window` integration

- `settings.uiState`/`saveUIState`/`restoreUIState` are unchanged — still
  only `Width`/`Height`/`SidebarOffset`, still keyed by the existing
  `componentID` constant.
- `buildTree()` currently ends with `t.OnSelected = w.selectNode` — that
  line is removed from `buildTree()`, since `Track` now owns
  `tree.OnSelected` and calls `w.selectNode` itself via
  `Options.OnSelected` (leaving the old line in place would just be
  silently overwritten the moment `Track` runs, which is harmless but
  confusing to a future reader).
- `NewWindow` calls `treestate.Track(database, componentID+".tree", w.tree,
  treestate.Options{Exists: func(uid string) bool { _, ok := w.byID[uid];
  return ok }, OnSelected: w.selectNode})` immediately after
  `w.tree = w.buildTree()`.
- After `win.SetContent(...)` (mirroring where `w.restoreUIState()` already
  runs), call the returned `*treestate.Tracker`'s `Restore()`.
- `w.selectNode` itself is unchanged — `Track` calls it via
  `Options.OnSelected` exactly as `tree.OnSelected` called it directly
  before this change, so its own behavior (parsing the UID, setting
  `w.selectedUID`, rendering the properties pane) is untouched.

## Data flow

1. User expands/collapses a branch or selects a node in the settings tree.
2. Fyne calls the wrapped callback `Track` installed on `w.tree`.
3. The wrapped callback updates the tracker's in-memory expanded-set/
   selection, marshals a `treeState`, and calls
   `database.SaveUIState(id, blob)` — then (if not currently restoring)
   calls the caller's `Options.On*` pass-through.
4. Next time `settings.NewWindow` runs (app restart, or window reopened),
   `restoreUIState()` (window size/splitter) and `tracker.Restore()` (tree
   expand/selection) both run independently, each reading their own blob
   via their own ID.

## Error handling

Matches the existing `restoreUIState`/`saveUIState` pattern: a
`LoadUIState`/`SaveUIState`/JSON marshal/unmarshal error is logged
(`log.Printf`) and otherwise swallowed — a UI-state persistence failure
must never block the settings window (or any future tree) from working.

## Testing

- `treestate` package: unit tests against a real `*widget.Tree` backed by a
  small fixture tree and an in-memory `db.DB` (via `go-ux/test`, mirroring
  `settings_test.go`'s existing pattern) — covering: open/close persists
  and round-trips through `Restore`; selection persists and round-trips;
  a stale persisted UID (not in `Exists`) is skipped on `Restore` without
  panicking; `Restore` does not itself trigger a further `SaveUIState`
  call (assert via a counting fake or by checking the blob's write
  timestamp/content doesn't change across `Restore`).
- `settings` package: extend existing tests (or add one) confirming a
  `settings.Window` reopened against the same `db.DB` restores the
  previously-expanded branches and re-renders the previously-selected
  node's properties pane, and that a deleted node's stale selection
  doesn't panic or wrongly select something else.

## Out of scope

- `terminal.Window` size/position persistence (see project memory).
- Any second real-world tree consumer (e.g. an actual project/files tree)
  — this spec only builds the reusable mechanism and its one consumer
  (`settings.Window`); a future tree just calls `treestate.Track` the same
  way.
- Debouncing/batching writes — every event writes immediately, per your
  answer; no plan to revisit this unless it proves to be a real problem.
