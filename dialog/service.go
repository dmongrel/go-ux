// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

// Package dialog is a Wails v3 replacement for go-ux's Fyne modal
// dialogs: native Info/Error dialogs via Wails' own app.Dialog API, plus a
// windowed Custom dialog (the arbitrary label+input property form the old
// Fyne Dialog builder supported) for the one case Wails has no native
// equivalent for.
package dialog

import (
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Kind selects a dialog's behavior. Info/Error use Wails' native dialogs;
// Custom opens its own window rendering Properties as a form.
type Kind string

const (
	KindInfo   Kind = "info"
	KindError  Kind = "error"
	KindCustom Kind = "custom"
)

// PropertyKind selects the input a Custom dialog's frontend renders for one
// Property row, matching go-ux/dialog's original Fyne widget choices.
type PropertyKind string

const (
	PropertyLabel       PropertyKind = "label"       // message only, no input, not included in the result
	PropertyBool        PropertyKind = "bool"        // checkbox, result value is bool
	PropertyTextField   PropertyKind = "textField"   // text input, result value is string
	PropertyInt         PropertyKind = "int"         // number input, result value is int
	PropertyList        PropertyKind = "list"        // editable string list, result value is []string
	PropertyDropdown    PropertyKind = "dropdown"    // select, result value is string
	PropertyMultiSelect PropertyKind = "multiSelect" // checkbox group, result value is []string
)

// ButtonKind is a button shown in a Custom dialog's button bar.
type ButtonKind string

const (
	ButtonOK     ButtonKind = "OK"
	ButtonCancel ButtonKind = "Cancel"
)

const (
	defaultWidth  = 800
	defaultHeight = 800
)

// Property is one label+input row in a Custom dialog's form.
type Property struct {
	Key      string
	Label    string
	Kind     PropertyKind
	Initial  []string // PropertyList seed items
	Options  []string // PropertyDropdown/PropertyMultiSelect choice set
	Selected []string // PropertyDropdown/PropertyMultiSelect pre-selection
}

// CustomDialogSpec describes a Custom dialog window's content — the Wails
// equivalent of go-ux/dialog's Dialog builder (NewCustom + SetTitle +
// SetButtons + AddProperty/AddPropertyList/AddPropertyOptions), passed as
// one value to ShowCustom instead of chained builder calls, since a Wails
// Service method call crosses a Go<->JS boundary rather than running
// entirely on one goroutine.
type CustomDialogSpec struct {
	Title      string
	Buttons    []ButtonKind
	Properties []Property
	Width      int
	Height     int
}

// Service is the Wails-bound replacement for go-ux/dialog.Dialog. Register
// it with app.RegisterService(application.NewService(dialog.NewService(app))).
type Service struct {
	app *application.App

	mu           sync.Mutex
	nextID       int
	pending      map[string]chan map[string]any
	specs        map[string]CustomDialogSpec
	imagePending map[string]chan string
	imageSpecs   map[string]ImageGridSpec
}

func NewService(app *application.App) *Service {
	return &Service{
		app:          app,
		pending:      make(map[string]chan map[string]any),
		specs:        make(map[string]CustomDialogSpec),
		imagePending: make(map[string]chan string),
		imageSpecs:   make(map[string]ImageGridSpec),
	}
}

// ShowInfo displays a native informational dialog.
func (s *Service) ShowInfo(title string, message string) {
	s.app.Dialog.Info().SetTitle(title).SetMessage(message).Show()
}

// ShowError displays a native error dialog.
func (s *Service) ShowError(title string, message string) {
	s.app.Dialog.Error().SetTitle(title).SetMessage(message).Show()
}

// PickFile shows a native "Open File" picker and returns the selected path,
// or "" if the user cancels.
func (s *Service) PickFile() (string, error) {
	return s.app.Dialog.OpenFile().SetTitle("Open File").PromptForSingleSelection()
}

// PickFiles shows a native "Open Files" picker allowing more than one
// selection, and returns the chosen paths — nil if the user cancels.
func (s *Service) PickFiles() ([]string, error) {
	return s.app.Dialog.OpenFile().SetTitle("Open Files").PromptForMultipleSelection()
}

// ShowCustom opens a window rendering spec's property form and blocks the
// calling goroutine until the user clicks a button or closes the window —
// matching go-ux/dialog.Dialog.Show's original blocking contract. Must be
// called from a goroutine other than the one running app.Run(), same
// restriction the original Fyne Dialog.Show documented.
//
// For OK, the result is keyed by each Property's Key with a value typed
// per its PropertyKind (bool/string/int, or []string for list and
// multi-select; dropdown is string). Cancel and closing the window both
// return nil.
func (s *Service) ShowCustom(spec CustomDialogSpec) map[string]any {
	spec = normalizeSpec(spec)

	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("%d", s.nextID)
	result := make(chan map[string]any, 1)
	s.pending[id] = result
	s.specs[id] = spec
	s.mu.Unlock()

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            spec.Title,
		Width:            spec.Width,
		Height:           spec.Height,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#dialog?id=" + id,
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.resolve(id, nil)
	})

	r := <-result

	s.mu.Lock()
	delete(s.specs, id)
	s.mu.Unlock()

	return r
}

