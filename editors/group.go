package editors

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/fsnotify/fsnotify"

	"go-ux/db"
	"go-ux/fontsettings"
)

// Group is the embeddable parent layout component: it owns the current
// split-geometry tree (see split.go) and rebuilds its own displayed
// content (a lone Pane, or a nested container.Split tree) every time that
// shape changes. It embeds widget.BaseWidget and satisfies
// fyne.CanvasObject, so a host app can drop it straight into any Fyne
// container — same embedding pattern as terminal.Session.
//
// Every exported method on Group (AddTab, SplitRight/SplitDown,
// MoveRight/MoveDown) is expected to be called on the Fyne UI goroutine —
// same documented contract as terminal.TabView's AddTab/CloseTab. Phase 1
// has no background goroutines anywhere in this package (confirmed: no
// `go` statements), so unlike terminal's uiMu (which coordinates real
// background PTY-reader/refresh loops against the UI goroutine), Group
// has no cross-goroutine state to guard and deliberately carries no
// internal lock — a mutex here with nothing on the other end of it would
// just be a false signal of thread-safety. If a later phase (e.g. file
// watching) introduces a background goroutine that touches Group's
// state, it must cross onto the UI goroutine via fyne.Do before calling
// any Group method, the same way terminal's background loops do — it
// should NOT reach for a mutex on Group instead.
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

	root    *node
	primary *Pane
	nextID  int

	container *fyne.Container // holds the single current CanvasObject rebuild() produces; container.NewStack(...) with exactly one child, swapped via .Objects on every rebuild — this indirection is what lets Group itself stay one stable CanvasObject (needed since a fyne.CanvasObject generally can't just "become" a different concrete object once placed in a parent container)

	database  *db.DB // nil means "no persistence" — matches terminal's NewWindow (no db) vs NewWindowFromSettings (db) pattern; every method below must stay nil-safe
	groupID   string // caller-chosen, unique per Group instance persisted independently — meaningless if database is nil
	restoring bool   // held true while NewGroupFromSettings is replaying a persisted layout (AddTab etc. run as part of that replay) so notifyChanged doesn't fire mid-restore and overwrite the still-under-construction tree with g.root's stale pre-restore shape — same guard pattern as treestate.restoring

	// batchDepth, while > 0, makes notifyChanged a no-op instead of
	// actually saving — see withBatchedSave's doc comment. A depth
	// counter, not a bool, so a batched operation (e.g. movePane's
	// auto-split-then-move) that itself calls another batched building
	// block (splitPane) doesn't have the inner call's completion turn
	// batching off early while the outer one is still in progress.
	batchDepth int

	// fonts is this Group's own independent font-size state (font.go),
	// Ctrl+scroll-adjustable from any Pane's content area — unlike
	// terminal's single package-global FontSettings shared by every open
	// Session everywhere, each Group gets its own, per the design plan's
	// "independent per Group instance" decision.
	fonts *fontsettings.State

	// fileWatchMode is FileWatchModeAuto or FileWatchModeNotify
	// (settings_schema.go/watch.go) — read once from database at
	// NewGroupFromSettings time, and re-synced live by ApplyEditorSettings
	// (settings_schema.go) after a settings-window OK/Apply.
	fileWatchMode string

	// watcher/watchedFiles are file-watching state (watch.go) — watcher is
	// lazily created on the first watched file (nil until then);
	// watchedFiles tracks which paths have already been added to it so
	// startWatching doesn't double-Add the same path (e.g. after a split
	// copies a Tab into a second Pane).
	watcher      *fsnotify.Watcher
	watchedFiles map[string]bool

	// OnSaveAsRequested, if set by the host app, is called when Ctrl+S is
	// pressed on a tab with no FilePath (see mcptooling.go's requestSaveAs
	// and Ctrl+S's own doc comment in font.go). This package has no file
	// picker of its own (same design decision as OpenFile — see its doc
	// comment) — the host is expected to show its own save dialog and
	// then call SaveTabAs with the chosen path. Left nil, Ctrl+S on such a
	// tab is simply a no-op.
	OnSaveAsRequested func(tab *Tab)
}

