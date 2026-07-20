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
func (d *Dialog) SetSize(width, height float32) *Dialog
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

The window defaults to 800x600 for info/error dialogs and 800x800 for custom
dialogs (custom dialogs tend to hold more content). `SetSize` overrides
either default; both `width` and `height` must be positive or the call is a
no-op.

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

| `PropertyKind`        | Widget                    | Result type | Notes                                                                  |
|------------------------|----------------------------|--------------|-------------------------------------------------------------------------|
| `PropertyLabel`        | `widget.Label`             | (none)       | Message only; not included in `Show`'s result map.                     |
| `PropertyBool`         | `widget.Check`             | `bool`       |                                                                          |
| `PropertyTextField`    | `widget.Entry`             | `string`     |                                                                          |
| `PropertyInt`          | validated `widget.Entry`   | `int`        | Rejects non-integer input via the entry's validator.                   |
| `PropertyList`         | `widget.List` + add/remove | `[]string`   | Added via `AddPropertyList`; add/remove edits the bound item list.     |
| `PropertyDropdown`     | `widget.Select`            | `string`     | Added via `AddPropertyOptions`; closed set of `options`, no free text. |
| `PropertyMultiSelect`  | `widget.CheckGroup`        | `[]string`   | Added via `AddPropertyOptions`; any subset of `options`.               |

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
