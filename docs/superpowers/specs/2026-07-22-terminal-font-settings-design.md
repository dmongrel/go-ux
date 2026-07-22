# Terminal font settings: design

## Scope

Add a configurable terminal font (family, size, line-height multiplier,
column-width multiplier) to `go-ux/terminal`, exposed through the same
settings-registry mechanism `default_shell`/`close_on_exit` already use, plus
a live Ctrl+scrollwheel font-size adjustment that applies to every open
terminal session immediately and persists the new size without the user
having to open the settings dialog.

Windows only, matching this package's existing scope (winpty backend, no
mobile/Unix support yet).

## Defaults

| Setting | Key | Type | Default |
|---|---|---|---|
| Font family | `font_family` | enum | `"(default)"` — the existing Fyne-bundled font |
| Font size | `font_size` | int | `13` |
| Line height | `line_height` | float (new) | `1.0` |
| Column width | `column_width` | float (new) | `1.0` |

Line height and column width are multipliers of the font's natural
per-cell size (1.0 = unchanged), not absolute pixel or character values —
confirmed against JetBrains' JediTerm (`TerminalPanel.java`:
`myCharSize.height = ceil(fontMetricsHeight * lineSpacing)`, where
`getLineSpacing()`'s doc comment reads "vertical scaling factor", default
`1.0f`). JediTerm has no equivalent horizontal multiplier of its own —
`column_width` generalizes the same pattern for consistency, since IntelliJ
newer versions do document a "Letter Spacing (Character Width)" setting
whose implementation isn't in the open-source jediterm repo to inspect
directly.

## Components

### 1. Font detection — `terminal/font_windows.go` (new)

```go
func DetectMonospaceFonts() []string
```

Mirrors `DetectShells()`'s shape: a plain function, no error return, empty
result handled gracefully by callers. Implementation:

1. Enumerate font registry entries under
   `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`
   (system-installed) and
   `HKEY_CURRENT_USER\SOFTWARE\Microsoft\Windows\CurrentVersion\Fonts`
   (per-user installed), via `golang.org/x/sys/windows/registry` — no cgo,
   matching `winpty_windows.go`'s existing no-cgo convention. Each value maps
   a display name to a filename (relative to `%WINDIR%\Fonts` for the system
   key, absolute for the user key).
2. For each candidate file, parse it with `golang.org/x/image/font/opentype`
   (already a dependency — `render.go` uses it for the bundled font today)
   and check whether it's genuinely monospace: compare the glyph advance
   width of a handful of representative ASCII characters (letters, digits,
   punctuation); if they don't all match, the font is filtered out.
3. Cache the result in a package-level, `sync.Once`-guarded slice — font
   installs don't change while the app is running, so this scans once per
   process, not once per settings-dialog open.

A font family that fails to load later (uninstalled between scan and use,
or the registry scan itself fails) falls back to the bundled font, the same
graceful-degradation pattern `loadMonospaceFace`'s existing opentype-parse
fallback already uses.

### 2. `db.PropertyFloat` — `db/db.go`, `settings/settings.go`

Extends the shared `PropertyType` enum (currently `Bool`/`String`/`Int`/
`Enum`) with a `Float` variant, and `settings.Window.propertyWidget`'s type
switch gets a new case rendering it as a `widget.Entry` with a
`strconv.ParseFloat` validator — mirrors the existing `PropertyInt` case
almost exactly. This is a small, backward-compatible addition to a shared
package: existing property types and stored values are unaffected.

### 3. Settings schema — `terminal/settings_schema.go`

Four new property keys added to the existing "Terminal" root node,
alongside `default_shell`/`close_on_exit`:

```go
const (
    KeyFontFamily  = "font_family"
    KeyFontSize    = "font_size"
    KeyLineHeight  = "line_height"
    KeyColumnWidth = "column_width"
)
```

