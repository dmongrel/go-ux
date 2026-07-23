package editors

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go-ux/fontsettings"
)

// newDocumentContent builds a Pane's center content area for tab: an
// editable, multi-line text widget bound to tab.Doc, kept in sync with any
// other Pane showing the same Document (split.go's "same underlying
// document, synced live" split semantics), Ctrl+scroll-adjustable in font
// size via fonts (font.go), Ctrl+S wired to onSave. key identifies the
// caller (see Document.RegisterListener/fontsettings.State.RegisterListener)
// — pane.go passes its own *Pane, since only one content widget is live
// per Pane at a time. Callers MUST call the returned cleanup func when
// this content is no longer shown (e.g. the active tab changes), or the
// Document/font state will keep pushing updates into an orphaned widget
// indefinitely.
func newDocumentContent(tab *Tab, key any, fonts *fontsettings.State, onSave func()) (content fyne.CanvasObject, cleanup func()) {
	var scroll *container.Scroll
	entry := newEditorEntry(fonts, func(ev *fyne.ScrollEvent) {
		if scroll != nil {
			scroll.Scrolled(ev)
		}
	})
	entry.onSave = onSave
	entry.Wrapping = fyne.TextWrapWord
	entry.SetText(tab.Doc.Text())

	sidebar := newGutter(tab.Doc.Text(), entry.Entry)
	entry.OnCursorChanged = func() { sidebar.SetActiveLine(entry.CursorRow) }

	// updating guards against Entry.SetText (driven by a Document
	// notification below) re-firing OnChanged back into Document.SetText —
	// Document.SetText's own no-op guard (see document.go) already breaks
	// the loop when the text matches, but this also avoids the redundant
	// SetText call itself, which would otherwise reset the cursor to the
	// start of the text on every keystroke.
	updating := false
	entry.OnChanged = func(text string) {
		sidebar.SetText(text)
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
		sidebar.SetText(text)
	})

	row := container.NewBorder(nil, nil, sidebar, nil, entry)

	bg := canvas.NewRectangle(darkenedContentBackground())
	scroll = container.NewScroll(row)
	stack := container.NewStack(bg, scroll)

	themed, themeCleanup := newContentThemeOverride(stack, fonts, key)
	return themed, func() {
		tab.Doc.UnregisterListener(key)
		themeCleanup()
	}
}

// newEmptyPaneContent builds the placeholder shown in a Pane's center area
// when it has no open tabs (a freshly created primary Pane, or one whose
// last tab was just closed) — previously just blank space. editors itself
// has no file picker of its own (see mcptooling.go's OpenFile doc
// comment — that's deliberately the host app's job), so this is
// intentionally just a neutral, muted message and not an actionable
// button.
func newEmptyPaneContent() fyne.CanvasObject {
	style := widget.RichTextStyleParagraph
	style.Alignment = fyne.TextAlignCenter
	style.ColorName = theme.ColorNamePlaceHolder
	text := widget.NewRichText(&widget.TextSegment{Text: "No file open", Style: style})
	return container.NewCenter(text)
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
