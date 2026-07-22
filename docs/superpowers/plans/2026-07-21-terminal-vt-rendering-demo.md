# `terminal` Package — VT Rendering, Widget, Demo

**Goal:** get from the PTY-only foundation (`docs/superpowers/plans/2026-07-20-terminal-pty-foundation.md`, landed on `master`) to something the user can actually run and look at: a window with **Open Terminal** / **Open Settings** buttons, where Open Terminal shows a real, VT-rendered shell.

**Source of truth for design decisions:** `C:\Users\jcaesar\.claude\plans\create-a-new-plan-reflective-fern.md` (the full 10-phase design — read the relevant phase section there for background/rationale; this plan's task briefs give the concrete files/interfaces/done-bar). This plan carves out the phases needed to reach a visible demo — Phases 4-6 and 10, plus a minimal slice of 7 — and defers the rest. Don't re-litigate scope decisions already made there (ConPTY-direct, `vt10x`, `canvas.Raster`, `container.NewDocTabs`, etc.) without discussion.

**What this plan does NOT cover** (explicit non-goals, deferred to later plans):
- Design Phase 8 (cursor shape, contrast, hyperlinks, mouse reporting, selection/copy, bell, font fallback/line-height)
- Design Phase 9 (OSC 133/OSC 7 shell integration, command separators, live-cwd UI-state capture)
- Full Phase 7 settings surface (all 22 properties) — this plan registers/uses only what's needed for the demo to have a non-empty, functional Settings dialog
- Embedding `Session`/`TabView` directly inside a host app's own layout (design defers this to a later manual check too)

## Global Constraints

- Go 1.26, module `go-ux`. `golang.org/x/sys/windows` is already a direct dependency (from the PTY foundation plan) — no version bump.
- New dependency this plan introduces: `github.com/hinshun/vt10x` (pure Go, no cgo) — add via `go get`, confirm it lands as a direct `require` in `go.mod`.
- No cgo, no C compiler, anywhere — `CGO_ENABLED=0` must build the whole repo throughout.
- Follow existing repo doc-comment style (see `dialog/dialog.go`, `settings/settings.go`): godoc on every exported symbol, comments explain *why* not *what*.
- `fyne.Do`/`fyne.DoAndWait` is REQUIRED for any code path that touches a `CanvasObject`/widget from a goroutine other than the Fyne UI goroutine — this is the first package in the repo that actually needs it for real. Keyboard-input handlers (`TypedRune`/`TypedKey`) already run on the UI goroutine and need no `fyne.Do`; only PTY-output-driven reads flowing back into the widget do. State which regime each new goroutine/callback is in, in a comment, at the point it's introduced.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` clean after every task.
- **Known environment constraint, carried forward from the PTY foundation plan:** on this dev machine (Windows build 10.0.26200.8875), ConPTY does not reliably deliver a live shell's real output/echo through the pipe — only ConPTY's own attach-handshake bytes reliably arrive (see `terminal/session_windows_test.go`'s `TestNewPtySessionSpawnsShellAndProducesOutput` doc comment for the full history). Build and unit-test the rendering/input layers against whatever *does* arrive, but do not treat "a live interactive shell doesn't render end-to-end on this machine" as a task-blocking failure — verify against synthetic/injected VT byte sequences instead (feeding bytes directly into `vtstate.go`'s parser, bypassing ConPTY) so the rendering and keyboard-input layers are provably correct independent of this machine's ConPTY limitation. Note clearly in each task's report which verification path was used.
- GUI/visual correctness (does text actually render legibly, are colors right, does resize look correct) needs a human at a Windows GUI — that is out of scope for automated tests and for subagent self-verification claims. Subagents should state what they verified (build/vet/test, and structural checks like "grid contains expected runes") and explicitly NOT claim visual correctness they can't observe.

---

### Task 1: VT state + rendering widget

**Files:**
- Create: `terminal/vtstate.go`
- Create: `terminal/render.go`
- Create: `terminal/widget.go`

**Interfaces:**
- Consumes: `ptySession` (`terminal/session.go`, from the PTY foundation plan) — specifically its `Read([]byte) (int, error)` method as the byte source.
- Produces: `type Session struct { ... }` (embeds `widget.BaseWidget`, implements `fyne.CanvasObject`) with `func NewSession(def ShellDef) (*Session, error)`, `func (s *Session) Close() error`, `func (s *Session) OnExit(fn func())` (fn invoked via `fyne.Do`), `func (s *Session) Title() string`. `Session` does not implement `fyne.Focusable` yet — that's Task 2.

Design Phase 4 (see reflective-fern.md) covers the rationale for wrapping `vt10x` and hand-rasterizing via `canvas.Raster` instead of per-cell/per-row Fyne objects — do not redesign that choice here.

- **`vtstate.go`**: wraps `vt10x.State` (or whatever the actual package API surface turns out to be — confirm exact type/method names against the real `github.com/hinshun/vt10x` source when you add the dependency, the design doc's naming is illustrative, not verbatim). Feeds it bytes read from `ptySession.Read` in a background goroutine (`readLoop`), exposes a way for `render.go` to get a consistent snapshot of the current grid (cell rune + fg/bg + attributes) for repainting.
- **`render.go`**: a `gridRenderer` holding the `vtstate` and a cached `image.RGBA`; on each refresh, walks visible cells and draws glyphs using Fyne's bundled monospace-capable font resource (confirm the exact loading API against the vendored Fyne source during implementation), feeds the image to `canvas.NewRaster`. Cursor is a separate small `canvas.Rectangle` overlay with its own blink ticker, not baked into the raster image.
- **`widget.go`**: `Session` ties `readLoop` (background goroutine, parses bytes into `vtstate`) to `render.go` (repaint) via `fyne.Do`, with **debounced refresh** — coalesce with a timer capping redraws at roughly 30-60Hz rather than one `fyne.Do` call per PTY read (call this out explicitly as its own concern, not an afterthought — a chatty process can produce thousands of writes/sec). Resize handling must keep PTY size, `vt10x` grid size, and raster cell count in sync.

**Verification approach:** since this task has no keyboard input yet (Task 2), verify by feeding a synthetic sequence of bytes (plain text plus at least one SGR color-attribute escape sequence) either through a real `ptySession` or directly into `vtstate.go`'s parser (whichever is more reliable given the machine's ConPTY limitation noted in Global Constraints), and assert the resulting grid snapshot contains the expected runes/attributes at the expected cell positions — a structural test, not a rendered-pixel test. Note in the report whether real ConPTY output or synthetic injection was used and why.

**Done:** `go build`/`vet`/`test` clean. A grid-snapshot test proves bytes flowing in produce correct cell state. `render.go` compiles and produces a `canvas.Raster` (no crash) from a non-empty grid — full "does it look right" is a manual follow-up, not this task's automated bar.

---

### Task 2: Keyboard input

**Files:**
- Create: `terminal/keymap.go`
- Modify: `terminal/widget.go` (add `fyne.Focusable` to `Session`)

**Interfaces:**
- Consumes: `Session` from Task 1.
- Produces: `Session` now implements `fyne.Focusable` (`TypedRune(rune)`, `TypedKey(*fyne.KeyEvent)`, `FocusGained()`, `FocusLost()`), focus-on-tap. A `keymap.go` table/function translating key events to VT byte sequences, written to the PTY's stdin via `ptySession.Write`.

Byte mapping (from the design doc — verbatim, use these exact sequences):

| Input | Bytes |
|---|---|
| Printable rune | UTF-8 verbatim |
| Enter | `\r` |
| Backspace | `\x7f` |
| Tab | `\t` |
| Escape | `\x1b` |
| Arrows | `\x1b[A` / `\x1b[B` / `\x1b[C` / `\x1b[D` (Up/Down/Right/Left) |
| Home/End | `\x1b[H` / `\x1b[F` |
| Delete | `\x1b[3~` |
| Page Up/Down | `\x1b[5~` / `\x1b[6~` |
| Ctrl+letter | `rune - 'a' + 1` (Ctrl+C → `0x03`, etc.) |
| Ctrl+[ \ ] | `0x1b` / `0x1c` / `0x1d` |

This is a deliberately pragmatic subset — no F-keys, no shift-arrow selection, no alt-as-meta. Note this scope explicitly in `keymap.go`'s doc comment (it'll also go in `terminal.md` in Task 5).

**Verification approach:** a table-driven unit test over `keymap.go`'s translation function alone (input key event → expected byte sequence) needs no PTY/ConPTY at all — deterministic, fast, not subject to the machine's ConPTY limitation. Additionally, a `Session`-level test can call `TypedRune`/`TypedKey` and assert the corresponding bytes were written to a fake/mock `ptySession.Write` (or the real one, checking `Write`'s return value only, not relying on ConPTY echoing them back).

**Done:** `go build`/`vet`/`test` clean. Keymap translation table fully covered by unit tests. `Session` implements `fyne.Focusable` and compiles into a widget usable in a window (manual typing-and-observing is a human follow-up, not this task's automated bar, per the Global Constraints ConPTY caveat).

---

### Task 3: Tabs + window wrapper

**Files:**
- Create: `terminal/tabs.go`
- Create: `terminal/window.go`

**Interfaces:**
- Consumes: `Session`/`NewSession` from Tasks 1-2, `ShellDef`/`DetectShells()` from the PTY foundation plan.
- Produces:
  ```go
  type TabView struct { /* wraps container.NewDocTabs */ }
  func NewTabView(shells []ShellDef) *TabView
  func (t *TabView) AddTab(def ShellDef) *Session
  func (t *TabView) CloseTab(s *Session)

  type Window struct { /* ... */ }
  func NewWindow(app fyne.App, shells []ShellDef) (*Window, error)
  func (w *Window) SetSize(width, height float32) *Window // chainable, width>0 && height>0 guard, same pattern as dialog.Dialog.SetSize / settings.Window.SetSize
  func (w *Window) Show() // non-blocking, settings.Window's model
  ```
  No `db` parameter yet — that's Task 4. `NewWindow` in this task takes `[]ShellDef` directly and is testable standalone.

Use `container.NewDocTabs` (verified against the vendored Fyne v2.8.0 source in the design doc — NOT `AppTabs`, which has no close support in this Fyne version). `DocTabs` provides `CreateTab func() *TabItem` (built-in "+" button — the "open a new shell" affordance) and `OnClosed func(*TabItem)` (fires on a tab's own close button) — use these directly rather than hand-rolling either.

**Done:** `go build`/`vet`/`test` clean. A test constructs a `TabView` with 2+ `ShellDef`s, adds/closes tabs, and confirms `CloseTab` terminates the underlying session's process (check via the same `ptySession`-liveness technique already established in `terminal/session_windows_test.go` — a blocking `WaitForSingleObject` past a short window, not an instant poll). Closing the last tab leaves an empty `TabView`, not a crash/panic.

---

### Task 4: Minimal settings/DB integration for the demo

**Files:**
- Create: `terminal/settings_schema.go`

**Interfaces:**
- Consumes: `go-ux/db` package's `*db.DB`, `AddNode`/`AddProperty`/`ListSettings` API (see `settings/` for existing usage patterns — read `settings/settings.go` or `db.md` for the exact method signatures before writing this).
- Produces: `func RegisterSettings(database *db.DB) error` — idempotent (checks for an existing root node with `Description == "Terminal"` via `ListSettings()`; if found, returns `nil` without touching it; if not, creates the node plus properties below).

Minimal property set for this task (NOT the design doc's full 22-row table — that's future work per this plan's non-goals):

| Key | Type | Default | Notes |
|---|---|---|---|
| `default_shell` | `PropertyEnum` | first name from `DetectShells()`, or `"PowerShell"` if none detected | options populated from `DetectShells()` names |
| `close_on_exit` | `PropertyBool` | `"true"` | |

`terminal.Window` (from Task 3) gets a small update here: an optional path (or a second constructor) that reads `default_shell`/`close_on_exit` from a `*db.DB` via this schema when present, matching how `settings.Window` reads its own registry. Don't overbuild — this task's bar is "Open Settings shows a real Terminal node with these two properties, and Open Terminal's default shell selection can come from it," not the full settings surface.

**Done:** `go build`/`vet`/`test` clean. A test calls `RegisterSettings` on a fresh in-memory `db.DB`, confirms the Terminal node and both properties exist with correct defaults/types, and confirms calling it a second time is a no-op (no duplicate node/properties).

---

### Task 5: Demo + docs

**Files:**
- Create: `terminaldemo/test_terminal.go` (own directory + `package main` — mirrors `dialogdemo/test_dialog.go`'s structure; a directory can only have one `package main`, and `test_settings.go` already occupies that slot at repo root, same reason `dialogdemo/` isn't at root)
- Create: `terminal.md` (repo root, alongside `settings.md`/`dialog.md`)
- Modify: `CLAUDE.md` (package layout list — add `terminal/`/`terminaldemo/` bullet)

**Interfaces:**
- Consumes: everything from Tasks 1-4 (`terminal.NewWindow`, `terminal.RegisterSettings`, `terminal.DetectShells`), plus `go-ux/settings`'s `NewWindow`/`Show` and `go-ux/db`'s in-memory-DB construction pattern (see `test/test.go` for how the repo's own test fixture seeds an in-memory `db.DB` — reuse that pattern, don't reinvent it).

`terminaldemo/test_terminal.go`: follows `dialogdemo`'s persistent-master-window shape (an always-open window anchors the app so Fyne's driver doesn't tear itself down between opened/closed windows — read `dialogdemo/test_dialog.go`'s own rationale comment on this before writing it). One small master window, two buttons:
- **"Open Settings"** — calls `terminal.RegisterSettings(database)` then `settings.NewWindow(app, database)` + `Show`.
- **"Open Terminal"** — calls `terminal.NewWindow(app, terminal.DetectShells())` + `Show` (or the db-aware constructor from Task 4, if that's what ended up as the public entry point — use whichever Task 4 actually produced).

Both share one `db.DB` (in-memory).

`terminal.md`: match the `settings.md`/`dialog.md` template structure exactly (read both before writing this) — Public API, Minimal usage (including the `RegisterSettings` call), package-specific sections, Constraints for callers last. Constraints section MUST include: the ConPTY environment-limitation note (from Global Constraints above, written for a downstream consumer, not phrased as "this dev machine" but as a known ConPTY-attach caveat on some Windows builds), the deferred-Phase-8/9 scope (what's NOT implemented yet: cursor shape/contrast/hyperlinks/mouse/selection/bell, OSC 133/7 shell integration), and a note that this is desktop-only (no mobile driver support), stronger than the other docs' mobile notes since PTYs fundamentally don't exist on mobile.

**Done:** `go build ./...` / `go vet ./...` pass with the new package and demo. `go run ./terminaldemo` opens the two-button master window (report this — a human needs to actually look at it to confirm visual correctness, per Global Constraints; the automated bar is that it builds, runs without an immediate crash/panic, and the button callbacks are wired to the right calls). `terminal.md` exists and follows the sibling docs' template. `CLAUDE.md` updated.

---

## Verification

- `go build ./...`, `go vet ./...`, `go test ./...` clean after every task.
- Manual `go run ./terminaldemo` smoke test at the end (no GUI automation, per repo convention — see CLAUDE.md/user memory on UI testing being the user's job, not something to screenshot/automate here).
