# `settings` package

Import path: `go-ux/settings`

A Wails v3 `Service` backing a settings control panel modeled on IntelliJ
Community Edition's Settings dialog: a searchable, nested tree of categories
and a generated properties form, OK/Cancel/Apply staging. It reads and writes
its data through a `*go-ux/db.DB` (see `db.md`) — it never touches SQLite
directly. The tree and form are rendered by the frontend
(`uxdemo/frontend/src/views/settings.ts`); `Service` only serves data and
persists staged edits + tree state (`go-ux/treestate`) — see `CLAUDE.md`'s
"Known limitation: frontend distribution".

## Public API (Go)

```go
func NewService(app *application.App, database *db.DB) (*Service, error)

func (s *Service) ListNodes() ([]db.Node, error)
func (s *Service) GetProperties(nodeID int64) ([]db.Property, error)
func (s *Service) AllProperties() (map[string][]db.Property, error)
func (s *Service) StageProperty(nodeID int64, key string, value string)
func (s *Service) Apply() error
func (s *Service) Cancel()
func (s *Service) InitialTreeState() TreeState
func (s *Service) SetExpanded(uid string, expanded bool)
func (s *Service) SetSelected(uid string)
func (s *Service) OpenWindow()
```

`NewService` indexes the current node set (used to filter stale tree-state
UIDs in `InitialTreeState`) — a settings instance with an entirely static
node set is the expected case. `ListNodes` returns every node; the frontend
builds the nested tree itself from `Node.ParentID` (`nil` = root). `GetProperties`
fetches one node's properties; `AllProperties` fetches every node's properties
at once, keyed by node ID as a string (map keys must be strings in JSON) —
used for instant, no-round-trip search across every property label.

`StageProperty` records an in-memory edit, not yet written to the db — called
on every form-field change. `Apply` writes every staged edit (one
`db.SaveProperties` call per node) and clears the staged set; `Cancel`
discards it. Nothing is written to `db` until `Apply` runs.

`InitialTreeState`/`SetExpanded`/`SetSelected` back the tree's expand/
collapse + selection persistence via `go-ux/treestate` (see `treestate.md`)
— `InitialTreeState` is pre-filtered against currently valid node IDs (a
persisted UID from a node that no longer exists is silently dropped).

`OpenWindow` opens the settings UI in its own window (`Title: "Settings"`,
1024x800).

## Minimal usage

```go
database, err := db.Open("settings.sqlite")
// ... seed database.AddNode/AddProperty rows first — this package only
// ever reads/writes existing rows, it has no seeding API ...

svc, err := settings.NewService(app, database)
app.RegisterService(application.NewService(svc))
```

```ts
// hub.ts
import {OpenWindow} from "../../bindings/go-ux/settings/service";
OpenWindow();
```

## Data flow / formats

Everything comes from `go-ux/db`; this package adds no new types beyond
`TreeState{Expanded []string; Selected string}`:

- **Tree structure**: `db.Node{ID, ParentID, Description, SortOrder}`.
  `ParentID == nil` means a root-level node. Nesting is arbitrary depth
  (adjacency list) — the frontend renders indentation by walking
  `ParentID` chains.
- **Properties page**: `db.Property{Key, Label, Type, Value, EnumOptions}`.
  `Type` drives which frontend control is generated:

  | `db.PropertyType` | Control            | `Value` encoding                          |
  |--------------------|--------------------|--------------------------------------------|
  | `PropertyBool`     | checkbox           | the literal string `"true"` or `"false"`   |
  | `PropertyString`   | text input         | raw string, unconstrained                  |
  | `PropertyInt`      | number input       | base-10 integer string                     |
  | `PropertyFloat`    | number input       | decimal string                             |
  | `PropertyEnum`     | select             | one of the strings in `EnumOptions`        |

## Search / filtering behavior

Typing in the frontend's search box filters the tree live (no round trip —
computed client-side against `AllProperties`' bulk-fetched data): a node is
shown if its own `Description` contains the query (case-insensitive
substring), or if any property on its page has a matching `Label` (so the
user can find a setting by the control's name, not just the category).
Ancestors of any shown node are shown too, and every visible branch is
auto-expanded for the duration of the search. Matches are highlighted.
Clearing the search restores the persisted expand/collapse state.

## Own UI state

The tree's own expand/collapse state and last-selected node are persisted
via `go-ux/treestate`, keyed by `componentID + ".tree"` (`componentID` is
hardcoded in `settings/service.go`), written live on every toggle/selection.
Unlike the original Fyne `Window`, this package does not persist the
*window's* own size/position — Wails' `WebviewWindowOptions` sets a fixed
starting size (`OpenWindow`); per-window size persistence isn't wired up.

## Constraints for callers

- `NewService` must be called after the registry (`db.Node`/`db.Property`
  rows) exists — there's no live-reload if you mutate the registry after
  construction; `ListNodes`/`GetProperties`/`AllProperties` always read
  live from `db`, but `InitialTreeState`'s valid-ID filter is snapshotted
  at construction time.
- One `db.DB` can back multiple `settings.Service` instances, but two
  registered against the same `db.DB` will both stage edits independently
  against the same underlying rows — last `Apply`/OK wins. Nothing in this
  package coordinates that.
- A `PropertyEnum` whose valid choices can change between launches (an OS
  voice or font list, say) needs its `EnumOptions` refreshed before this
  package's `ListNodes`/`GetProperties`/`AllProperties` will reflect the
  change — there's no live-reload for that either. Use `db.DB`'s
  `UpdatePropertyOptions(nodeID, key, enumOptions)` (see `db.md`) at startup,
  before constructing the `settings.Service`; `StageProperty`/`Apply` only
  ever write `Value`, never `EnumOptions`.
