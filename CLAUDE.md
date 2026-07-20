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
- `settings/` — the settings control panel: Fyne `Window` with a tree (left) and generated properties form (right), OK/Cancel/Apply staging
- `db/` — general-purpose persistence package; owns all SQLite access (settings registry + per-component UI-state blobs); no other package touches SQLite directly
- `internal/sqlite/` — pure-Go SQLite connection + schema migration (backs `db`), not exported
- `test/` — in-memory `db.DB` fixture + example data seeding for this repo's own tests only
- `test_settings.go` — manual/visual entry point (`go run test_settings.go`) that seeds example Terminal/Version Control data and opens the settings window

Build/test:
- `go build ./...`, `go vet ./...`, `go test ./...`
- `go run test_settings.go` to visually exercise the settings window
