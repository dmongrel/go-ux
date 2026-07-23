package editors

import (
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
	contentCleanup func()          // unregisters center's current content from its Document's listeners AND its font-size theme override; nil when center is empty

	fonts *fontsettings.State // this Pane's Group's font-size state (font.go); a standalone default when group is nil (pure pane-level tests)
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
	p.southBar = NewSouthBar()
	p.center = container.NewStack()
	p.ExtendBaseWidget(p)
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
	if tab != nil {
		content, cleanup := newDocumentContent(tab, p, p.fonts)
		p.center.Objects = []fyne.CanvasObject{content}
		p.contentCleanup = cleanup
	} else {
		p.center.Objects = nil
	}
	p.center.Refresh()
	p.tabBar.Tabs = p.tabs
	p.tabBar.Active = tab
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
// tab chip's right-click menu (split/move/close actions).
func (p *Pane) showContextMenu(tab *Tab, pos fyne.Position) {
	canvas := fyne.CurrentApp().Driver().CanvasForObject(p)
	if canvas == nil {
		return
	}

	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Split Right", func() {
			if p.group != nil {
				p.group.SplitRight(p)
			}
		}),
		fyne.NewMenuItem("Split Down", func() {
			if p.group != nil {
				p.group.SplitDown(p)
			}
		}),
		fyne.NewMenuItem("Move Right", func() {
			if p.group != nil {
				p.group.MoveRight(p, tab)
			}
		}),
		fyne.NewMenuItem("Move Down", func() {
			if p.group != nil {
				p.group.MoveDown(p, tab)
			}
		}),
		fyne.NewMenuItem("Close Tab", func() { p.closeTabRequested(tab) }),
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, canvas, pos)
}
