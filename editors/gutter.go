package editors

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// gutter is the line-number sidebar shown to the left of a content entry:
// a right-aligned, non-wrapping list of "1".."N" (N = the current line
// count), plus a right-click menu to toggle the entry's soft-wrap setting
// (design plan: "right-click menu on the sidebar: enable/disable soft
// wrap"). It has no independent state of its own — content.go pushes the
// current text to SetText on every change (both local edits and Document
// listener updates), so the gutter never needs its own copy of the text.
//
// Line numbers only line up exactly with entry's visible lines when
// Wrapping is off (one logical line == one visual line); with word wrap
// on, a long logical line spans multiple visual lines but still gets a
// single gutter number, a known, accepted approximation for now — fully
// syncing wrapped-line numbering would need reading back entry's actual
// rendered line breaks, which isn't exposed by widget.Entry today.
type gutter struct {
	widget.BaseWidget
	label *widget.Label
	entry *widget.Entry
}

func newGutter(text string, entry *widget.Entry) *gutter {
	g := &gutter{label: widget.NewLabel(gutterText(text)), entry: entry}
	g.label.Alignment = fyne.TextAlignTrailing
	g.label.Wrapping = fyne.TextWrapOff
	g.ExtendBaseWidget(g)
	return g
}

func (g *gutter) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.label)
}

// SetText recomputes the displayed line numbers for text.
func (g *gutter) SetText(text string) {
	g.label.SetText(gutterText(text))
}

// TappedSecondary shows the soft-wrap toggle menu, satisfying
// fyne.SecondaryTappable.
func (g *gutter) TappedSecondary(ev *fyne.PointEvent) {
	canvas := fyne.CurrentApp().Driver().CanvasForObject(g)
	if canvas == nil {
		return
	}
	item := fyne.NewMenuItem("Toggle Soft Wrap", func() {
		if g.entry.Wrapping == fyne.TextWrapOff {
			g.entry.Wrapping = fyne.TextWrapWord
		} else {
			g.entry.Wrapping = fyne.TextWrapOff
		}
		g.entry.Refresh()
	})
	menu := fyne.NewMenu("", item)
	widget.ShowPopUpMenuAtPosition(menu, canvas, ev.AbsolutePosition)
}

// gutterText returns "1\n2\n...\nN" for a text with N lines (N-1 newlines).
func gutterText(text string) string {
	n := strings.Count(text, "\n") + 1
	lines := make([]string, n)
	for i := range lines {
		lines[i] = strconv.Itoa(i + 1)
	}
	return strings.Join(lines, "\n")
}
