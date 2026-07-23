package editors

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// chipActiveColor fills the background rectangle behind the active tab
// chip's title, distinguishing it from its inactive siblings — the same
// rectangle-behind-text technique settings.go's highlightColor uses to mark
// a search match, reused here for a different purpose (selection rather
// than search).
var chipActiveColor = color.NRGBA{R: 66, G: 133, B: 244, A: 90}

// TabBar is the North, always-visible strip of IntelliJ-style tab chips
// for one Pane. It owns no tab data itself — Tabs is the live slice a
// Pane provides and TabBar renders; call Refresh after mutating Tabs
// (adding/removing/reordering) to redraw.
//
// This does NOT reuse container.DocTabs, unlike terminal/tabs.go's
// TabView. DocTabs conflates "which tab is selected" with "swap the
// displayed content" — selecting a DocTabs item replaces the visible
// canvas.Object with that item's Content wholesale. That fits terminal's
// model (each tab owns its own independent Session) but not this
// package's: a Pane's center content is Pane-owned and, in the shared-
// document/split-pane design (see the design plan), a Tab is just a UI
// entry that can point at a document shared across multiple Panes —
// switching the active Tab in one Pane must not implicitly own or
// reparent any content object the way DocTabs' Select does. Hand-rolling
// the strip keeps "which tab is selected" (TabBar's Active field) and
// "what the Pane displays" (content.go, driven by Pane, a later task)
// fully decoupled.
type TabBar struct {
	widget.BaseWidget

	Tabs   []*Tab
	Active *Tab // which of Tabs is currently selected; nil if Tabs is empty

	// OnSelected fires when the user taps a tab chip (not called for the
	// initial Active value or programmatic changes to Active — callers
	// that programmatically change Active are expected to already know
	// about it and don't need a redundant callback).
	OnSelected func(*Tab)
	// OnClosed fires when the user taps a tab chip's close glyph. TabBar
	// does not remove the tab from Tabs itself — the caller (Pane) is
	// responsible for actually closing it (which may involve more than
	// just this bar, e.g. persistence) and then updating Tabs + calling
	// Refresh.
	OnClosed func(*Tab)
	// OnContextMenu fires on a tab chip's right-click/secondary-tap, with
	// the tab and the screen position to show a menu at. TabBar itself
	// builds no menu — a later task (menu.go) owns constructing and
	// showing the actual right-click menu (split-right/split-down/
	// move-right/move-down/close), keeping this file focused on rendering
	// and selection only.
	OnContextMenu func(tab *Tab, pos fyne.Position)

	// strip is the HBox of chips built by rebuild and shown by the
	// renderer. Kept as a field (rather than rebuilt as a whole new
	// container each render) so Refresh can replace its Objects in place
	// without CreateRenderer running again.
	strip *fyne.Container
}

