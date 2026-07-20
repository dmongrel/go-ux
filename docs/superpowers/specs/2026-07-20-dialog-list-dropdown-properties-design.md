# Custom dialog: editable list & dropdown/multi-select properties

## Context

`dialog.Dialog`'s custom kind (`dialog/dialog.go`) builds a form via
`AddProperty(key, label, kind)` for four kinds: `PropertyLabel`,
`PropertyBool`, `PropertyTextField`, `PropertyInt`. The form rows are
already wrapped in `container.NewVScroll` (dialog.go:235), so scrolling
for many properties already works and needs no change.

This adds two more property kinds — an editable list of strings, and a
dropdown/checkbox-multi-select backed by a fixed option set — so callers
can collect list- and choice-shaped input the same way they collect
bool/text/int input today.

## API surface

Two new `PropertyKind` constants:

```go
PropertyList        PropertyKind = "list"        // editable widget.List, add/remove, result []string
PropertyDropdown    PropertyKind = "dropdown"     // widget.Select, result string
PropertyMultiSelect PropertyKind = "multiSelect"  // widget.CheckGroup, result []string
```

Two new chainable builder methods, following the existing `AddProperty`
pattern (custom-dialog-only; no-op and return `d` unchanged if
`d.kind != KindCustom`):

```go
// AddPropertyList adds an editable list row: an add/remove-capable list of
// strings seeded with initial. Result value is []string.
func (d *Dialog) AddPropertyList(key, label string, initial []string) *Dialog

// AddPropertyOptions adds a dropdown (PropertyDropdown, result string) or
// checkbox multi-select (PropertyMultiSelect, result []string) row. options
// is the fixed choice set; selected are the options pre-chosen. For
// PropertyDropdown, only selected[0] (if present) is used as the initial
// selection.
func (d *Dialog) AddPropertyOptions(key, label string, kind PropertyKind, options, selected []string) *Dialog
```

No validation is performed on `kind` in `AddPropertyOptions` beyond the
`KindCustom` guard, consistent with the existing loose `AddProperty`
contract — callers are trusted to pass a kind the form builder knows how
to render.

## Internal data model

`property` gains three fields:

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

`builtDialog.fields` changes type from `map[string]fyne.CanvasObject` to
`map[string]any`. This is required because the list property stores a
`binding.StringList` (not a `fyne.CanvasObject`) as its field value, so
`collectResult` can read the live item set without walking widget
internals. Existing type assertions (`field.(*widget.Check)`,
`field.(*widget.Entry)`) are unaffected by the map value type change.

## Widget construction (`buildCustomForm`)

- **PropertyList**: `binding.NewStringList()` seeded from `p.initial` via
  `Set`. Drives a `widget.NewListWithData` (bound `widget.Label` per row).
  Below the list: an `Entry` plus an "Add" button (appends the entry's
  text via `data.Append`, then clears the entry — no-op on empty text)
  and a "Remove" button (removes the currently selected row, tracked via
  the list's `OnSelected`/`OnUnselected` callbacks — no-op if nothing is
  selected). The list is wrapped in a container with a fixed min height
  (enough for a handful of visible rows, e.g. via
  `container.NewGridWrap`/an explicit `Resize` on the list) so it renders
  a bounded, usable size when nested inside the outer `VScroll` instead
  of collapsing to zero height or fighting the outer scroll area for
  space. `fields[p.key]` stores the `binding.StringList`.
- **PropertyDropdown**: `widget.NewSelect(p.options, nil)`; if
  `len(p.selected) > 0`, `SetSelected(p.selected[0])`. `fields[p.key]`
  stores the `*widget.Select`.
- **PropertyMultiSelect**: `widget.NewCheckGroup(p.options, nil)`;
  `SetSelected(p.selected)`. `fields[p.key]` stores the
  `*widget.CheckGroup`.

Each of these rows follows the existing layout convention: label to the
left (`container.NewBorder(nil, nil, widget.NewLabel(p.label), nil, field)`),
except PropertyList, whose control cluster (list + entry/add/remove) is
tall enough that the label instead sits above it.

## Result collection (`collectResult`)

Three more cases:

```go
case PropertyList:
	items, _ := field.(binding.StringList).Get()
	result[p.key] = items
case PropertyDropdown:
	result[p.key] = field.(*widget.Select).Selected
case PropertyMultiSelect:
	result[p.key] = field.(*widget.CheckGroup).Selected
```

## Tests & demo

- `dialog_test.go`: add cases covering list add/remove producing the
  expected `[]string` result, dropdown selection producing the expected
  `string` result, multi-select producing the expected `[]string`
  result, and the `KindCustom`-only guard (no-op on `NewInfo`/`NewError`)
  for both `AddPropertyList` and `AddPropertyOptions`.
- `dialogdemo/test_dialog.go`: extend the custom dialog call to include
  one `AddPropertyList` and one `AddPropertyOptions` of each new kind, so
  the manual/visual entry point continues to exercise every property
  kind.

## Out of scope

- Free-text/editable dropdown (closed set only, per decision).
- Visual grouping/section headers within the scrollable form (existing
  flat `VBox` of rows is retained; only the scrolling behavior itself was
  in question, and it already works).
- Reordering list items (add/remove only).
