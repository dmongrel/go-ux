package editors

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// SouthBarMode identifies which of SouthBar's states is currently shown.
type SouthBarMode int

const (
	// SouthBarHidden is the default/idle state: no diff in progress, no
	// pending file-change notification. The bar renders as zero-height
	// (or is otherwise not visible) in this mode.
	SouthBarHidden SouthBarMode = iota
	// SouthBarDiffReview shows Accept/Cancel — an mcp_tooling-proposed
	// diff is awaiting the user's decision (Phase 2 wires the real
	// trigger; Phase 1 only builds the widget/state machine).
	SouthBarDiffReview
	// SouthBarFileChanged shows "Load from Disk"/"Keep from Memory" — the
	// open file changed on disk and the user's file-watch setting is
	// "notify" rather than "auto" (Phase 2 wires the real trigger).
	SouthBarFileChanged
)

// SouthBar is the South button bar shared by both of a Pane's dual
// purposes — diff review and file-changed notification — rather than two
// separate widgets, because only one can ever be relevant at a time for a
// given open tab (see the design plan) and reusing one bar keeps the
// Pane layout simpler (one fixed south slot, not a slot that sometimes
// holds one widget and sometimes another).
type SouthBar struct {
	widget.BaseWidget

	mode SouthBarMode

	// onPrimary/onSecondary are called by the mode-appropriate button:
	// Accept/Load-from-Disk is "primary", Cancel/Keep-from-Memory is
	// "secondary". Set via SetMode, not directly — see SetMode's doc
	// comment for why the callbacks are mode-scoped rather than
	// standalone OnAccept/OnCancel/OnLoad/OnKeep fields.
	onPrimary, onSecondary func()
}

// NewSouthBar creates a SouthBar in its default SouthBarHidden state.
func NewSouthBar() *SouthBar {
	s := &SouthBar{}
	s.ExtendBaseWidget(s)
	return s
}

// SetMode switches the bar's visible state and (re)wires its two buttons'
// callbacks in one call, rather than exposing separate OnAccept/OnCancel/
// OnLoadFromDisk/OnKeepFromMemory fields independently settable at any
// time — a caller switching from diff-review to file-changed mode (or
// back to hidden) should never be able to leave a stale callback from the
// PREVIOUS mode still wired to a button that's now showing different
// text/meaning; bundling mode+callbacks into one call makes that
// impossible by construction. primary/secondary may be nil (e.g. when
// mode is SouthBarHidden, both are typically nil since no buttons are
// shown).
func (s *SouthBar) SetMode(mode SouthBarMode, primary, secondary func()) {
	s.mode = mode
	s.onPrimary = primary
	s.onSecondary = secondary
	s.Refresh()
}

// Mode reports the bar's current state.
func (s *SouthBar) Mode() SouthBarMode {
	return s.mode
}

// CreateRenderer builds a renderer whose content is derived fresh from s's
// current mode/callbacks each time it runs (at construction, and again on
// every Refresh triggered by SetMode) — see southBarRenderer.Refresh.
func (s *SouthBar) CreateRenderer() fyne.WidgetRenderer {
	r := &southBarRenderer{bar: s}
	r.rebuild()
	return r
}

// southBarRenderer holds a reference back to its SouthBar so it can rebuild
// its own displayed content (Objects()/layout) on demand — the idiomatic
// Fyne approach for a widget whose visible content depends on mutable state
// set after construction (SetMode), mirroring terminal/widget.go's
// sessionRenderer.
type southBarRenderer struct {
	bar     *SouthBar
	content fyne.CanvasObject
}

// rebuild reconstructs r.content from the bar's current mode/callbacks.
func (r *southBarRenderer) rebuild() {
	switch r.bar.mode {
	case SouthBarDiffReview:
		accept := widget.NewButton("Accept", r.bar.onPrimary)
		cancel := widget.NewButton("Cancel", r.bar.onSecondary)
		r.content = container.NewHBox(layout.NewSpacer(), accept, cancel)
	case SouthBarFileChanged:
		load := widget.NewButton("Load from Disk", r.bar.onPrimary)
		keep := widget.NewButton("Keep from Memory", r.bar.onSecondary)
		r.content = container.NewHBox(layout.NewSpacer(), load, keep)
	default: // SouthBarHidden
		r.content = container.NewWithoutLayout()
	}
}

func (r *southBarRenderer) Layout(size fyne.Size) {
	r.content.Resize(size)
}

func (r *southBarRenderer) MinSize() fyne.Size {
	return r.content.MinSize()
}

// Refresh rebuilds content from the bar's current mode/callbacks — called
// both by Fyne's own refresh machinery and directly by SetMode (via
// SouthBar.Refresh), which is how a mode change after construction gets
// picked up: BaseWidget.Refresh() (invoked from SetMode) calls back into
// this renderer's Refresh, so rebuild() always sees the just-set mode.
func (r *southBarRenderer) Refresh() {
	r.rebuild()
	r.content.Refresh()
}

func (r *southBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.content}
}

func (r *southBarRenderer) Destroy() {}
