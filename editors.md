# `editors` package

Import path: `go-ux/editors`

A Wails v3 `Service` backing a text-editor-with-tabs component — built for
a novel-writing / prose-editing use case, plus a diff-review API surface an
AI coding assistant can drive to propose a diff for human review. The
split-pane tree, live cross-pane document sync, and Split/Move menu all live
in the frontend (`uxdemo/frontend/src/views/editor.ts`) — there is no
Go-side notion of a Pane. `Service` owns: the open-tab list, real file I/O,
file watching, diff-review state, and per-instance font settings.

## Public API (Go)

```go
func NewService(app *application.App, database *db.DB, groupID string) *Service

func (s *Service) ListTabs() []TabInfo
func (s *Service) NewTab() []TabInfo
func (s *Service) SaveTab(id string, text string) ([]TabInfo, error)
func (s *Service) OpenFile(path string) ([]TabInfo, error)
func (s *Service) OpenFileDialog() ([]TabInfo, error)
func (s *Service) SaveTabAs(id string, path string) ([]TabInfo, error)
func (s *Service) SaveTabAsDialog(id string) ([]TabInfo, error)
func (s *Service) ReloadTab(id string) ([]TabInfo, error)
func (s *Service) CloseTab(id string) []TabInfo
func (s *Service) ProposeDiff(id string, newText string) ([]TabInfo, error)
func (s *Service) AcceptDiff(id string, finalText string) ([]TabInfo, error)
func (s *Service) CancelDiff(id string) ([]TabInfo, error)
func (s *Service) CurrentFontSettings() fontsettings.FontSettings
func (s *Service) SetFontSettings(f fontsettings.FontSettings) error
func (s *Service) SaveLayout(root LayoutNode) error
func (s *Service) LoadLayout() (*LayoutNode, error)
func (s *Service) OpenWindow()
func (s *Service) Close()

type TabInfo struct {
    ID          string
    Title       string
    FilePath    string
    Text        string
    Dirty       bool
    PendingDiff *string
}

type LayoutTab struct {
    TabID    string
    FilePath string
}

type LayoutNode struct {
    Axis        string // "row"/"column"; "" means this is a leaf pane
    SplitOffset float64
    A           *LayoutNode
    B           *LayoutNode
    Tabs        []LayoutTab
    ActiveTabID string
}

func RegisterSettings(database *db.DB, groupID string) error
func ApplyEditorSettings(database *db.DB, groupID string, svc *Service) error
```

`NewService` seeds two in-memory placeholder tabs ("Chapter One"/"Notes"),
matching the shape of an earlier Wails prototype's demo content, so there's
something to show without opening a real file first. Every method returning
`[]TabInfo` returns the full current tab list — the frontend replaces its
whole local copy on each call, matching the request/response nature of a
Wails RPC boundary (there is no live Go→JS tab-list push).

`SaveTab` updates a tab's text; if the tab has a `FilePath` (opened via
`OpenFile`/`SaveTabAs`), also writes it to disk and marks it clean — a tab
with no `FilePath` just updates in-memory (not an error, unlike the original
Fyne `Group.SaveTab`, since this Service's own demo seeds memory-only tabs a
user should still be able to "Save"). `OpenFile` dedups against an
already-open path (matched by `FilePath`) rather than opening a duplicate
tab. `OpenFileDialog`/`SaveTabAsDialog` wrap `OpenFile`/`SaveTabAs` with
Wails' native file pickers (`app.Dialog.OpenFile()`/`.SaveFile()`) — this
package has no picker of its own beyond that. `ReloadTab` re-reads a tab's
`FilePath` from disk, discarding unsaved edits — the "Load from Disk" half
of the file-changed-on-disk flow (see "File watching" below).

`ProposeDiff` sets a tab's `PendingDiff`; this package computes and renders
no diff itself — the frontend's `@codemirror/merge` `unifiedMergeView`
does, comparing `Text` (old) against `PendingDiff` (new). `AcceptDiff`
commits the frontend's (possibly per-chunk-edited) final text — not
necessarily the originally proposed text — writing it to disk if the tab
has a `FilePath`. `CancelDiff` discards the proposal, `Text` untouched.

