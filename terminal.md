# `terminal` package

Import path: `go-ux/terminal`

A Fyne terminal-emulator window: one or more shell sessions (PowerShell, Git
Bash, cmd.exe) rendered as VT100/xterm-style grids, each in its own tab. Like
`go-ux/settings`, its windows are non-blocking (`NewWindow`+`Show`). Like
`go-ux/settings`, it can optionally read its configuration (default shell,
close-on-exit) from a `*go-ux/db.DB` registry — but unlike `settings`, that
registry is optional: `terminal.Window` works standalone with a plain
`[]ShellDef`, with no `db` dependency at all, if a caller doesn't need
persisted configuration.

## Public API

```go
func DetectShells() []ShellDef

type ShellDef struct {
    Name    string
    Path    string
    Args    []string
    WorkDir string
    Env     map[string]string
}

func NewSession(def ShellDef) (*Session, error)
func (s *Session) Close() error
func (s *Session) OnExit(fn func())
func (s *Session) Title() string
// Session also implements fyne.CanvasObject and fyne.Focusable.

func NewTabView(shells []ShellDef) *TabView
func (t *TabView) AddTab(def ShellDef) *Session
func (t *TabView) CloseTab(s *Session)

func NewWindow(app fyne.App, shells []ShellDef) (*Window, error)
func NewWindowFromSettings(app fyne.App, database *db.DB) (*Window, error)
func (w *Window) SetSize(width, height float32) *Window
func (w *Window) Show()

func RegisterSettings(database *db.DB) error
func ApplyFontSettings(database *db.DB) error
func DetectMonospaceFonts() []string
```

`DetectShells` probes the current machine for runnable shells — PowerShell
(`pwsh.exe` preferred over `powershell.exe`), Git Bash, and `cmd.exe` on
Windows — returning only the ones actually found. Callers should handle a
partial (even empty) result gracefully; `Window`/`TabView` do. It leaves
`WorkDir` empty on every `ShellDef` it returns.

`ShellDef.WorkDir` is the directory a spawned shell starts in. Left empty
(the default `DetectShells()` and `NewWindowFromSettings` both use), the
spawned shell inherits whatever directory the *host process* itself was
launched from (winpty's `cwd` is passed as `NULL`, which is `CreateProcess`'s
own "inherit the caller's current directory" behavior) — not the project a
host app might currently have open, and not anything this package tracks or
infers on its own. A host app that wants shells to start in a specific
directory (e.g. the currently-open project's root) must set `WorkDir`
itself on each `ShellDef` before spawning:

```go
shells := terminal.DetectShells()
for i := range shells {
    shells[i].WorkDir = currentProjectDir
}
win, err := terminal.NewWindow(app, shells)
```

`NewWindowFromSettings` doesn't expose an equivalent override — it always
calls `DetectShells()` internally with no way to inject a `WorkDir` — so a
caller that wants both the settings-registry integration (`default_shell`/
`close_on_exit`) and a specific starting directory needs the manual
`DetectShells()` + `WorkDir` + `NewWindow` path above instead.

`NewSession` spawns def's shell process (via winpty) and returns a `Session`:
a Fyne widget rendering that shell's live VT-parsed screen grid. `OnExit`'s
`fn` fires (via `fyne.Do`) when the shell process exits on its own — useful
for auto-closing a tab, which `TabView` does when `close_on_exit` is set (see
below). `Close` terminates the process if still running; safe to call more
than once.

`NewTabView` builds a `container.DocTabs`-backed multi-tab container, one
tab per shell in `shells` (`shells` may be empty). Its built-in "+" button
spawns another instance of `shells[0]`'s shell (a no-op if `shells` was
empty). `AddTab`/`CloseTab` add/remove tabs programmatically.

`NewWindow` wraps a `TabView` in a plain `fyne.Window` (1024x700 default).
`NewWindowFromSettings` is a second constructor: it still calls
`DetectShells()` itself for the actual runnable-shell list, but reads
`default_shell` (reordered to the front, for the "+" button and initial tab)
and `close_on_exit` from `database`'s registry, as seeded by
`RegisterSettings`. If `RegisterSettings` was never called against
`database`, `NewWindowFromSettings` falls back to `DetectShells()`'s own
ordering and close-on-exit off, rather than failing.

`SetSize`/`Show` behave exactly like `settings.Window`'s: `SetSize` is a
chainable override (both `width` and `height` must be positive or it's a
no-op), `Show` is non-blocking.

`RegisterSettings` seeds a root "Terminal" node with `default_shell`
(`PropertyEnum`, options from `DetectShells()`), `close_on_exit`
(`PropertyBool`, default `"true"`), and four font properties — `font_family`
(`PropertyEnum`, options from `DetectMonospaceFonts()` plus a `"(default)"`
sentinel meaning the bundled font, which is also the seeded default),
`font_size` (`PropertyInt`, default `13`), `line_height` and `column_width`
(`PropertyFloat`, both default `1.0`, multipliers of the font's natural
per-cell size) — in `database`'s registry, if one isn't already present
(idempotent — safe on every app startup, same as `test.SeedExample`'s role
for `test_settings.go`).

