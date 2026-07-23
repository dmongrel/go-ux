package editors

import (
	"log"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"go-ux/fontsettings"
)

// Pane is one editor sub-component: a TabBar (North, always visible), a
// center content area showing the active Tab's content (Phase 1:
// NewPlaceholderContent's static text), and a SouthBar (South, hidden
// until Phase 2's diff-review/file-watch features drive it). It embeds
// widget.BaseWidget and satisfies fyne.CanvasObject so it can be a leaf
// in split.go's node tree (see rebuild's `return root.pane` case) and a
// direct child of a container.Split.
type Pane struct {
	widget.BaseWidget

	id        string
	group     *Group // back-reference: split/move/close menu actions on this Pane's tabs operate on the whole Group's layout, not just this Pane
	isPrimary bool

	tabBar   *TabBar
	southBar *SouthBar

	tabs   []*Tab
	active *Tab

	center         *fyne.Container // holds the current active Tab's content; swapped in place on tab switch
	contentCleanup func()          // unregisters center's current content from its Document's listeners AND its font-size theme override; nil when center is empty or showing a markdown preview snapshot

	fonts *fontsettings.State // this Pane's Group's font-size state (font.go); a standalone default when group is nil (pure pane-level tests)

	// previewMode is whether center is currently showing a rendered
	// markdown preview (markdown.go's renderMarkdown) of the active tab
	// rather than the normal editable content — toggled via
	// tabBar.OnTogglePreview (wired in newPane, handled by togglePreview
	// below). Reset to false on every tab switch (setActive) — preview is
	// a per-view toggle, not state that follows a tab around.
	previewMode bool
}

// newPane builds a Pane wired into group (which may be nil in pure
// pane-level tests; every call site below guards p.group != nil before
// dereferencing it), with the given identity/role. Callers add content via
// AddTab.
func newPane(group *Group, id string, isPrimary bool) *Pane {
	p := &Pane{id: id, group: group, isPrimary: isPrimary}
	if group != nil {
		p.fonts = group.fonts
	} else {
		p.fonts = fontsettings.NewState(fontsettings.DefaultFontSettings)
	}
	p.tabBar = NewTabBar()
	p.tabBar.OnSelected = p.selectTab
	p.tabBar.OnClosed = p.closeTabRequested
	p.tabBar.OnContextMenu = p.showContextMenu
	p.tabBar.OnTogglePreview = p.togglePreview
	p.southBar = NewSouthBar()
	p.center = container.NewStack()
	p.ExtendBaseWidget(p)
	p.rebuildCenterContent() // shows the empty-pane placeholder immediately, not just after a tab is added and later closed
	return p
}

// CreateRenderer lays out the Pane as tabBar (North) / southBar (South) /
// center (the remaining space) — the standard container.NewBorder shape
// used throughout this repo (see terminal/widget.go).
func (p *Pane) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewBorder(p.tabBar, p.southBar, nil, nil, p.center))
}

// AddTab appends tab to this Pane and makes it the active tab. This is
// Phase 1's only way to populate a Pane with content — the demo harness
// and Group.AddTab both funnel through this.
func (p *Pane) AddTab(tab *Tab) {
	p.tabs = append(p.tabs, tab)
	p.setActive(tab)
	if p.group != nil {
		p.group.startWatching(tab)
		p.group.notifyChanged()
	}
}

// selectTab is TabBar.OnSelected's handler: the user tapped a tab chip.
func (p *Pane) selectTab(tab *Tab) {
	p.setActive(tab)
	if p.group != nil {
		p.group.notifyChanged()
	}
}

// selectAdjacentTab moves the active tab by delta positions within
// p.tabs, wrapping around — Ctrl+PageDown/Ctrl+PageUp's handler
// (font.go's editorEntry.TypedShortcut, wired via
// documentContentCallbacks in rebuildCenterContent), for cycling through
// a Pane's open tabs without leaving the content area to click a tab
// chip. A no-op if there are fewer than 2 tabs (nothing to cycle to) or
// no active tab.
func (p *Pane) selectAdjacentTab(delta int) {
	if len(p.tabs) < 2 || p.active == nil {
		return
	}
	idx := -1
	for i, t := range p.tabs {
		if t == p.active {
			idx = i
			break
		}
	}
	if idx == -1 {
		return
	}
	next := (idx + delta + len(p.tabs)) % len(p.tabs)
	p.selectTab(p.tabs[next])
}

// setActive sets tab as this Pane's active tab and redraws the center
// content, and the tab bar's chip strip + highlight, to match. Internal —
// callers that also need to persist or notify do so themselves after
// calling this (see AddTab/selectTab).
//
// Also syncs p.tabBar.Tabs = p.tabs every time, not just Active — AddTab
// and selectTab never touch p.tabBar.Tabs themselves (only
// removeTabLocally's callers, closeTabRequested/movePane, used to do that
// explicitly), so without this the tab bar's chip strip stayed
// permanently empty after every AddTab: the bar rendered zero chips (no
// visible height), leaving only the center content visible — exactly the
// "just a window with a label, no tab bar" symptom this fixes.
func (p *Pane) setActive(tab *Tab) {
	if p.contentCleanup != nil {
		p.contentCleanup()
		p.contentCleanup = nil
	}

	p.active = tab
	p.previewMode = false // switching tabs always lands on the editable view, not a stale preview snapshot of the new tab
	p.rebuildCenterContent()
	p.tabBar.Tabs = p.tabs
	p.tabBar.Active = tab
	p.tabBar.PreviewMode = p.previewMode
	p.tabBar.Refresh()
}