`CurrentFontSettings`/`SetFontSettings` are per-`Service`-instance (matches
the original design's "independent per Group instance," not
`go-ux/terminal`'s single global value): `SetFontSettings` clamps, updates
the live value, broadcasts it via the `editors:font` event, and persists it
to `database`'s Editors node.

`OpenWindow` opens the editor UI in its own window (`Title: "Editor"`,
900x650). `Close` stops the background file watcher, if one was started.

`RegisterSettings` seeds a root `"Editors: " + groupID` node (font settings
+ `file_watch_mode`), idempotent, scoped per `groupID` (independent
font/watch-mode per instance, unlike `terminal`'s single shared node).
`ApplyEditorSettings` re-reads it and pushes the values into a live
`Service` — `NewService` calls it once at construction; a host app calls it
again after a Settings-window OK/Apply.

## Minimal usage

```go
database, err := db.Open("editors.sqlite")
if err := editors.RegisterSettings(database, "myapp.editor"); err != nil { log.Fatal(err) }

svc := editors.NewService(app, database, "myapp.editor")
app.RegisterService(application.NewService(svc))
app.OnShutdown(func() { svc.Close() })
```

```ts
// hub.ts
import {OpenWindow} from "../../bindings/go-ux/editors/service";
OpenWindow();
```

## File watching

One `*fsnotify.Watcher` per `Service` (not per tab), started lazily on the
first file-backed tab. `file_watch_mode` (`"auto"`/`"notify"`, see
`RegisterSettings`) controls the reaction to an external change:
`FileWatchModeAuto` silently reloads the tab's in-memory text unless it has
unsaved edits (never clobbers them — falls back to notify in that case);
`FileWatchModeNotify` emits the `editors:filechanged` event with the tab's
ID, which the frontend surfaces as a banner with "Load from Disk"
(`ReloadTab`) / "Keep from Memory" choices — the Wails equivalent of the
original Fyne version's south-bar `SouthBarFileChanged` mode. A watch is
never explicitly removed once started (lives for the `Service`'s lifetime);
`Close` stops the whole watcher.

## Split panes, persisted layout, and Markdown preview — frontend-owned

The split-pane tree, live cross-pane document sync (`SharedDoc`), and the
Split/Split-and-Move/Move right-click menu (`uxdemo/frontend/src/views/editor.ts`)
were ported from a standalone Wails prototype that had already independently
re-derived the original Fyne `split.go` tree algorithm — and *generalized*
it beyond what the original supported (collecting into a single pane when
moving out of a 2-pane stack, and quadrant positional correspondence for
Move). There is no Go-side pane/split state — a `Service` only knows about
a flat list of open tabs — but the frontend persists the *shape* of that
tree via `SaveLayout`/`LoadLayout`, stored as one JSON blob (via
`db.SaveUIState`/`LoadUIState`, keyed by `groupID + ".layout"`) rather than
the relational schema the original Fyne version's `db.EditorPane`/
`EditorTab` used (removed — there's no Go-owned Pane tree left to map rows
onto). The frontend calls `SaveLayout` after every structural change and
tab selection; on mount, `LoadLayout` is replayed by resolving each
`LayoutTab` — preferring `TabID` (works whenever this `Service`'s process
hasn't restarted since, memory-only tabs included) and falling back to
reopening `FilePath` from disk otherwise (a memory-only tab whose ID is
gone from a real restart has no such fallback and is dropped).

Markdown preview is rendered client-side (`marked`, a snapshot render —
doesn't live-update if edited from another pane showing the same tab)
rather than porting the original goldmark-AST-to-Fyne-widget renderer,
since the preview target is now HTML, not a Fyne canvas tree. Soft-wrap is
a per-pane toggle on the line-number gutter's own right-click menu (an
IDE-style placement, not a toolbar button) — CodeMirror's `basicSetup`
gives line numbers by default but not wrapping.

## Constraints for callers

- `ProposeDiff`/`AcceptDiff`/`CancelDiff` operate on a single `Tab` by ID —
  if the same tab is shown in multiple frontend panes, it's the frontend's
  own responsibility (`refreshPanesShowingTab` in `editor.ts`) to update
  every pane showing it; this package has no notion of "which panes show
  this tab."
- `SetFontSettings` is per-`Service` instance, not per-tab or per-pane.
- `SaveLayout`/`LoadLayout` persist pane *shape* and which tabs are open
  where — not resize-bar split-offset values (`LayoutNode.SplitOffset` is
  always written as `0.5`; the frontend doesn't yet support dragging a
  resize bar at all, so there's nothing else to persist there).
