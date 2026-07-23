# `treestate` package

Import path: `go-ux/treestate`

Persists a Fyne `*widget.Tree`'s expand/collapse state and selected node,
live, reusable by any tree-based UI backed by a `*go-ux/db.DB` — not tied
to `go-ux/settings`, whose properties tree is simply its first consumer.

## Public API

```go
type Options struct {
    Exists         func(uid string) bool
    OnSelected     func(uid string)
    OnBranchOpened func(uid string)
    OnBranchClosed func(uid string)
}

func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker
func (t *Tracker) Restore()
```

`Track` takes over `tree`'s `OnSelected`/`OnBranchOpened`/`OnBranchClosed`
callback fields. Every toggle or selection writes the tree's *entire*
current expand-set and selection to `database`, immediately, as one JSON
blob via `database.SaveUIState(id, blob)` — the same generic, arbitrary-ID
opaque-blob store any `go-ux` component's own UI state uses (see `db.md`).
There's no explicit `Save` call and no debounce.

`id` is caller-chosen and must be unique per tree instance you want tracked
independently — e.g. `"myapp.settings.tree"` for one tree,
`"myapp.projectExplorer.tree"` for another; nothing enforces uniqueness,
duplicate IDs simply share (and clobber each other's) persisted state.

### `Options` field reference

| Field            | Required? | Purpose                                                                 | If omitted (nil)                                                                 |
|-------------------|-----------|--------------------------------------------------------------------------|------------------------------------------------------------------------------------|
| `Exists`          | Should be set | Reports whether a UID is still valid in the tree's *current* data. `Restore` uses it to drop stale persisted references (a node deleted since last session) — no error, no fallback selection. | Safe: `Track` defaults it to "every UID is valid," which disables stale-reference filtering. A deleted node's old selection could then be replayed against a tree that no longer has it. Set it if your tree's node set can ever shrink. |
| `OnSelected`      | Optional  | Called after persisting a selection — put your existing "render details for this node" logic here. Also fires during `Restore`'s replay, so the last-selected node's details render automatically on reopen. | No pass-through call happens; persistence itself still works. |
| `OnBranchOpened`  | Optional  | Called after persisting a branch open. Also fires during `Restore`'s replay. | No pass-through call happens; persistence itself still works. |
| `OnBranchClosed`  | Optional  | Called after persisting a branch close.                                  | No pass-through call happens; persistence itself still works. |

`Track`/`Restore` are the only two calls needed — there is no `Save`,
`Unsubscribe`, or `Close` method. `Track` takes over the tree's callback
fields for the tree's lifetime; there's currently no way to detach a
`Tracker` from its tree once created (no consumer has needed it yet).

## Minimal usage

```go
tree := widget.NewTree(childUIDsFn, isBranchFn, createFn, updateFn)

tracker := treestate.Track(database, "myapp.explorer.tree", tree, treestate.Options{
    Exists: func(uid string) bool {
        _, ok := myNodes[uid]
        return ok
    },
    OnSelected: func(uid string) {
        renderDetails(uid)
    },
})

// ... attach tree to your window/container, show it ...

tracker.Restore() // after the tree is populated and visible
```

## Works identically in a dedicated window or embedded in a host app

`Track`/`Restore` only ever touch the `*widget.Tree` and the `*db.DB` you
pass in — nothing about `treestate` knows or cares whether that tree lives
in its own `fyne.Window` (like `go-ux/settings.Window` does) or is embedded
as one panel inside a larger host application's existing window (a project
tree in the sidebar of an IDE-style app, for instance). There is no
`Window` type in this package's API at all.

Concretely, this means:

- **Standalone window** (e.g. `settings.Window`'s pattern): build the tree,
  call `Track`, attach the tree to the window's content, call
  `win.Show()`, then call `tracker.Restore()`. See `settings/settings.go`'s
  `NewWindow` for the exact call order.
- **Embedded in a host app's own window**: identical — build the tree,
  call `Track`, add the tree to whatever `fyne.Container` the host is
  already assembling (a `container.Border`, a tab, a split pane, ...),
  then call `tracker.Restore()` once that container is part of the host's
  visible content. No `go-ux` `Window` wrapper is required or expected.

The only thing that matters for correctness in either case is *ordering*:
`Track` before the tree is shown (so no toggle/selection is missed), and
`Restore` after the tree has real data and is attached to whatever
container will display it (so `OpenBranch`/`Select` have something
meaningful to act on).

## Fyne gotcha this package works around for callers

`widget.Tree.OpenAllBranches()` mutates the tree's internal open-set
directly and does **not** invoke `OnBranchOpened` — so if you use it (e.g.
to force-reveal search matches, as `go-ux/settings` used to), those opens
are invisible to `Track` and never get persisted. Open branches
individually instead (`tree.OpenBranch(uid)` per branch node) if you want
programmatic expansion to persist the same way a user's click does — see
`settings/settings.go`'s `applySearch` for the pattern.

## Restore does not re-persist

`Restore`'s own `OpenBranch`/`Select` calls do not themselves trigger
another `SaveUIState` write — internally guarded for the duration of
`Restore`, so calling it against an already-persisted, unmodified tree is
a no-op from the db's perspective (aside from the one `LoadUIState` read).

## Error handling

A `SaveUIState`/`LoadUIState`/JSON marshal/unmarshal failure is logged
(`log.Printf`) and otherwise swallowed — a UI-state persistence failure
must never block the tree (or its host window) from working. This matches
`go-ux/settings`'s own established convention for its window-level UI
state.
