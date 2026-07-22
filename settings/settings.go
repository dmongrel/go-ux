// Package settings is a Fyne settings control panel: a tree of settings on
// the left and a properties form on the right, modeled on IntelliJ
// Community Edition's Settings dialog. Settings data comes from a
// registry (go-ux/db); no SQLite is touched directly by this package.
package settings

import (
	"encoding/json"
	"image/color"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go-ux/db"
	"go-ux/treestate"
)

// componentID identifies the settings window itself for UI-state persistence
// (window size). Hardcoded per component, as any go-ux window/dialog does.
const componentID = "b6f6c9d1-3f2b-4b8a-9e3a-2f1c7a5d9e10"

const rootUID = ""

// highlightColor is the filled rectangle used to mark a search match, in
// both the tree and the properties page. Matched text is drawn in black so
// it stays readable against it.
var highlightColor = color.NRGBA{R: 255, G: 235, B: 59, A: 255}

// panelBorderColor outlines the two main layout panels (sidebar, properties page).
var panelBorderColor = color.NRGBA{R: 64, G: 64, B: 64, A: 255}

func textColor(highlighted bool) color.Color {
	if highlighted {
		return color.Black
	}
	return theme.Color(theme.ColorNameForeground)
}

// Window is a settings control panel window backed by a db.DB registry.
type Window struct {
	win fyne.Window
	db  *db.DB

	byParent map[string][]string
	byID     map[string]db.Node
	allProps map[string][]db.Property // uid -> properties, prefetched for search

	tree       *widget.Tree
	formHolder *fyne.Container
	split      *container.Split

	selectedUID string

	// search state, recomputed on every keystroke
	searchText string
	visible    map[string]bool            // nil means "no filter, show everything"
	descMatch  map[string]bool            // uid -> node description matched the search
	propMatch  map[string]map[string]bool // uid -> property key -> label matched the search

	// staged holds in-memory edits, keyed by node ID then property key,
	// not yet written to the db. Apply/OK flush it; Cancel discards it.
	staged map[int64]map[string]string

	// unsubscribers cancels every db.OnPropertiesChanged subscription this
	// Window registered in NewWindow, called when the window closes.
	unsubscribers []func()

	// closed is set once the close intercept has run. A db write can still
	// arrive after that (SaveProperties on this Window's db from any
	// goroutine, e.g. a live Ctrl+scroll elsewhere) before the
	// already-queued unsubscribe calls take effect, or if this Window was
	// torn down by a path that bypasses SetCloseIntercept entirely (e.g.
	// fyne.App.Quit()) — acceptExternalChange checks this to avoid touching
	// a torn-down window's widgets. Both the write (close intercept) and
	// the read (acceptExternalChange, via its fyne.Do wrapper) happen on
	// the UI goroutine, so no separate lock is needed, consistent with this
	// type's other UI-goroutine-only fields.
	closed bool
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

	win := app.NewWindow("Settings")
	win.Resize(fyne.NewSize(1024, 800))
	w.win = win

	w.tree = w.buildTree()
	tracker := treestate.Track(database, componentID+".tree", w.tree, treestate.Options{
		Exists: func(uid string) bool {
			_, ok := w.byID[uid]
			return ok
		},
		OnSelected: w.selectNode,
	})
	w.formHolder = container.NewStack()

	search := widget.NewEntry()
	search.SetPlaceHolder("Search settings...")
	search.OnChanged = w.applySearch

	clear := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		search.SetText("")
		w.applySearch("")
	})
	clear.Importance = widget.LowImportance

	searchRow := container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), clear, search)
	sidebar := bordered(container.NewBorder(searchRow, nil, nil, nil, w.tree))
	formPanel := bordered(container.NewVScroll(w.formHolder))

	buttons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("OK", w.handleOK),
		widget.NewButton("Cancel", w.handleCancel),
		widget.NewButton("Apply", w.handleApply),
	)

	w.split = container.NewHSplit(sidebar, formPanel)
	w.split.Offset = 0.3

	win.SetContent(container.NewBorder(nil, buttons, nil, nil, w.split))

	w.restoreUIState()
	tracker.Restore()
	win.SetCloseIntercept(func() {
		w.saveUIState()
		w.closed = true
		for _, unsubscribe := range w.unsubscribers {
			unsubscribe()
		}
		win.Close()
	})

	return w, nil
}