// rebuildCenterContent (re)builds center's content for the current
// active/previewMode combination: nothing (no active tab), a rendered
// markdown snapshot (previewMode on a Markdown tab), or the normal
// editable Document-backed content otherwise. Callers that change
// previewMode or active must call this themselves (see setActive,
// togglePreview) — it does not touch tabBar.
func (p *Pane) rebuildCenterContent() {
	if p.active == nil {
		p.center.Objects = []fyne.CanvasObject{newEmptyPaneContent()}
		p.center.Refresh()
		return
	}

	if p.previewMode && isMarkdownFile(p.active.FilePath) {
		preview := container.NewScroll(renderMarkdown([]byte(p.active.Doc.Text())))
		p.center.Objects = []fyne.CanvasObject{preview}
		p.center.Refresh()
		return
	}

	tab := p.active
	content, cleanup := newDocumentContent(tab, p, p.fonts, documentContentCallbacks{
		onSave: func() {
			if p.group == nil {
				return
			}
			if err := p.group.SaveTab(tab); err != nil {
				log.Printf("editors: save %s: %v", tab.FilePath, err)
			}
		},
		onNextTab: func() { p.selectAdjacentTab(1) },
		onPrevTab: func() { p.selectAdjacentTab(-1) },
	})
	p.center.Objects = []fyne.CanvasObject{content}

	// A second, independent Document listener (content.go's own, keyed the
	// same way, only syncs the editable widget's text) — this one exists
	// solely to redraw the tab bar's "*" dirty indicator (tabbar.go's
	// chipTitle) the moment Dirty() actually changes: once on the first
	// edit (false -> true), and once on a successful Ctrl+S/Accept
	// (true -> false, since Document.MarkClean also notifies listeners).
	// Guarded to skip a Refresh (a full chip-strip rebuild) on every
	// keystroke once already dirty, when Dirty() hasn't actually flipped.
	lastDirty := tab.Dirty()
	tab.Doc.RegisterListener(p.tabBar, func(string) {
		if tab.Dirty() == lastDirty {
			return
		}
		lastDirty = tab.Dirty()
		p.tabBar.Refresh()
	})
	p.contentCleanup = func() {
		cleanup()
		tab.Doc.UnregisterListener(p.tabBar)
	}
	p.center.Refresh()
}

// showDiffReview switches center to a read-only colored diff between
// tab.Doc.Text() and tab.pendingDiff.newText, and puts southBar into
// SouthBarDiffReview mode with Accept/Cancel wired to the owning Group —
// called by Group.ProposeDiff for every Pane currently showing tab.
// Requires tab.pendingDiff to be non-nil (the caller, ProposeDiff, always
// sets it just before calling this).
func (p *Pane) showDiffReview(tab *Tab) {
	if p.contentCleanup != nil {
		p.contentCleanup()
		p.contentCleanup = nil
	}
	lines := computeDiff(tab.Doc.Text(), tab.pendingDiff.newText)
	p.center.Objects = []fyne.CanvasObject{container.NewScroll(renderDiff(lines))}
	p.center.Refresh()

	p.southBar.SetMode(SouthBarDiffReview, func() {
		if p.group != nil {
			p.group.acceptDiff(tab)
		}
	}, func() {
		if p.group != nil {
			p.group.cancelDiff(tab)
		}
	})
}

// exitDiffReview returns southBar to hidden and center to its normal
// (editable or preview) content — called by Group.exitDiffReview once a
// pending diff has been accepted or cancelled.
func (p *Pane) exitDiffReview() {
	p.southBar.SetMode(SouthBarHidden, nil, nil)
	p.rebuildCenterContent()
}

// showFileChangedNotice puts southBar into SouthBarFileChanged mode over
// tab: "Load from Disk" re-reads tab.FilePath and applies it to the
// Document (live-syncing any other Pane showing the same Tab, same as any
// other Document edit); "Keep from Memory" just dismisses the notice,
// leaving the Document as-is. Called by Group.handleFileChanged
// (watch.go) for every Pane currently showing tab as active.
func (p *Pane) showFileChangedNotice(tab *Tab) {
	p.southBar.SetMode(SouthBarFileChanged, func() {
		data, err := os.ReadFile(tab.FilePath)
		if err == nil {
			tab.Doc.SetText(string(data))
		}
		p.southBar.SetMode(SouthBarHidden, nil, nil)
	}, func() {
		p.southBar.SetMode(SouthBarHidden, nil, nil)
	})
}

