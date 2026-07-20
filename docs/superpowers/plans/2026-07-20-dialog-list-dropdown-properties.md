# Custom dialog: editable list & dropdown/multi-select properties Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two new `dialog.Dialog` custom-form property kinds — an editable string list and a dropdown/checkbox-multi-select backed by a fixed option set — plus a `dialog.md` doc for the package, matching the design in `docs/superpowers/specs/2026-07-20-dialog-list-dropdown-properties-design.md`.

**Architecture:** Extend the existing `property` struct, `PropertyKind` enum, `buildCustomForm`, and `collectResult` switch statements in `dialog/dialog.go` — no new files, no new packages. Two new chainable builder methods (`AddPropertyList`, `AddPropertyOptions`) follow the existing `AddProperty` no-op-on-non-custom pattern.

**Tech Stack:** Go, `fyne.io/fyne/v2` v2.8.0 (`widget`, `data/binding`), existing `fynetest`-based test style in `dialog/dialog_test.go`.

## Global Constraints

- `fyne.io/fyne/v2` is pinned at v2.8.0 (go.mod) — `binding.List[string]` (alias `binding.StringList`), `widget.NewListWithData`, `widget.NewCheckGroup`, `widget.NewSelect` are all available at this version; verified against the vendored source at `C:\Users\jcaesar\go\pkg\mod\fyne.io\fyne\v2@v2.8.0`.
- `binding.List[T]` interface: `Append(T) error`, `Get() ([]T, error)`, `Remove(T) error` (removes first matching value, not by index), `Set([]T) error`.
- `*widget.CheckGroup` has exported field `Selected []string`; `*widget.Select` has exported field `Selected string`.
- Follow existing package doc-comment style: no comments beyond exported-symbol godoc and non-obvious "why" comments (see CLAUDE.md, repo root).
- Run `go build ./...`, `go vet ./...`, and `go test ./...` at the end of every task.

---

### Task 1: `PropertyKind` constants, `property` struct fields, and builder methods

**Files:**
- Modify: `dialog/dialog.go:27-35` (PropertyKind consts), `dialog/dialog.go:50-54` (property struct), `dialog/dialog.go:105-114` (after AddProperty)
- Test: `dialog/dialog_test.go` (append new test functions)

**Interfaces:**
- Produces: `PropertyList`, `PropertyDropdown`, `PropertyMultiSelect` (`PropertyKind` consts); `property.initial []string`, `property.options []string`, `property.selected []string`; `func (d *Dialog) AddPropertyList(key, label string, initial []string) *Dialog`; `func (d *Dialog) AddPropertyOptions(key, label string, kind PropertyKind, options, selected []string) *Dialog`.

- [ ] **Step 1: Write the failing tests**

Append to `dialog/dialog_test.go`:

```go
func TestAddPropertyListNoOpOnNonCustom(t *testing.T) {
	d := NewInfo("hello").AddPropertyList("items", "Items", []string{"a", "b"})
	if len(d.props) != 0 {
		t.Errorf("AddPropertyList should have no effect on an info dialog, got %d props", len(d.props))
	}
}

func TestAddPropertyOptionsNoOpOnNonCustom(t *testing.T) {
	d := NewInfo("hello").AddPropertyOptions("choice", "Choice", PropertyDropdown, []string{"x", "y"}, nil)
	if len(d.props) != 0 {
		t.Errorf("AddPropertyOptions should have no effect on an info dialog, got %d props", len(d.props))
	}
}

func TestAddPropertyListStoresInitialItems(t *testing.T) {
	d := NewCustom().AddPropertyList("items", "Items", []string{"a", "b"})
	if len(d.props) != 1 {
		t.Fatalf("expected 1 prop, got %d", len(d.props))
	}
	p := d.props[0]
	if p.key != "items" || p.label != "Items" || p.kind != PropertyList {
		t.Errorf("unexpected property: %#v", p)
	}
	if len(p.initial) != 2 || p.initial[0] != "a" || p.initial[1] != "b" {
		t.Errorf("initial = %#v, want [a b]", p.initial)
	}
}

func TestAddPropertyOptionsStoresOptionsAndSelected(t *testing.T) {
	d := NewCustom().AddPropertyOptions("choice", "Choice", PropertyDropdown, []string{"x", "y"}, []string{"y"})
	if len(d.props) != 1 {
		t.Fatalf("expected 1 prop, got %d", len(d.props))
	}
	p := d.props[0]
	if p.key != "choice" || p.label != "Choice" || p.kind != PropertyDropdown {
		t.Errorf("unexpected property: %#v", p)
	}
	if len(p.options) != 2 || p.options[0] != "x" || p.options[1] != "y" {
		t.Errorf("options = %#v, want [x y]", p.options)
	}
	if len(p.selected) != 1 || p.selected[0] != "y" {
		t.Errorf("selected = %#v, want [y]", p.selected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./dialog/... -run 'TestAddProperty(List|Options)' -v`
