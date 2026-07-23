package editors

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

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
// deleted lines tinted red, inserted lines tinted green, unchanged lines
// plain — the content area's display while a Pane is in diff-review mode
// (pane.go's showDiffReview).
func renderDiff(lines []diffLine) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(lines))
	for _, l := range lines {
		label := widget.NewLabel(strings.TrimRight(l.text, "\n"))
		label.TextStyle = fyne.TextStyle{Monospace: true}

		var fill color.Color = color.Transparent
		switch l.kind {
		case diffDelete:
			fill = diffDeleteColor
		case diffInsert:
			fill = diffInsertColor
		}
		rect := canvas.NewRectangle(fill)
		rows = append(rows, container.NewStack(rect, label))
	}
	return container.NewVBox(rows...)
}
