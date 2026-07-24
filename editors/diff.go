package editors

import (
	"fmt"
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
	// diffCollapsed is a synthetic summary line standing in for a long
	// stretch of unchanged lines beyond what's kept as context — see
	// collapseContext. Never produced by computeDiff itself.
	diffCollapsed
)

// diffLine is one line of a computeDiff result.
type diffLine struct {
	kind diffLineKind
	text string
}

// computeDiff returns a line-by-line diff between oldText and newText,
// unchanged lines included in full (context trimming for rendering is
// renderDiff's job — see collapseContext — not this function's; other
// callers, e.g. tests, want the untrimmed result).
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

// diffContextLines caps how many unchanged lines are kept immediately
// before/after a change; a longer unchanged stretch collapses into one
// diffCollapsed summary line. Matches `git diff`'s own default context
// size (-U3). renderDiff draws one canvas object per rendered line
// (see its own doc comment), so without this cap a diff against a large,
// mostly-unchanged file (a real prose chapter, not a small source diff)
// renders a canvas-object count proportional to the WHOLE file's length
// rather than to how much actually changed — this was measured taking
// minutes to lay out for a ~600-line file, during which the pane looked
// unresponsive. Capping context bounds render size to roughly
// (number of changes) * 2 * diffContextLines, independent of file length.
const diffContextLines = 3

// collapseContext returns lines with every maximal run of diffEqual lines
// longer than its surrounding context trimmed down to at most
// diffContextLines lines of leading and/or trailing context (whichever
// side borders an actual change), replacing the trimmed middle with one
// diffCollapsed line summarizing how many lines were hidden. A run at the
// very start of the file only needs trailing context (nothing precedes
// it to explain); a run at the very end only needs leading context;
// everything else needs both. Runs already at or under the context
// budget are returned unchanged.
func collapseContext(lines []diffLine) []diffLine {
	var out []diffLine
	i := 0
	for i < len(lines) {
		if lines[i].kind != diffEqual {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].kind == diffEqual {
			j++
		}
		out = append(out, collapseEqualRun(lines[i:j], i == 0, j == len(lines))...)
		i = j
	}
	return out
}

// collapseEqualRun collapses one maximal run of diffEqual lines per
// collapseContext's rule, given whether it's the first and/or last run in
// the whole diff.
func collapseEqualRun(run []diffLine, isFirst, isLast bool) []diffLine {
	keepHead, keepTail := diffContextLines, diffContextLines
	if isFirst {
		keepHead = 0
	}
	if isLast {
		keepTail = 0
	}
	if len(run) <= keepHead+keepTail {
		return run
	}

	hidden := len(run) - keepHead - keepTail
	summary := diffLine{diffCollapsed, fmt.Sprintf("… %d unchanged lines …", hidden)}

	out := make([]diffLine, 0, keepHead+1+keepTail)
	out = append(out, run[:keepHead]...)
	out = append(out, summary)
	out = append(out, run[len(run)-keepTail:]...)
	return out
}

var (
	diffDeleteColor = color.NRGBA{R: 244, G: 67, B: 54, A: 60}
	diffInsertColor = color.NRGBA{R: 76, G: 175, B: 80, A: 60}
)

// diffRun is a maximal contiguous stretch of lines sharing the same kind
// — e.g. 3 consecutive inserted lines. renderDiff renders one rectangle
// per run, not one per line (see its own doc comment for why).
type diffRun struct {
	kind  diffLineKind
	lines []string
}

// groupDiffRuns collapses lines into maximal contiguous same-kind runs,
// preserving order.
func groupDiffRuns(lines []diffLine) []diffRun {
	var runs []diffRun
	for _, l := range lines {
		if n := len(runs); n > 0 && runs[n-1].kind == l.kind {
			runs[n-1].lines = append(runs[n-1].lines, l.text)
			continue
		}
		runs = append(runs, diffRun{kind: l.kind, lines: []string{l.text}})
	}
	return runs
}

// renderDiff builds a read-only, line-by-line colored view of lines
// (first passed through collapseContext, so long unchanged stretches
// render as a single dimmed italic summary line rather than every
// individual line): deleted lines prefixed "- " and tinted red, inserted
// lines prefixed "+ " and tinted green, unchanged lines plain (a leading
// "  " so every row's text still lines up under the same left margin) —
// the content area's display while a Pane is in diff-review mode
// (pane.go's showDiffReview).
//
// Rows use canvas.Text directly, not widget.Label — Label carries the
// current theme's widget padding (theme.SizeNamePadding) around its
// text, which left a visible gap of unhighlighted background between the
// colored rect and the text itself; canvas.Text has no such padding, so
// the highlight now hugs the text exactly.
//
// Renders one rectangle per contiguous run of same-kind lines (groupDiffRuns),
// not one rectangle per individual line: two adjacent same-color
// rectangles, positioned edge-to-edge by container.NewVBox's own layout
// math, occasionally left a hairline of the background showing through
// at their shared boundary — a sub-pixel rounding artifact of stacking
// many separately-drawn rectangles, visible as a faint 1px line between
// some (not all) lines of an otherwise-uniform colored block. A single
// rectangle spanning an entire run has no such internal seam, since nothing
// is drawn where the seam previously was.
func renderDiff(lines []diffLine) fyne.CanvasObject {
	lines = collapseContext(lines)

	th := fyne.CurrentApp().Settings().Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	fg := th.Color(theme.ColorNameForeground, variant)
	dim := th.Color(theme.ColorNamePlaceHolder, variant)

	bands := make([]fyne.CanvasObject, 0, len(lines))
	for _, run := range groupDiffRuns(lines) {
		prefix := "  "
		textColor := fg
		var fill color.Color = color.Transparent
		switch run.kind {
		case diffDelete:
			prefix = "- "
			fill = diffDeleteColor
		case diffInsert:
			prefix = "+ "
			fill = diffInsertColor
		case diffCollapsed:
			prefix = ""
			textColor = dim
		}

		texts := make([]fyne.CanvasObject, 0, len(run.lines))
		for _, l := range run.lines {
			text := canvas.NewText(prefix+strings.TrimRight(l, "\n"), textColor)
			// Monospace-only, no Italic: the bundled test theme (and
			// potentially a real theme) has no font resource for the
			// Italic+Monospace combination, which panics at layout time
			// (fyne.io/fyne/v2/internal/painter.loadMeasureFont) rather
			// than falling back gracefully. The dim color alone already
			// distinguishes a collapsed summary line from a real one.
			text.TextStyle = fyne.TextStyle{Monospace: true}
			texts = append(texts, text)
		}

		rect := canvas.NewRectangle(fill)
		bands = append(bands, container.NewStack(rect, container.NewVBox(texts...)))
	}

	// container.NewVBox's default layout also inserts theme.SizeNamePadding
	// between every row/band (nested VBoxes included, since ThemeOverride
	// applies recursively to every descendant widget) — on top of Label's
	// own padding (fixed above), this left visible gaps of unhighlighted
	// background between adjacent lines/bands. zeroPaddingTheme collapses
	// every such gap to 0, same technique font.go's sizeOverrideTheme uses
	// for a different size name, so rows sit flush against each other.
	return container.NewThemeOverride(container.NewVBox(bands...), &zeroPaddingTheme{base: th})
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