Expected: FAIL — `d.AddPropertyList` / `d.AddPropertyOptions` undefined (compile error).

- [ ] **Step 3: Add the constants, struct fields, and builder methods**

In `dialog/dialog.go`, extend the `PropertyKind` const block (currently lines 27-35):

```go
// PropertyKind selects the input widget AddProperty adds for a custom dialog.
type PropertyKind string

const (
	PropertyLabel       PropertyKind = "label"       // message only, no input, not included in the result
	PropertyBool        PropertyKind = "bool"        // widget.Check, result value is bool
	PropertyTextField   PropertyKind = "textField"    // widget.Entry, result value is string
	PropertyInt         PropertyKind = "int"          // validated widget.Entry, result value is int
	PropertyList        PropertyKind = "list"         // editable widget.List, add/remove, result value is []string
	PropertyDropdown    PropertyKind = "dropdown"     // widget.Select, result value is string
	PropertyMultiSelect PropertyKind = "multiSelect"  // widget.CheckGroup, result value is []string
)
```

Extend the `property` struct (currently lines 50-54):

```go
type property struct {
	key      string
	label    string
	kind     PropertyKind
	initial  []string // PropertyList seed items
	options  []string // PropertyDropdown/PropertyMultiSelect choice set
	selected []string // PropertyDropdown/PropertyMultiSelect pre-selection
}
```

Add two new methods directly after `AddProperty` (currently ending at line 114):

```go
// AddPropertyList adds an editable list row: an add/remove-capable list of
// strings seeded with initial. Result value is []string. Only effective on
// custom dialogs. Chainable.
func (d *Dialog) AddPropertyList(key, label string, initial []string) *Dialog {
	if d.kind != KindCustom {
		return d
	}
	d.props = append(d.props, property{key: key, label: label, kind: PropertyList, initial: initial})
	return d
}

// AddPropertyOptions adds a dropdown (PropertyDropdown, result value is
// string) or checkbox multi-select (PropertyMultiSelect, result value is
// []string) row. options is the fixed choice set; selected are the options
// pre-chosen. For PropertyDropdown, only selected[0] (if present) is used
// as the initial selection. Only effective on custom dialogs. Chainable.
func (d *Dialog) AddPropertyOptions(key, label string, kind PropertyKind, options, selected []string) *Dialog {
	if d.kind != KindCustom {
		return d
	}
	d.props = append(d.props, property{key: key, label: label, kind: kind, options: options, selected: selected})
	return d
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./dialog/... -run 'TestAddProperty(List|Options)' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Build and commit**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass (existing `buildCustomForm`/`collectResult` switches simply won't have cases for the three new kinds yet, which is fine — they compile and are unreachable until Task 2/3).

```bash
git add dialog/dialog.go dialog/dialog_test.go
git commit -m "Add PropertyList/PropertyDropdown/PropertyMultiSelect kinds and builder methods"
```

---

### Task 2: Render and collect `PropertyList`

**Files:**
- Modify: `dialog/dialog.go:135-141` (`builtDialog.fields` type), `dialog/dialog.go:205-236` (`buildCustomForm`), `dialog/dialog.go:238-260` (`collectResult`)
- Test: `dialog/dialog_test.go`

**Interfaces:**
- Consumes: `PropertyList`, `property{key, label, kind, initial}` from Task 1.
- Produces: `builtDialog.fields` is now `map[string]any` (was `map[string]fyne.CanvasObject`); for a `PropertyList` property, `fields[key]` holds a `binding.StringList` (i.e. `binding.List[string]`).

- [ ] **Step 1: Write the failing tests**

Append to `dialog/dialog_test.go` (needs a new import, added in Step 3 below — write the test first, it just won't compile yet):

```go
func TestCustomDialogListDefaultsToInitialItems(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyList("items", "Items", []string{"a", "b"})
	b := d.build(app)

	res := tapAndWait(t, b.okButton, b.resultCh)

	items, ok := res["items"].([]string)
	if !ok {
		t.Fatalf("items = %#v, want []string", res["items"])
	}
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Errorf("items = %#v, want [a b]", items)
	}
}

