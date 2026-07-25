# Changelog

All notable changes to `go-ux` are documented here.

Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
There are no tagged releases yet (consumers pin a commit SHA — see
`CLAUDE.md`), so everything lives under **Unreleased** until the module path
gets fixed and versioning starts. Entries are grouped by section but not
dated; check `git log` for exact timing.

History before the Fyne→Wails v3 migration (see below) isn't backfilled here
— that architecture is gone, and its own history is superseded.

## [Unreleased]

### Added
- `db`: `UpdatePropertyOptions(nodeID, key, enumOptions)` for a
  `PropertyEnum` whose valid choices can change between launches (an OS
  voice list, a detected font list) — `SaveProperties` only ever updated a
  property's stored value, with no way to refresh `EnumOptions`. Deliberately
  does not fire `OnPropertiesChanged` (that callback's contract is
  specifically about `SaveProperties` writes, not definitional changes to
  what choices exist).
- `uxdemo`: Open File Dialog test button on the hub, exercising native
  file-picker wiring.

### Changed
- Migrated the entire library from Fyne (`fyne.io/fyne`) to Wails v3.
  Every UI-facing package (`settings`, `dialog`, `terminal`, `editors`) was
  rebuilt as a Wails `Service` + companion TypeScript frontend view; `db`
  and `internal/sqlite` carried over unchanged (already zero Fyne
  dependency), as did `fontsettings` (only its Ctrl+scroll wiring moved).
  `editors`' pane-split tree and `terminal`'s VT100 rendering moved from
  Go to frontend-owned equivalents, since the render target is now a web
  view, not a Fyne canvas. See `CLAUDE.md`'s "Migration history" for the
  full rationale.
- `db.md`: corrected a stale claim that Wails has no cross-platform
  window-position API — the pinned version (`v3.0.0-alpha2.117`) does
  expose `WebviewWindow.RelativePosition()`/`SetRelativePosition()`; what's
  actually missing is a cross-platform move *event* (`WindowDidMove` is
  macOS-only), so live position capture means polling, not no position API
  at all.
- Package docs (`*.md`) and dead Fyne-era code swept/removed after the
  migration.