// NewTabBar builds an empty TabBar. Set Tabs (and, once non-empty,
// typically Active) before or after showing it; call Refresh after any
// later mutation of either field.
func NewTabBar() *TabBar {
	t := &TabBar{}
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer builds the chip strip. Fyne calls this once per widget
// instance; later structural changes go through Refresh, not a second
// CreateRenderer call, which is why strip is stashed on the TabBar itself
// (see its doc comment) rather than only living inside the renderer.
func (t *TabBar) CreateRenderer() fyne.WidgetRenderer {
	t.strip = container.NewHBox()
	t.rebuild()
	return widget.NewSimpleRenderer(t.strip)
}

// Refresh rebuilds the chip strip from the current Tabs/Active before
// delegating to widget.BaseWidget.Refresh's usual redraw — the standard
// Fyne "override Refresh to react to mutable exported fields" pattern
// (see terminal/widget.go's sessionRenderer.Refresh for another instance
// of the same idea, there driven by grid/cursor state instead).
func (t *TabBar) Refresh() {
	if t.strip != nil {
		t.rebuild()
	}
	t.BaseWidget.Refresh()
}

// rebuild replaces strip's chips with a fresh set built from Tabs/Active.
// Called by both CreateRenderer (first build) and Refresh (every
// subsequent one) — see their doc comments.
func (t *TabBar) rebuild() {
	chips := make([]fyne.CanvasObject, 0, len(t.Tabs))
	for _, tab := range t.Tabs {
		chips = append(chips, t.newChip(tab))
	}
	t.strip.Objects = chips
	t.strip.Refresh()
}

// newChip builds one tab's visual chip: a title (tap to select,
// secondary-tap for the context menu) beside a close glyph (tap to
// close). The two sit in a zero-gap layout (layout.NewCustomPaddedHBoxLayout(0)
// — Fyne's regular HBox always inserts theme padding between children,
// which read as two separate buttons rather than one) with a single
// shared background rectangle spanning both, so title+close visually
// read as one cohesive chip — closer to "close button inside the tab"
// than two adjacent controls, without the hit-testing complexity of
// actually overlaying the glyph inside the title's own bounds.
func (t *TabBar) newChip(tab *Tab) fyne.CanvasObject {
	title := newChipTitle(tab, t)
	closeGlyph := newChipClose(tab, t)

	rect := canvas.NewRectangle(color.Transparent)
	if t.Active == tab {
		rect.FillColor = chipActiveColor
	}

	titleAndClose := container.New(layout.NewCustomPaddedHBoxLayout(0), title, closeGlyph)
	return container.NewStack(rect, titleAndClose)
}

// selectTab is chipTitle's Tapped handler: sets Active, fires OnSelected,
// and redraws so the newly active chip's highlight shows immediately.
func (t *TabBar) selectTab(tab *Tab) {
	t.Active = tab
	if t.OnSelected != nil {
		t.OnSelected(tab)
	}
	t.Refresh()
}

// closeTab is chipClose's Tapped handler. Deliberately does not touch
// Active or Tabs — see OnClosed's doc comment on TabBar.
func (t *TabBar) closeTab(tab *Tab) {
	if t.OnClosed != nil {
		t.OnClosed(tab)
	}
}

// contextMenu is chipTitle's TappedSecondary handler.
func (t *TabBar) contextMenu(tab *Tab, pos fyne.Position) {
	if t.OnContextMenu != nil {
		t.OnContextMenu(tab, pos)
	}
}

// chipTitle is one tab chip's title area: tapping it selects the tab,
// secondary-tapping it requests the context menu. Split out from
// chipClose (rather than one widget handling both gestures based on tap
// position) so each glyph's hit area is exactly its own widget's bounds —
// no manual hit-testing math needed to tell "tapped the title" from
// "tapped the close glyph" apart.
type chipTitle struct {
	widget.BaseWidget
	tab *Tab
	bar *TabBar
}

func newChipTitle(tab *Tab, bar *TabBar) *chipTitle {
	c := &chipTitle{tab: tab, bar: bar}
	c.ExtendBaseWidget(c)
	return c
}

func (c *chipTitle) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel(c.tab.Title)
	return widget.NewSimpleRenderer(label)
}

// Tapped satisfies fyne.Tappable.
func (c *chipTitle) Tapped(*fyne.PointEvent) {
	c.bar.selectTab(c.tab)
}

// TappedSecondary satisfies fyne.SecondaryTappable (right-click on desktop,
// long-press equivalent elsewhere).
func (c *chipTitle) TappedSecondary(ev *fyne.PointEvent) {
	c.bar.contextMenu(c.tab, ev.AbsolutePosition)
}

// chipClose is one tab chip's close ("×") glyph: tapping it requests the
// tab be closed (see TabBar.OnClosed's doc comment on why TabBar itself
// doesn't remove anything).
type chipClose struct {
	widget.BaseWidget
	tab *Tab
	bar *TabBar
}

func newChipClose(tab *Tab, bar *TabBar) *chipClose {
	c := &chipClose{tab: tab, bar: bar}
	c.ExtendBaseWidget(c)
	return c
}

func (c *chipClose) CreateRenderer() fyne.WidgetRenderer {
	label := widget.NewLabel("×")
	return widget.NewSimpleRenderer(label)
}

// Tapped satisfies fyne.Tappable.
func (c *chipClose) Tapped(*fyne.PointEvent) {
	c.bar.closeTab(c.tab)
}
