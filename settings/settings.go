// Package settings is a Fyne settings control panel: a tree of settings on
// the left and a properties form on the right, modeled on IntelliJ
// Community Edition's Settings dialog. Settings data comes from a
// registry (go-ux/db); no SQLite is touched directly by this package.
package settings

import (
	"encoding/json"
	"log"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"go-ux/db"
)

// componentID identifies the settings window itself for UI-state persistence
// (window size). Hardcoded per component, as any go-ux window/dialog does.
const componentID = "b6f6c9d1-3f2b-4b8a-9e3a-2f1c7a5d9e10"

const rootUID = ""

// Window is a settings control panel window backed by a db.DB registry.
type Window struct {
	win fyne.Window
	db  *db.DB

	byParent map[string][]string
	byID     map[string]db.Node

	tree       *widget.Tree
	formHolder *fyne.Container

	// staged holds in-memory edits, keyed by node ID then property key,
	// not yet written to the db. Apply/OK flush it; Cancel discards it.
	staged map[int64]map[string]string
}

// NewWindow builds a settings window backed by database. It reads the
// current settings tree immediately; call Show to display it.
func NewWindow(app fyne.App, database *db.DB) (*Window, error) {
	w := &Window{
		db:     database,
		staged: make(map[int64]map[string]string),
	}

	nodes, err := database.ListSettings()
	if err != nil {
		return nil, err
	}
	w.indexNodes(nodes)

	win := app.NewWindow("Settings")
	win.Resize(fyne.NewSize(1024, 1200))
	w.win = win

	w.tree = w.buildTree()
	w.formHolder = container.NewStack()

	buttons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("Cancel", w.handleCancel),
		widget.NewButton("Apply", w.handleApply),
		widget.NewButton("OK", w.handleOK),
	)

	split := container.NewHSplit(w.tree, w.formHolder)
	split.Offset = 0.3

	win.SetContent(container.NewBorder(nil, buttons, nil, nil, split))

	w.restoreUIState()
	win.SetCloseIntercept(func() {
		w.saveUIState()
		win.Close()
	})

	return w, nil
}

// Show displays the settings window.
func (w *Window) Show() {
	w.win.Show()
}

func (w *Window) indexNodes(nodes []db.Node) {
	w.byParent = make(map[string][]string)
	w.byID = make(map[string]db.Node)
	for _, n := range nodes {
		uid := strconv.FormatInt(n.ID, 10)
		w.byID[uid] = n
		parentUID := rootUID
		if n.ParentID != nil {
			parentUID = strconv.FormatInt(*n.ParentID, 10)
		}
		w.byParent[parentUID] = append(w.byParent[parentUID], uid)
	}
}

func (w *Window) buildTree() *widget.Tree {
	t := widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID {
			children := w.byParent[uid]
			ids := make([]widget.TreeNodeID, len(children))
			copy(ids, children)
			return ids
		},
		func(uid widget.TreeNodeID) bool {
			return len(w.byParent[uid]) > 0
		},
		func(branch bool) fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(w.byID[uid].Description)
		},
	)
	t.OnSelected = w.selectNode
	return t
}

func (w *Window) selectNode(uid widget.TreeNodeID) {
	nodeID, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		return
	}

	props, err := w.db.GetProperties(nodeID)
	if err != nil {
		log.Printf("settings: get properties for node %d: %v", nodeID, err)
		return
	}

	form := widget.NewForm()
	for _, p := range props {
		form.Append(p.Label, w.propertyWidget(nodeID, p))
	}

	w.formHolder.Objects = []fyne.CanvasObject{form}
	w.formHolder.Refresh()
}

func (w *Window) propertyWidget(nodeID int64, p db.Property) fyne.CanvasObject {
	value := p.Value
	if staged, ok := w.staged[nodeID]; ok {
		if v, ok := staged[p.Key]; ok {
			value = v
		}
	}

	switch p.Type {
	case db.PropertyBool:
		chk := widget.NewCheck("", func(checked bool) {
			w.stage(nodeID, p.Key, strconv.FormatBool(checked))
		})
		chk.SetChecked(value == "true")
		return chk

	case db.PropertyInt:
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.Validator = func(s string) error {
			_, err := strconv.Atoi(s)
			return err
		}
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry

	case db.PropertyEnum:
		sel := widget.NewSelect(p.EnumOptions, func(s string) {
			w.stage(nodeID, p.Key, s)
		})
		sel.SetSelected(value)
		return sel

	default: // db.PropertyString and anything unrecognized
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry
	}
}

func (w *Window) stage(nodeID int64, key, value string) {
	if w.staged[nodeID] == nil {
		w.staged[nodeID] = make(map[string]string)
	}
	w.staged[nodeID][key] = value
}

func (w *Window) handleApply() {
	for nodeID, values := range w.staged {
		if err := w.db.SaveProperties(nodeID, values); err != nil {
			log.Printf("settings: save properties for node %d: %v", nodeID, err)
		}
	}
	w.staged = make(map[int64]map[string]string)
}

func (w *Window) handleOK() {
	w.handleApply()
	w.win.Close()
}

func (w *Window) handleCancel() {
	w.staged = make(map[int64]map[string]string)
	w.win.Close()
}

// uiState is the blob persisted for the settings window's own UI state.
// Fyne's desktop driver does not expose a cross-platform window position API,
// so only size is tracked.
type uiState struct {
	Width  float32
	Height float32
}

func (w *Window) restoreUIState() {
	blob, err := w.db.LoadUIState(componentID)
	if err != nil {
		log.Printf("settings: load ui state: %v", err)
		return
	}
	if blob == nil {
		return
	}

	var s uiState
	if err := json.Unmarshal(blob, &s); err != nil {
		log.Printf("settings: unmarshal ui state: %v", err)
		return
	}
	if s.Width > 0 && s.Height > 0 {
		w.win.Resize(fyne.NewSize(s.Width, s.Height))
	}
}

func (w *Window) saveUIState() {
	size := w.win.Canvas().Size()
	blob, err := json.Marshal(uiState{Width: size.Width, Height: size.Height})
	if err != nil {
		log.Printf("settings: marshal ui state: %v", err)
		return
	}
	if err := w.db.SaveUIState(componentID, blob); err != nil {
		log.Printf("settings: save ui state: %v", err)
	}
}
