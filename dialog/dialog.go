// Package dialog is a modal Fyne dialog window: info, error, and
// custom (label + input) variants, all built on a plain app.Window
// with a dynamic title, close button, and resizing. Unlike
// go-ux/settings, showing a dialog blocks the calling goroutine until
// the user dismisses it — see Dialog.Show.
package dialog

import (
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// Kind selects a dialog's built-in behavior and layout.
type Kind string

const (
	KindInfo   Kind = "info"
	KindError  Kind = "error"
	KindCustom Kind = "custom"
)

// PropertyKind selects the input widget AddProperty adds for a custom dialog.
type PropertyKind string

const (
	PropertyLabel       PropertyKind = "label"       // message only, no input, not included in the result
	PropertyBool        PropertyKind = "bool"        // widget.Check, result value is bool
	PropertyTextField   PropertyKind = "textField"   // widget.Entry, result value is string
	PropertyInt         PropertyKind = "int"         // validated widget.Entry, result value is int
	PropertyList        PropertyKind = "list"        // editable widget.List, add/remove, result value is []string
	PropertyDropdown    PropertyKind = "dropdown"    // widget.Select, result value is string
	PropertyMultiSelect PropertyKind = "multiSelect" // widget.CheckGroup, result value is []string
)

// ButtonKind is a button that can appear in a custom dialog's button bar.
type ButtonKind string

const (
	ButtonOK     ButtonKind = "OK"
	ButtonCancel ButtonKind = "Cancel"
)

const (
	defaultWidth  = 800
	defaultHeight = 600
)

type property struct {
	key      string
	label    string
	kind     PropertyKind
	initial  []string // PropertyList seed items
	options  []string // PropertyDropdown/PropertyMultiSelect choice set
	selected []string // PropertyDropdown/PropertyMultiSelect pre-selection
}

// Dialog is a modal dialog under construction. Build one with NewInfo,
// NewError, or NewCustom, optionally configure it, then call Show.
type Dialog struct {
	kind    Kind
	title   string
	message string
	buttons []ButtonKind
	props   []property
}

// NewInfo builds an informational dialog showing message in a scrollable
// text area, with a single OK button and the title "Info".
func NewInfo(message string) *Dialog {
	return &Dialog{kind: KindInfo, title: "Info", message: message, buttons: []ButtonKind{ButtonOK}}
}

// NewError builds an informational dialog showing message in a scrollable
// text area, with a single OK button and the title "Error".
func NewError(message string) *Dialog {
	return &Dialog{kind: KindError, title: "Error", message: message, buttons: []ButtonKind{ButtonOK}}
}

// NewCustom builds a dialog whose content is defined by AddProperty calls.
// Its default title is "Custom" and its default button list is a single OK;
// use SetTitle/SetButtons to change either.
func NewCustom() *Dialog {
	return &Dialog{kind: KindCustom, title: "Custom", buttons: []ButtonKind{ButtonOK}}
}

// SetTitle overrides the dialog's window title. Chainable.
func (d *Dialog) SetTitle(title string) *Dialog {
	d.title = title
	return d
}

// SetButtons sets the button bar for a custom dialog, up to two buttons.
// It has no effect on info/error dialogs, which always show a single OK.
// Chainable.
func (d *Dialog) SetButtons(buttons ...ButtonKind) *Dialog {
	if d.kind != KindCustom {
		return d
	}
	if len(buttons) > 2 {
		buttons = buttons[:2]
	}
	d.buttons = buttons
	return d
}

// AddProperty adds one label + input row to a custom dialog. key identifies
// the value in Show's result map; label is display text only. Only effective
// on custom dialogs. Chainable.
func (d *Dialog) AddProperty(key, label string, kind PropertyKind) *Dialog {
	if d.kind != KindCustom {
		return d
	}
	d.props = append(d.props, property{key: key, label: label, kind: kind})
	return d
}

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

// Show builds and displays the dialog window, then blocks the calling
// goroutine until the user clicks OK, clicks Cancel, or closes the window.
//
// Must be called from a goroutine other than the one that called
// fyneApp.Run() — Fyne's event loop keeps running on that goroutine while
// Show blocks here waiting for it.
//
// For a custom dialog, OK returns a map keyed by each AddProperty's key with
// a value typed per its PropertyKind (bool/string/int); Cancel and closing
// the window both return nil. info/error dialogs always return nil.
func (d *Dialog) Show(fyneApp fyne.App) map[string]any {
	b := d.build(fyneApp)
	b.win.Show()
	return <-b.resultCh
}

// builtDialog is the unexported handle to a constructed-but-not-yet-shown
// dialog window, split out from Show so tests can drive it without a real
// user (see dialog_test.go).
type builtDialog struct {
	win          fyne.Window
	resultCh     chan map[string]any
	okButton     *widget.Button
	cancelButton *widget.Button
	fields       map[string]fyne.CanvasObject // property key -> input widget, custom dialogs only
}

func (d *Dialog) build(fyneApp fyne.App) *builtDialog {
	win := fyneApp.NewWindow(d.title)
	win.Resize(fyne.NewSize(defaultWidth, defaultHeight))

	b := &builtDialog{
		win:      win,
		resultCh: make(chan map[string]any),
	}

	var content fyne.CanvasObject
	if d.kind == KindCustom {
		content, b.fields = d.buildCustomForm()
	} else {
		content = buildMessageArea(d.message)
	}

	buttons := d.buttons
	if d.kind != KindCustom {
		buttons = []ButtonKind{ButtonOK}
	}
	if len(buttons) == 0 {
		buttons = []ButtonKind{ButtonOK}
	}

	var once sync.Once
	send := func(result map[string]any) {
		once.Do(func() { b.resultCh <- result })
	}

	buttonBar := container.NewHBox(layout.NewSpacer())
	for _, kind := range buttons {
		switch kind {
		case ButtonOK:
			b.okButton = widget.NewButton("OK", func() {
				send(d.collectResult(b.fields))
				win.Close()
			})
			buttonBar.Add(b.okButton)
		case ButtonCancel:
			b.cancelButton = widget.NewButton("Cancel", func() {
				send(nil)
				win.Close()
			})
			buttonBar.Add(b.cancelButton)
		}
	}

	win.SetContent(container.NewBorder(nil, buttonBar, nil, nil, content))
	win.SetCloseIntercept(func() {
		send(nil)
		win.Close()
	})

	return b
}

func buildMessageArea(message string) fyne.CanvasObject {
	text := widget.NewLabel(message)
	text.Wrapping = fyne.TextWrapWord
	return container.NewVScroll(text)
}

func (d *Dialog) buildCustomForm() (fyne.CanvasObject, map[string]fyne.CanvasObject) {
	fields := make(map[string]fyne.CanvasObject)
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
		}
	}

	return container.NewVScroll(rows), fields
}

func (d *Dialog) collectResult(fields map[string]fyne.CanvasObject) map[string]any {
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
		}
	}
	return result
}
