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

`opts.Exists` should be set: it reports whether a given tree node UID is
still valid in the tree's *current* data. `Restore` uses it to filter out
stale references — a UID that was expanded/selected in a previous session
but no longer exists (e.g. the underlying data changed) is silently
skipped, with no fallback selection. Leaving it nil is safe (`Track`
defaults it to "every UID is valid") but disables that stale-reference
filtering entirely, so a deleted node's old selection could be replayed
against a tree that no longer has it — set it if your tree's node set can
ever shrink.

`opts.OnSelected`/`OnBranchOpened`/`OnBranchClosed` are optional
pass-throughs: `Track` persists first, then calls these (if set), so a
caller's own reaction to tree events (e.g. rendering a properties pane)
keeps working exactly as if it had set the tree's callbacks directly. These
also fire during `Restore`'s replay — e.g. a caller using `OnSelected` to
render a properties pane will see it render for whatever node `Restore`
found persisted as selected.

`id` is caller-chosen and must be unique per tree instance you want tracked
independently — e.g. `"myapp.settings.tree"` for one tree,
`"myapp.projectExplorer.tree"` for another; nothing enforces uniqueness,
duplicate IDs simply share (and clobber each other's) persisted state.

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
