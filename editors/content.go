package editors

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// NewPlaceholderContent builds the Phase 1 stand-in for a Pane's center
// content area: a read-only, non-interactive display of tab's placeholder
// text. Phase 2 replaces this with a real editable text widget backed by
// a shared Document (line numbers, soft wrap, markdown preview toggle,
// etc. — see the design plan) — this function's signature and the fact
// that it returns a plain fyne.CanvasObject are deliberately minimal so
// that swap doesn't ripple into pane.go's composition code.
func NewPlaceholderContent(tab *Tab) fyne.CanvasObject {
	label := widget.NewLabel(tab.Text)
	label.Wrapping = fyne.TextWrapWord
	return label
}
