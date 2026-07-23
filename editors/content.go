package editors

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// NewPlaceholderContent builds the Phase 1 stand-in for a Pane's center
// content area: a read-only, non-interactive display of tab's placeholder
// text. Phase 2 replaces this with a real editable text widget backed by
// a shared Document (line numbers, soft wrap, markdown preview toggle,
// etc. — see the design plan) — this function's signature and the fact
// that it returns a plain fyne.CanvasObject are deliberately minimal so
// that swap doesn't ripple into pane.go's composition code.
//
// Wrapped in a container.NewScroll rather than returned bare: an
// unwrapped widget.Label's MinSize grows to fit its full text (Fyne has
// no "clip and let the parent decide" mode for Label), which propagates
// up through the Pane's Border layout and the surrounding container.Split
// and forces the whole window to grow to accommodate long text instead of
// scrolling — the same class of bug terminal/widget.go's sessionRenderer
// MinSize fix addresses for the terminal grid. Scroll's own MinSize is a
// small fixed value independent of its content's size, so it absorbs the
// overflow instead of forcing growth, and gives scrollbars for free.
func NewPlaceholderContent(tab *Tab) fyne.CanvasObject {
	label := widget.NewLabel(tab.Text)
	label.Wrapping = fyne.TextWrapWord

	bg := canvas.NewRectangle(darkenedContentBackground())
	return container.NewStack(bg, container.NewScroll(label))
}

// darkenedContentBackground returns the current theme's background color,
// darkened by 20%, so the content area reads as visually distinct from the
// rest of the Pane chrome (tab bar, south bar) without going fully black —
// stays theme-aware (light vs. dark) rather than a fixed color, unlike
// tabbar.go's chipActiveColor, since a flat dark color here would look
// wrong against a light theme's background.
func darkenedContentBackground() color.Color {
	th := fyne.CurrentApp().Settings().Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	bg := th.Color(theme.ColorNameBackground, variant)

	r, g, b, a := bg.RGBA()
	darken := func(v uint32) uint8 { return uint8(v * 8 / 10 >> 8) }
	return color.NRGBA{R: darken(r), G: darken(g), B: darken(b), A: uint8(a >> 8)}
}
