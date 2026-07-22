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
```

`DetectShells` probes the current machine for runnable shells — PowerShell
(`pwsh.exe` preferred over `powershell.exe`), Git Bash, and `cmd.exe` on
Windows — returning only the ones actually found. Callers should handle a
partial (even empty) result gracefully; `Window`/`TabView` do.

`NewSession` spawns def's shell process (via ConPTY) and returns a `Session`:
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

`RegisterSettings` seeds a root "Terminal" node with two properties
(`default_shell` — `PropertyEnum`, options from `DetectShells()`;
`close_on_exit` — `PropertyBool`, default `"true"`) in `database`'s registry,
if one isn't already present (idempotent — safe on every app startup, same
as `test.SeedExample`'s role for `test_settings.go`).

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
fed by a background goroutine reading the shell's ConPTY output
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

- **ConPTY output is not guaranteed to arrive reliably on every Windows
  build/machine.** ConPTY's attach-handshake bytes are reliable, but some
  Windows builds/environments have been observed not to deliver a live
  shell's subsequent output/echo through the pipe consistently. If a
  `Session` opens but its grid never updates after the initial handshake,
  that is a known ConPTY-attach limitation on the host environment, not
  necessarily a bug in this package — this package's rendering/input layers
  are verified against synthetic VT byte sequences fed directly into the
  parser, independent of any single machine's ConPTY behavior, but end-to-end
  liveness on a given machine is not something this package can guarantee.
- **Deferred / not implemented**: cursor shape variants and contrast
  handling, hyperlink detection, mouse reporting, text selection and
  copy/paste, terminal bell, font fallback and configurable line-height,
  OSC 133/OSC 7 shell-integration (command-boundary markers, live-cwd
  capture for UI state). `RegisterSettings` seeds only two properties
  (`default_shell`, `close_on_exit`) — a placeholder slice of a much larger
  planned settings surface (~22 properties), not the full design.
- **Desktop only — more strongly than this repo's other packages.**
  `settings`/`dialog` are untested against Fyne's mobile driver but could in
  principle run there; `terminal` cannot, even in principle: it shells out to
  a native process via ConPTY (Windows) / a PTY (the eventual Unix
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
