package editors

import (
	"fmt"
	"log"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"go-ux/db"
)

// Group is the embeddable parent layout component: it owns the current
// split-geometry tree (see split.go) and rebuilds its own displayed
// content (a lone Pane, or a nested container.Split tree) every time that
// shape changes. It embeds widget.BaseWidget and satisfies
// fyne.CanvasObject, so a host app can drop it straight into any Fyne
// container — same embedding pattern as terminal.Session.
//
// NewGroup's signature is deliberately minimal in this Phase 1 task — no
// *db.DB parameter yet. A later task (layoutstate.go) adds live
// persistence and will extend this constructor (or add a second one,
// e.g. NewGroupFromSettings) to take a *db.DB and a groupID string; that
// is an intentional, expected follow-up change to this file, not a gap to
// fill in now.
type Group struct {
	widget.BaseWidget

	app fyne.App

	mu      sync.Mutex // guards root/nextID; Fyne callbacks (menu actions, tab bar events) all run on the UI goroutine already, so this is defense-in-depth rather than a proven necessity — cheap enough to include, matches this repo's general carefulness around shared mutable widget state (see terminal's uiMu precedent, though Group's own mutex is package-local to Group, not shared)
	root    *node
	primary *Pane
	nextID  int

	container *fyne.Container // holds the single current CanvasObject rebuild() produces; container.NewStack(...) with exactly one child, swapped via .Objects on every rebuild — this indirection is what lets Group itself stay one stable CanvasObject (needed since a fyne.CanvasObject generally can't just "become" a different concrete object once placed in a parent container)

	database  *db.DB // nil means "no persistence" — matches terminal's NewWindow (no db) vs NewWindowFromSettings (db) pattern; every method below must stay nil-safe
	groupID   string // caller-chosen, unique per Group instance persisted independently — meaningless if database is nil
	restoring bool   // held true while NewGroupFromSettings is replaying a persisted layout (AddTab etc. run as part of that replay) so notifyChanged doesn't fire mid-restore and overwrite the still-under-construction tree with g.root's stale pre-restore shape — same guard pattern as treestate.restoring
}

// NewGroup builds a Group with a single, empty primary Pane. Call AddTab
// to populate it (the Phase 1 demo harness adds 3 placeholder tabs this
// way).
func NewGroup(app fyne.App) *Group {
	g := &Group{app: app}
	g.primary = newPane(g, "pane-0", true)
	g.root = leaf(g.primary)
	g.container = container.NewStack(g.primary)
	g.ExtendBaseWidget(g)
	return g
}

func (g *Group) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.container)
}

// notifyChanged is called by Pane after every tab/structural mutation. If
// this Group has a database (see NewGroupFromSettings), it persists the
// ENTIRE current layout immediately — matching SaveEditorLayout's
// full-replace design and this package's "live, not just on close"
// persistence decision. A no-op if database is nil (NewGroup-only
// callers) or while a persisted layout is being replayed (see
// g.restoring's doc comment).
func (g *Group) notifyChanged() {
	if g.database == nil || g.restoring {
		return
	}
	panes, tabs := g.buildPersistedLayout()
	if err := g.database.SaveEditorLayout(g.groupID, panes, tabs); err != nil {
		log.Printf("editors: save layout: %v", err)
	}
}

// AddTab is Phase 1's way to seed tabs into the primary pane (the demo
// harness needs this before split-based layout has any tabs to move
// around).
func (g *Group) AddTab(tab *Tab) {
	g.primary.AddTab(tab)
}

// nextPaneID returns a fresh, unique pane identifier for use by
// SplitRight/SplitDown.
func (g *Group) nextPaneID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	return fmt.Sprintf("pane-%d", g.nextID)
}

// SplitRight splits source horizontally, adding a fresh empty Pane to its
// right. A no-op if source is not eligible to split further (see
// canSplit) — a real UI would grey out the menu item, but Phase 1's
// showContextMenu always offers all items, an acceptable known gap.
func (g *Group) SplitRight(source *Pane) {
	g.splitPane(source, axisHorizontal)
}

// SplitDown splits source vertically, adding a fresh empty Pane below it.
// Same eligibility rule as SplitRight.
func (g *Group) SplitDown(source *Pane) {
	g.splitPane(source, axisVertical)
}

func (g *Group) splitPane(source *Pane, axis splitAxis) *Pane {
	newPaneObj := newPane(g, g.nextPaneID(), false)
	newRoot, ok := split(g.root, source, axis, newPaneObj)
	if !ok {
		return nil
	}
	g.root = newRoot
	g.rebuildContent()
	g.notifyChanged()
	return newPaneObj
}

// MoveRight moves tab out of source and into the Pane to source's right,
// auto-creating that split (via SplitRight) first if one doesn't already
// exist — decided "auto-split-then-move" semantics.
func (g *Group) MoveRight(source *Pane, tab *Tab) {
	g.movePane(source, tab, axisHorizontal)
}

// MoveDown is MoveRight's vertical counterpart.
func (g *Group) MoveDown(source *Pane, tab *Tab) {
	g.movePane(source, tab, axisVertical)
}

func (g *Group) movePane(source *Pane, tab *Tab, axis splitAxis) {
	target, ok := adjacentPane(g.root, source, axis)
	if !ok {
		if g.splitPane(source, axis) == nil {
			return // source wasn't eligible to split; nothing to move into
		}
		target, ok = adjacentPane(g.root, source, axis)
		if !ok {
			return // shouldn't happen after a successful split, but be defensive
		}
	}

	stillHasTabs := source.removeTabLocally(tab)
	source.tabBar.Tabs = source.tabs
	source.tabBar.Refresh()

	target.AddTab(tab)

	if !stillHasTabs && !source.isPrimary {
		g.closePane(source) // closePane rebuilds content and notifies; avoid double work below
		return
	}

	g.rebuildContent()
	g.notifyChanged()
}

// closePane removes p from the layout entirely, promoting its sibling up
// one level — called when a non-primary Pane's last tab closes. A no-op
// if p is not eligible for removal (e.g. it's the primary pane).
func (g *Group) closePane(p *Pane) {
	newRoot, ok := removePane(g.root, p, g.primary)
	if !ok {
		return
	}
	g.root = newRoot
	g.rebuildContent()
	g.notifyChanged()
}

// rebuildContent reconstructs the Group's displayed content from the
// current root shape. Called after every structural change above.
func (g *Group) rebuildContent() {
	g.container.Objects = []fyne.CanvasObject{rebuild(g.root)}
	g.container.Refresh()
}