func TestCustomDialogListReflectsLiveEdits(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyList("items", "Items", []string{"a"})
	b := d.build(app)

	data, ok := b.fields["items"].(binding.StringList)
	if !ok {
		t.Fatalf("fields[items] = %#v, want binding.StringList", b.fields["items"])
	}
	if err := data.Append("c"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := data.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	res := tapAndWait(t, b.okButton, b.resultCh)

	items, ok := res["items"].([]string)
	if !ok {
		t.Fatalf("items = %#v, want []string", res["items"])
	}
	if len(items) != 1 || items[0] != "c" {
		t.Errorf("items = %#v, want [c]", items)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./dialog/... -run TestCustomDialogList -v`
Expected: FAIL (compile error — `binding` package not imported yet, `fields["items"]` doesn't produce a usable list result).

- [ ] **Step 3: Implement**

In `dialog/dialog.go`, add the import:

```go
import (
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)
```

Change `builtDialog.fields` type (currently line 140):

```go
type builtDialog struct {
	win          fyne.Window
	resultCh     chan map[string]any
	okButton     *widget.Button
	cancelButton *widget.Button
	fields       map[string]any // property key -> input widget or binding, custom dialogs only
}
```

In `buildCustomForm`, change the `fields` map type and add the `PropertyList` case:

```go
func (d *Dialog) buildCustomForm() (fyne.CanvasObject, map[string]any) {
	fields := make(map[string]any)
	rows := container.NewVBox()

	for _, p := range d.props {
		switch p.kind {
		case PropertyLabel:
			rows.Add(widget.NewLabel(p.label))

		case PropertyBool:
			field := widget.NewCheck(p.label, nil)
			fields[p.key] = field
			rows.Add(field)

		case PropertyTextField:
			field := widget.NewEntry()
			fields[p.key] = field
			rows.Add(container.NewBorder(nil, nil, widget.NewLabel(p.label), nil, field))

		case PropertyInt:
			field := widget.NewEntry()
			field.Validator = func(s string) error {
				_, err := strconv.Atoi(s)
				return err
			}
			fields[p.key] = field
			rows.Add(container.NewBorder(nil, nil, widget.NewLabel(p.label), nil, field))

		case PropertyList:
			rows.Add(buildListProperty(p, fields))
		}
	}

	return container.NewVScroll(rows), fields
}

// buildListProperty builds an editable list row (bound list + entry/add/remove
// controls) for p and registers p's binding.StringList in fields.
func buildListProperty(p property, fields map[string]any) fyne.CanvasObject {
	data := binding.NewStringList()
	_ = data.Set(p.initial)
	fields[p.key] = data

	list := widget.NewListWithData(data,
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(item binding.DataItem, obj fyne.CanvasObject) {
			obj.(*widget.Label).Bind(item.(binding.String))
		})
	list.Resize(fyne.NewSize(0, 120))

	var selected widget.ListItemID
	hasSelection := false
	list.OnSelected = func(id widget.ListItemID) {
		selected = id
		hasSelection = true
	}
	list.OnUnselected = func(widget.ListItemID) {
		hasSelection = false
	}

	entry := widget.NewEntry()
	addButton := widget.NewButton("Add", func() {
		if entry.Text == "" {
			return
		}
		_ = data.Append(entry.Text)
		entry.SetText("")
	})
	removeButton := widget.NewButton("Remove", func() {
		if !hasSelection {
			return
		}
		items, _ := data.Get()
		if selected < 0 || int(selected) >= len(items) {
			return
		}
		_ = data.Remove(items[selected])
		hasSelection = false
	})

	listArea := container.NewBorder(nil, nil, nil, nil, list)
	listArea.Resize(fyne.NewSize(0, 120))
	controls := container.NewBorder(nil, nil, nil, container.NewHBox(addButton, removeButton), entry)

	return container.NewVBox(widget.NewLabel(p.label), listArea, controls)
}
```

In `collectResult`, add the `PropertyList` case:

```go
func (d *Dialog) collectResult(fields map[string]any) map[string]any {
	if d.kind != KindCustom {
		return nil
	}

	result := make(map[string]any, len(fields))
	for _, p := range d.props {
		field, ok := fields[p.key]
		if !ok {
			continue // PropertyLabel has no field
		}
		switch p.kind {
		case PropertyBool:
			result[p.key] = field.(*widget.Check).Checked
		case PropertyTextField:
			result[p.key] = field.(*widget.Entry).Text
		case PropertyInt:
			n, _ := strconv.Atoi(field.(*widget.Entry).Text)
			result[p.key] = n
		case PropertyList:
			items, _ := field.(binding.StringList).Get()
			result[p.key] = items
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./dialog/... -v`
Expected: PASS — all previous tests plus the two new ones.

- [ ] **Step 5: Commit**

```bash
git add dialog/dialog.go dialog/dialog_test.go
git commit -m "Render and collect PropertyList (editable string list) in custom dialogs"
```

---

### Task 3: Render and collect `PropertyDropdown` and `PropertyMultiSelect`

**Files:**
- Modify: `dialog/dialog.go` (`buildCustomForm`, `collectResult` — both edited again from Task 2's state)
- Test: `dialog/dialog_test.go`

**Interfaces:**
- Consumes: `PropertyDropdown`, `PropertyMultiSelect`, `property{key, label, kind, options, selected}` from Task 1; `fields map[string]any` from Task 2.
- Produces: for `PropertyDropdown`, `fields[key]` holds `*widget.Select`, result is `string`; for `PropertyMultiSelect`, `fields[key]` holds `*widget.CheckGroup`, result is `[]string`.

- [ ] **Step 1: Write the failing tests**

Append to `dialog/dialog_test.go`:

```go
func TestCustomDialogDropdownDefaultAndSelection(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyOptions("choice", "Choice", PropertyDropdown, []string{"x", "y", "z"}, []string{"y"})
	b := d.build(app)

	sel, ok := b.fields["choice"].(*widget.Select)
	if !ok {
		t.Fatalf("fields[choice] = %#v, want *widget.Select", b.fields["choice"])
	}
	if sel.Selected != "y" {
		t.Errorf("initial Selected = %q, want %q", sel.Selected, "y")
	}
	sel.SetSelected("z")

	res := tapAndWait(t, b.okButton, b.resultCh)

	if res["choice"] != "z" {
		t.Errorf("choice = %#v, want %q", res["choice"], "z")
	}
}

func TestCustomDialogMultiSelectDefaultAndSelection(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyOptions("tags", "Tags", PropertyMultiSelect, []string{"a", "b", "c"}, []string{"a"})
	b := d.build(app)

	group, ok := b.fields["tags"].(*widget.CheckGroup)
	if !ok {
		t.Fatalf("fields[tags] = %#v, want *widget.CheckGroup", b.fields["tags"])
	}
	if len(group.Selected) != 1 || group.Selected[0] != "a" {
		t.Errorf("initial Selected = %#v, want [a]", group.Selected)
	}
	group.SetSelected([]string{"a", "c"})

	res := tapAndWait(t, b.okButton, b.resultCh)

	tags, ok := res["tags"].([]string)
	if !ok {
		t.Fatalf("tags = %#v, want []string", res["tags"])
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "c" {
		t.Errorf("tags = %#v, want [a c]", tags)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./dialog/... -run 'TestCustomDialog(Dropdown|MultiSelect)' -v`
Expected: FAIL — `res["choice"]`/`res["tags"]` are `nil` (no case in `collectResult`, no widget added by `buildCustomForm`).

- [ ] **Step 3: Implement**

In `buildCustomForm`'s switch, add two more cases (after the `PropertyList` case added in Task 2):

```go
		case PropertyDropdown:
			field := widget.NewSelect(p.options, nil)
			if len(p.selected) > 0 {
				field.SetSelected(p.selected[0])
			}
			fields[p.key] = field
			rows.Add(container.NewBorder(nil, nil, widget.NewLabel(p.label), nil, field))

		case PropertyMultiSelect:
			field := widget.NewCheckGroup(p.options, nil)
			field.SetSelected(p.selected)
			fields[p.key] = field
			rows.Add(container.NewVBox(widget.NewLabel(p.label), field))
```

In `collectResult`'s switch, add:

```go
		case PropertyDropdown:
			result[p.key] = field.(*widget.Select).Selected
		case PropertyMultiSelect:
			result[p.key] = field.(*widget.CheckGroup).Selected
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./dialog/... -v`
Expected: PASS — full suite, all previous tests plus these two.

- [ ] **Step 5: Commit**

```bash
git add dialog/dialog.go dialog/dialog_test.go
git commit -m "Render and collect PropertyDropdown and PropertyMultiSelect in custom dialogs"
```

---

### Task 4: Extend the visual demo and write `dialog.md`

**Files:**
- Modify: `dialogdemo/test_dialog.go`
- Create: `dialog.md` (repo root, alongside `settings.md` and `db.md`)

**Interfaces:**
- Consumes: the full public API from Tasks 1-3 (`AddPropertyList`, `AddPropertyOptions`, `PropertyList`, `PropertyDropdown`, `PropertyMultiSelect`, plus the pre-existing `NewInfo`, `NewError`, `NewCustom`, `SetTitle`, `SetButtons`, `AddProperty`, `Show`).
- Produces: nothing consumed by later tasks — this is the last task.

- [ ] **Step 1: Extend the demo**

In `dialogdemo/test_dialog.go`, add the new property calls to the existing custom-dialog chain:

```go
		result := dialog.NewCustom().
			SetTitle("Custom").
			SetButtons(dialog.ButtonOK, dialog.ButtonCancel).
			AddProperty("message", "A custom dialog", dialog.PropertyLabel).
			AddProperty("boolean", "boolean", dialog.PropertyBool).
			AddProperty("textField", "textField", dialog.PropertyTextField).
			AddProperty("int", "int", dialog.PropertyInt).
			AddPropertyList("items", "items", []string{"a", "b"}).
			AddPropertyOptions("dropdown", "dropdown", dialog.PropertyDropdown, []string{"x", "y", "z"}, []string{"y"}).
			AddPropertyOptions("tags", "tags", dialog.PropertyMultiSelect, []string{"a", "b", "c"}, []string{"a"}).
			Show(fyneApp)
		log.Printf("custom dialog result: %#v", result)
```

- [ ] **Step 2: Build and vet the demo**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 3: Write `dialog.md`**

Create `dialog.md` at the repo root:

```markdown
# `dialog` package

Import path: `go-ux/dialog`

A modal Fyne dialog window: info, error, and custom (label + input) variants,
built on a plain `app.Window`. Unlike `go-ux/settings`, `Dialog.Show` blocks
the calling goroutine until the user dismisses the dialog, so it must be
called from a goroutine other than the one running `fyneApp.Run()`.

## Public API

```go
func NewInfo(message string) *Dialog
func NewError(message string) *Dialog
func NewCustom() *Dialog

func (d *Dialog) SetTitle(title string) *Dialog
func (d *Dialog) SetButtons(buttons ...ButtonKind) *Dialog
func (d *Dialog) AddProperty(key, label string, kind PropertyKind) *Dialog
func (d *Dialog) AddPropertyList(key, label string, initial []string) *Dialog
func (d *Dialog) AddPropertyOptions(key, label string, kind PropertyKind, options, selected []string) *Dialog
func (d *Dialog) Show(fyneApp fyne.App) map[string]any
```

`NewInfo`/`NewError` build a single-button (OK) dialog showing `message` in
a scrollable text area, titled "Info"/"Error" by default. `NewCustom` builds
a dialog whose content is entirely defined by `AddProperty`/`AddPropertyList`/
`AddPropertyOptions` calls, titled "Custom" by default with a single OK
button; `SetTitle`/`SetButtons` override either. `SetButtons` and the
`AddProperty*` methods are no-ops (return `d` unchanged) on non-custom
dialogs.

`Show` builds and displays the window, then blocks until OK, Cancel, or the
window is closed. For a custom dialog, OK returns a map keyed by each
property's `key`; Cancel and closing the window both return `nil`. Info and
error dialogs always return `nil`.

## Minimal usage

```go
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

	"go-ux/dialog"
)

func main() {
	fyneApp := app.NewWithID("your.app.id")

	go func() {
		dialog.NewInfo("This is an informational message.").Show(fyneApp)

		result := dialog.NewCustom().
			SetTitle("Preferences").
			SetButtons(dialog.ButtonOK, dialog.ButtonCancel).
			AddProperty("enabled", "Enabled", dialog.PropertyBool).
			AddPropertyOptions("mode", "Mode", dialog.PropertyDropdown,
				[]string{"fast", "balanced", "thorough"}, []string{"balanced"}).
			Show(fyneApp)
		log.Printf("result: %#v", result)

		fyneApp.Quit()
	}()

	fyneApp.Run()
}
```

## Custom dialog property kinds

| `PropertyKind`        | Widget                     | Result type | Notes                                                                 |
|------------------------|-----------------------------|--------------|------------------------------------------------------------------------|
| `PropertyLabel`        | `widget.Label`              | (none)       | Message only; not included in `Show`'s result map.                    |
| `PropertyBool`         | `widget.Check`               | `bool`       |                                                                          |
| `PropertyTextField`    | `widget.Entry`               | `string`     |                                                                          |
| `PropertyInt`          | validated `widget.Entry`     | `int`        | Rejects non-integer input via the entry's validator.                  |
| `PropertyList`         | `widget.List` + add/remove   | `[]string`   | Added via `AddPropertyList`; add/remove edits the bound item list.    |
| `PropertyDropdown`     | `widget.Select`              | `string`     | Added via `AddPropertyOptions`; closed set of `options`, no free text.|
| `PropertyMultiSelect`  | `widget.CheckGroup`          | `[]string`   | Added via `AddPropertyOptions`; any subset of `options`.              |

`AddPropertyOptions`'s `selected` argument is the pre-chosen subset for
`PropertyMultiSelect`, or a single-element slice (`selected[0]`) for
`PropertyDropdown` — a `PropertyDropdown` ignores any elements after the
first.

All rows in a custom dialog are laid out in a single scrollable panel
(`container.NewVScroll`), so a form with many properties scrolls rather than
growing the window indefinitely.

## Constraints for callers

- `Show` must be called off the goroutine running `fyneApp.Run()` — it
  blocks on an internal channel until the dialog closes.
- `SetButtons` accepts at most two `ButtonKind` values (`ButtonOK`,
  `ButtonCancel`); extra values beyond the first two are dropped.
- This package assumes desktop Fyne (native title bar, resizable window). It
  has not been tested against Fyne's mobile driver.
```

- [ ] **Step 4: Full verification**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add dialogdemo/test_dialog.go dialog.md
git commit -m "Exercise new dialog property kinds in the demo and document the dialog package"
```
