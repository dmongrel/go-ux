package editors

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// gutter is the line-number sidebar shown to the left of a content entry:
// a right-aligned, non-wrapping list of "1".."N" (N = the current line
// count), the active line's number at full brightness and every other
// line's number dimmed (IntelliJ-style), plus a right-click menu to
// toggle the entry's soft-wrap setting (design plan: "right-click menu on
// the sidebar: enable/disable soft wrap"). It has no independent copy of
// the text itself — content.go pushes the current text to SetText on
// every change (both local edits and Document listener updates) and the
// current cursor row to SetActiveLine (via entry.OnCursorChanged).
//
// Built from widget.RichText (one TextSegment per line, not a single
// widget.Label) specifically because Label has no way to color individual
// lines differently — RichText's per-segment ColorName is what makes the
// active-line highlight possible at all.
//
// Line numbers only line up exactly with entry's visible lines when
// Wrapping is off (one logical line == one visual line); with word wrap
// on, a long logical line spans multiple visual lines but still gets a
// single gutter number, a known, accepted approximation for now — fully
// syncing wrapped-line numbering would need reading back entry's actual
// rendered line breaks, which isn't exposed by widget.Entry today.
type gutter struct {
	widget.BaseWidget
	rich  *widget.RichText
	entry *widget.Entry

	lineCount  int
	activeLine int // 0-indexed, matches widget.Entry.CursorRow
}

func newGutter(text string, entry *widget.Entry) *gutter {
	g := &gutter{rich: widget.NewRichText(), entry: entry}
	g.ExtendBaseWidget(g)
	g.SetText(text)
	return g
}

func (g *gutter) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.rich)
}

// SetText recomputes the displayed line numbers for text, preserving the
// current active-line highlight.
func (g *gutter) SetText(text string) {
	g.lineCount = strings.Count(text, "\n") + 1
	g.rebuild()
}

// SetActiveLine moves the highlighted (full-brightness) line number to
// row (0-indexed) — called from content.go's entry.OnCursorChanged.
// No-op (skips the rebuild) if row is already the active line.
func (g *gutter) SetActiveLine(row int) {
	if g.activeLine == row {
		return
	}
	g.activeLine = row
	g.rebuild()
}

// rebuild replaces rich's segments with one TextSegment per line,
// 1-indexed, right-aligned, the activeLine one styled at
// theme.ColorNameForeground (full brightness) and every other line at
// theme.ColorNameDisabled (dimmed).
func (g *gutter) rebuild() {
	segs := make([]widget.RichTextSegment, g.lineCount)
	for i := 0; i < g.lineCount; i++ {
		style := widget.RichTextStyleParagraph
		style.Alignment = fyne.TextAlignTrailing
		if i == g.activeLine {
			style.ColorName = theme.ColorNameForeground
		} else {
			style.ColorName = theme.ColorNameDisabled
		}
		segs[i] = &widget.TextSegment{Text: strconv.Itoa(i + 1), Style: style}
	}
	g.rich.Segments = segs
	g.rich.Refresh()
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