`RegisterSettings` seeds them with the defaults above (`font_family`'s enum
options are `DetectMonospaceFonts()`'s result plus a leading `"(default)"`
sentinel, which is also the seeded value). `readTerminalSettings` is
extended to also return a `FontSettings` value (see below). Both keep the
existing idempotent/graceful-fallback behavior the two current properties
already have.

### 4. Live shared font state — `terminal/font.go` (new)

```go
type FontSettings struct {
    Family      string  // "" or "(default)" = bundled font
    Size        int     // point size
    LineHeight  float64 // multiplier
    ColumnWidth float64 // multiplier
}
```

A package-level, mutex-guarded "current" `FontSettings`, plus a registry of
every live `*Session` that reads from it. This is what makes "changing the
font size affects every open tab, in every open window" possible without
threading a shared object through every constructor — same category of
package-level shared state this package already has (`uiMu`, `winptyMu`),
consistent with the existing design.

- `NewSession` registers the new `*Session` against this shared state (and
  applies whatever the current values are, immediately, so a freshly opened
  tab matches already-open ones) and reads it for its own font metrics
  instead of `render.go`'s previous hardcoded `defaultCellHeight`; `Close`
  unregisters it.
- Changing the shared state (from Ctrl+scroll, or from a host app applying
  settings — see below) notifies every registered session to recompute its
  font face/metrics and re-layout — each session's own `doUI`-guarded
  refresh path, already established.
- A package-level `*db.DB` reference, set only by `NewWindowFromSettings`/an
  explicit new `ApplyFontSettings(database *db.DB) error` function (nil
  otherwise) — this is what keeps `terminal`'s existing "the db registry is
  optional" design intact: `NewWindow`-only callers get live, in-process
  font changes with no persistence at all, exactly like today when no db is
  involved in the first place. `ApplyFontSettings` is also what a host app
  calls after a Settings-window OK/Apply to push newly staged values into
  the live state (re-reading `readTerminalSettings`), separate from
  `NewWindowFromSettings`'s own one-time initial read at window-open time.

### 5. Ctrl+scrollwheel — `terminal/widget.go`

Fyne's `Scrollable.Scrolled(*ScrollEvent)` carries no modifier-key
information (`ScrollEvent` is just `PointEvent` + `Scrolled Delta`), so
detecting "Ctrl held during this scroll" needs independent modifier
tracking. `Session` additionally implements `desktop.Keyable`
(`KeyDown(*fyne.KeyEvent)`, `KeyUp(*fyne.KeyEvent)`) alongside its existing
`fyne.Focusable`/`Shortcutable`, maintaining a `ctrlHeld bool` set by
comparing `event.Name` against `desktop.KeyControlLeft`/`KeyControlRight`.
`Scrolled` checks that flag:

- Not held: no-op (this package has no scrollback/mouse-reporting yet, per
  `terminal.md`'s already-documented deferred scope — Ctrl+scroll is the
  first scroll-wheel behavior this package implements at all).
- Held: `Scrolled.DY`'s sign picks direction; one tick = 2pt, clamped to the
  8–36pt range (IntelliJ's own Ctrl+scroll zoom step/bounds live in the
  closed-source platform layer, not the open-source jediterm/pty4j repos, so
  this isn't independently verifiable against their exact numbers — these
  are the user-specified fallback values). Updates the shared `FontSettings`
  immediately (live redraw across every session), and (re)starts a debounce
  timer that persists to the package-level db reference — if any — a short
  idle period after the last scroll tick, rather than writing on every tick.

### 6. Renderer changes — `render.go`, `widget.go`