// SetSize overrides the settings window's size (default 1024x800, or a
// previously saved size restored from UI state — see "Own UI state" in
// settings.md). Both width and height must be positive or the call has no
// effect. Call before Show. Chainable.
func (w *Window) SetSize(width, height float32) *Window {
	if width > 0 && height > 0 {
		w.win.Resize(fyne.NewSize(width, height))
	}
	return w
}

// Show displays the settings window.
func (w *Window) Show() {
	w.win.Show()
}

// bordered wraps content in a 1px, 50%-grey outline. Used to frame the two
// main layout panels (sidebar, properties page).
func bordered(content fyne.CanvasObject) fyne.CanvasObject {
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = panelBorderColor
	border.StrokeWidth = 1
	return container.NewStack(border, container.NewPadded(content))
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

// prefetchProperties loads every node's properties up front so search can
// match against property labels without a round trip per keystroke.
func (w *Window) prefetchProperties() error {
	w.allProps = make(map[string][]db.Property, len(w.byID))
	for uid := range w.byID {
		nodeID, err := strconv.ParseInt(uid, 10, 64)
		if err != nil {
			continue
		}
		props, err := w.db.GetProperties(nodeID)
		if err != nil {
			return err
		}
		w.allProps[uid] = props
	}
	return nil
}

func (w *Window) buildTree() *widget.Tree {
	t := widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID {
			children := w.byParent[uid]
			var ids []widget.TreeNodeID
			for _, c := range children {
				if w.visible == nil || w.visible[c] {
					ids = append(ids, c)
				}
			}
			return ids
		},
		func(uid widget.TreeNodeID) bool {
			return len(w.byParent[uid]) > 0
		},
		func(branch bool) fyne.CanvasObject {
			rect := canvas.NewRectangle(color.Transparent)
			text := canvas.NewText("", textColor(false))
			return container.NewStack(rect, text)
		},
		func(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
			cell := obj.(*fyne.Container)
			rect := cell.Objects[0].(*canvas.Rectangle)
			text := cell.Objects[1].(*canvas.Text)
			text.Text = w.byID[uid].Description
			matched := w.descMatch[uid]
			if matched {
				rect.FillColor = highlightColor
			} else {
				rect.FillColor = color.Transparent
			}
			text.Color = textColor(matched)
			text.Refresh()
			rect.Refresh()
		},
	)
	return t
}

func (w *Window) selectNode(uid widget.TreeNodeID) {
	if _, err := strconv.ParseInt(uid, 10, 64); err != nil {
		return
	}
	w.selectedUID = uid
	w.renderProperties(uid)
}

// renderProperties builds the properties page for uid, highlighting any
// property whose label matched the current search.
func (w *Window) renderProperties(uid string) {
	nodeID, err := strconv.ParseInt(uid, 10, 64)
	if err != nil {
		return
	}

	rows := container.NewVBox()
	for _, p := range w.allProps[uid] {
		highlighted := w.propMatch[uid] != nil && w.propMatch[uid][p.Key]
		rows.Add(w.buildPropertyRow(nodeID, p, highlighted))
	}

	w.formHolder.Objects = []fyne.CanvasObject{rows}
	w.formHolder.Refresh()
}

func (w *Window) buildPropertyRow(nodeID int64, p db.Property, highlighted bool) fyne.CanvasObject {
	label := canvas.NewText(p.Label, textColor(highlighted))
	label.TextStyle = fyne.TextStyle{Bold: true}

	var labelObj fyne.CanvasObject = label
	if highlighted {
		rect := canvas.NewRectangle(highlightColor)
		labelObj = container.NewStack(rect, label)
	}

	return container.NewBorder(nil, nil, labelObj, nil, w.propertyWidget(nodeID, p))
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

	case db.PropertyFloat:
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.Validator = func(s string) error {
			_, err := strconv.ParseFloat(s, 64)
			return err
		}
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry

	default: // db.PropertyString and anything unrecognized
		entry := widget.NewEntry()
		entry.SetText(value)
		entry.OnChanged = func(s string) { w.stage(nodeID, p.Key, s) }
		return entry
	}
}

// PropertyWidgetForTest exposes propertyWidget for settings_test's
// external test package — this package has no other way to inspect a
// generated form widget's type/validator from outside.
func (w *Window) PropertyWidgetForTest(nodeID int64, p db.Property) fyne.CanvasObject {
	return w.propertyWidget(nodeID, p)
}