// NewGroup builds a Group with a single, empty primary Pane. Call AddTab
// to populate it (the Phase 1 demo harness adds 3 placeholder tabs this
// way).
func NewGroup(app fyne.App) *Group {
	g := &Group{app: app, fonts: fontsettings.NewState(fontsettings.DefaultFontSettings), fileWatchMode: FileWatchModeNotify}
	g.primary = newPane(g, "pane-0", true)
	g.root = leaf(g.primary)
	g.container = container.NewStack(g.primary)
	g.ExtendBaseWidget(g)
	return g
}

func (g *Group) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.container)
}

// Close stops g's background file watcher (watch.go), if one was ever
// started (startWatching creates it lazily on the first watched file —
// see its own doc comment on why there's no per-Tab stop, only this
// whole-Group one). Safe to call even if no file was ever watched.
//
// Callers embedding a Group in a real window should call this when the
// window closes, so the watch.go goroutine doesn't keep running (and
// potentially calling fyne.Do against a torn-down app) indefinitely.
// Tests that call OpenFile/ProposeDiff (which start real, live fsnotify
// watches on real files) MUST call this via t.Cleanup, or the watcher's
// background goroutine can outlive the test and fire fyne.Do against a
// DIFFERENT, later test's fynetest.NewApp() — this was a real, observed
// failure (a later test's south bar rendering panicked from a stale
// watcher's delayed event) before Close existed.
func (g *Group) Close() {
	if g.watcher != nil {
		g.watcher.Close()
	}
}

// notifyChanged is called by Pane after every tab/structural mutation. If
// this Group has a database (see NewGroupFromSettings), it persists the
// ENTIRE current layout immediately — matching SaveEditorLayout's
// full-replace design and this package's "live, not just on close"
// persistence decision. A no-op if database is nil (NewGroup-only
// callers), while a persisted layout is being replayed (see
// g.restoring's doc comment), or while a batched operation is in
// progress (see withBatchedSave).
//
// Also syncs every split's live drag offset back into the node tree first
// (see node.live's doc comment in split.go) — Fyne's container.Split has
// no drag-end callback, so a pure resize with no other change in between
// isn't captured the instant the drag ends; this is what makes the most
// recent resize get captured the next time anything else triggers a
// save, rather than never at all.
func (g *Group) notifyChanged() {
	if g.database == nil || g.restoring || g.batchDepth > 0 {
		return
	}
	syncOffsets(g.root)
	panes, tabs := g.buildPersistedLayout()
	if err := g.database.SaveEditorLayout(g.groupID, panes, tabs); err != nil {
		log.Printf("editors: save layout: %v", err)
	}
}

