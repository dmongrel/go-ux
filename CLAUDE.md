# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is the `ux` library: a collection of Wails v3 UI components (dialogs, a settings control panel, a terminal emulator, a text editor) intended to be composed into other Go+Wails applications. Each package exposes a Go `Service` struct (bound to a Wails `*application.App`) plus a companion frontend view under `uxdemo/frontend/src/views/` — a consuming app registers the Service(s) it needs and references (today: copies) the matching view module(s) into its own Wails frontend build. Originally a Fyne-based library (`fyne.io/fyne`); migrated to Wails v3 — see "Migration history" below for what changed and why.

## Research reference

When researching UX/design patterns for dialogs and control panels (e.g. settings panel layout and behavior), use the IntelliJ Community Edition source as a reference: https://github.com/jetbrains/intellij-community

## Wails v3 conventions

- Each package's `Service` struct is constructed with the host's `*application.App` (+ `*db.DB` where relevant) and registered via `app.RegisterService(application.NewService(svc))`. An `OpenWindow()` method opens that component in its own `application.WebviewWindowOptions`-configured window sharing the host's process/bindings.
- Backend → frontend push uses `application.RegisterEvent[T](name)` (called from the owning package's own `init()`) + `app.Event.Emit(name, value)`; the frontend subscribes via `@wailsio/runtime`'s `Events.On(name, ...)`.
- Every window loads the same compiled frontend bundle; which view mounts is decided client-side by a hash router reading `location.hash` (`#hub`, `#dialog`, `#settings`, `#terminal`, `#editor`) — see `uxdemo/frontend/src/main.ts`.
- Go struct fields must stay untagged (no `json:"..."` struct tags) for any type crossing to the frontend — Wails' binding generator serializes with Go's exact PascalCase field names when untagged; adding lowercase json tags was a real, previously-hit bug (Vite/esbuild doesn't type-check TS, so a field-name mismatch silently resolves to `undefined` instead of failing loudly).
- A Go method's `(T, error)` return becomes a `Promise<T>` in the generated TS binding that rejects on a non-nil error — callers should `await`/`.catch` or wrap in try/catch, not expect a tuple.
- `wails3 build` (run from `uxdemo/`) handles bindings codegen (`wails3 generate bindings`), the frontend's Vite build, and the final Go binary in one step; `wails3 generate bindings` alone is enough after a Go-only signature change if you don't need a fresh frontend build.

## Project status

Package layout:
- `settings/` — settings control panel backend: `Service` (tree read via `ListNodes`/nested by `Node.ParentID`, `AllProperties` for instant client-side search, `StageProperty`/`Apply`/`Cancel` for OK/Cancel/Apply staging, `InitialTreeState`/`SetExpanded`/`SetSelected` via `treestate`). Frontend (`uxdemo/frontend/src/views/settings.ts`) renders a real nested, expand/collapse tree — not the flat list an early Wails prototype (terminal-poc) shipped.
- `dialog/` — `Service`: `ShowInfo`/`ShowError` via Wails' native `app.Dialog`; `ShowCustom(spec CustomDialogSpec)` opens a real window rendering an arbitrary label+input property form (all 7 original property kinds: label/bool/textField/int/list/dropdown/multiSelect) and blocks the caller until OK/Cancel/close, matching the original blocking `Dialog.Show` contract — Wails has no native equivalent for arbitrary dialog content, so this is a real (small) window, not `app.Dialog`.
- `terminal/` — `Service`: multi-session PTY backend (winpty on Windows, `winpty_windows.go`), shell detection/selection (`DetectShells`), raw-byte streaming to the frontend via the `terminal:data` event (payload includes `SessionID` — multiple tabs share one event channel), Ctrl+scroll font sizing shared live across every open terminal window via the `terminal:font` event. VT100 parsing/rendering is owned entirely by the frontend (`@xterm/xterm`, `uxdemo/frontend/src/views/terminal.ts`) — there is no Go-side terminal grid/render code anymore.
- `db/` — general-purpose persistence package; owns all SQLite access (settings registry + per-component UI-state blobs + editors layout); no other package touches SQLite directly. Unchanged by the Wails migration (already had zero Fyne dependency).
- `treestate/` — persists a tree UI's expand/collapse state and selection, backed by `db`'s generic UI-state blob store. `Tracker` is a plain request/response state store (`SetExpanded`/`SetSelected`/`Expanded`/`Selected`) — unlike the old Fyne version (which took over a live `*widget.Tree`'s callback fields), there is no live Go-side tree widget to hang callbacks off; the frontend tree component calls these on every toggle/select and replays the persisted state itself. Filtering out UIDs that no longer exist in the tree's current data is the caller's job (see `settings.Service.InitialTreeState`).
- `editors/` — text-editor-with-tabs backend: `Service` (in-memory `[]*Tab`, no Go-side pane/split tree — see below), real file I/O (`OpenFile`/`SaveTab`/`SaveTabAs`, plus native-dialog wrappers `OpenFileDialog`/`SaveTabAsDialog`), file watching (`fsnotify`, one `*fsnotify.Watcher` per `Service`, auto-reload or `editors:filechanged` notify per a persisted `file_watch_mode` setting, `ReloadTab` for the "Load from Disk" half), diff review (`ProposeDiff`/`AcceptDiff`/`CancelDiff` — this package computes and renders no diff itself anymore; the frontend's `@codemirror/merge` `unifiedMergeView` does, comparing a `Tab`'s current text against its `PendingDiff`), per-instance Ctrl+scroll font sizing (`fontsettings`, broadcast via `editors:font`). The split-pane tree, live cross-pane document sync, and the Split/Move right-click menu all live entirely in the frontend (`uxdemo/frontend/src/views/editor.ts`) now — ported from a standalone Wails prototype (terminal-poc) that had already independently re-derived and *generalized* the original Fyne `split.go` tree algorithm (collect-into-single-pane and quadrant positional correspondence for Move, which the original never supported); there is no Go-side notion of a Pane.
- `fontsettings/` — shared Ctrl+scroll-adjustable font configuration (`FontSettings`, clamping, a per-instance listener-registry `State` type, `DetectMonospaceFonts` (Windows), `SeedFontProperties`/`ReadFontProperties`). Already had zero Fyne dependency before the migration (only its own `_test.go` incidentally touched Fyne, for a test-fixture font file — now sourced from `golang.org/x/image/font/gofont` instead) — reused verbatim. `terminal`/`editors` each get a small `Service`-level wrapper (`CurrentFontSettings`/`SetFontSettings`) exposing it as bound methods; the actual Ctrl+scroll wheel-listener wiring lives in each package's own frontend view.
- `internal/sqlite/` — pure-Go SQLite connection + schema migration (backs `db`), not exported. Unchanged.
- `test/` — in-memory `db.DB` fixture + example data seeding for this repo's own tests only.
- `uxdemo/` — the manual/visual verification vehicle for every component, replacing the four separate Fyne demos this repo used to ship (`test_settings.go`, `dialogdemo/`, `terminaldemo/`, `editorsdemo/`). A Wails app is one Go process bound to one compiled frontend bundle — unlike those `go run`-able Fyne demos, every component now shares this single entry point: a Hub window opens at startup with a button per component, each opening in its own window sharing the same backend/bindings. `uxdemo/frontend/` is also, today, the *only* copy of each package's frontend view module — see "Known limitation" below.

Docs for downstream consumers: `settings.md`, `db.md`, `dialog.md`, `terminal.md`, `treestate.md`, `editors.md` (project root) — one per package with a public API meant for consumers.

Build/test:
- `go build ./...`, `go vet ./...`, `go test ./...`
- `wails3 build` (from `uxdemo/`) + run `uxdemo/bin/uxdemo.exe` to visually exercise every window

## Known limitation: frontend distribution

Fyne let this library ship embeddable `fyne.CanvasObject`s any host app could mount directly. Wails has no equivalent — a Wails app is one Go process bound to one compiled frontend bundle, so a consuming app needs both the Go `Service` (a normal Go import) *and* the matching TypeScript view module, and Wails has no mechanism for a Go module to also ship a redistributable frontend component. Today, `uxdemo/frontend/src/views/*.ts` is that component source, and a consuming app is expected to copy the relevant file(s) into its own Vite build alongside registering the Go Service(s) it needs — there is no npm package/versioning story yet. See each package's `.md` doc for specifics.

## Migration history

This library was originally built on Fyne (`fyne.io/fyne`) and migrated to Wails v3 in place. Two things carried over unchanged: `db/` and `internal/sqlite/` (already zero Fyne dependency), and `fontsettings/` (same — only wired into Ctrl+scroll differently per consumer now). Everything UI-facing was rebuilt: Fyne windows/widgets became Wails Services + frontend TypeScript views; Fyne's `container.Split`-based pane tree (`editors`) and its VT100/canvas rendering (`terminal`) were replaced by frontend-owned equivalents (a generalized TS port of the original split algorithm, and `@xterm/xterm`, respectively) rather than ported as Go logic, since the rendering target is now a web view, not a Fyne canvas.
