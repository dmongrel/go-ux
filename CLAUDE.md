# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Purpose

This repository is the `ux` library: a collection of Fyne-composed dialogs and windows (`fyne.io/fyne` `app.Window`s) — e.g. a settings control panel — intended to be imported as a dependency by other Go Fyne applications.

## Research reference

When researching UX/design patterns for dialogs and control panels (e.g. settings panel layout and behavior), use the IntelliJ Community Edition source as a reference: https://github.com/jetbrains/intellij-community

## Fyne conventions

- Use the new `fyne.Do` threading model for UI-affecting code (required for anything not on the main goroutine, e.g. callbacks from background work or timers): https://docs.fyne.io/started/goroutines
- Fyne 2.8 prints a runtime warning ("has not been migrated to the fyne.Do threading model") for apps that don't use it — new code should use `fyne.Do`/`fyne.DoAndWait` rather than mutating widgets directly from other goroutines

## Project status

Package layout:
- `settings/` — the settings control panel: Fyne `Window` with a tree (left) and generated properties form (right), OK/Cancel/Apply staging. Non-blocking (`NewWindow`+`Show`).
- `dialog/` — modal info/error/custom dialog windows. `Show` blocks the calling goroutine until OK/Cancel/close, so it must be called from a goroutine other than the one running `app.Run()`. See `dialog/dialog.go` doc comment.
- `terminal/` — terminal-emulator window: ConPTY-backed shell sessions rendered as VT100/xterm grids (`vt10x` + hand-rasterized `canvas.Raster`), tabbed via `container.DocTabs`. Non-blocking (`NewWindow`/`NewWindowFromSettings`+`Show`), `db` integration optional. See `terminal.md`.
- `db/` — general-purpose persistence package; owns all SQLite access (settings registry + per-component UI-state blobs); no other package touches SQLite directly
- `treestate/` — reusable live persistence of a Fyne `*widget.Tree`'s expand/collapse state and selected node, backed by `db`'s generic UI-state blob store; used by `settings`'s properties tree, not tied to it
- `editors/` — embeddable text-editor-with-tabs component (novel-writing / prose-editing focus, plus an AI-diff-review API surface). Phase 1 (current): layout shell only — `Group` (parent, up to 4 `Pane`s via one level of nested `container.Split`), hand-rolled tab bar, live-persisted layout (`db.EditorPane`/`EditorTab`, a new relational domain on `db.DB`). No real text backend, diff view, markdown preview, font settings, or file watching yet. See `editors.md`.
- `internal/sqlite/` — pure-Go SQLite connection + schema migration (backs `db`), not exported
- `test/` — in-memory `db.DB` fixture + example data seeding for this repo's own tests only
- `test_settings.go` — manual/visual entry point (`go run test_settings.go`) that seeds example Terminal/Version Control data and opens the settings window
- `dialogdemo/` — manual/visual entry point (`go run ./dialogdemo`) that shows one of each dialog kind. In its own directory, not at repo root, because a directory can only have one `package main`/`func main` and `test_settings.go` already occupies that slot at root.
- `terminaldemo/` — manual/visual entry point (`go run ./terminaldemo`) with Open Settings / Open Terminal buttons sharing one in-memory `db.DB`. Own directory for the same one-`package main`-per-directory reason as `dialogdemo/`.
- `editorsdemo/` — manual/visual entry point (`go run ./editorsdemo`) embedding one `editors.Group` with 3 pre-opened placeholder tabs, backed by a file-based db so layout persistence can be verified across two runs. Own directory for the same reason.

Docs for downstream consumers: `settings.md`, `db.md`, `dialog.md`, `terminal.md`, `treestate.md`, `editors.md` (project root) — one per package with a public API meant for consumers.

Build/test:
- `go build ./...`, `go vet ./...`, `go test ./...`
- `go run test_settings.go` / `go run ./dialogdemo` to visually exercise the windows