// GetSpec is called by a Custom dialog window's own frontend on mount, to
// fetch the CustomDialogSpec ShowCustom is waiting on.
func (s *Service) GetSpec(id string) CustomDialogSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.specs[id]
}

// Submit is called by a Custom dialog window's OK button, delivering the
// collected form result and letting the matching ShowCustom call return.
func (s *Service) Submit(id string, result map[string]any) {
	s.resolve(id, result)
}

// CancelDialog is called by a Custom dialog window's Cancel button.
func (s *Service) CancelDialog(id string) {
	s.resolve(id, nil)
}

// normalizeSpec fills in defaults ShowCustom needs (a default OK button, a
// default window size) — split out from ShowCustom so it's testable
// without a running Wails app.
func normalizeSpec(spec CustomDialogSpec) CustomDialogSpec {
	if len(spec.Buttons) == 0 {
		spec.Buttons = []ButtonKind{ButtonOK}
	}
	if spec.Width <= 0 {
		spec.Width = defaultWidth
	}
	if spec.Height <= 0 {
		spec.Height = defaultHeight
	}
	return spec
}

// ImageOption is one selectable thumbnail in a ShowImageGrid grid.
type ImageOption struct {
	// Key identifies the option; not shown to the user. Returned by
	// ShowImageGrid when this option is picked.
	Key string
	// ImageData is the option's raw image bytes (JPEG/PNG/etc.), served to
	// the frontend as a data: URI — Wails' JSON binding encodes a Go []byte
	// field as a base64 string automatically, so no separate asset-serving
	// endpoint is needed.
	ImageData []byte
}

// ImageGridSpec describes a ShowImageGrid window's content: an image-only
// thumbnail grid (no labels), matching the original Fyne parchment-texture
// picker's behavior this replaces.
type ImageGridSpec struct {
	Title    string
	Options  []ImageOption
	Selected string // currently-selected Key, highlighted; "" if none
	Width    int
	Height   int
}

const (
	defaultImageGridWidth  = 480
	defaultImageGridHeight = 400
)

// normalizeImageGridSpec fills in ShowImageGrid's default window size —
// split out so it's testable without a running Wails app, same reason
// normalizeSpec is split from ShowCustom.
func normalizeImageGridSpec(spec ImageGridSpec) ImageGridSpec {
	if spec.Width <= 0 {
		spec.Width = defaultImageGridWidth
	}
	if spec.Height <= 0 {
		spec.Height = defaultImageGridHeight
	}
	return spec
}

// ShowImageGrid opens a window rendering spec's image thumbnails in a grid
// and blocks the calling goroutine until the user clicks one or closes the
// window — same blocking contract as ShowCustom, and the same restriction:
// must be called from a goroutine other than the one running app.Run().
// Returns the clicked option's Key, or "" if the window was closed without
// a selection.
func (s *Service) ShowImageGrid(spec ImageGridSpec) string {
	spec = normalizeImageGridSpec(spec)

	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("%d", s.nextID)
	result := make(chan string, 1)
	s.imagePending[id] = result
	s.imageSpecs[id] = spec
	s.mu.Unlock()

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            spec.Title,
		Width:            spec.Width,
		Height:           spec.Height,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#imagegrid?id=" + id,
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.resolveImageGrid(id, "")
	})

	selected := <-result

	s.mu.Lock()
	delete(s.imageSpecs, id)
	s.mu.Unlock()

	return selected
}

// GetImageGridSpec is called by a ShowImageGrid window's own frontend on
// mount, to fetch the ImageGridSpec ShowImageGrid is waiting on.
func (s *Service) GetImageGridSpec(id string) ImageGridSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imageSpecs[id]
}

// SelectImage is called by a ShowImageGrid window's frontend when the user
// clicks a thumbnail, delivering its Key and letting the matching
// ShowImageGrid call return.
func (s *Service) SelectImage(id string, key string) {
	s.resolveImageGrid(id, key)
}

// CancelImageGrid is called by a ShowImageGrid window's frontend if it adds
// an explicit Cancel affordance beyond just closing the window.
func (s *Service) CancelImageGrid(id string) {
	s.resolveImageGrid(id, "")
}

// resolveImageGrid delivers key to id's pending ShowImageGrid call, if it
// hasn't already been resolved — same idempotency guard resolve documents
// (a click and the window-closing hook can race).
func (s *Service) resolveImageGrid(id string, key string) {
	s.mu.Lock()
	ch, ok := s.imagePending[id]
	if ok {
		delete(s.imagePending, id)
	}
	s.mu.Unlock()
	if ok {
		ch <- key
	}
}

// resolve delivers result to id's pending ShowCustom call, if it hasn't
// already been resolved (Submit/CancelDialog and the window-closing hook
// can race — whichever fires first wins, the rest are no-ops).
func (s *Service) resolve(id string, result map[string]any) {
	s.mu.Lock()
	ch, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	s.mu.Unlock()
	if ok {
		ch <- result
	}
}

