# `editors` package

Import path: `go-ux/editors`

A Fyne text-editor-with-tabs component, embeddable in a host Go app —
built for a novel-writing / prose-editing use case, plus a Go API surface
an AI coding assistant (via a host app's own tooling) can drive to propose
a diff for human review.

**Phase 1 (current state, this document):** the layout/interaction shell
— tab bar, split/move-pane menus, resize bars, persisted layout — with a
placeholder, read-only, static-text content area. No real text editing,
no file I/O, no diff review, no markdown preview, no font settings, and no
file watching yet — those are deferred to later phases so the interaction
surface could be verified independently first.

## Public API

```go
func NewGroup(app fyne.App) *Group
func NewGroupFromSettings(app fyne.App, database *db.DB, groupID string) *Group

func (g *Group) AddTab(tab *Tab)
func (g *Group) SplitRight(source *Pane)
func (g *Group) SplitDown(source *Pane)
func (g *Group) MoveRight(source *Pane, tab *Tab)
func (g *Group) MoveDown(source *Pane, tab *Tab)
// Group also implements fyne.CanvasObject (embeds widget.BaseWidget), so
// it can be dropped straight into any Fyne container/window.

func NewTab(id, title, filePath, text string) *Tab
```

`NewGroup` builds a `Group` — the embeddable parent layout component —
with a single, empty primary pane and no persistence. `NewGroupFromSettings`
is a second constructor (same `NewWindow`/`NewWindowFromSettings` dual-
constructor pattern `go-ux/terminal` already uses): it sources the initial
layout (panes, splits, open tabs) from `database`'s persisted state for
`groupID`, and keeps writing to it live thereafter — every tab
open/close/split/move persists immediately, not just on some periodic or
close-time save. If nothing has been saved yet for `groupID` (or
`database` is nil), it falls back to the same single-empty-primary-pane
default `NewGroup` builds — a caller that forgets to persist, or doesn't
need to, still gets a working `Group`.

`AddTab` adds `tab` to the primary pane — Phase 1's way to seed content
(see "Minimal usage" below); Phase 2 will likely add a more direct
`OpenFile`-style entry point once there's a real file-backed `Document`
to open.

## Layout model

A `Group` shows 1–4 "editor sub-components" (`Pane`s), each with a North
tab bar (always visible; see "Splitting and moving" below for how new
panes appear), a placeholder center content area (Phase 1), and a South
bar (currently always hidden — Phase 2's diff-review and file-watch
notifications will drive it).

## Splitting and moving

Right-click a tab bar to get **Split Right**, **Split Down**, **Move
Right**, **Move Down**, and **Close Tab**.

- **Split** creates a new, empty `Pane` in the given direction. Nesting is
  capped at one level, but independently per side: after an initial split,
  either resulting pane can itself be split on the *other* axis (giving up
  to 4 panes total, arranged as independent quadrants), but a pane that's
  already the result of one split can't be split again.
- **Move** relocates a tab to the adjacent pane on the given side. If that
  adjacent pane doesn't exist yet, Move auto-creates the split first (same
  as Split), then moves the tab into it — matching IntelliJ's own "Move
  Right creates a group if none exists" behavior.
- Closing a non-primary pane's last tab closes that pane, collapsing the
  split. The original/primary pane can never be closed this way, even when
  empty.

Phase 1's placeholder content area has no real document sharing yet, so
"split" doesn't yet show synced live edits across panes the way it
eventually will (see the design plan) — that lands with Phase 2's real
text backend.

## Persistence

Live — every structural or tab change writes the whole current layout
immediately via new `go-ux/db.DB` methods (`SaveEditorLayout`/
`LoadEditorLayout`, a third persistence domain alongside `db`'s existing
settings registry and opaque UI-state blobs — see `db.md`). This is NOT an
opaque blob like `go-ux/treestate` uses for tree state; it's a real
relational shape (`db.EditorPane`/`db.EditorTab`), matching the settings
registry's `Node`/`Property` precedent, because a caller-visible "what
tabs are open" structure was a deliberate design goal here (unlike
`treestate`'s state, which stays entirely internal).

## Minimal usage

```go
package main

import (
	"fyne.io/fyne/v2/app"

	"go-ux/db"
	"go-ux/editors"
)

func main() {
	database, err := db.Open("editors.sqlite") // or ":memory:"
	if err != nil {
		panic(err)
	}
	defer database.Close()

	fyneApp := app.NewWithID("your.app.id")

	group := editors.NewGroupFromSettings(fyneApp, database, "myapp.mainEditor")
	group.AddTab(editors.NewTab("t1", "Chapter 1", "chapter1.txt", "It was a dark and stormy night..."))

	win := fyneApp.NewWindow("My App")
	win.SetContent(group) // embed directly — Group is a fyne.CanvasObject, not its own Window
	win.Show()

	fyneApp.Run()
}
```

See `editorsdemo/` (`go run ./editorsdemo`) for a runnable example with 3
pre-opened tabs — it also demonstrates layout persistence: run it, split/
move some tabs around, close it, and run it again against the same db file
to see the layout come back exactly as it was left.

## Deferred (not yet built)

Real text editing (line numbers, soft wrap, markdown preview via
goldmark), font settings (a shared `Ctrl+scroll`-adjustable font-size
pattern reused from `go-ux/terminal`, factored into a new shared
`fontsettings` package), diff review (via `go-difflib`) plus an external
API for a host app's own AI-assistant tooling to propose edits, and file
watching (via `fsnotify`, auto-load or notify on external changes) are all
designed but not yet implemented.
