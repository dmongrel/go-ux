# `terminal` package

Import path: `go-ux/terminal`

A Wails v3 `Service` backing a terminal emulator: one or more PTY-backed
shell sessions (PowerShell, Git Bash, cmd.exe on Windows), rendered by the
frontend via `@xterm/xterm` (`uxdemo/frontend/src/views/terminal.ts`) — Go
owns only the PTY process lifecycle and shuttles raw bytes; there is no
VT100 parsing or grid rendering in this package anymore (unlike the original
Fyne version, which hand-rasterized the screen itself).

## Public API (Go)

```go
func DetectShells() []ShellDef

type ShellDef struct {
    Name    string
    Path    string
    Args    []string
    WorkDir string
    Env     map[string]string
}

func NewService(app *application.App, database *db.DB) *Service

func (s *Service) DetectShells() []ShellDef
func (s *Service) Start(shellName string, cols, rows int) (string, error)
func (s *Service) WriteInput(sessionID string, data string) error
func (s *Service) Resize(sessionID string, cols int, rows int) error
func (s *Service) CloseSession(sessionID string) error
func (s *Service) CloseOnExit() bool
func (s *Service) Close()
func (s *Service) CurrentFontSettings() FontSettings
func (s *Service) SetFontSettings(f FontSettings) error
func (s *Service) OpenWindow()

func RegisterSettings(database *db.DB) error
func ApplyFontSettings(database *db.DB) error
func DetectMonospaceFonts() []string
```

`DetectShells` (package-level) probes the current machine for runnable
shells — PowerShell (`pwsh.exe` preferred over `powershell.exe`), Git Bash,
and `cmd.exe` on Windows — returning only the ones actually found.
`Service.DetectShells` wraps it, reordering the configured `default_shell`
(if `RegisterSettings` has been called) to the front, for a frontend shell
picker.

`Start` spawns a new PTY session (via winpty) running `shellName` (matched
against `DetectShells`' `Name`; the first detected shell is used if empty or
not found) and returns a session ID. Output streams asynchronously via the
`terminal:data` event (`SessionOutput{SessionID, Data}` — multiple
sessions/tabs share one event channel, routed client-side by `SessionID`).
`WriteInput`/`Resize`/`CloseSession` operate on a session by ID.
`CloseOnExit` reports whether a tab whose shell process exits on its own
should be auto-closed — the frontend's `terminal:exit` handler checks it
before closing (`RegisterSettings`' seeded default is `true`; `false` if
`RegisterSettings` was never called). `Close` terminates every open
session — call it from `app.OnShutdown` so a closed terminal window never
leaves an orphaned shell process running.

`CurrentFontSettings`/`SetFontSettings` expose the live, shared font
configuration (every open session across every open terminal window renders
against the same value): `SetFontSettings` clamps, updates the live value,
broadcasts it to every open terminal window via the `terminal:font` event,
and persists it to `database`'s Terminal node (if `RegisterSettings` was
called) — the frontend calls it on Ctrl+scroll.

`OpenWindow` opens the terminal UI in its own window (`Title: "Terminal"`,
1024x700).

`RegisterSettings` seeds a root "Terminal" node with the package's full
22-row property set (unchanged from the original design — see
`terminal/settings_schema.go`'s exported `Key*` constants), idempotent. Only
`default_shell`, `close_on_exit`, and the four font properties drive live
behavior; the rest are seeded placeholders. `ApplyFontSettings` re-reads the
font properties and pushes them into the live shared font state —
`NewService` calls it once at construction.

`DetectMonospaceFonts() []string` probes installed fonts (Windows registry)
for genuinely monospace ones.

## Minimal usage

```go
database, err := db.Open("settings.sqlite")
if err := terminal.RegisterSettings(database); err != nil { log.Fatal(err) }

svc := terminal.NewService(app, database)
app.RegisterService(application.NewService(svc))
app.OnShutdown(func() { svc.Close() })
```

```ts
// hub.ts
import {OpenWindow} from "../../bindings/go-ux/terminal/service";
OpenWindow();
```

`terminal.ts` (see `uxdemo/frontend/src/views/terminal.ts`) owns: xterm.js
instance creation/sizing (`@xterm/addon-fit`), a tab strip with a shell
picker on "+", piping `term.onData`/`terminal:data` to/from `WriteInput`,
and a Ctrl+scroll wheel handler calling `SetFontSettings`.

## Constraints for callers

- **PTY backend is winpty, not the native ConPTY API** (unchanged from the
  original Fyne version — see `winpty_windows.go`'s doc comments for why).
- **Deferred / not implemented**: most of the 22-row settings surface has no
  consuming behavior yet (mouse reporting, hyperlink detection, shell
  integration, etc.) — see `RegisterSettings`' doc comment for the full
  list.
- **Windows only.** This package shells out to a native process via winpty;
  there is no other-OS implementation.
- `Start`/`WriteInput`/`Resize`/`CloseSession` are safe to call concurrently
  from multiple frontend windows against the same `Service` (guarded by an
  internal mutex over the session map) — but `SetFontSettings` is a single
  shared value across every session, not per-session.

## Attribution

Third-party code and design this package depends on or was informed by
(unchanged from the original Fyne version — the PTY backend itself did not
change in the Wails migration):

- **[winpty](https://github.com/rprichard/winpty)** (rprichard, MIT license)
  — the PTY backend on Windows. `winpty.dll`/`winpty-agent.exe` are vendored
  binaries, embedded via `go:embed` (`terminal/winpty/`, license copy at
  `terminal/winpty/LICENSE`); `winpty_windows.go` is this package's own Go
  bindings against winpty's C API.
- **[pty4j](https://github.com/JetBrains/pty4j)** (JetBrains, Eclipse Public
  License v1.0) — JetBrains' own winpty binding. No pty4j code is
  copied/vendored, but two correctness-critical designs in
  `winpty_windows.go` were ported from its source: the
  overlapped-I/O-plus-shutdown-event pattern for `Read`/`Write`/`Close`
  (avoids `STATUS_HEAP_CORRUPTION`), and the `winpty_set_size` retry loop
  before spawning the child process.
- **[IntelliJ Community Edition](https://github.com/jetbrains/intellij-community)**
  — the general design reference this repo's `CLAUDE.md` names for UX
  patterns; the terminal-specific research led to pty4j directly.
- **[xterm.js](https://xtermjs.org/)** (MIT license) — the VT100/xterm
  parser and renderer now used entirely in the frontend
  (`@xterm/xterm`/`@xterm/addon-fit`), replacing this package's own
  `vt10x`-based parser and hand-rasterized `canvas.Raster` renderer.
