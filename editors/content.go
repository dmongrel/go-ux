package editors

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// newDocumentContent builds a Pane's center content area for tab: an
// editable, multi-line text widget bound to tab.Doc, kept in sync with any
// other Pane showing the same Document (split.go's "same underlying
// document, synced live" split semantics). key identifies the caller (see
// Document.RegisterListener) — pane.go passes its own *Pane, since only
// one content widget is live per Pane at a time. Callers MUST call the
// returned cleanup func when this content is no longer shown (e.g. the
// active tab changes), or the Document will keep pushing updates into an
// orphaned widget indefinitely.
func newDocumentContent(tab *Tab, key any) (content fyne.CanvasObject, cleanup func()) {
	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapWord
	entry.SetText(tab.Doc.Text())

	// updating guards against Entry.SetText (driven by a Document
	// notification below) re-firing OnChanged back into Document.SetText —
	// Document.SetText's own no-op guard (see document.go) already breaks
	// the loop when the text matches, but this also avoids the redundant
	// SetText call itself, which would otherwise reset the cursor to the
	// start of the text on every keystroke.
	updating := false
	entry.OnChanged = func(text string) {
		if updating {
			return
		}
		tab.Doc.SetText(text)
	}

	tab.Doc.RegisterListener(key, func(text string) {
		if entry.Text == text {
			return
		}
		updating = true
		entry.SetText(text)
		updating = false
	})

	bg := canvas.NewRectangle(darkenedContentBackground())
	stack := container.NewStack(bg, container.NewScroll(entry))
	return stack, func() { tab.Doc.UnregisterListener(key) }
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
