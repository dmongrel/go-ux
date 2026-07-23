# `editors` package

Import path: `go-ux/editors`

A Fyne text-editor-with-tabs component, embeddable in a host Go app —
built for a novel-writing / prose-editing use case, plus a Go API surface
an AI coding assistant (via a host app's own tooling) can drive to propose
a diff for human review.

**Current state (this document):** the layout/interaction shell — tab bar,
split/move-pane menus, resize bars, persisted layout — plus a real,
editable, Document-backed content area with line numbers, a soft-wrap
toggle, Ctrl+scroll font sizing, and Markdown preview (see "Documents and
editing", "Font settings", and "Markdown preview" below). No file I/O
(content is seeded in memory by the caller, not loaded from disk), no diff
review, and no file watching yet — those are deferred to later phases so
the interaction surface could be verified independently first.

## Public API

```go
func NewGroup(app fyne.App) *Group
func NewGroupFromSettings(app fyne.App, database *db.DB, groupID string) *Group

func (g *Group) AddTab(tab *Tab)
func (g *Group) SplitRight(source *Pane)
func (g *Group) SplitDown(source *Pane)
func (g *Group) MoveRight(source *Pane, tab *Tab)
func (g *Group) MoveDown(source *Pane, tab *Tab)
func (g *Group) OpenFile(path string) (*Tab, error)
func (g *Group) ProposeDiff(path string, newText string) error
// Group also implements fyne.CanvasObject (embeds widget.BaseWidget), so
// it can be dropped straight into any Fyne container/window.

func NewTab(id, title, filePath, text string) *Tab

func (t *Tab) Text() string
func (t *Tab) Dirty() bool
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
panes appear), an editable center content area (see "Documents and
editing" below), and a South bar (currently always hidden — a later phase's
diff-review and file-watch notifications will drive it).

## Documents and editing

Every `Tab` points at a `Document` — the actual shared text buffer,
separate from the `Tab` UI entry itself. `NewTab`'s `text` argument seeds a
fresh `Document`. `Tab.Text()`/`Tab.Dirty()` read the current state of that
`Document`.

The content area is a real, editable multi-line text widget bound to its
Tab's `Document`. When two `Pane`s show the same `Document` (i.e. after a
Split, since Split copies the same `Tab` — and so the same `Document` —
into the new `Pane`), typing in either one's content area updates the
other immediately: `Document` keeps a listener registry, and each `Pane`'s
content widget registers/unregisters itself as its active tab changes.

There's no file I/O yet — `Document` is purely an in-memory buffer. A
caller wanting to load a file's contents reads it themselves and passes
the text to `NewTab`; there's no `OpenFile`-from-disk helper yet (that,
along with `Document.Dirty()`-aware save/reload and file watching, is a
later phase).

A right-aligned line-number gutter runs down the left side of the content
area. Right-click it for **Toggle Soft Wrap** — off, long lines overflow
horizontally (scrollbar); on (the default), they wrap within the visible
width. Line numbers track logical lines exactly when wrap is off; with
wrap on, a long logical line still gets a single gutter number even though
it spans multiple visual lines (an accepted approximation, not yet true
visual-line numbering).

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

Since Split copies the same `Tab` (and so the same `Document`) into the new
`Pane`, edits made in either pane after a split show up in both
immediately — see "Documents and editing" above.

## Persistence

Live — every tab open/close and every split/move writes the whole current
layout immediately via new `go-ux/db.DB` methods (`SaveEditorLayout`/
`LoadEditorLayout`, a third persistence domain alongside `db`'s existing
settings registry and opaque UI-state blobs — see `db.md`). This is NOT an
opaque blob like `go-ux/treestate` uses for tree state; it's a real
relational shape (`db.EditorPane`/`db.EditorTab`), matching the settings
registry's `Node`/`Property` precedent, because a caller-visible "what
tabs are open" structure was a deliberate design goal here (unlike
`treestate`'s state, which stays entirely internal).

**Resize-bar (split offset) persistence is opportunistic, not instant.**
Fyne's `container.Split` has no drag-end callback in this Fyne version, so
there's no signal the moment the user finishes dragging a divider. A
resize is captured and persisted the *next* time anything else changes
(open/close/split/move a tab) — not the instant the drag ends. Dragging a
divider and then immediately closing the app with no other action loses
that particular resize. A future phase may add proper drag-end capture
(likely via a small custom split wrapper); tracked as a known Phase 1 gap,
not a design decision.

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

## Font settings

Each `Group` gets its own independent, Ctrl+scroll-adjustable font size —
scroll while holding Ctrl over any pane's content area to grow/shrink it.
This reuses `go-ux/fontsettings` (extracted from `go-ux/terminal`'s
original font-settings mechanism, generalized into a per-instance
`*fontsettings.State` rather than one value shared by every open thing
everywhere). Unlike `terminal`, which rasterizes its own glyphs onto a
canvas, the content area is a native `widget.Entry`, so applying a font
size doesn't need any font *loading* — it's done via a
`container.NewThemeOverride` that overrides `theme.SizeNameText` from the
live `*fontsettings.State`.

Font size is **not yet persisted** — it resets to the default on every
`NewGroup`/`NewGroupFromSettings` call. There's no `editors` settings-tree
node yet (the way `terminal/settings_schema.go` registers one for
`terminal`), so there's nowhere to save it to. A later phase should add
that, mirroring `terminal`'s `RegisterSettings`/`ApplyFontSettings` pattern
using `fontsettings.SeedFontProperties`/`ReadFontProperties`.

## Markdown preview

Tabs whose `FilePath` ends in `.md`/`.markdown` get a **Preview**/**Edit**
toggle button, right-aligned in that pane's tab bar. Preview mode renders
the current text via goldmark (`markdown.go`'s `renderMarkdown`) —
headings, paragraphs, bold/italic/inline code, links, blockquotes,
ordered/unordered lists, fenced/indented code blocks, and thematic breaks
— in place of the editable content area. This is a **snapshot**, not a
live view: if the same `Document` is being edited in another split pane
while this one shows a preview, the preview won't update until toggled off
and back on.

## Diff review (mcp_tooling)

`editors` implements no MCP protocol/server itself — it exposes a plain Go
API for a host app's own separately-implemented tooling (e.g. a Claude
Code `/ide`-style integration) to drive:

- `OpenFile(path string) (*Tab, error)` — the "click a file in a host
  app's sidebar" entry point. Reads `path` from disk (the package's first
  real file I/O — every other tab-opening path so far seeds text purely
  in memory) if it isn't already open in this `Group`; returns the
  existing `Tab` instead of a duplicate if it is.
- `ProposeDiff(path string, newText string) error` — opens `path` (via
  `OpenFile`) if needed, then switches every `Pane` currently showing that
  tab into a **diff-review** mode: the content area shows a read-only,
  line-by-line red/green diff between the current text and `newText`
  (unchanged lines included, for full context — this favors reviewing
  prose edits over terse source-code hunks), and the south bar shows
  **Accept**/**Cancel**. Accept applies `newText` to the `Document` (which
  live-syncs to any other pane already showing it, same as a normal edit)
  and writes it to disk; Cancel discards the proposal, `Document`
  untouched. A disk-write failure on Accept is logged, not surfaced as an
  error — the in-memory edit still applies either way.

Both are plain synchronous methods (no goroutine/channel API) — same
UI-goroutine-only contract as `AddTab`/`SplitRight`/etc.

## Deferred (not yet built)

File I/O beyond `OpenFile`/`ProposeDiff` (there's still no "save this tab
to disk" for a normal, non-diff-review edit) and file watching (via
`fsnotify`, auto-load or notify on external changes); and persisting the
font size setting (see "Font settings" above). All designed but not yet
implemented.
