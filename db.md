# `db` package

Import path: `go-ux/db`

The general-purpose persistence layer for `go-ux` components. It owns all
SQLite access (via the pure-Go, no-cgo driver in `internal/sqlite`) — no
other package, and no consumer app, should open the SQLite file directly.
One `*db.DB` handle is meant to be shared by every `go-ux` component in a
process (the settings window, and any other window/dialog you add).

It stores two independent kinds of data in the same SQLite file:

1. The **settings registry** — used by `go-ux/settings` (see `settings.md`).
2. **Per-component UI state** — an opaque blob store any `go-ux` window can
   use to remember things like its size, position, or last-used state. This
   is the part relevant if you're persisting a window's position/size.

## Opening the database

```go
database, err := db.Open("path/to/app.sqlite") // creates the file/schema if missing
// or, for tests / ephemeral use:
database, err := db.Open(":memory:")
if err != nil { ... }
defer database.Close()
```

`Open` is idempotent — safe to call against an existing file; it only
creates tables that don't already exist (`CREATE TABLE IF NOT EXISTS`).

## Saving a window's position/size: the UI-state API

```go
func (d *DB) SaveUIState(componentID string, blob []byte) error
func (d *DB) LoadUIState(componentID string) ([]byte, error)
```

This is the mechanism for persisting arbitrary per-window UI state —
position, size, sidebar widths, last-opened file, scroll offset, anything.
The value is an **opaque `[]byte` blob**: `db` does not interpret it. You
choose the encoding (JSON is the convention used elsewhere in this repo —
see `settings/settings.go`'s `uiState` struct — but any `[]byte` works).

### The UUID requirement

**Every caller of `SaveUIState`/`LoadUIState` must supply a stable
`componentID` string, and it must be a hardcoded UUID identifying that
specific window/component — not derived from window title, not generated at
runtime.** This is the primary key in the `ui_state` table
(`component_id TEXT PRIMARY KEY`), so:

- If you use a random ID generated fresh each run, you'll never find your
  saved state again (`LoadUIState` will always return `nil, nil` — no rows).
- If two different windows share an ID, they'll silently overwrite each
  other's state.
- The ID does not need to be registered anywhere else; just define it as an
  unexported `const` next to the window/component that owns it. Follow the
  pattern in `settings/settings.go`:

  ```go
  const componentID = "b6f6c9d1-3f2b-4b8a-9e3a-2f1c7a5d9e10"
  ```

  Generate a fresh UUID once (e.g. `uuidgen`, or any online/offline UUIDv4
  generator) when you add a new window, paste it in as a literal, and never
  change it — changing it orphans any previously saved state for that
  window.

### Example: persisting a window's size on close, restoring it on open

```go
const myWindowID = "3f9c0a2e-6b1d-4e77-9e2a-5d6c7b8a9f01" // generated once, hardcoded

type uiState struct {
	Width, Height float32
}

func restoreSize(database *db.DB, win fyne.Window) {
	blob, err := database.LoadUIState(myWindowID)
	if err != nil || blob == nil {
		return // no saved state yet (or a real error you may want to log)
	}
	var s uiState
	if err := json.Unmarshal(blob, &s); err != nil {
		return
	}
	if s.Width > 0 && s.Height > 0 {
		win.Resize(fyne.NewSize(s.Width, s.Height))
	}
}

func saveSize(database *db.DB, win fyne.Window) {
	size := win.Canvas().Size()
	blob, _ := json.Marshal(uiState{Width: size.Width, Height: size.Height})
	_ = database.SaveUIState(myWindowID, blob)
}

// wire it up:
restoreSize(database, win)
win.SetCloseIntercept(func() {
	saveSize(database, win)
	win.Close()
})
```

Note the save is **live**, not staged: call `SaveUIState` whenever the state
changes (or, as above, once on close) — there is no Apply/Cancel concept for
UI state the way there is for settings properties. `LoadUIState` returns
`(nil, nil)` (no error) when nothing has been saved yet for that
`componentID`, so always check for a nil blob before unmarshalling.

Fyne's desktop driver has no cross-platform window-position API, so only
size (and, in the settings window's case, sidebar-splitter offset) is
practical to persist this way — see `settings.md` for that concrete
example.

## The settings registry API (for reference)

Used by `go-ux/settings`; documented fully in `settings.md`. Summary of the
methods that touch it:

```go
func (d *DB) ListSettings() ([]Node, error)
func (d *DB) GetProperties(nodeID int64) ([]Property, error)
func (d *DB) SaveProperties(nodeID int64, values map[string]string) error
func (d *DB) AddNode(parentID *int64, description string, sortOrder int) (int64, error)
func (d *DB) AddProperty(nodeID int64, key, label string, ptype PropertyType, value string, enumOptions []string) error
```

`Node` and `Property` types, and the `PropertyType` enum
(`PropertyBool`/`PropertyString`/`PropertyInt`/`PropertyEnum`), are exported
from this package. Use `AddNode`/`AddProperty` to seed the registry before
opening a `settings.Window` — see `go-ux/test.SeedExample` for a working
example.

## Constraints for callers

- Don't open the SQLite file this package manages with any other SQLite
  library/connection — `db.DB` assumes it's the only writer.
- `SaveProperties` and the UI-state writes are independent; there's no
  cross-domain transaction tying a settings Apply to a UI-state save.
- All methods are safe to call from the Fyne main/UI goroutine as currently
  written (they're synchronous, blocking SQLite calls) — there is no
  internal async/queueing. If you call them from a background goroutine and
  then touch Fyne widgets with the result, follow Fyne's `fyne.Do` threading
  guidance (see the repo's `CLAUDE.md`).
