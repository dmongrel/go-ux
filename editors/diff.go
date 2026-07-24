package editors

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/pmezard/go-difflib/difflib"
)

// pendingDiff is an mcp_tooling-proposed replacement for a Tab's text,
// awaiting the user's Accept/Cancel via the south bar. See
// Group.ProposeDiff and Group.acceptDiff/cancelDiff (mcptooling.go).
type pendingDiff struct {
	newText string
}

// diffLineKind classifies one rendered diff line.
type diffLineKind int

const (
	diffEqual diffLineKind = iota
	diffDelete
	diffInsert
)

// diffLine is one line of a computeDiff result.
type diffLine struct {
	kind diffLineKind
	text string
}

// computeDiff returns a line-by-line diff between oldText and newText,
// unchanged lines included (so renderDiff can show full surrounding
// context, not just the changed hunks — appropriate for reviewing a
// proposed edit to prose, where context matters more than for typical
// source-code diffs).
func computeDiff(oldText, newText string) []diffLine {
	a := difflib.SplitLines(oldText)
	b := difflib.SplitLines(newText)
	matcher := difflib.NewMatcher(a, b)

	var lines []diffLine
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			for _, l := range a[op.I1:op.I2] {
				lines = append(lines, diffLine{diffEqual, l})
			}
		case 'd':
			for _, l := range a[op.I1:op.I2] {
				lines = append(lines, diffLine{diffDelete, l})
			}
		case 'i':
			for _, l := range b[op.J1:op.J2] {
				lines = append(lines, diffLine{diffInsert, l})
			}
		case 'r':
			for _, l := range a[op.I1:op.I2] {
				lines = append(lines, diffLine{diffDelete, l})
			}
			for _, l := range b[op.J1:op.J2] {
				lines = append(lines, diffLine{diffInsert, l})
			}
		}
	}
	return lines
}

var (
	diffDeleteColor = color.NRGBA{R: 244, G: 67, B: 54, A: 60}
	diffInsertColor = color.NRGBA{R: 76, G: 175, B: 80, A: 60}
)

// renderDiff builds a read-only, line-by-line colored view of lines:
// deleted lines prefixed "- " and tinted red, inserted lines prefixed
// "+ " and tinted green, unchanged lines plain (a leading "  " so every
// row's text still lines up under the same left margin) — the content
// area's display while a Pane is in diff-review mode (pane.go's
// showDiffReview).
//
// Rows use canvas.Text directly, not widget.Label — Label carries the
// current theme's widget padding (theme.SizeNamePadding) around its
// text, which left a visible gap of unhighlighted background between the
// colored rect and the text itself; canvas.Text has no such padding, so
// the highlight now hugs the text exactly.
func renderDiff(lines []diffLine) fyne.CanvasObject {
	th := fyne.CurrentApp().Settings().Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	fg := th.Color(theme.ColorNameForeground, variant)

	rows := make([]fyne.CanvasObject, 0, len(lines))
	for _, l := range lines {
		prefix := "  "
		var fill color.Color = color.Transparent
		switch l.kind {
		case diffDelete:
			prefix = "- "
			fill = diffDeleteColor
		case diffInsert:
			prefix = "+ "
			fill = diffInsertColor
		}

		text := canvas.NewText(prefix+strings.TrimRight(l.text, "\n"), fg)
		text.TextStyle = fyne.TextStyle{Monospace: true}

		rect := canvas.NewRectangle(fill)
		rows = append(rows, container.NewStack(rect, text))
	}

	// container.NewVBox's default layout also inserts theme.SizeNamePadding
	// between every row — on top of Label's own padding (fixed above),
	// this left visible gaps of unhighlighted background between adjacent
	// colored rows. zeroPaddingTheme collapses that inter-row gap to 0,
	// same technique font.go's sizeOverrideTheme uses for a different size
	// name, so rows sit flush against each other.
	return container.NewThemeOverride(container.NewVBox(rows...), &zeroPaddingTheme{base: th})
}

// zeroPaddingTheme wraps a base fyne.Theme, overriding only
// theme.SizeNamePadding to 0 — every other size/color/font/icon lookup
// delegates to base unchanged. Used only to flush-stack renderDiff's rows;
// not a general-purpose theme.
type zeroPaddingTheme struct {
	base fyne.Theme
}

func (t *zeroPaddingTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}

func (t *zeroPaddingTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *zeroPaddingTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *zeroPaddingTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNamePadding {
		return 0
	}
	return t.base.Size(name)
}
