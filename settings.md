# `settings` package

Import path: `go-ux/settings`

A Fyne settings control panel window, modeled on IntelliJ Community Edition's
Settings dialog: a searchable tree of categories on the left, a generated
properties form on the right, and OK/Cancel/Apply at the bottom. It reads and
writes its data through a `*go-ux/db.DB` (see `db.md`) — it never touches
SQLite directly, and has no data format of its own beyond what `db.DB`
already defines.

## Public API

```go
func NewWindow(app fyne.App, database *db.DB) (*Window, error)
func (w *Window) SetSize(width, height float32) *Window
func (w *Window) Show()
```

`NewWindow`:

1. Reads the full settings tree via `database.ListSettings()` and every
   node's properties via `database.GetProperties()` (used to build the tree
   and to power search).
2. Builds the Fyne window (1024x800 default, resizable, title "Settings").
3. Restores the window's own saved size and sidebar-splitter position from
   `database.LoadUIState()` (see "Own UI state" below).

`SetSize` overrides the window's current size — the 1024x800 default, or
whatever was just restored from saved UI state in step 3 above, whichever
applies. Both `width` and `height` must be positive or the call is a no-op.
Call it after `NewWindow` and before `Show` if you want to force a specific
size regardless of what was previously saved.

`Show()` just calls the underlying `fyne.Window.Show()`. Nothing else is
exported — there is no way to reach into the tree, force a selection, or
read staged edits from outside the package. If you need programmatic control
beyond opening the window, that's a signal the API needs to grow, not that
you should reach around it.

## Minimal usage

```go
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

	"go-ux/db"
	"go-ux/settings"
)

func main() {
	database, err := db.Open("settings.sqlite") // or ":memory:"
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	fyneApp := app.NewWithID("your.app.id") // NewWithID, not New — see db.md
	win, err := settings.NewWindow(fyneApp, database)
	if err != nil {
		log.Fatal(err)
	}
	win.Show()

	fyneApp.Run()
}
```

The registry (`db.Node` / `db.Property` rows) must already exist in the
database before `NewWindow` is called — this package only ever reads/writes
existing rows via `db.DB.SaveProperties`; it has no seeding API. Populate the
registry yourself with `db.DB.AddNode` / `db.DB.AddProperty` (see `db.md`),
or copy the pattern in `go-ux/test.SeedExample` used by
`test_settings.go` at the repo root.

## Data flow / formats

Everything here comes from `go-ux/db`; this package adds no new types or
encodings:

- **Tree structure**: `db.Node{ID, ParentID, Description, SortOrder}`.
  `ParentID == nil` means a root-level node. The tree widget's node label is
  `Node.Description`. Nesting is arbitrary depth (adjacency list).
- **Properties page**: `db.Property{Key, Label, Type, Value, EnumOptions}`.
  `Type` drives which Fyne widget is generated:

  | `db.PropertyType` | Widget            | `Value` encoding                          |
  |--------------------|--------------------|--------------------------------------------|
  | `PropertyBool`     | `widget.Check`     | the literal string `"true"` or `"false"`   |
  | `PropertyString`   | `widget.Entry`     | raw string, unconstrained                  |
  | `PropertyInt`      | `widget.Entry`     | base-10 integer string (`strconv.Atoi`)    |
  | `PropertyEnum`     | `widget.Select`    | one of the strings in `EnumOptions`        |

  Any `Type` other than the four above falls back to a plain string `Entry`.
- **Persisted edits**: edits are staged in memory (keyed by node ID then
  property key) as the user types/toggles/selects. Nothing is written to the
  `db` until the user clicks **Apply** or **OK**, via
  `database.SaveProperties(nodeID, map[string]string{key: newValue, ...})`.
  **Cancel** discards the staged map and closes the window without writing
  anything.

## Own UI state (dogfooding `db`'s UI-state store)

The settings window persists its *own* size and sidebar-splitter position
using the same UUID-keyed blob mechanism any consumer app can use for its
own windows (see `db.md`). Its component ID is hardcoded in
`settings/settings.go`:

```go
const componentID = "b6f6c9d1-3f2b-4b8a-9e3a-2f1c7a5d9e10"
```

This is saved live via `win.SetCloseIntercept` (i.e. on window close, not
staged/Apply-gated like property edits) and restored at the start of
`NewWindow`. The blob is a small JSON object (`{Width, Height,
SidebarOffset}`) — an implementation detail, not something a consumer needs
to construct or parse.

The properties tree's own expand/collapse state and last-selected node are
persisted separately, via `go-ux/treestate` (see `treestate.md`) — its own
blob, keyed by `componentID + ".tree"`, written live on every branch
toggle/selection rather than only on window close. This is what makes
reopening Settings automatically show the same tree shape and the same
selected node's properties page as when it was last closed.

## Search / filtering behavior

Typing in the search box at the top of the sidebar filters the tree
live (no debounce): a node is shown if its own `Description` contains the
query (case-insensitive substring), or if any property on its page has a
matching `Label` — in which case the node is shown even though its own name
didn't match, so the user can find a setting by the name of the control, not
just the category. Ancestors of any shown node are always shown too, so the
result stays reachable in the tree. Matches are highlighted with a filled
yellow rectangle (both in the tree and on the properties page) with the
matched text redrawn in black for contrast. Clearing the search (or clicking
the "X" button) removes the filter and shows everything again.

## Constraints for callers

- `NewWindow` must be called after `database.ListSettings()` would return
  the data you want shown — there's no live-reload if you mutate the
  registry after the window is already open.
- One `db.DB` can back multiple `settings.NewWindow` calls (e.g. reopening
  the dialog), but two *simultaneously open* settings windows sharing one
  `db.DB` will both stage edits independently against the same underlying
  rows — last Apply/OK wins. Nothing in this package coordinates that.
- This package assumes desktop Fyne (native title bar, min/max/close, and a
  resizable window). It has not been tested against Fyne's mobile driver.