// HandleOKForTest exposes handleOK for settings_test's external test
// package.
func (w *Window) HandleOKForTest() {
	w.handleOK()
}

// acceptExternalChange reacts to a db write this Window didn't itself
// make (see db.OnPropertiesChanged) — force-accepting it means updating
// the cached Property.Value so it's correct even before next rendered, and
// discarding any staged-but-uncommitted edit for that same key, so a
// later OK/Apply can't overwrite the newer external value with a stale one.
// Runs on the UI goroutine (the caller wraps it in fyne.Do) since it
// touches formHolder when uid is the currently displayed page.
func (w *Window) acceptExternalChange(uid string, nodeID int64, values map[string]string) {
	if w.closed {
		return
	}
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

// applySearch recomputes which tree nodes are visible/highlighted for a
// type-ahead query. A node is shown if its own description matches, or if
// any property on its properties page matches (so the user can find it by
// setting name too); ancestors of a shown node are shown as well so it stays
// reachable in the tree.
func (w *Window) applySearch(query string) {
	w.searchText = query
	q := strings.ToLower(strings.TrimSpace(query))

	if q == "" {
		w.visible = nil
		w.descMatch = nil
		w.propMatch = nil
		w.tree.Refresh()
		if w.selectedUID != "" {
			w.renderProperties(w.selectedUID)
		}
		return
	}

	visible := make(map[string]bool)
	descMatch := make(map[string]bool)
	propMatch := make(map[string]map[string]bool)

	for uid, node := range w.byID {
		if strings.Contains(strings.ToLower(node.Description), q) {
			descMatch[uid] = true
			markVisible(w.byID, uid, visible)
		}
	}

	for uid, props := range w.allProps {
		for _, p := range props {
			if !strings.Contains(strings.ToLower(p.Label), q) {
				continue
			}
			if propMatch[uid] == nil {
				propMatch[uid] = make(map[string]bool)
			}
			propMatch[uid][p.Key] = true
			markVisible(w.byID, uid, visible)
		}
	}

	w.visible = visible
	w.descMatch = descMatch
	w.propMatch = propMatch

	w.tree.Refresh()
	for uid := range w.byID {
		if len(w.byParent[uid]) > 0 {
			w.tree.OpenBranch(uid)
		}
	}
	if w.selectedUID != "" {
		w.renderProperties(w.selectedUID)
	}
}

// markVisible marks uid and all of its ancestors as visible.
func markVisible(byID map[string]db.Node, uid string, visible map[string]bool) {
	for {
		visible[uid] = true
		node := byID[uid]
		if node.ParentID == nil {
			return
		}
		uid = strconv.FormatInt(*node.ParentID, 10)
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
// so only size and the sidebar splitter position are tracked.
type uiState struct {
	Width         float32
	Height        float32
	SidebarOffset float64
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
	if s.SidebarOffset > 0 && s.SidebarOffset < 1 {
		w.split.Offset = s.SidebarOffset
		w.split.Refresh()
	}
}

func (w *Window) saveUIState() {
	size := w.win.Canvas().Size()
	blob, err := json.Marshal(uiState{
		Width:         size.Width,
		Height:        size.Height,
		SidebarOffset: w.split.Offset,
	})
	if err != nil {
		log.Printf("settings: marshal ui state: %v", err)
		return
	}
	if err := w.db.SaveUIState(componentID, blob); err != nil {
		log.Printf("settings: save ui state: %v", err)
	}
}

// TreeForTest exposes the underlying *widget.Tree for settings_test's
// external test package (e.g. to check IsBranchOpen after a restore).
func (w *Window) TreeForTest() *widget.Tree {
	return w.tree
}

// SelectedNodeForTest exposes the currently-selected tree node's UID, for
// settings_test's external test package.
func (w *Window) SelectedNodeForTest() string {
	return w.selectedUID
}

// TreeComponentIDForTest exposes the db component ID used for tree-state
// persistence, for settings_test's external test package to write a
// synthetic stale blob against.
func TreeComponentIDForTest() string {
	return componentID + ".tree"
}

// ApplySearchForTest exposes applySearch for settings_test's external test
// package (e.g. to verify search-driven branch expansion persists).
func (w *Window) ApplySearchForTest(query string) {
	w.applySearch(query)
}