`ApplyFontSettings(database *db.DB) error` re-reads those four font
properties and pushes them into the live, process-wide font state every
open `Session` renders against — `NewWindowFromSettings` calls it once at
construction; a host app calls it again after a Settings-window OK/Apply
commits new font values, so open terminal windows pick up the change
without needing to be reopened. Ctrl+scrollwheel over any `Session` also
adjusts font size live (2pt per tick, clamped 8–36pt) across every open
session at once, debounce-persisting to whichever database
`NewWindowFromSettings` was last called with (nothing persists if no
`NewWindowFromSettings` call has been made at all — matches `Window`'s
existing "the db registry is optional" design).

`DetectMonospaceFonts() []string` mirrors `DetectShells()`'s shape: probes
installed fonts (system-wide and per-user) for genuinely monospace ones,
returning only their names, gracefully empty on any enumeration failure.

## Minimal usage

```go
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

	"go-ux/db"
	"go-ux/terminal"
)

func main() {
	database, err := db.Open("settings.sqlite") // or ":memory:"
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	if err := terminal.RegisterSettings(database); err != nil {
		log.Fatal(err)
	}

	fyneApp := app.NewWithID("your.app.id") // NewWithID, not New — see db.md

	win, err := terminal.NewWindowFromSettings(fyneApp, database)
	if err != nil {
		log.Fatal(err)
	}
	win.Show()

	fyneApp.Run()
}
```

A `db`-free caller can skip `RegisterSettings`/`NewWindowFromSettings`
entirely and just do:

```go
win, err := terminal.NewWindow(fyneApp, terminal.DetectShells())
```

## Rendering

Each `Session` wraps a `vt10x.State` (a pure-Go VT100/xterm parser, no cgo)
fed by a background goroutine reading the shell's winpty output
(`readLoop`). The screen grid is hand-rasterized into a `canvas.Raster` each
refresh (not per-cell/per-row Fyne widgets) using Fyne's bundled monospace
font. Refreshes are debounced (capped at roughly 30-60Hz) rather than firing
once per PTY read, since a chatty process can produce thousands of writes per
second. The cursor is drawn as a separate blinking `canvas.Rectangle`
overlay, not baked into the raster image.

`readLoop`'s goroutine parses PTY bytes into the VT state directly (no Fyne
objects touched there); the redraw that follows goes through `fyne.Do`, since
it touches a `CanvasObject` from a non-UI goroutine. Keyboard input
(`TypedRune`/`TypedKey`, below) already runs on the Fyne UI goroutine and
needs no `fyne.Do`.

## Keyboard input

`Session` implements `fyne.Focusable` — click a tab's content to focus it,
then type. Supported translation from Fyne key events to bytes written to
the shell's stdin:

| Input | Bytes |
|---|---|
| Printable rune | UTF-8 verbatim |
| Enter | `\r` |
| Backspace | `\x7f` |
| Tab | `\t` |
| Escape | `\x1b` |
| Arrows (Up/Down/Right/Left) | `\x1b[A` / `\x1b[B` / `\x1b[C` / `\x1b[D` |
| Home/End | `\x1b[H` / `\x1b[F` |
| Delete | `\x1b[3~` |
| Page Up/Down | `\x1b[5~` / `\x1b[6~` |
| Ctrl+letter | `rune - 'a' + 1` (Ctrl+C → `0x03`, etc.) |
| Ctrl+`[` / `\` / `]` | `0x1b` / `0x1c` / `0x1d` |

This is a deliberately pragmatic subset — no F-keys, no shift-arrow
selection, no Alt-as-Meta. See "Constraints for callers" below for the fuller
list of what's not implemented.

## Constraints for callers

- **PTY backend is winpty, not the native ConPTY API.** This package bundles
  its own copy of [winpty](https://github.com/rprichard/winpty) (MIT-licensed
  binaries embedded via `go:embed`, extracted to a per-user cache directory
  on first use — see `winpty_windows.go`), the same approach IntelliJ takes
  of shipping its own PTY-hosting agent rather than depending on the host's
  ConPTY support. That extraction step means `NewSession`'s error can now
  also come from failing to write/load the embedded DLL (e.g. a locked-down
  cache directory), not only from the spawn itself. This package's
  rendering/input layers are additionally verified against synthetic VT byte
  sequences fed directly into the parser, independent of any live PTY
  transport.
- **Deferred / not implemented**: cursor shape variants and contrast
  handling, hyperlink detection, mouse reporting, text selection and
  copy/paste, terminal bell, font fallback and configurable line-height,
  OSC 133/OSC 7 shell-integration (command-boundary markers, live-cwd
  capture for UI state). `RegisterSettings` seeds only two properties
  (`default_shell`, `close_on_exit`) — a placeholder slice of a much larger
  planned settings surface (~22 properties), not the full design.
- **Known open rendering bugs**, found via manual visual testing (see
  render.go and vtstate.go), not yet root-caused:
  - Glyph/line rendering is visually inconsistent — some horizontal lines
    render at 2px where they should be 1px, inconsistently across the grid.
    Suspected cause: `gridRenderer` rasterizes into a fixed-size
    `image.RGBA` at `cellW`/`cellH` *device* pixels (render.go's
    `loadMonospaceFace`, `DPI: 72` i.e. 1 point == 1 pixel), but
    `canvas.Raster` then nearest-neighbor-scales (`ImageScalePixels`) that
    image to whatever *logical* size Fyne's layout assigns the widget —
    `Session.Resize`'s `gridDims` mixes the two unit spaces (dividing a
    logical `fyne.Size` by device-pixel `cellW`/`cellH`) without accounting
    for `canvas.Scale()`. A non-1:1 or non-integer logical-to-device ratio
    would make nearest-neighbor scaling round row/column boundaries
    inconsistently — matches the reported symptom, but not yet confirmed.
  - ~~The cursor can end up positioned north of (above) the actual prompt
    line after a scrolling command~~ — **fixed**: `refreshCursor` (widget.go)
    was positioning the cursor overlay from the raw per-cell font metrics
    (`cellW`/`cellH`) rather than the widget's actual current size divided
    by the grid dimensions. `canvas.Raster` stretches its rasterized image
    to whatever size Fyne's layout actually assigns the widget, which isn't
    always exactly `cols*cellW x rows*cellH` (that's only `MinSize`); the
    drift grew with row number and was worst at the bottom row — exactly
    where scrolled output leaves the cursor, matching the symptom.
- **Desktop only — more strongly than this repo's other packages.**
  `settings`/`dialog` are untested against Fyne's mobile driver but could in
  principle run there; `terminal` cannot, even in principle: it shells out to
  a native process via winpty (Windows) / a PTY (the eventual Unix
  equivalent), and no mobile OS exposes that primitive to an app. This
  package assumes native desktop Fyne (resizable window, real OS process
  spawning) unconditionally.
- `AddTab`/`CloseTab` and `Window`'s exported methods are expected to be
  called on the Fyne UI goroutine, same as every other exported method on
  `settings.Window`/`dialog.Dialog` — they touch `CanvasObject`s directly
  with no `fyne.Do` wrapping of their own.
- `NewWindowFromSettings` must be called after `RegisterSettings` (or an
  equivalently-shaped "Terminal" node) exists in `database` for its
  registry-sourced defaults to take effect; otherwise it silently falls back
  to `DetectShells()`'s own ordering and close-on-exit off.

## Attribution

Third-party code and design this package depends on or was informed by:

- **[winpty](https://github.com/rprichard/winpty)** (rprichard, MIT license)
  — the actual PTY backend on Windows. `winpty.dll`/`winpty-agent.exe` are
  vendored binaries, embedded via `go:embed` (`terminal/winpty/`, license
  copy at `terminal/winpty/LICENSE`); `winpty_windows.go` is this package's
  own Go bindings against winpty's C API, not winpty's own code.
- **[pty4j](https://github.com/JetBrains/pty4j)** (JetBrains, Eclipse Public
  License v1.0) — JetBrains' own winpty binding, the one IntelliJ's terminal
  uses. No pty4j code is copied or vendored (this package has no Java/JVM
  dependency and pty4j's own EPL terms aren't triggered), but two
  correctness-critical designs in `winpty_windows.go` were read directly
  from pty4j's source and ported to Go because independently-arrived-at
  approaches here were provably wrong: the overlapped-I/O-plus-shutdown-
  event pattern for `Read`/`Write`/`Close` (mirroring pty4j's
  `NamedPipe.java`, needed to avoid `STATUS_HEAP_CORRUPTION` from closing a
  pipe handle while a synchronous read/write was still pending on it — see
  `overlappedIO`'s doc comment), and the `winpty_set_size` retry loop called
  before spawning the child process (mirroring pty4j's `WinPty.java`,
  documented there as a workaround for a winpty/console rendering bug —
  fixed PowerShell's console host never painting its first prompt).
- **[vt10x](https://github.com/hinshun/vt10x)** (James Gray, MIT license) —
  the VT100/xterm parser this package wraps (`vtstate.go`); a normal Go
  module dependency (see `go.mod`), not vendored or modified.
- **[golang.org/x/image](https://pkg.go.dev/golang.org/x/image)** (the Go
  team, BSD-3-Clause) — `font/opentype` for loading Fyne's bundled monospace
  font at a specific pixel size, and `font/basicfont` as the fallback face
  if that parse ever fails (`render.go`'s `loadMonospaceFace`); a normal Go
  module dependency, not vendored or modified.
- **[IntelliJ Community Edition](https://github.com/jetbrains/intellij-community)**
  — the general design reference this repo's own `CLAUDE.md` names for
  UX/design patterns (settings panel layout, control panel behavior); the
  terminal-specific research above led to pty4j directly rather than
  intellij-community itself, since pty4j (not intellij-community, which only
  depends on it) is where IntelliJ's actual winpty integration code lives.
