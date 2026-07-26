# `treestate` package

Import path: `github.com/dmongrel/go-ux/treestate`

Persists a tree UI's expand/collapse state and selected node, live, reusable
by any tree-based component backed by a `*github.com/dmongrel/go-ux/db.DB` — not tied to
`github.com/dmongrel/go-ux/settings`, whose properties tree is simply its first consumer.

Unlike the original Fyne version (which took over a live `*widget.Tree`'s
callback fields), there is no live Go-side tree widget in a Wails app to
hang callbacks off — the tree lives entirely in the frontend. `Tracker` is
instead a plain request/response state store: the frontend calls
`SetExpanded`/`SetSelected` on every toggle/selection, and reads
`Expanded`/`Selected` back (typically once, when a tree first mounts) to
replay the persisted state itself.

## Public API

```go
func New(database *db.DB, id string) *Tracker

func (t *Tracker) Expanded() []string
func (t *Tracker) Selected() string
func (t *Tracker) SetExpanded(uid string, expanded bool)
func (t *Tracker) SetSelected(uid string)
```

`New` loads any previously persisted state immediately. `id` is
caller-chosen and must be unique per tree instance you want tracked
independently — e.g. `"myapp.settings.tree"` for one tree,
`"myapp.projectExplorer.tree"` for another; nothing enforces uniqueness,
duplicate IDs simply share (and clobber each other's) persisted state.

`SetExpanded`/`SetSelected` write the tracker's *entire* current
expand-set and selection to `database`, immediately, as one JSON blob via
`database.SaveUIState(id, blob)` — the same generic, arbitrary-ID
opaque-blob store any `go-ux` component's own UI state uses (see `db.md`).
There's no explicit `Save` call and no debounce.

`Expanded`/`Selected` return the tracker's current in-memory state
(loaded at construction, updated live by every `SetExpanded`/`SetSelected`
call) — there is no notion of "current tree shape" in this package, so a
caller wanting to filter out stale UIDs (a persisted reference to a node
that's since been removed) must do that itself before replaying the result
— see `github.com/dmongrel/go-ux/settings.Service.InitialTreeState` for the pattern.

## Minimal usage

```go
tracker := treestate.New(database, "myapp.explorer.tree")

// serve tracker.Expanded()/tracker.Selected() (filtered against your
// current node set) to the frontend when its tree first mounts

// on every frontend toggle/selection:
tracker.SetExpanded(uid, expanded)
tracker.SetSelected(uid)
```

## Error handling

A `SaveUIState`/`LoadUIState`/JSON marshal/unmarshal failure is logged
(`log.Printf`) and otherwise swallowed — a UI-state persistence failure
must never block the tree from working.