// withBatchedSave runs fn with notifyChanged suppressed for its duration,
// then performs exactly one real save afterward (if database is set).
// Several of this package's own lower-level building blocks each call
// notifyChanged on their own (Pane.AddTab, closePane, splitPane) so that
// calling any one of them directly still persists correctly — but an
// operation that's really one user action calling several of them in
// sequence (movePane's auto-split-then-move: splitPane, then AddTab/
// setActive on the target, then possibly closePane on the now-empty
// source) would otherwise trigger the same full-layout save 2-4 times
// over for that single action. Harmless (SaveEditorLayout is a full
// replace, so extra calls are idempotent, not incorrect) but wasteful.
// MoveRight/MoveDown/SplitRight/SplitDown wrap their bodies in this.
func (g *Group) withBatchedSave(fn func()) {
	g.batchDepth++
	fn()
	g.batchDepth--
	if g.batchDepth == 0 {
		g.notifyChanged()
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
	g.nextID++
	return fmt.Sprintf("pane-%d", g.nextID)
}

// SplitRight splits source horizontally, adding a new Pane to its right
// showing the same active Tab. A no-op if source is not eligible to
// split further (see canSplit) — pane.go's showContextMenu greys out the
// menu item in that case, but this itself stays defensive since it's
// also reachable directly, not just via that menu.
//
// Wrapped in withBatchedSave (group.go) since splitPane's own internal
// AddTab call already triggers a save on its own — without this, a bare
// SplitRight/SplitDown would persist the layout twice for one call.
func (g *Group) SplitRight(source *Pane) {
	g.withBatchedSave(func() { g.splitPane(source, axisHorizontal) })
}

// SplitDown splits source vertically, adding a new Pane below it.
// Same eligibility rule and batched-save reasoning as SplitRight.
func (g *Group) SplitDown(source *Pane) {
	g.withBatchedSave(func() { g.splitPane(source, axisVertical) })
}

func (g *Group) splitPane(source *Pane, axis splitAxis) *Pane {
	newPaneObj := newPane(g, g.nextPaneID(), false)
	newRoot, ok := split(g.root, source, axis, newPaneObj)
	if !ok {
		return nil
	}
	g.root = newRoot

	// Split shows the same document in both panes ("the new pane shows
	// the SAME underlying document, synced live" — see the design's split
	// semantics decision, distinguishing it from Move, which relocates
	// rather than duplicates). Phase 1 has no separate shared Document
	// object yet (that's Phase 2), so the *Tab itself stands in for that:
	// both panes' tab lists end up holding the identical *Tab pointer,
	// not a copy — closing it in one pane only removes it from that
	// pane's own list, leaving the other pane's reference untouched,
	// matching IntelliJ's split-editor behavior.
	if source.active != nil {
		newPaneObj.AddTab(source.active)
	}

	g.rebuildContent()
	g.notifyChanged()
	return newPaneObj
}

// MoveRight moves tab out of source and into the Pane to source's right,
// auto-creating that split (via SplitRight) first if one doesn't already
// exist — decided "auto-split-then-move" semantics.
//
// Wrapped in withBatchedSave: the auto-split path alone can otherwise
// trigger up to 3-4 redundant full-layout saves for this one call
// (splitPane's own AddTab + its own save, then this method's own
// AddTab/closePane) — see withBatchedSave's doc comment.
func (g *Group) MoveRight(source *Pane, tab *Tab) {
	g.withBatchedSave(func() { g.movePane(source, tab, axisHorizontal) })
}

// MoveDown is MoveRight's vertical counterpart.
func (g *Group) MoveDown(source *Pane, tab *Tab) {
	g.withBatchedSave(func() { g.movePane(source, tab, axisVertical) })
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
	// The target may already have tab — either splitPane (just above)
	// copied it there, or it was already adjacent and had already
	// received the same shared-Document tab some other way (e.g.
	// Move-Right immediately after a Split copied that very tab into
	// this same adjacent pane). Either way, AddTab-ing it again below
	// would duplicate it, so just make sure it's the active one there
	// instead. This check used to only run in the auto-split branch
	// above, missing the already-adjacent case — a real bug (silent tab
	// duplication) caught by a strengthened test that happened to move
	// a just-split tab to an already-existing adjacent pane.
	alreadyInTarget := target.hasTab(tab)

	stillHasTabs := source.removeTabLocally(tab)
	source.tabBar.Tabs = source.tabs
	source.tabBar.Refresh()

	if alreadyInTarget {
		target.setActive(tab)
	} else {
		target.AddTab(tab)
	}

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
//
// Also prunes any empty non-primary pane from g.root first (pruneEmpty —
// the same self-heal layoutstate.go's restore path already uses), rather
// than relying solely on each individual call site (closeTabRequested,
// movePane) to remember to check "did this leave a pane with zero tabs?"
// itself. closeTabRequested/movePane's own checks stay in place (they
// give the immediate, correct behavior — auto-closing the instant a
// pane's last tab is closed or moved out, in the same user action,
// rather than waiting for the next rebuild); this is a second,
// independent guarantee that the invariant "no non-primary pane has zero
// tabs" holds after every structural mutation, regardless of which path
// produced the emptiness — including ones not anticipated by an explicit
// check at the time it was written.
func (g *Group) rebuildContent() {
	g.root = pruneEmpty(g.root, g.primary)
	g.container.Objects = []fyne.CanvasObject{rebuild(g.root)}
	g.container.Refresh()
}