// togglePreview is tabBar.OnTogglePreview's handler: flips previewMode and
// rebuilds center accordingly, without touching which tab is active. A
// no-op if there's no active tab (the preview button is hidden in that
// case anyway — see tabbar.go's rebuild).
func (p *Pane) togglePreview() {
	if p.active == nil {
		return
	}
	if p.contentCleanup != nil {
		p.contentCleanup()
		p.contentCleanup = nil
	}
	p.previewMode = !p.previewMode
	p.rebuildCenterContent()
	p.tabBar.PreviewMode = p.previewMode
	p.tabBar.Refresh()
}

// hasTab reports whether tab (by pointer identity) is already in p.tabs —
// used by Group's move logic to detect when an auto-split (see splitPane)
// already copied the tab being moved into the target pane, so it isn't
// added a second time.
func (p *Pane) hasTab(tab *Tab) bool {
	for _, t := range p.tabs {
		if t == tab {
			return true
		}
	}
	return false
}

// removeTabLocally splices tab out of p.tabs by pointer identity and, if
// it was the active tab, picks a reasonable neighbor to activate next
// (the tab that was to closed tab's right, or the new last tab if the
// closed one was rightmost) — matching common editor UX (e.g. IntelliJ).
// It does not touch p.tabBar or notify the Group; callers do that
// themselves, since the two call sites (closeTabRequested, the Group's
// move logic) need to sequence those differently. Returns whether p.tabs
// is still non-empty afterward.
func (p *Pane) removeTabLocally(tab *Tab) (stillHasTabs bool) {
	idx := -1
	for i, t := range p.tabs {
		if t == tab {
			idx = i
			break
		}
	}
	if idx == -1 {
		return len(p.tabs) > 0
	}
	wasActive := p.active == tab
	p.tabs = append(p.tabs[:idx], p.tabs[idx+1:]...)

	if len(p.tabs) == 0 {
		p.setActive(nil)
		return false
	}

	if wasActive {
		next := idx
		if next >= len(p.tabs) {
			next = len(p.tabs) - 1
		}
		p.setActive(p.tabs[next])
	}
	return true
}

// closeTabRequested is TabBar.OnClosed's handler: the user tapped a tab
// chip's close glyph. Removes tab from p.tabs, picks a new active
// neighbor if needed, and — per the design — closing the last tab in a
// non-primary (split) Pane closes the Pane itself, while closing the last
// tab in the primary Pane just leaves it empty.
func (p *Pane) closeTabRequested(tab *Tab) {
	stillHasTabs := p.removeTabLocally(tab)

	if !stillHasTabs && !p.isPrimary {
		p.tabBar.Tabs = p.tabs
		p.tabBar.Refresh()
		if p.group != nil {
			p.group.closePane(p) // closePane rebuilds content and notifies; avoid double-notifying here
		}
		return
	}

	p.tabBar.Tabs = p.tabs
	p.tabBar.Refresh()
	if p.group != nil {
		p.group.notifyChanged()
	}
}

// showContextMenu is TabBar.OnContextMenu's handler: builds and shows the
// tab chip's right-click menu (split/move/close actions). Split/Move items
// are greyed out (MenuItem.Disabled) when the action genuinely isn't
// possible right now (a pane already nested one level deep can't be split
// or auto-split-then-moved any further — see split.go's one-level-nesting
// cap) — previously every item was always enabled and an ineligible one
// was just a silent no-op when clicked, a real, if low-severity, UX gap
// flagged since Phase 1's original whole-branch review.
func (p *Pane) showContextMenu(tab *Tab, pos fyne.Position) {
	canvas := fyne.CurrentApp().Driver().CanvasForObject(p)
	if canvas == nil {
		return
	}

	var root *node
	if p.group != nil {
		root = p.group.root
	}
	splittable := p.group != nil && canSplit(root, p)
	canMoveRight := p.group != nil && (canAdjacentOrSplit(root, p, axisHorizontal))
	canMoveDown := p.group != nil && (canAdjacentOrSplit(root, p, axisVertical))

	items := []*fyne.MenuItem{
		{Label: "Split Right", Disabled: !splittable, Action: func() {
			if p.group != nil {
				p.group.SplitRight(p)
			}
		}},
		{Label: "Split Down", Disabled: !splittable, Action: func() {
			if p.group != nil {
				p.group.SplitDown(p)
			}
		}},
		{Label: "Move Right", Disabled: !canMoveRight, Action: func() {
			if p.group != nil {
				p.group.MoveRight(p, tab)
			}
		}},
		{Label: "Move Down", Disabled: !canMoveDown, Action: func() {
			if p.group != nil {
				p.group.MoveDown(p, tab)
			}
		}},
		fyne.NewMenuItem("Close Tab", func() { p.closeTabRequested(tab) }),
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, canvas, pos)
}

// canAdjacentOrSplit reports whether Move{Right,Down} would actually do
// anything for source along axis: either an adjacent pane already exists
// there, or one could be auto-created via a split (movePane's own
// "auto-split-then-move" fallback — see group.go).
func canAdjacentOrSplit(root *node, source *Pane, axis splitAxis) bool {
	if _, ok := adjacentPane(root, source, axis); ok {
		return true
	}
	return canSplit(root, source)
}