- `loadMonospaceFace` gains a `family string` parameter (empty string keeps
  today's bundled-font path unchanged); non-empty loads that family's file
  via the same registry lookup `font_windows.go` uses, falling back to the
  bundled font on any failure.
- `gridRenderer.paint` (already rewritten in the prior session to compute
  `cellW := w/cols`, `cellH := h/rows` from the raster's actual requested
  pixel size) applies `LineHeight`/`ColumnWidth` as multipliers on top of
  that: `cellH *= LineHeight`, `cellW *= ColumnWidth`.
- `gridDims` (`widget.go`, converts a pixel size + natural per-cell box into
  a column/row count) needs the same multipliers factored in when computing
  how many columns/rows fit a given pixel size — a taller effective cell
  means fewer rows fit the same height, and the multipliers must be
  consistent between "how big is a cell" (paint) and "how many cells fit"
  (gridDims), or the grid and the raster's actual per-cell drawing size
  drift apart (the exact class of bug the prior session's cursor-drift fix
  addressed).

## Data flow

```
Settings dialog (OK/Apply)
    │  writes font_family/font_size/line_height/column_width to db
    ▼
Host app calls terminal.ApplyFontSettings(database)
    │  re-reads the four properties, updates the shared FontSettings
    ▼
Every registered *Session notified → recomputes font face/metrics →
re-layout (doUI-guarded, same path PTY-output-driven repaints already use)

Ctrl+scroll on any Session
    │  updates shared FontSettings.Size directly (no db round-trip)
    ▼
Every registered *Session notified → re-layout
    │  (debounced) 
    ▼
db write-through, if a db is registered — independent of any Settings
window that might be open with unsaved staged edits (see below)
```

## Error handling

- `DetectMonospaceFonts()` returns an empty slice on any registry/enumeration
  failure rather than an error — matches `DetectShells()`'s own
  never-fails-the-caller contract. `font_family`'s enum then only ever
  offers `"(default)"`.
- A font family that fails to load at render time (uninstalled since the
  registry scan, or a corrupt file) falls back to the bundled font — same
  fallback `loadMonospaceFace` already has for its own bundled-resource
  parse failure.
- Out-of-range `font_size`/`line_height`/`column_width` values (e.g. a
  hand-edited db row) are clamped, not rejected, whenever read —
  consistent with how a caller is expected to tolerate a partially-invalid
  registry already (`readTerminalSettings`'s existing graceful-fallback
  pattern). Bounds: `font_size` to the same 8–36pt range Ctrl+scroll itself
  is clamped to (one range, used everywhere font size is read, not two
  separate limits); `line_height`/`column_width` to 0.5–3.0 (anything
  outside that is either illegibly cramped or wastes most of the widget on
  blank space between cells).
- The Settings-window-staging-vs-live-write race (user has unsaved edits
  open in Settings while Ctrl+scrolling in a terminal) is left unresolved by
  design, per explicit direction: whichever action the user does last to
  actually commit (click OK/Apply, or another scroll) wins — no attempt to
  detect or merge the conflict.

## Testing

- `font_windows.go`: unit tests against a small set of known-monospace and
  known-proportional TTFs (fixtures, not relying on whatever happens to be
  installed on the CI/dev machine) to verify the metrics-based filter.
- `db`/`settings`: `PropertyFloat` round-trips through `AddProperty`/
  `GetProperties` and renders as a validated `Entry` — same shape as the
  existing `PropertyInt` tests.
- `render.go`/`widget.go`: extend the existing pure-state tests
  (`TestGridRendererResizeKeepsSizesConsistent`-style) to cover
  `LineHeight`/`ColumnWidth` multipliers changing `gridDims`'s output and
  `paint`'s per-cell math, independent of any live PTY or GUI.
- Ctrl+scroll's actual modifier-key detection and live cross-session
  broadcast are GUI-dependent behavior verified manually (same
  "visual correctness needs a human at a GUI" carve-out this package's other
  visual tests already use) — but the debounce/clamp/db-write logic
  triggered by a font-size change is unit-testable independent of the real
  scroll-event plumbing.

## Out of scope

- Per-tab/per-session font overrides (explicitly ruled out — global only).
- Non-Windows font enumeration (no Unix/macOS backend for this package yet
  at all).
- Any UI for browsing/uploading a font file directly (system-installed only).
- Resolving the Settings-staging-vs-live-write race described above.
