# Terminal Font Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a configurable terminal font (family/size/line-height/column-width) to `go-ux/terminal`, registered through the same settings-registry mechanism as `default_shell`/`close_on_exit`, plus live Ctrl+scrollwheel font-size adjustment that applies across every open session and persists without opening the settings dialog.

**Architecture:** A new `db.PropertyFloat` type plus a `db.OnPropertiesChanged` notification hook (both in the shared `db`/`settings` packages) let `settings.Window` force-accept an externally-written value over a stale staged edit. `terminal` gets Windows registry-based monospace font detection, a package-level shared `FontSettings` every live `Session` reads from, and Ctrl+scroll (via `desktop.Keyable` + `fyne.Scrollable`) that mutates that shared state directly and debounce-persists to the db.

**Tech Stack:** Go 1.26, Fyne v2.8.0, `golang.org/x/sys/windows/registry`, `golang.org/x/image/font/opentype` (both already indirect dependencies via existing code), SQLite via `go-ux/db`.

## Global Constraints

- No cgo anywhere in this repo (matches `winpty_windows.go`'s existing no-cgo convention for Windows-specific code).
- Windows only for the new font-detection code (`terminal` package's existing scope — no Unix/macOS backend yet).
- `db`/`settings` changes must be backward compatible: existing `PropertyType` values and callers unaffected.
- Line height / column width are multipliers (1.0 = natural size), not absolute values — confirmed against JetBrains JediTerm's `getLineSpacing()` ("vertical scaling factor", default `1.0f`).
- Defaults: `font_family` = `"(default)"` (bundled font), `font_size` = `13`, `line_height` = `1.0`, `column_width` = `1.0`.
- Ctrl+scroll: 2pt per tick, clamped to the 8–36pt range (also the clamp range for any stored `font_size` value read anywhere); `line_height`/`column_width` clamped to 0.5–3.0 wherever read.
- Design spec: `docs/superpowers/specs/2026-07-22-terminal-font-settings-design.md` — read it before starting if anything below is unclear on intent.

---

### Task 1: `db.PropertyFloat`

**Files:**
- Modify: `db/db.go:20-25` (the `PropertyType` const block)
- Test: `db/db_test.go`

**Interfaces:**
- Produces: `db.PropertyFloat db.PropertyType = "float"`, usable anywhere `db.PropertyBool`/`db.PropertyInt`/etc. already are (`AddProperty`, `GetProperties`, `SaveProperties` are all untyped string plumbing already — no other `db.go` changes needed).

- [ ] **Step 1: Write the failing test**

Add to `db/db_test.go` (same file, `db_test` package, using the existing `test.NewDB()`/`test.SeedExample()` helpers already imported there):

```go
func TestPropertyFloatRoundTrips(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Float Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "ratio", "Ratio", db.PropertyFloat, "1.5", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("GetProperties: got %d properties, want 1", len(props))
	}
	if props[0].Type != db.PropertyFloat {
		t.Errorf("Type = %q, want %q", props[0].Type, db.PropertyFloat)
	}
	if props[0].Value != "1.5" {
		t.Errorf("Value = %q, want %q", props[0].Value, "1.5")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/... -run TestPropertyFloatRoundTrips -v`
Expected: FAIL — `db.PropertyFloat` undefined.

- [ ] **Step 3: Add the type**

In `db/db.go`, change:

```go
const (
	PropertyBool   PropertyType = "bool"
	PropertyString PropertyType = "string"
	PropertyInt    PropertyType = "int"
	PropertyEnum   PropertyType = "enum"
)
```

to:

```go
const (
	PropertyBool   PropertyType = "bool"
	PropertyString PropertyType = "string"
	PropertyInt    PropertyType = "int"
	PropertyEnum   PropertyType = "enum"
	PropertyFloat  PropertyType = "float"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./db/... -run TestPropertyFloatRoundTrips -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/db.go db/db_test.go
git commit -m "db: add PropertyFloat type for decimal-valued settings"
```

---

### Task 2: `db.OnPropertiesChanged`

**Files:**
- Modify: `db/db.go`
- Test: `db/db_test.go`

**Interfaces:**
- Consumes: nothing new (existing `SaveProperties(nodeID int64, values map[string]string) error`).
- Produces: `func (d *DB) OnPropertiesChanged(nodeID int64, fn func(values map[string]string)) (unsubscribe func())`. `fn` fires synchronously, on whatever goroutine called `SaveProperties`, after the write commits successfully. Safe to call `OnPropertiesChanged`/the returned `unsubscribe` from any goroutine.

- [ ] **Step 1: Write the failing test**

Add to `db/db_test.go`:

```go
func TestOnPropertiesChangedFiresAfterSaveProperties(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Notify Test", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "size", "Size", db.PropertyInt, "10", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	var got map[string]string
	unsubscribe := d.OnPropertiesChanged(nodeID, func(values map[string]string) {
		got = values
	})
	defer unsubscribe()

	if err := d.SaveProperties(nodeID, map[string]string{"size": "20"}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}
	if got == nil {
		t.Fatal("OnPropertiesChanged callback did not fire")
	}
	if got["size"] != "20" {
		t.Errorf("callback values[\"size\"] = %q, want %q", got["size"], "20")
	}

	unsubscribe()
	got = nil
	if err := d.SaveProperties(nodeID, map[string]string{"size": "30"}); err != nil {
		t.Fatalf("SaveProperties (after unsubscribe): %v", err)
	}
	if got != nil {
		t.Error("callback fired after unsubscribe")
	}
}

func TestOnPropertiesChangedOnlyFiresForItsOwnNode(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeA, err := d.AddNode(nil, "A", 0)
	if err != nil {
		t.Fatalf("AddNode A: %v", err)
	}
	nodeB, err := d.AddNode(nil, "B", 0)
	if err != nil {
		t.Fatalf("AddNode B: %v", err)
	}
	if err := d.AddProperty(nodeA, "k", "K", db.PropertyString, "v", nil); err != nil {
		t.Fatalf("AddProperty A: %v", err)
	}
	if err := d.AddProperty(nodeB, "k", "K", db.PropertyString, "v", nil); err != nil {
		t.Fatalf("AddProperty B: %v", err)
	}

	fired := false
	unsubscribe := d.OnPropertiesChanged(nodeA, func(map[string]string) { fired = true })
	defer unsubscribe()

	if err := d.SaveProperties(nodeB, map[string]string{"k": "changed"}); err != nil {
		t.Fatalf("SaveProperties(nodeB): %v", err)
	}
	if fired {
		t.Error("nodeA's callback fired for a write to nodeB")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/... -run TestOnPropertiesChanged -v`
Expected: FAIL — `d.OnPropertiesChanged` undefined.

- [ ] **Step 3: Implement the notification hook**

In `db/db.go`, add `"sync"` to the import block:

```go
import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"go-ux/internal/sqlite"
)
```

Add a `listeners` field to `DB` and initialize it in `Open`:

```go
// DB is a handle to the go-ux persistence store.
type DB struct {
	conn *sql.DB

	mu        sync.Mutex
	listeners map[int64][]func(values map[string]string)
}
```

```go
func Open(path string) (*DB, error) {
	conn, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{conn: conn, listeners: make(map[int64][]func(values map[string]string))}, nil
}
```

Add the subscription method and wire the notification into `SaveProperties`:

```go
// OnPropertiesChanged registers fn to be called, synchronously and on
// whatever goroutine calls it, after every successful SaveProperties(nodeID,
// ...) — with the same values map that was passed to it. Returns an
// unsubscribe function; safe to call OnPropertiesChanged and the returned
// function from any goroutine.
//
// This exists so a UI displaying nodeID's properties (go-ux/settings.Window)
// can react to a write it didn't itself make — e.g. go-ux/terminal writing a
// live Ctrl+scroll font-size change directly to the db while a Settings
// window happens to be open on the same node.
func (d *DB) OnPropertiesChanged(nodeID int64, fn func(values map[string]string)) (unsubscribe func()) {
	d.mu.Lock()
	d.listeners[nodeID] = append(d.listeners[nodeID], fn)
	idx := len(d.listeners[nodeID]) - 1
	d.mu.Unlock()

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		fns := d.listeners[nodeID]
		if idx >= len(fns) || fns[idx] == nil {
			return
		}
		fns[idx] = nil // leave a hole rather than reslicing, so other subscribers' idx stay valid
	}
}

func (d *DB) notifyPropertiesChanged(nodeID int64, values map[string]string) {
	d.mu.Lock()
	fns := append([]func(map[string]string){}, d.listeners[nodeID]...)
	d.mu.Unlock()

	for _, fn := range fns {
		if fn != nil {
			fn(values)
		}
	}
}
```

Change `SaveProperties` to notify on success:

```go
func (d *DB) SaveProperties(nodeID int64, values map[string]string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("db: save properties: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE settings_properties SET value = ? WHERE node_id = ? AND key = ?`)
	if err != nil {
		return fmt.Errorf("db: save properties: %w", err)
	}
	defer stmt.Close()

	for key, value := range values {
		if _, err := stmt.Exec(value, nodeID, key); err != nil {
			return fmt.Errorf("db: save properties: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: save properties: %w", err)
	}
	d.notifyPropertiesChanged(nodeID, values)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./db/... -run TestOnPropertiesChanged -v`
Expected: PASS (both new tests)

- [ ] **Step 5: Run the full db package test suite**

Run: `go test ./db/... -v`
Expected: PASS (all tests, including the pre-existing `TestSettingsRegistry`/`TestUIState`)

- [ ] **Step 6: Commit**

```bash
git add db/db.go db/db_test.go
git commit -m "db: add OnPropertiesChanged notification for external writes"
```

---

### Task 3: `settings.Window` renders `PropertyFloat`

**Files:**
- Modify: `settings/settings.go:266-305` (`propertyWidget`)
- Test: `settings/settings_test.go`

**Interfaces:**
- Consumes: `db.PropertyFloat` (Task 1).
- Produces: `propertyWidget` returns a `*widget.Entry` with a float-parse validator for `db.PropertyFloat` properties, same shape as the existing `db.PropertyInt` case.

- [ ] **Step 1: Write the failing test**

`settings/settings_test.go` is package `settings_test` (external test package — confirmed), importing `fyne.io/fyne/v2/test` under the alias `fynetest` (since `go-ux/test` already claims the plain name `test` in that file) and using `go-ux/test`'s `NewDB()`/`SeedExample()` helpers. Add this test using that same style, plus `"go-ux/db"` and `"fyne.io/fyne/v2/widget"` (not yet imported in that file — add both):

```go
func TestPropertyFloatRendersValidatedEntry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Float Node", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "ratio", "Ratio", db.PropertyFloat, "1.2", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	// settings.Window has no public Close method (confirmed: not in
	// settings.go) — none of this file's other tests close their Window
	// either, since it holds no real OS resources under the Fyne test
	// driver. No cleanup call needed here.

	props, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	obj := w.PropertyWidgetForTest(nodeID, props[0])
	entry, ok := obj.(*widget.Entry)
	if !ok {
		t.Fatalf("propertyWidget(PropertyFloat) = %T, want *widget.Entry", obj)
	}
	if entry.Text != "1.2" {
		t.Errorf("entry.Text = %q, want %q", entry.Text, "1.2")
	}
	if entry.Validator == nil {
		t.Fatal("entry.Validator is nil, want a float validator")
	}
	if err := entry.Validator("not-a-number"); err == nil {
		t.Error("Validator(\"not-a-number\") = nil, want an error")
	}
	if err := entry.Validator("2.5"); err != nil {
		t.Errorf("Validator(\"2.5\") = %v, want nil", err)
	}
}
```

This test needs a small export: `propertyWidget` is unexported and the existing test file doesn't already have a way to reach it from outside the package (check: `settings_test.go`'s package clause — if it's `package settings_test` like `db_test.go`'s pattern, it can't call `w.propertyWidget` directly). Add a thin test-only export in `settings/settings.go` right after `propertyWidget`:

```go
// PropertyWidgetForTest exposes propertyWidget for settings_test's
// external test package — this package has no other way to inspect a
// generated form widget's type/validator from outside.
func (w *Window) PropertyWidgetForTest(nodeID int64, p db.Property) fyne.CanvasObject {
	return w.propertyWidget(nodeID, p)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./settings/... -run TestPropertyFloatRendersValidatedEntry -v`
Expected: FAIL — falls into `propertyWidget`'s `default` case (plain `Entry`, no validator), so the `Validator == nil` check fails.

- [ ] **Step 3: Add the `PropertyFloat` case**

In `settings/settings.go`, `propertyWidget`, add a case between the existing `PropertyInt` and `PropertyEnum` cases:

```go
	case db.PropertyInt:
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.Validator = func(s string) error {
			_, err := strconv.Atoi(s)
			return err
		}
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry

	case db.PropertyFloat:
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.Validator = func(s string) error {
			_, err := strconv.ParseFloat(s, 64)
			return err
		}
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry

	case db.PropertyEnum:
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./settings/... -run TestPropertyFloatRendersValidatedEntry -v`
Expected: PASS

- [ ] **Step 5: Run the full settings package test suite**

Run: `go test ./settings/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add settings/settings.go settings/settings_test.go
git commit -m "settings: render PropertyFloat as a validated numeric Entry"
```

---

### Task 4: `settings.Window` force-accepts external writes

**Files:**
- Modify: `settings/settings.go` (`NewWindow`, add subscription/force-accept logic)
- Test: `settings/settings_test.go`

**Interfaces:**
- Consumes: `db.OnPropertiesChanged` (Task 2).
- Produces: nothing new externally — this is purely internal `Window` behavior. Any code elsewhere that calls `database.SaveProperties` on a node an open `settings.Window` has prefetched will see that window's displayed value (and staged map) update to match, discarding a stale staged edit for that same key.

- [ ] **Step 1: Write the failing test**

Add to `settings/settings_test.go`:

```go
func TestExternalWriteForceAcceptsOverStaleStagedEdit(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	nodeID, err := d.AddNode(nil, "Force Accept Node", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(nodeID, "size", "Size", db.PropertyInt, "13", nil); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	// User types a value in the dialog (staged, not yet committed).
	props, _ := d.GetProperties(nodeID)
	obj := w.PropertyWidgetForTest(nodeID, props[0])
	entry := obj.(*widget.Entry)
	entry.SetText("20")

	// An external write happens (e.g. terminal's Ctrl+scroll) — bypasses
	// the dialog's own staging entirely.
	if err := d.SaveProperties(nodeID, map[string]string{"size": "15"}); err != nil {
		t.Fatalf("SaveProperties (external): %v", err)
	}

	// The dialog's own control for that property must now show/use 15, not
	// the user's stale staged 20 — and OK must persist 15, not 20.
	w.HandleOKForTest()

	final, err := d.GetProperties(nodeID)
	if err != nil {
		t.Fatalf("GetProperties (final): %v", err)
	}
	if final[0].Value != "15" {
		t.Errorf("final size = %q, want %q (external write must win over stale staged edit)", final[0].Value, "15")
	}
}
```

This needs one more thin test-only export alongside `PropertyWidgetForTest` (`handleOK` is unexported):

```go
// HandleOKForTest exposes handleOK for settings_test's external test
// package.
func (w *Window) HandleOKForTest() {
	w.handleOK()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./settings/... -run TestExternalWriteForceAcceptsOverStaleStagedEdit -v`
Expected: FAIL — `final[0].Value` is `"20"` (the stale staged edit wins), not `"15"`.

- [ ] **Step 3: Add the subscription and force-accept logic**

In `settings/settings.go`, add an `unsubscribers []func()` field to `Window`:

```go
	// staged holds in-memory edits, keyed by node ID then property key,
	// not yet written to the db. Apply/OK flush it; Cancel discards it.
	staged map[int64]map[string]string

	// unsubscribers cancels every db.OnPropertiesChanged subscription this
	// Window registered in NewWindow, called when the window closes.
	unsubscribers []func()
```

In `NewWindow`, after `w.indexNodes(nodes)` and `w.prefetchProperties()` (both need `w.byID` to already be populated for the loop below), subscribe to every node:

```go
	w.indexNodes(nodes)
	if err := w.prefetchProperties(); err != nil {
		return nil, err
	}
	for uid, node := range w.byID {
		nodeID := node.ID
		unsubscribe := database.OnPropertiesChanged(nodeID, func(values map[string]string) {
			fyne.Do(func() { w.acceptExternalChange(uid, nodeID, values) })
		})
		w.unsubscribers = append(w.unsubscribers, unsubscribe)
	}
```

(`uid` is a `map` range variable used inside a closure — Go 1.22+ loop-var semantics, already this module's floor per `go.mod`'s `go 1.26`, make this safe without a manual `uid := uid` shadow copy; each iteration's closure correctly captures its own `uid`.)

Add the handler and wire unsubscription into the close intercept:

```go
// acceptExternalChange reacts to a db write this Window didn't itself
// make (see db.OnPropertiesChanged) — force-accepting it means updating
// the cached Property.Value so it's correct even before next rendered, and
// discarding any staged-but-uncommitted edit for that same key, so a
// later OK/Apply can't overwrite the newer external value with a stale one.
// Runs on the UI goroutine (the caller wraps it in fyne.Do) since it
// touches formHolder when uid is the currently displayed page.
func (w *Window) acceptExternalChange(uid string, nodeID int64, values map[string]string) {
	props := w.allProps[uid]
	for key, value := range values {
		for i := range props {
			if props[i].Key == key {
				props[i].Value = value
			}
		}
		if w.staged[nodeID] != nil {
			delete(w.staged[nodeID], key)
		}
	}
	if uid == w.selectedUID {
		w.renderProperties(uid)
	}
}
```

```go
	w.restoreUIState()
	win.SetCloseIntercept(func() {
		w.saveUIState()
		for _, unsubscribe := range w.unsubscribers {
			unsubscribe()
		}
		win.Close()
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./settings/... -run TestExternalWriteForceAcceptsOverStaleStagedEdit -v`
Expected: PASS

- [ ] **Step 5: Run the full settings package test suite, plain and race**

Run: `go test ./settings/... -v` then `go test ./settings/... -race -v`
Expected: PASS both — the `-race` run specifically matters here since this task's whole point is a cross-goroutine notification path (`db.SaveProperties` can be called from any goroutine; `fyne.Do` is what makes touching `w.staged`/`w.allProps`/`w.formHolder` from `acceptExternalChange` safe).

- [ ] **Step 6: Commit**

```bash
git add settings/settings.go settings/settings_test.go
git commit -m "settings: force-accept externally-written property values"
```

---

### Task 5: Windows monospace font detection

**Files:**
- Create: `terminal/font_windows.go`
- Test: `terminal/font_windows_test.go`

**Interfaces:**
- Consumes: nothing new (`golang.org/x/image/font/opentype`, already used by `render.go`; `golang.org/x/sys/windows/registry`, same module already used by `winpty_windows.go`).
- Produces: `func DetectMonospaceFonts() []string` — cached, never errors, mirrors `DetectShells()`'s shape.

- [ ] **Step 1: Write the failing test**

`terminal/font_windows_test.go` needs real font file fixtures (one monospace, one proportional) to test the filter against — not relying on whatever happens to be installed on the machine running the test. Use Fyne's own bundled monospace resource (already used by `render.go`/`loadMonospaceFace`, guaranteed present) as the "known monospace" fixture, and Fyne's bundled *regular* (proportional) text resource as the "known proportional" fixture:

```go
//go:build windows

package terminal

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/theme"
)

func TestIsMonospaceFontFile(t *testing.T) {
	dir := t.TempDir()

	monoPath := filepath.Join(dir, "mono.ttf")
	if err := os.WriteFile(monoPath, theme.DefaultTextMonospaceFont().Content(), 0o644); err != nil {
		t.Fatalf("write mono fixture: %v", err)
	}
	if !isMonospaceFontFile(monoPath) {
		t.Error("isMonospaceFontFile(bundled monospace font) = false, want true")
	}

	propPath := filepath.Join(dir, "prop.ttf")
	if err := os.WriteFile(propPath, theme.DefaultTextFont().Content(), 0o644); err != nil {
		t.Fatalf("write proportional fixture: %v", err)
	}
	if isMonospaceFontFile(propPath) {
		t.Error("isMonospaceFontFile(bundled proportional font) = true, want false")
	}

	if isMonospaceFontFile(filepath.Join(dir, "does-not-exist.ttf")) {
		t.Error("isMonospaceFontFile(missing file) = true, want false")
	}
}

func TestDetectMonospaceFontsNeverErrors(t *testing.T) {
	// No assertion on the actual list contents (machine-dependent) — the
	// contract under test is "never panics, never blocks meaningfully,
	// callable repeatedly", matching DetectShells()'s own test shape.
	got := DetectMonospaceFonts()
	got2 := DetectMonospaceFonts()
	if len(got) != len(got2) {
		t.Errorf("DetectMonospaceFonts() not stable across calls: %d then %d results", len(got), len(got2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run "TestIsMonospaceFontFile|TestDetectMonospaceFontsNeverErrors" -v`
Expected: FAIL — `isMonospaceFontFile`/`DetectMonospaceFonts` undefined.

- [ ] **Step 3: Implement font detection**

Create `terminal/font_windows.go`:

```go
//go:build windows

package terminal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/sys/windows/registry"
)

// monospaceFontsOnce/monospaceFonts cache DetectMonospaceFonts' result for
// the process lifetime — font installs don't change while this app is
// running, so there's no reason to re-scan (and re-parse every candidate
// font file) on every call.
var (
	monospaceFontsOnce sync.Once
	monospaceFonts     []string
)

// DetectMonospaceFonts scans fonts installed on this machine (system-wide
// and per-user) and returns the names of the ones that are genuinely
// monospace (fixed glyph advance width across a representative ASCII
// sample) — the property that makes a font usable for a terminal grid.
// Mirrors DetectShells()'s contract: never returns an error, an empty
// result on any enumeration failure is handled gracefully by callers
// (font_family's registered enum then only offers "(default)", the bundled
// font).
func DetectMonospaceFonts() []string {
	monospaceFontsOnce.Do(func() {
		monospaceFonts = scanMonospaceFonts()
	})
	return monospaceFonts
}

func scanMonospaceFonts() []string {
	winDir := os.Getenv("SystemRoot")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")

	seen := make(map[string]bool)
	var names []string

	// System-installed fonts: HKLM's registry values are filenames relative
	// to %WINDIR%\Fonts.
	scanFontRegistryKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		func(file string) string {
			if filepath.IsAbs(file) {
				return file
			}
			return filepath.Join(fontsDir, file)
		}, seen, &names)

	// Per-user installed fonts: HKCU's registry values are already absolute
	// paths.
	scanFontRegistryKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Fonts`,
		func(file string) string { return file }, seen, &names)

	sort.Strings(names)
	return names
}

// scanFontRegistryKey enumerates one font registry key's values, resolves
// each to a file path via resolvePath, and appends the display name to
// *names (deduplicated via seen) for every file that passes
// isMonospaceFontFile. Any failure to open/read the key is silent — the
// other key (system vs. per-user) may still yield results, and
// DetectMonospaceFonts' own contract is "never error, possibly empty".
func scanFontRegistryKey(root registry.Key, path string, resolvePath func(string) string, seen map[string]bool, names *[]string) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer key.Close()

	valueNames, err := key.ReadValueNames(-1)
	if err != nil {
		return
	}
	for _, regName := range valueNames {
		file, _, err := key.GetStringValue(regName)
		if err != nil || file == "" {
			continue
		}
		display := fontDisplayName(regName)
		if seen[display] {
			continue
		}
		if isMonospaceFontFile(resolvePath(file)) {
			seen[display] = true
			*names = append(*names, display)
		}
	}
}

// fontDisplayName strips the "(TrueType)"/"(OpenType)" suffix Windows adds
// to font registry value names, leaving the plain family name a user would
// recognize (e.g. "Consolas" not "Consolas (TrueType)").
func fontDisplayName(regName string) string {
	if i := strings.LastIndex(regName, " ("); i >= 0 {
		return regName[:i]
	}
	return regName
}

// isMonospaceFontFile reports whether the font file at path has a fixed
// glyph advance width across a representative sample of ASCII letters,
// digits, and punctuation. Any parse failure (missing file, unsupported
// format, a face that can't report advances) is treated as "not
// monospace" rather than propagated as an error — a single bad font
// shouldn't break the whole scan.
func isMonospaceFontFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	fnt, err := opentype.Parse(data)
	if err != nil {
		return false
	}
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{Size: 13, DPI: 72, Hinting: xfont.HintingNone})
	if err != nil {
		return false
	}
	defer f.Close()

	var want int32
	for i, r := range "ABCabc012.,;" {
		adv, ok := f.GlyphAdvance(r)
		if !ok {
			return false
		}
		if i == 0 {
			want = int32(adv)
			continue
		}
		if int32(adv) != want {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run "TestIsMonospaceFontFile|TestDetectMonospaceFontsNeverErrors" -v`
Expected: PASS

- [ ] **Step 5: Run go vet**

Run: `go vet ./terminal/...`
Expected: clean (no `unsafeptr` or other findings — this task doesn't touch raw syscalls directly, only the typed `registry` package wrapper).

- [ ] **Step 6: Commit**

```bash
git add terminal/font_windows.go terminal/font_windows_test.go
git commit -m "terminal: add Windows monospace font detection"
```

---

### Task 6: Shared live `FontSettings` state

**Files:**
- Create: `terminal/font.go`
- Test: `terminal/font_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type FontSettings struct { Family string; Size int; LineHeight, ColumnWidth float64 }`
  - `func currentFontSettings() FontSettings` — package-internal read of the shared state.
  - `func setFontSettings(s FontSettings)` — package-internal write; notifies every registered `*Session`.
  - `func registerFontListener(s *Session)` / `func unregisterFontListener(s *Session)` — package-internal, called from `NewSession`/`Close` (Task 9).
  - `func clampFontSettings(s FontSettings) FontSettings` — applies the bounds from Global Constraints (8–36 size, 0.5–3.0 multipliers); used by both this file and Task 7/10.

- [ ] **Step 1: Write the failing test**

Create `terminal/font_test.go`:

```go
package terminal

import (
	"sync/atomic"
	"testing"
)

func TestDefaultFontSettings(t *testing.T) {
	got := currentFontSettings()
	want := FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}
	if got != want {
		t.Errorf("currentFontSettings() = %+v, want %+v", got, want)
	}
}

func TestSetFontSettingsNotifiesRegisteredListeners(t *testing.T) {
	defer setFontSettings(FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}) // restore default for other tests

	var calls int32
	unregister := registerFontListenerFunc(func(FontSettings) {
		atomic.AddInt32(&calls, 1)
	})
	defer unregister()

	setFontSettings(FontSettings{Family: "", Size: 20, LineHeight: 1.0, ColumnWidth: 1.0})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("listener called %d times, want 1", got)
	}
	if got := currentFontSettings().Size; got != 20 {
		t.Errorf("currentFontSettings().Size = %d, want 20", got)
	}
}

func TestClampFontSettings(t *testing.T) {
	got := clampFontSettings(FontSettings{Size: 200, LineHeight: 10, ColumnWidth: 0.01})
	if got.Size != 36 {
		t.Errorf("Size = %d, want 36 (clamped)", got.Size)
	}
	if got.LineHeight != 3.0 {
		t.Errorf("LineHeight = %v, want 3.0 (clamped)", got.LineHeight)
	}
	if got.ColumnWidth != 0.5 {
		t.Errorf("ColumnWidth = %v, want 0.5 (clamped)", got.ColumnWidth)
	}

	got2 := clampFontSettings(FontSettings{Size: 1, LineHeight: 0.01, ColumnWidth: 10})
	if got2.Size != 8 {
		t.Errorf("Size = %d, want 8 (clamped)", got2.Size)
	}
	if got2.LineHeight != 0.5 {
		t.Errorf("LineHeight = %v, want 0.5 (clamped)", got2.LineHeight)
	}
	if got2.ColumnWidth != 3.0 {
		t.Errorf("ColumnWidth = %v, want 3.0 (clamped)", got2.ColumnWidth)
	}
}
```

`registerFontListenerFunc` (taking a plain `func(FontSettings)` rather than a `*Session`) is a small test-only seam — real callers (Task 9) use `registerFontListener(s *Session)`/`unregisterFontListener(s *Session)`, which this task's implementation defines in terms of the same underlying listener list `registerFontListenerFunc` uses, so this test exercises the real notification path without needing a full `*Session` (which needs a live PTY to construct).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run "TestDefaultFontSettings|TestSetFontSettingsNotifiesRegisteredListeners|TestClampFontSettings" -v`
Expected: FAIL — none of these symbols exist yet.

- [ ] **Step 3: Implement shared font state**

Create `terminal/font.go`:

```go
package terminal

import "sync"

// FontSettings is the live, global terminal font configuration: every open
// Session, in every open Window/TabView, renders against the same shared
// value (see registerFontListener) — this is what makes Ctrl+scroll (Task
// 10) or a Settings-window Apply (Task 7's ApplyFontSettings) affect every
// tab at once, without threading a shared object through every
// constructor. Family "" means "use the bundled font" (loadMonospaceFace's
// existing fallback path, Task 8).
type FontSettings struct {
	Family      string
	Size        int
	LineHeight  float64
	ColumnWidth float64
}

// defaultFontSettings is FontSettings' zero-configuration value — matches
// RegisterSettings' own seeded defaults (Task 7) so a caller that never
// touches font settings at all sees identical behavior to before this
// feature existed.
var defaultFontSettings = FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}

const (
	minFontSize = 8
	maxFontSize = 36

	minFontMultiplier = 0.5
	maxFontMultiplier = 3.0
)

// clampFontSettings bounds Size/LineHeight/ColumnWidth to the ranges
// Global Constraints defines, leaving Family untouched. Applied wherever a
// FontSettings value is about to be read for rendering or written to the
// db — a hand-edited db row or a scroll-driven step past the edge must not
// produce an unusable (or negative/zero) font size.
func clampFontSettings(s FontSettings) FontSettings {
	s.Size = clampInt(s.Size, minFontSize, maxFontSize)
	s.LineHeight = clampFloat(s.LineHeight, minFontMultiplier, maxFontMultiplier)
	s.ColumnWidth = clampFloat(s.ColumnWidth, minFontMultiplier, maxFontMultiplier)
	return s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// fontState is the package-level shared FontSettings plus its listeners.
// mu guards both fields; current is only ever replaced wholesale (via
// setFontSettings), never mutated in place, so a reader never needs to
// hold mu past the point it copies current out.
var fontState = struct {
	mu        sync.Mutex
	current   FontSettings
	listeners map[*Session]func(FontSettings)
}{
	current:   defaultFontSettings,
	listeners: make(map[*Session]func(FontSettings)),
}

// currentFontSettings returns the live shared font configuration.
func currentFontSettings() FontSettings {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	return fontState.current
}

// setFontSettings replaces the shared font configuration (clamped) and
// notifies every registered listener — every live Session's own reaction
// (Task 9) is what actually recomputes font metrics and re-layouts;
// setFontSettings itself has no Fyne/UI dependency at all, so it's testable
// in isolation (see font_test.go).
func setFontSettings(s FontSettings) {
	s = clampFontSettings(s)

	fontState.mu.Lock()
	fontState.current = s
	fns := make([]func(FontSettings), 0, len(fontState.listeners))
	for _, fn := range fontState.listeners {
		fns = append(fns, fn)
	}
	fontState.mu.Unlock()

	for _, fn := range fns {
		fn(s)
	}
}

// registerFontListener subscribes s to future setFontSettings calls,
// applying the change via fn (Task 9 wires fn to recompute s's font
// face/metrics and re-layout). Called from NewSession.
func registerFontListener(s *Session, fn func(FontSettings)) {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	fontState.listeners[s] = fn
}

// unregisterFontListener removes s's subscription. Called from Close.
func unregisterFontListener(s *Session) {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	delete(fontState.listeners, s)
}

// registerFontListenerFunc is a test-only seam: registerFontListener keys
// its map on *Session (a real PTY-backed widget, expensive to construct
// just to test notification plumbing), but the underlying mechanism doesn't
// actually need a *Session — any comparable key works. Tests use a
// throwaway *Session-shaped key via this helper instead of a real Session.
func registerFontListenerFunc(fn func(FontSettings)) (unregister func()) {
	key := new(Session)
	registerFontListener(key, fn)
	return func() { unregisterFontListener(key) }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run "TestDefaultFontSettings|TestSetFontSettingsNotifiesRegisteredListeners|TestClampFontSettings" -v`
Expected: PASS

- [ ] **Step 5: Run with race detector**

Run: `go test ./terminal/... -run "TestDefaultFontSettings|TestSetFontSettingsNotifiesRegisteredListeners|TestClampFontSettings" -race -v`
Expected: PASS — `fontState.mu` is what's under test here as much as the values.

- [ ] **Step 6: Commit**

```bash
git add terminal/font.go terminal/font_test.go
git commit -m "terminal: add shared live FontSettings state"
```

---

### Task 7: Settings schema + `ApplyFontSettings`

**Files:**
- Modify: `terminal/settings_schema.go`
- Test: `terminal/settings_schema_test.go`

**Interfaces:**
- Consumes: `DetectMonospaceFonts()` (Task 5), `FontSettings`/`clampFontSettings`/`setFontSettings` (Task 6).
- Produces:
  - `terminal.KeyFontFamily`, `KeyFontSize`, `KeyLineHeight`, `KeyColumnWidth` constants.
  - `func ApplyFontSettings(database *db.DB) error` — re-reads the four properties from `database`'s Terminal node and pushes them into the live shared state (Task 6). Returns an error only for a genuine db read failure; a missing Terminal node (never registered) is not an error — leaves the shared state untouched.
  - `RegisterSettings` seeds the four new properties (idempotent, same as today).
  - `readTerminalSettings` additionally returns a `FontSettings` value.

- [ ] **Step 1: Write the failing test**

Check the existing `terminal/settings_schema_test.go` first for its exact helper names (`test.NewDB()` vs. `db.Open(":memory:")` — earlier session transcript shows both patterns used in different `terminal` test files; match whichever this specific file already uses) before writing these, then add:

This file already has a `newTestDB(t *testing.T) *db.DB` helper (opens an in-memory db, registers `t.Cleanup` to close it) — use it instead of calling `db.Open` directly, matching every other test in this file:

```go
func TestRegisterSettingsSeedsFontProperties(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		t.Fatal("Terminal node not found")
	}
	props, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}

	byKey := make(map[string]db.Property)
	for _, p := range props {
		byKey[p.Key] = p
	}

	fontFamily, ok := byKey[KeyFontFamily]
	if !ok {
		t.Fatal("font_family property not seeded")
	}
	if fontFamily.Value != "(default)" {
		t.Errorf("font_family default = %q, want %q", fontFamily.Value, "(default)")
	}
	if fontFamily.Type != db.PropertyEnum {
		t.Errorf("font_family type = %q, want %q", fontFamily.Type, db.PropertyEnum)
	}
	found := false
	for _, opt := range fontFamily.EnumOptions {
		if opt == "(default)" {
			found = true
		}
	}
	if !found {
		t.Error("font_family enum options missing \"(default)\" sentinel")
	}

	fontSize, ok := byKey[KeyFontSize]
	if !ok || fontSize.Value != "13" || fontSize.Type != db.PropertyInt {
		t.Errorf("font_size = %+v, want value \"13\" type PropertyInt", fontSize)
	}

	lineHeight, ok := byKey[KeyLineHeight]
	if !ok || lineHeight.Value != "1.0" || lineHeight.Type != db.PropertyFloat {
		t.Errorf("line_height = %+v, want value \"1.0\" type PropertyFloat", lineHeight)
	}

	columnWidth, ok := byKey[KeyColumnWidth]
	if !ok || columnWidth.Value != "1.0" || columnWidth.Type != db.PropertyFloat {
		t.Errorf("column_width = %+v, want value \"1.0\" type PropertyFloat", columnWidth)
	}
}

func TestApplyFontSettingsPushesDbValuesIntoLiveState(t *testing.T) {
	defer setFontSettings(defaultFontSettings) // restore for other tests

	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, terminalSettingsLabel)

	if err := d.SaveProperties(node.ID, map[string]string{
		KeyFontSize:   "18",
		KeyLineHeight: "1.4",
	}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	if err := ApplyFontSettings(d); err != nil {
		t.Fatalf("ApplyFontSettings: %v", err)
	}

	got := currentFontSettings()
	if got.Size != 18 {
		t.Errorf("currentFontSettings().Size = %d, want 18", got.Size)
	}
	if got.LineHeight != 1.4 {
		t.Errorf("currentFontSettings().LineHeight = %v, want 1.4", got.LineHeight)
	}
}

func TestApplyFontSettingsNoTerminalNodeIsNotAnError(t *testing.T) {
	d := newTestDB(t)

	if err := ApplyFontSettings(d); err != nil {
		t.Errorf("ApplyFontSettings (no Terminal node): %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run "TestRegisterSettingsSeedsFontProperties|TestApplyFontSettings" -v`
Expected: FAIL — `KeyFontFamily` etc. undefined, `ApplyFontSettings` undefined.

- [ ] **Step 3: Implement the schema extension**

In `terminal/settings_schema.go`, extend the key constants:

```go
const (
	KeyDefaultShell = "default_shell"
	KeyCloseOnExit  = "close_on_exit"
	KeyFontFamily   = "font_family"
	KeyFontSize     = "font_size"
	KeyLineHeight   = "line_height"
	KeyColumnWidth  = "column_width"
)

// fontFamilyDefault is the sentinel font_family value meaning "use the
// bundled font" — DetectMonospaceFonts() never returns this string itself
// (it only lists real installed font names), so it can't collide with a
// genuine family name.
const fontFamilyDefault = "(default)"
```

In `RegisterSettings`, after the existing `close_on_exit` `AddProperty` call, add the four new properties:

```go
	if err := database.AddProperty(nodeID, KeyCloseOnExit, "Close tab on shell exit", db.PropertyBool, "true", nil); err != nil {
		return err
	}

	fontOptions := append([]string{fontFamilyDefault}, DetectMonospaceFonts()...)
	if err := database.AddProperty(nodeID, KeyFontFamily, "Font", db.PropertyEnum, fontFamilyDefault, fontOptions); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyFontSize, "Font size", db.PropertyInt, "13", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyLineHeight, "Line height", db.PropertyFloat, "1.0", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyColumnWidth, "Column width", db.PropertyFloat, "1.0", nil); err != nil {
		return err
	}
	return nil
}
```

Extend `readTerminalSettings` to also parse and return a `FontSettings`:

```go
func readTerminalSettings(database *db.DB) (defaultShell string, closeOnExit bool, font FontSettings, found bool, err error) {
	nodes, err := database.ListSettings()
	if err != nil {
		return "", false, FontSettings{}, false, err
	}

	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		return "", false, FontSettings{}, false, nil
	}

	props, err := database.GetProperties(node.ID)
	if err != nil {
		return "", false, FontSettings{}, false, err
	}

	closeOnExit = true // matches RegisterSettings' seeded default
	font = defaultFontSettings
	for _, p := range props {
		switch p.Key {
		case KeyDefaultShell:
			defaultShell = p.Value
		case KeyCloseOnExit:
			closeOnExit = p.Value == "true"
		case KeyFontFamily:
			if p.Value != fontFamilyDefault {
				font.Family = p.Value
			}
		case KeyFontSize:
			if v, err := strconv.Atoi(p.Value); err == nil {
				font.Size = v
			}
		case KeyLineHeight:
			if v, err := strconv.ParseFloat(p.Value, 64); err == nil {
				font.LineHeight = v
			}
		case KeyColumnWidth:
			if v, err := strconv.ParseFloat(p.Value, 64); err == nil {
				font.ColumnWidth = v
			}
		}
	}
	font = clampFontSettings(font)
	return defaultShell, closeOnExit, font, true, nil
}
```

This changes `readTerminalSettings`'s signature — find its one existing caller (`NewWindowFromSettings` in `terminal/window.go`) and update the call site in this same step:

```go
	defaultShell, closeOnExit, font, found, err := readTerminalSettings(database)
	if err != nil {
		return nil, err
	}
	if found {
		shells = withDefaultFirst(shells, defaultShell)
		setFontSettings(font)
	}
```

(Insert `setFontSettings(font)` right after the existing `shells = withDefaultFirst(...)` line inside that `if found` block — check `terminal/window.go`'s exact current line numbers before editing, since Task 8/9's changes elsewhere in this plan don't touch `window.go` and this is the only place `readTerminalSettings` is called.)

Add the import for `strconv` at the top of `settings_schema.go`:

```go
import (
	"strconv"

	"go-ux/db"
)
```

Add `ApplyFontSettings`:

```go
// ApplyFontSettings re-reads font_family/font_size/line_height/column_width
// from database's Terminal node and pushes them into the live shared
// FontSettings (font.go) — every open Session re-renders against the new
// values immediately. A host app calls this after a Settings-window
// OK/Apply commits new font values, the same way NewWindowFromSettings
// applies them once at window-construction time. A database with no
// Terminal node yet (RegisterSettings never called) is not an error —
// ApplyFontSettings simply leaves the live state untouched, same
// graceful-fallback contract readTerminalSettings itself already has.
func ApplyFontSettings(database *db.DB) error {
	_, _, font, found, err := readTerminalSettings(database)
	if err != nil {
		return err
	}
	if found {
		setFontSettings(font)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run "TestRegisterSettingsSeedsFontProperties|TestApplyFontSettings" -v`
Expected: PASS

- [ ] **Step 5: Run the full terminal package test suite (non-Windows-specific parts still compile/run)**

Run: `go build ./... && go vet ./...`
Expected: clean — this step's `readTerminalSettings` signature change must not have broken `window.go`'s call site.

Run: `go test ./terminal/... -run "TestRegisterSettings|TestNewWindowFromSettings" -v`
Expected: PASS (both the new tests and the pre-existing `default_shell`/`close_on_exit` tests, confirming the signature change didn't regress existing behavior)

- [ ] **Step 6: Commit**

```bash
git add terminal/settings_schema.go terminal/settings_schema_test.go terminal/window.go
git commit -m "terminal: register font settings and add ApplyFontSettings"
```

---

### Task 8: Renderer supports family/line-height/column-width

**Files:**
- Modify: `terminal/render.go`
- Test: `terminal/render_test.go`

**Interfaces:**
- Consumes: `FontSettings`, `currentFontSettings()` (Task 6).
- Produces: `loadMonospaceFace` gains a `family string` parameter. `newGridRenderer` reads `currentFontSettings()` at construction. New method `func (r *gridRenderer) applyFontSettings(s FontSettings)` — reloads the font face if `Family`/`Size` changed and repaints; `paint`'s per-cell math applies `s.LineHeight`/`s.ColumnWidth` as multipliers (fields added to `gridRenderer` to hold the current multipliers).

- [ ] **Step 1: Write the failing test**

Add to `terminal/render_test.go` (after checking its existing imports match — it already imports `"image"` and this package; no new imports needed for these two tests beyond what Task 6 added to the package):

```go
func TestGridRendererAppliesLineHeightAndColumnWidthMultipliers(t *testing.T) {
	v := newVTState(10, 5)
	r := newGridRenderer(v)

	baseW, baseH := r.pixelSize()

	r.applyFontSettings(FontSettings{Family: "", Size: 13, LineHeight: 2.0, ColumnWidth: 1.5})

	gotW, gotH := r.pixelSize()
	wantW := int(float64(baseW) * 1.5)
	wantH := int(float64(baseH) * 2.0)
	if gotW != wantW {
		t.Errorf("pixelSize width after ColumnWidth=1.5 = %d, want %d", gotW, wantW)
	}
	if gotH != wantH {
		t.Errorf("pixelSize height after LineHeight=2.0 = %d, want %d", gotH, wantH)
	}
}

func TestPaintUsesMultipliedCellSize(t *testing.T) {
	v := newVTState(4, 2)
	r := newGridRenderer(v)
	r.applyFontSettings(FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 2.0})

	wantW, wantH := r.pixelSize()
	img := r.raster.Generator(wantW, wantH)
	b := img.Bounds()
	if b.Dx() != wantW || b.Dy() != wantH {
		t.Errorf("painted image = %dx%d, want %dx%d (pixelSize after ColumnWidth=2.0)", b.Dx(), b.Dy(), wantW, wantH)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run "TestGridRendererAppliesLineHeightAndColumnWidthMultipliers|TestPaintUsesMultipliedCellSize" -v`
Expected: FAIL — `r.applyFontSettings` undefined.

- [ ] **Step 3: Implement family/multiplier support**

In `terminal/render.go`, add multiplier fields to `gridRenderer` and change construction to read the live font settings:

```go
type gridRenderer struct {
	state *vtState

	face   xfont.Face
	cellW  int // natural per-cell size from the loaded face, before lineHeight/columnWidth
	cellH  int
	ascent int // baseline offset from the cell's top, in pixels

	// lineHeight/columnWidth are the multipliers applied on top of
	// cellW/cellH — see paint's use of them and applyFontSettings.
	lineHeight  float64
	columnWidth float64

	mu  sync.Mutex // guards img against concurrent refresh + raster generate
	img *image.RGBA

	raster *canvas.Raster
}
```

```go
func newGridRenderer(state *vtState) *gridRenderer {
	r := &gridRenderer{state: state, lineHeight: 1.0, columnWidth: 1.0}
	s := currentFontSettings()
	r.face, r.cellW, r.cellH, r.ascent = loadMonospaceFace(s.Family, float64(s.Size))
	r.lineHeight, r.columnWidth = s.LineHeight, s.ColumnWidth

	r.raster = canvas.NewRaster(r.paint)
	r.raster.ScaleMode = canvas.ImageScalePixels
	return r
}

// applyFontSettings reloads the font face (if Family/Size changed) and
// updates the line-height/column-width multipliers, then repaints. Called
// once at construction (via currentFontSettings(), above) and again
// whenever the shared FontSettings changes (widget.go wires this to
// registerFontListener — Task 9).
func (r *gridRenderer) applyFontSettings(s FontSettings) {
	r.mu.Lock()
	r.face, r.cellW, r.cellH, r.ascent = loadMonospaceFace(s.Family, float64(s.Size))
	r.lineHeight, r.columnWidth = s.LineHeight, s.ColumnWidth
	r.mu.Unlock()

	r.refresh()
}
```

Update `paint` and `drawCell` to apply the multipliers (`paint` now needs `r.mu` held slightly earlier, to read `lineHeight`/`columnWidth` alongside `cellW`/`cellH`/`face`/`ascent` — all under the one lock, consistent with the existing comment that `img` and these now-mutable font fields must stay coherent with each other):

```go
func (r *gridRenderer) paint(w, h int) image.Image {
	snap := r.state.snapshot()

	r.mu.Lock()
	defer r.mu.Unlock()

	w, h = max(w, 1), max(h, 1)
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	cols, rows := max(snap.Cols, 1), max(snap.Rows, 1)
	cellW := float64(w) / float64(cols)
	cellH := float64(h) / float64(rows)

	drawer := &xfont.Drawer{Dst: r.img, Face: r.face}
	for y := 0; y < snap.Rows; y++ {
		for x := 0; x < snap.Cols; x++ {
			r.drawCell(drawer, snap.Cells[y][x], x, y, cellW, cellH)
		}
	}
	return r.img
}
```

(`paint`'s own body is unchanged from before this task — `cellW`/`cellH` here are already derived from the raster's actual requested pixel size per Task 8's prerequisite work from the prior session, and `pixelSize`, changed below, is what makes that requested size reflect the multipliers in the first place; `drawCell`'s ascent-scaling line already divides by `r.cellH`, which now means "natural cellH before multipliers" — no change needed there, since `cellH` passed in is still the multiplied *actual* per-cell height, exactly what that scaling calculation wants.)

Update `pixelSize` to include the multipliers, since it's `pixelSize` that determines `MinSize`, which is what ultimately makes `gridDims` (Task 9) and `paint`'s own `(w, h)` request reflect `LineHeight`/`ColumnWidth` at all:

```go
// pixelSize reports the natural pixel size of the current grid — including
// the LineHeight/ColumnWidth multipliers (applyFontSettings) — used by the
// widget renderer's MinSize/Layout so the on-screen raster maps 1:1 to the
// rasterized cells before any scaling.
func (r *gridRenderer) pixelSize() (w, h int) {
	r.mu.Lock()
	cellW := float64(r.cellW) * r.columnWidth
	cellH := float64(r.cellH) * r.lineHeight
	r.mu.Unlock()

	cols, rows := r.state.size()
	return int(float64(cols) * cellW), int(float64(rows) * cellH)
}
```

Update `loadMonospaceFace` to accept a family:

```go
// loadMonospaceFace loads the named font family at the given pixel size and
// reports the resulting fixed cell box and baseline. family == "" loads
// Fyne's bundled monospace font (this package's original, still-default
// behavior); a non-empty family is looked up the same way
// DetectMonospaceFonts (font_windows.go) found it, by scanning the Windows
// font registry for a matching display name. Any failure at any step —
// unknown family, a file that no longer exists, a parse error — falls back
// to the bundled font, then to golang.org/x/image/font/basicfont if even
// that fails to parse, so rendering can never fail to produce *some*
// legible grid.
func loadMonospaceFace(family string, sizePx float64) (face xfont.Face, cellW, cellH, ascent int) {
	if family != "" {
		if data, ok := loadSystemFontFile(family); ok {
			if fnt, err := opentype.Parse(data); err == nil {
				if f, cw, ch, asc, ok := faceMetrics(fnt, sizePx); ok {
					return f, cw, ch, asc
				}
			}
		}
	}

	res := theme.DefaultTextMonospaceFont()
	fnt, err := opentype.Parse(res.Content())
	if err == nil {
		if f, cw, ch, asc, ok := faceMetrics(fnt, sizePx); ok {
			return f, cw, ch, asc
		}
	}

	// Fallback: fixed bitmap face with known metrics.
	bf := basicfont.Face7x13
	return bf, defaultCellWidth, defaultCellHeight, bf.Ascent
}

// faceMetrics builds a face from fnt at sizePx and reports its cell box —
// factored out of loadMonospaceFace so both the system-font and
// bundled-font paths share the same "measure via GlyphAdvance('M')" logic.
// ok is false if the face can't be built or can't report a usable advance.
func faceMetrics(fnt *opentype.Font, sizePx float64) (face xfont.Face, cellW, cellH, ascent int, ok bool) {
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     72, // 1 point == 1 pixel, so Size is effectively in pixels
		Hinting: xfont.HintingFull,
	})
	if err != nil {
		return nil, 0, 0, 0, false
	}
	m := f.Metrics()
	adv, advOK := f.GlyphAdvance('M')
	if !advOK || adv <= 0 {
		return nil, 0, 0, 0, false
	}
	cellW = adv.Ceil()
	cellH = (m.Ascent + m.Descent).Ceil()
	return f, max(cellW, 1), max(cellH, 1), m.Ascent.Ceil(), true
}
```

Add `loadSystemFontFile` — this needs the same registry lookup `font_windows.go` already does, factored so both files can use it. Add to `terminal/font_windows.go` (not `render.go`, since it's Windows-registry-specific — `render.go` has no build tag and must stay buildable on a hypothetical future non-Windows target):

```go
// loadSystemFontFile finds family among the system/per-user font registry
// keys (the same ones scanFontRegistryKey enumerates) and returns its raw
// file bytes. ok is false if no matching display name is found or its file
// can't be read — loadMonospaceFace (render.go) treats that as "fall back
// to the bundled font", not an error.
func loadSystemFontFile(family string) (data []byte, ok bool) {
	winDir := os.Getenv("SystemRoot")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")

	find := func(root registry.Key, path string, resolvePath func(string) string) (string, bool) {
		key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
		if err != nil {
			return "", false
		}
		defer key.Close()

		valueNames, err := key.ReadValueNames(-1)
		if err != nil {
			return "", false
		}
		for _, regName := range valueNames {
			if fontDisplayName(regName) != family {
				continue
			}
			file, _, err := key.GetStringValue(regName)
			if err != nil || file == "" {
				continue
			}
			return resolvePath(file), true
		}
		return "", false
	}

	if path, found := find(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		func(file string) string {
			if filepath.IsAbs(file) {
				return file
			}
			return filepath.Join(fontsDir, file)
		}); found {
		if data, err := os.ReadFile(path); err == nil {
			return data, true
		}
	}
	if path, found := find(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Fonts`,
		func(file string) string { return file }); found {
		if data, err := os.ReadFile(path); err == nil {
			return data, true
		}
	}
	return nil, false
}
```

This duplicates the registry-walking shape of `scanFontRegistryKey` — acceptable per YAGNI (a shared abstraction over "walk both keys" would need a callback returning "stop early with a result" vs. "collect all", different enough shapes that factoring them together now would obscure both, not simplify either).

`render.go`'s existing `loadMonospaceFace(float64(defaultCellHeight))` call site inside `newGridRenderer` already gets rewritten in this same step (shown above, `newGridRenderer` now calls `loadMonospaceFace(s.Family, float64(s.Size))`) — search `render.go` for any *other* callers of `loadMonospaceFace` before finishing this step; there should be none besides `newGridRenderer` and this task's own `applyFontSettings`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run "TestGridRendererAppliesLineHeightAndColumnWidthMultipliers|TestPaintUsesMultipliedCellSize" -v`
Expected: PASS

- [ ] **Step 5: Run the full render_test.go suite (regression check on prior-session tests)**

Run: `go test ./terminal/... -run TestGridRenderer -v`
Expected: PASS — including `TestGridRendererResizeKeepsSizesConsistent`, `TestGridRendererImageMatchesGridPixelSize`, `TestGridRendererCellSizePositive` from the prior session's raster rewrite (these call `newGridRenderer`/`pixelSize`/`resize` with no font-settings changes, so they exercise the `LineHeight=1.0, ColumnWidth=1.0` no-op path — a regression here means the multiplier math broke the unmultiplied case).

- [ ] **Step 6: Commit**

```bash
git add terminal/render.go terminal/font_windows.go terminal/render_test.go
git commit -m "terminal: apply font family/size/line-height/column-width in the renderer"
```

---

### Task 9: `gridDims` multipliers + Session reacts to live font changes

**Files:**
- Modify: `terminal/widget.go`
- Test: `terminal/widget_windows_test.go` (existing file — check its current content for the package/build-tag pattern before adding)

**Interfaces:**
- Consumes: `applyFontSettings` (Task 8), `registerFontListener`/`unregisterFontListener` (Task 6).
- Produces: `gridDims` gains `lineHeight, columnWidth float64` parameters. `NewSession` registers a font listener; `Close` unregisters it.

- [ ] **Step 1: Write the failing test**

Add to `terminal/widget_windows_test.go` (this is a `_windows_test.go` file — check its existing `//go:build windows` tag and package clause before adding; it needs a real PTY, matching this file's existing pattern for `TestNewSessionConstructsAndCloses`-style tests):

```go
func TestGridDimsAppliesLineHeightAndColumnWidthMultipliers(t *testing.T) {
	cols, rows := gridDims(800, 480, 10, 20, 1.0, 1.0)
	wantCols, wantRows := 80, 24
	if cols != wantCols || rows != wantRows {
		t.Fatalf("gridDims(800, 480, 10, 20, 1.0, 1.0) = %d,%d, want %d,%d", cols, rows, wantCols, wantRows)
	}

	// Doubling column width halves how many columns fit in the same pixel
	// width; doubling line height halves how many rows fit.
	cols2, rows2 := gridDims(800, 480, 10, 20, 2.0, 2.0)
	if cols2 != wantCols/2 {
		t.Errorf("gridDims with ColumnWidth=2.0: cols = %d, want %d", cols2, wantCols/2)
	}
	if rows2 != wantRows/2 {
		t.Errorf("gridDims with LineHeight=2.0: rows = %d, want %d", rows2, wantRows/2)
	}
}

func TestSessionReactsToLiveFontSettingsChange(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings) // restore for other tests

	sess, err := NewSession(cmdDef("font-live-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	before := sess.render.cellH
	setFontSettings(FontSettings{Family: "", Size: 24, LineHeight: 1.0, ColumnWidth: 1.0})

	after := sess.render.cellH
	if after == before {
		t.Errorf("session's render.cellH unchanged after setFontSettings (before=%d, after=%d) — session did not react to the live font change", before, after)
	}
}
```

(`cmdDef` is this test file's existing helper for building a `ShellDef` pointed at `cmd.exe` — reuse it, matching every other test in this file; check its exact name/signature in the file before writing this step if it differs from `cmdDef(name string) ShellDef`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run "TestGridDimsAppliesLineHeightAndColumnWidthMultipliers|TestSessionReactsToLiveFontSettingsChange" -v`
Expected: FAIL — `gridDims` called with 6 args against a 4-arg signature (compile error) and/or `sess.render.cellH` unchanged (no listener wired yet).

- [ ] **Step 3: Update `gridDims` and wire the Session listener**

In `terminal/widget.go`, change `gridDims`:

```go
// gridDims converts a pixel size, per-cell box, and LineHeight/ColumnWidth
// multipliers into a grid cell count, clamped to at least 1x1 so a
// zero-area layout pass can't collapse the grid. Extracted as a pure
// function so the size math is unit-testable without spawning a PTY.
func gridDims(width, height, cellW, cellH int, lineHeight, columnWidth float64) (cols, rows int) {
	if cellW <= 0 || cellH <= 0 {
		return 1, 1
	}
	effW := float64(cellW) * columnWidth
	effH := float64(cellH) * lineHeight
	if effW <= 0 || effH <= 0 {
		return 1, 1
	}
	return max(1, int(float64(width)/effW)), max(1, int(float64(height)/effH))
}
```

Update `Resize`'s call site to pass the current multipliers (read from `s.render`, which Task 8 already keeps up to date via `applyFontSettings`):

```go
func (s *Session) Resize(size fyne.Size) {
	s.BaseWidget.Resize(size)

	s.render.mu.Lock()
	lineHeight, columnWidth := s.render.lineHeight, s.render.columnWidth
	s.render.mu.Unlock()

	cols, rows := gridDims(int(size.Width), int(size.Height), s.cellW, s.cellH, lineHeight, columnWidth)
	if cols < minSaneCols || rows < minSaneRows {
```

(The rest of `Resize`'s body — the `ptyResized` check, the `exited`-guarded `s.pty.Resize` call, `s.render.resize`, `s.refreshCursor()` — is unchanged; only the `gridDims` call itself gains the two new arguments, sourced from `s.render`'s now-exported-within-package `lineHeight`/`columnWidth` fields under its existing `mu`.)

This reaches into `s.render`'s private fields (`mu`, `lineHeight`, `columnWidth`) from `widget.go` — legal since both files are in the same `terminal` package, and consistent with `Resize` already reaching into `s.cellW`/`s.cellH` (`Session`'s own copies) alongside `s.render` today.

Register/unregister the font listener in `NewSession`/`Close`:

```go
	s.wg.Add(3)
	go s.readLoop()
	go s.refreshLoop()
	go s.blinkLoop()
	go s.waitLoop()

	registerFontListener(s, func(fs FontSettings) {
		s.render.applyFontSettings(fs)
		doUI(func() {
			s.Resize(s.Size())
			s.refreshCursor()
		})
	})

	return s, nil
}
```

(Insert the `registerFontListener` call between the four `go s.*Loop()` lines and `return s, nil` in `NewSession`.) The listener's own closure body does two things on two different sides of the UI-goroutine boundary, deliberately: `s.render.applyFontSettings(fs)` first — `gridRenderer.applyFontSettings` (Task 8) only touches `r.mu`-guarded fields and calls `canvas.Refresh` (already established as safe to call off the UI goroutine, matching `refresh`'s existing doc comment) — then `doUI(...)` for the parts that must run on the UI goroutine: re-running `Resize` with the widget's current size (so `gridDims` picks up the new multipliers and the PTY/vt10x legs stay in sync, same three-way sync `Resize` already owns) and an explicit `refreshCursor()` (cursor size/position depends on cell size too).

Add the unregister call to `Close`:

```go
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = s.pty.Close()
	})
	s.wg.Wait()
	unregisterFontListener(s)
	return s.closeErr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run "TestGridDimsAppliesLineHeightAndColumnWidthMultipliers|TestSessionReactsToLiveFontSettingsChange" -v`
Expected: PASS

- [ ] **Step 5: Run the full terminal package test suite, plain and race, multiple times**

Run (repeat 3x, since this project's history shows real flakiness only surfaces on repeated runs): `go test ./terminal/... -count=1 -timeout 60s -v` then `go test ./terminal/... -race -count=1 -timeout 60s -v`
Expected: PASS every time — this task adds a new cross-goroutine notification path (`setFontSettings` can be called from any goroutine, same category of risk the prior session's `uiMu`/uses of `doUI` exist to guard against) into `Session`'s existing goroutine set, so this is exactly the kind of change that previously produced intermittent `STATUS_HEAP_CORRUPTION` — do not skip the repeated-run check.

- [ ] **Step 6: Commit**

```bash
git add terminal/widget.go terminal/widget_windows_test.go
git commit -m "terminal: gridDims applies font multipliers; Session reacts to live font changes"
```

---

### Task 10: Ctrl+scrollwheel font-size adjustment

**Files:**
- Modify: `terminal/widget.go`
- Test: `terminal/widget_windows_test.go`

**Interfaces:**
- Consumes: `desktop.Keyable`/`desktop.KeyControlLeft`/`desktop.KeyControlRight` (`fyne.io/fyne/v2/driver/desktop`), `fyne.Scrollable`/`fyne.ScrollEvent`, `currentFontSettings`/`setFontSettings` (Task 6), package-level active-db reference (new in this task).
- Produces: `Session` implements `desktop.Keyable` (`KeyDown`/`KeyUp`) and `fyne.Scrollable` (`Scrolled`). `func setActiveFontDB(database *db.DB)` — package-internal, called from `NewWindowFromSettings` (Task 7's `window.go` edit gets one more line in this task) so Ctrl+scroll's debounced write has somewhere to persist to; nil (the zero value) is valid and means "no persistence", matching `NewWindow`-only callers per the design's "db is optional" principle.

- [ ] **Step 1: Write the failing test**

Add to `terminal/widget_windows_test.go`:

```go
func TestCtrlScrollAdjustsFontSizeLiveAndClamps(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings) // restore for other tests

	sess, err := NewSession(cmdDef("ctrl-scroll-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	sess.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}}) // scroll "up" = larger

	got := currentFontSettings().Size
	want := defaultFontSettings.Size + fontSizeScrollStep
	if got != want {
		t.Errorf("font size after one Ctrl+scroll-up tick = %d, want %d", got, want)
	}

	// Clamp: many ticks shouldn't exceed maxFontSize.
	for i := 0; i < 30; i++ {
		sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}})
	}
	if got := currentFontSettings().Size; got != maxFontSize {
		t.Errorf("font size after many Ctrl+scroll-up ticks = %d, want %d (clamped)", got, maxFontSize)
	}

	sess.KeyUp(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	before := currentFontSettings().Size
	sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}}) // Ctrl no longer held: no-op
	if got := currentFontSettings().Size; got != before {
		t.Errorf("font size changed on a scroll without Ctrl held: %d -> %d, want unchanged", before, got)
	}
}
```

`widget_windows_test.go`'s current imports are `"os"`, `"testing"`, `"time"`, and `"fyne.io/fyne/v2/test"` (as plain `test` — confirmed, no `go-ux/test` import in this file to conflict with, unlike `settings_test.go`). This test needs two more: `"fyne.io/fyne/v2"` (for `fyne.KeyEvent`/`fyne.ScrollEvent`/`fyne.Delta`, not currently imported at all — only the `/test` subpackage is) and `"fyne.io/fyne/v2/driver/desktop"` (for `desktop.KeyControlLeft`). Add both.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run TestCtrlScrollAdjustsFontSizeLiveAndClamps -v`
Expected: FAIL — `sess.KeyDown`/`sess.Scrolled` undefined (`*Session` doesn't implement `desktop.Keyable`/`fyne.Scrollable` yet), `fontSizeScrollStep` undefined.

- [ ] **Step 3: Implement Ctrl+scroll**

In `terminal/widget.go`, add the import:

```go
import (
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)
```

Add a `ctrlHeld` field to `Session` (guarded by the existing `mu`) and the scroll-step constant plus a debounce duration:

```go
	mu       sync.Mutex // guards onExit, blinkOn, focused, and ctrlHeld
	onExit   func()
	exitDone bool
	blinkOn  bool
	ctrlHeld bool
```

```go
const (
	// fontSizeScrollStep/fontSizeSaveDebounce drive Ctrl+scroll's live font-
	// size adjustment (KeyDown/KeyUp/Scrolled below): 2pt per wheel tick,
	// and a live-typing-speed idle period before persisting to the db, so a
	// fast scroll doesn't write to SQLite dozens of times a second.
	fontSizeScrollStep    = 2
	fontSizeSaveDebounce  = 400 * time.Millisecond
)
```

Add `KeyDown`/`KeyUp` (`desktop.Keyable`):

```go
// KeyDown/KeyUp track Ctrl's held state (desktop.Keyable) — used only by
// Scrolled, below, to distinguish a plain scroll (a no-op today: this
// package has no scrollback/mouse-reporting yet) from a Ctrl+scroll
// (adjusts font size). fyne.ScrollEvent itself carries no modifier-key
// information, so this is the only way to know whether Ctrl was held
// during a given scroll. Same UI-goroutine threading note as
// TypedRune/TypedKey: Fyne's input dispatch calls these directly on the UI
// goroutine.
func (s *Session) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyControlLeft || ev.Name == desktop.KeyControlRight {
		s.mu.Lock()
		s.ctrlHeld = true
		s.mu.Unlock()
	}
}

func (s *Session) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyControlLeft || ev.Name == desktop.KeyControlRight {
		s.mu.Lock()
		s.ctrlHeld = false
		s.mu.Unlock()
	}
}

// Scrolled (fyne.Scrollable) adjusts the live shared font size when Ctrl is
// held (see KeyDown/KeyUp) — one fontSizeScrollStep per wheel tick, clamped
// to [minFontSize, maxFontSize] by setFontSettings itself. Without Ctrl
// held, this is a no-op: this package has no scrollback/mouse-reporting
// yet (see terminal.md's deferred-features list). Same UI-goroutine
// threading note as TypedRune/TypedKey.
func (s *Session) Scrolled(ev *fyne.ScrollEvent) {
	s.mu.Lock()
	held := s.ctrlHeld
	s.mu.Unlock()
	if !held {
		return
	}

	current := currentFontSettings()
	delta := fontSizeScrollStep
	if ev.Scrolled.DY < 0 {
		delta = -delta
	}
	current.Size += delta
	setFontSettings(current)

	scheduleFontSizeSave()
}
```

Add the debounced-save machinery and the active-db reference at package scope (below `uiMu`/`doUI`, since it's the same category of package-level shared state):

```go
// activeFontDB is the db Ctrl+scroll's debounced save writes font_size to —
// nil means "no persistence", the same as if no db were involved in the
// first place (NewWindow-only callers). Set by setActiveFontDB, called
// from NewWindowFromSettings (window.go) — the only terminal constructor
// that has a *db.DB to begin with.
var (
	activeFontDBMu sync.Mutex
	activeFontDB   *db.DB

	fontSizeSaveTimerMu sync.Mutex
	fontSizeSaveTimer   *time.Timer
)

// setActiveFontDB records database as the target for Ctrl+scroll's
// debounced font-size persistence. Called once per NewWindowFromSettings
// call; the most recent call wins if more than one Window is open against
// different databases (matches this package's existing single-active-
// registry assumption — see uiMu/winptyMu's own package-level-state
// precedent).
func setActiveFontDB(database *db.DB) {
	activeFontDBMu.Lock()
	activeFontDB = database
	activeFontDBMu.Unlock()
}

// scheduleFontSizeSave (re)starts a fontSizeSaveDebounce timer that writes
// the current shared FontSettings.Size to activeFontDB (if one is set) once
// it fires — repeated calls before it fires (a fast scroll) just push the
// deadline back, so a burst of scroll ticks produces one write, not one per
// tick.
func scheduleFontSizeSave() {
	fontSizeSaveTimerMu.Lock()
	defer fontSizeSaveTimerMu.Unlock()

	if fontSizeSaveTimer != nil {
		fontSizeSaveTimer.Stop()
	}
	fontSizeSaveTimer = time.AfterFunc(fontSizeSaveDebounce, saveFontSizeNow)
}

func saveFontSizeNow() {
	activeFontDBMu.Lock()
	database := activeFontDB
	activeFontDBMu.Unlock()
	if database == nil {
		return
	}

	nodes, err := database.ListSettings()
	if err != nil {
		return
	}
	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		return
	}

	size := currentFontSettings().Size
	_ = database.SaveProperties(node.ID, map[string]string{
		KeyFontSize: strconv.Itoa(size),
	})
}
```

Add `"go-ux/db"` and `"strconv"` to `widget.go`'s import block alongside the existing ones.

Wire `setActiveFontDB` into `NewWindowFromSettings` — in `terminal/window.go`, right after Task 7's `setFontSettings(font)` line:

```go
	if found {
		shells = withDefaultFirst(shells, defaultShell)
		setFontSettings(font)
	}
	setActiveFontDB(database)
```

(`setActiveFontDB(database)` is unconditional, outside the `if found` block — even a database with no Terminal node yet should still receive a Ctrl+scroll-driven `font_size` write attempt; `saveFontSizeNow`'s own `findRootNode` check already handles "no Terminal node" gracefully by simply not writing, same as `readTerminalSettings`'s existing contract.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./terminal/... -run TestCtrlScrollAdjustsFontSizeLiveAndClamps -v`
Expected: PASS

- [ ] **Step 5: Write and run a debounced-save test**

Add to `terminal/widget_windows_test.go`. Uses `newTestDB(t)` (defined in `settings_schema_test.go`, same `terminal` package, no import needed for it) — this test itself only additionally needs `"strconv"` imported in the test file (not present yet):

```go
func TestCtrlScrollDebouncedSavePersistsAfterIdle(t *testing.T) {
	test.NewApp()
	defer setFontSettings(defaultFontSettings)
	defer setActiveFontDB(nil)

	d := newTestDB(t) // helper already defined in settings_schema_test.go, same package
	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	setActiveFontDB(d)

	sess, err := NewSession(cmdDef("debounce-test"))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	sess.KeyDown(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	for i := 0; i < 3; i++ {
		sess.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: 1}})
	}

	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, terminalSettingsLabel)
	props, _ := d.GetProperties(node.ID)
	for _, p := range props {
		if p.Key == KeyFontSize && p.Value != "13" {
			t.Fatal("font_size written to db before the debounce period elapsed")
		}
	}

	time.Sleep(fontSizeSaveDebounce + 100*time.Millisecond)

	props, err = d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties (after debounce): %v", err)
	}
	want := strconv.Itoa(13 + 3*fontSizeScrollStep)
	found := false
	for _, p := range props {
		if p.Key == KeyFontSize {
			found = true
			if p.Value != want {
				t.Errorf("font_size in db = %q, want %q", p.Value, want)
			}
		}
	}
	if !found {
		t.Fatal("font_size property not found")
	}
}
```

Run: `go test ./terminal/... -run TestCtrlScrollDebouncedSavePersistsAfterIdle -v -timeout 10s`
Expected: PASS (the `time.Sleep` makes this a slower test — acceptable given it's exercising real debounce timing, not busy-waiting)

- [ ] **Step 6: Run the full terminal package test suite, plain and race, multiple times**

Run (3x each, same rationale as Task 9 Step 5): `go test ./terminal/... -count=1 -timeout 60s -v` and `go test ./terminal/... -race -count=1 -timeout 60s -v`
Expected: PASS every time.

- [ ] **Step 7: Commit**

```bash
git add terminal/widget.go terminal/widget_windows_test.go terminal/window.go
git commit -m "terminal: add Ctrl+scrollwheel live font-size adjustment with debounced persistence"
```

---

### Task 11: Full-project verification and docs

**Files:**
- Modify: `terminal.md` (mark the font-settings work as shipped; document the new public API)
- No new source files.

**Interfaces:** none new — this task is verification + documentation only.

- [ ] **Step 1: Full build/vet across the whole module**

Run: `go build ./...` then `go vet ./...`
Expected: both clean.

- [ ] **Step 2: Full test suite, every package, plain and race, repeated**

Run (repeat the full sequence 3x):
```
go test ./... -count=1 -timeout 120s -v
go test ./... -race -count=1 -timeout 120s -v
```
Expected: PASS every time, every package (`db`, `settings`, `terminal`, plus anything else in the module).

- [ ] **Step 3: Update `terminal.md`**

Add the four new `ShellDef`-adjacent constants and `ApplyFontSettings` to the "Public API" code block (find the existing block listing `KeyDefaultShell`-equivalent info — actually check: the current doc doesn't list `Key*` constants explicitly, only `RegisterSettings`'s prose describes `default_shell`/`close_on_exit`; follow that same pattern for the font keys, prose not a literal const listing) and extend the `RegisterSettings` prose paragraph:

```markdown
`RegisterSettings` seeds a root "Terminal" node with `default_shell`
(`PropertyEnum`, options from `DetectShells()`), `close_on_exit`
(`PropertyBool`, default `"true"`), and four font properties — `font_family`
(`PropertyEnum`, options from `DetectMonospaceFonts()` plus a `"(default)"`
sentinel meaning the bundled font, which is also the seeded default),
`font_size` (`PropertyInt`, default `13`), `line_height` and `column_width`
(`PropertyFloat`, both default `1.0`, multipliers of the font's natural
per-cell size) — in `database`'s registry, if one isn't already present.

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
```

- [ ] **Step 4: Commit**

```bash
git add terminal.md
git commit -m "docs: document terminal font settings in terminal.md"
```

- [ ] **Step 5: Push**

Only if the user has explicitly asked for a push in this session (check before running) — this plan's own scope ends at a clean, fully-committed local history; per this repo's established convention (CLAUDE.md), pushing/PRs happen only on explicit request.

## Out of scope (per design spec)

- Per-tab/per-session font overrides.
- Non-Windows font enumeration.
- A file-picker UI for an arbitrary font file.
- The visual/manual verification of Ctrl+scroll's actual modifier-key behavior and cross-window live redraw is GUI-dependent and left to the user testing the running app, same as this package's other visual work — not part of any task's automated test.
