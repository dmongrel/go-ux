package editors

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	fynetest "fyne.io/fyne/v2/test"
)

func TestComputeDiffIdenticalTextIsAllEqual(t *testing.T) {
	lines := computeDiff("a\nb\nc", "a\nb\nc")
	for i, l := range lines {
		if l.kind != diffEqual {
			t.Errorf("line %d kind = %v, want diffEqual", i, l.kind)
		}
	}
}

func TestComputeDiffDetectsInsertedLine(t *testing.T) {
	lines := computeDiff("a\nc", "a\nb\nc")

	var inserted []string
	for _, l := range lines {
		if l.kind == diffInsert {
			inserted = append(inserted, l.text)
		}
	}
	if len(inserted) != 1 || inserted[0] != "b\n" {
		t.Errorf("inserted lines = %v, want [\"b\\n\"]", inserted)
	}
}

func TestComputeDiffDetectsDeletedLine(t *testing.T) {
	lines := computeDiff("a\nb\nc", "a\nc")

	var deleted []string
	for _, l := range lines {
		if l.kind == diffDelete {
			deleted = append(deleted, l.text)
		}
	}
	if len(deleted) != 1 || deleted[0] != "b\n" {
		t.Errorf("deleted lines = %v, want [\"b\\n\"]", deleted)
	}
}

func TestComputeDiffReplaceProducesDeleteThenInsert(t *testing.T) {
	lines := computeDiff("old line", "new line")

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (one delete, one insert)", len(lines))
	}
	// difflib.SplitLines always appends a trailing "\n" to the last line,
	// even when the input has none (see its own source) — computeDiff's
	// consumers (renderDiff) trim it back off for display, but computeDiff
	// itself returns the raw split lines.
	if lines[0].kind != diffDelete || lines[0].text != "old line\n" {
		t.Errorf("lines[0] = %+v, want delete %q", lines[0], "old line\n")
	}
	if lines[1].kind != diffInsert || lines[1].text != "new line\n" {
		t.Errorf("lines[1] = %+v, want insert %q", lines[1], "new line\n")
	}
}

func TestRenderDiffColorsDeleteAndInsertRows(t *testing.T) {
	fynetest.NewApp()

	lines := []diffLine{
		{diffEqual, "same\n"},
		{diffDelete, "removed\n"},
		{diffInsert, "added\n"},
	}
	obj := renderDiff(lines)
	override, ok := obj.(*container.ThemeOverride)
	if !ok {
		t.Fatalf("renderDiff returned %T, want *container.ThemeOverride", obj)
	}
	vbox, ok := override.Content.(*fyne.Container)
	if !ok {
		t.Fatalf("override.Content = %T, want *fyne.Container", override.Content)
	}
	if len(vbox.Objects) != 3 {
		t.Fatalf("got %d rows, want 3", len(vbox.Objects))
	}

	rowColor := func(i int) interface{} {
		row := vbox.Objects[i].(*fyne.Container)
		rect := row.Objects[0].(*canvas.Rectangle)
		return rect.FillColor
	}
	rowText := func(i int) string {
		row := vbox.Objects[i].(*fyne.Container)
		lineBox := row.Objects[1].(*fyne.Container)
		return lineBox.Objects[0].(*canvas.Text).Text
	}

	if rowColor(1) != diffDeleteColor {
		t.Errorf("delete row color = %v, want %v", rowColor(1), diffDeleteColor)
	}
	if rowColor(2) != diffInsertColor {
		t.Errorf("insert row color = %v, want %v", rowColor(2), diffInsertColor)
	}
	if rowText(0) != "  same" || rowText(1) != "- removed" || rowText(2) != "+ added" {
		t.Errorf("row texts = %q, %q, %q, want %q, %q, %q", rowText(0), rowText(1), rowText(2), "  same", "- removed", "+ added")
	}
}

// TestGroupDiffRunsMergesConsecutiveSameKindLines is a regression test for
// a rendering seam: renderDiff used to draw one rectangle per line, and
// two adjacent same-color rectangles occasionally left a hairline of
// background showing through at their shared boundary (a rounding
// artifact of stacking many separately-drawn rectangles). Grouping
// consecutive same-kind lines into one run means renderDiff draws a
// single rectangle across the whole run, with no internal seam.
func TestGroupDiffRunsMergesConsecutiveSameKindLines(t *testing.T) {
	fynetest.NewApp()

	lines := []diffLine{
		{diffEqual, "a\n"},
		{diffDelete, "b\n"},
		{diffDelete, "c\n"},
		{diffDelete, "d\n"},
		{diffInsert, "e\n"},
		{diffInsert, "f\n"},
		{diffEqual, "g\n"},
	}
	runs := groupDiffRuns(lines)
	if len(runs) != 4 {
		t.Fatalf("got %d runs, want 4 (equal, delete x3, insert x2, equal): %+v", len(runs), runs)
	}
	wantKinds := []diffLineKind{diffEqual, diffDelete, diffInsert, diffEqual}
	wantLens := []int{1, 3, 2, 1}
	for i, run := range runs {
		if run.kind != wantKinds[i] {
			t.Errorf("runs[%d].kind = %v, want %v", i, run.kind, wantKinds[i])
		}
		if len(run.lines) != wantLens[i] {
			t.Errorf("runs[%d] has %d lines, want %d", i, len(run.lines), wantLens[i])
		}
	}

	obj := renderDiff(lines)
	override := obj.(*container.ThemeOverride)
	vbox := override.Content.(*fyne.Container)
	if len(vbox.Objects) != 4 {
		t.Fatalf("renderDiff produced %d top-level bands, want 4 (one per run, not one per line)", len(vbox.Objects))
	}
	deleteBand := vbox.Objects[1].(*fyne.Container)
	deleteLines := deleteBand.Objects[1].(*fyne.Container)
	if len(deleteLines.Objects) != 3 {
		t.Errorf("delete band has %d text lines, want 3 (b, c, d all under one rectangle)", len(deleteLines.Objects))
	}
}

// buildEqualRun returns n synthetic diffEqual lines, numbered so failures
// are easy to read ("line 0", "line 1", ...).
func buildEqualRun(n int) []diffLine {
	lines := make([]diffLine, n)
	for i := range lines {
		lines[i] = diffLine{diffEqual, fmt.Sprintf("line %d\n", i)}
	}
	return lines
}

// TestCollapseContextLeavesShortRunsAlone confirms a run at or under the
// context budget on both sides is untouched — the common case for a
// small diff, and the case every pre-existing renderDiff test already
// exercises without going through collapseContext explicitly.
func TestCollapseContextLeavesShortRunsAlone(t *testing.T) {
	lines := append(append(buildEqualRun(diffContextLines),
		diffLine{diffDelete, "changed\n"}),
		buildEqualRun(diffContextLines)...)
	got := collapseContext(lines)
	if len(got) != len(lines) {
		t.Fatalf("collapseContext trimmed a run within budget: got %d lines, want unchanged %d", len(got), len(lines))
	}
	for _, l := range got {
		if l.kind == diffCollapsed {
			t.Errorf("unexpected diffCollapsed line in a run within budget: %+v", got)
		}
	}
}

// TestCollapseContextCollapsesLongMiddleRun is a regression test for a
// real reported freeze: proposing a diff against a large, mostly
// unchanged file (hundreds of lines) rendered one canvas object per
// unchanged line, taking minutes to lay out and leaving the pane looking
// stuck. A long diffEqual run strictly between two changes must collapse
// to diffContextLines of leading context, one diffCollapsed summary line,
// and diffContextLines of trailing context — regardless of how long the
// original run was.
func TestCollapseContextCollapsesLongMiddleRun(t *testing.T) {
	lines := []diffLine{{diffDelete, "before\n"}}
	lines = append(lines, buildEqualRun(50)...)
	lines = append(lines, diffLine{diffInsert, "after\n"})

	got := collapseContext(lines)

	// delete + 3 leading + 1 summary + 3 trailing + insert = 9
	if len(got) != 9 {
		t.Fatalf("got %d lines, want 9: %+v", len(got), got)
	}
	if got[0].kind != diffDelete {
		t.Errorf("got[0].kind = %v, want diffDelete", got[0].kind)
	}
	for i := 1; i <= diffContextLines; i++ {
		if got[i].kind != diffEqual || got[i].text != fmt.Sprintf("line %d\n", i-1) {
			t.Errorf("got[%d] = %+v, want leading context line %d", i, got[i], i-1)
		}
	}
	summary := got[1+diffContextLines]
	if summary.kind != diffCollapsed {
		t.Fatalf("got[%d].kind = %v, want diffCollapsed: %+v", 1+diffContextLines, summary.kind, summary)
	}
	wantHidden := 50 - 2*diffContextLines
	if summary.text != fmt.Sprintf("… %d unchanged lines …", wantHidden) {
		t.Errorf("summary text = %q, want to mention %d hidden lines", summary.text, wantHidden)
	}
	for i := 0; i < diffContextLines; i++ {
		idx := 2 + diffContextLines + i
		wantLineNum := 50 - diffContextLines + i
		if got[idx].kind != diffEqual || got[idx].text != fmt.Sprintf("line %d\n", wantLineNum) {
			t.Errorf("got[%d] = %+v, want trailing context line %d", idx, got[idx], wantLineNum)
		}
	}
	if got[len(got)-1].kind != diffInsert {
		t.Errorf("got[last].kind = %v, want diffInsert", got[len(got)-1].kind)
	}
}

// TestCollapseContextLeadingRunOnlyKeepsTrailingContext confirms a run at
// the very start of the file (nothing precedes it) drops its leading
// portion entirely rather than keeping context on both sides — there's
// nothing "before" the start of the file to provide context for.
func TestCollapseContextLeadingRunOnlyKeepsTrailingContext(t *testing.T) {
	lines := append(buildEqualRun(50), diffLine{diffInsert, "change\n"})
	got := collapseContext(lines)

	// 1 summary + 3 trailing context + 1 insert = 5
	if len(got) != 1+diffContextLines+1 {
		t.Fatalf("got %d lines, want %d: %+v", len(got), 1+diffContextLines+1, got)
	}
	if got[0].kind != diffCollapsed {
		t.Fatalf("got[0].kind = %v, want diffCollapsed (no leading context kept at file start)", got[0].kind)
	}
}

// TestCollapseContextTrailingRunOnlyKeepsLeadingContext is the mirror of
// the leading-run case: a run at the very end of the file keeps only
// leading context, dropping everything after it.
func TestCollapseContextTrailingRunOnlyKeepsLeadingContext(t *testing.T) {
	lines := append([]diffLine{{diffDelete, "change\n"}}, buildEqualRun(50)...)
	got := collapseContext(lines)

	// 1 delete + 3 leading context + 1 summary = 5
	if len(got) != 1+diffContextLines+1 {
		t.Fatalf("got %d lines, want %d: %+v", len(got), 1+diffContextLines+1, got)
	}
	if got[len(got)-1].kind != diffCollapsed {
		t.Fatalf("got[last].kind = %v, want diffCollapsed (no trailing context kept at file end)", got[len(got)-1].kind)
	}
}

// TestRenderDiffCollapsedLineIsDimWithNoRectangle confirms the collapsed
// summary line renders distinctly from a real diff line: no colored
// background (it's not a change), dimmed text, no +/-/space prefix.
func TestRenderDiffCollapsedLineIsDimWithNoRectangle(t *testing.T) {
	fynetest.NewApp()

	lines := []diffLine{{diffDelete, "before\n"}}
	lines = append(lines, buildEqualRun(50)...)
	lines = append(lines, diffLine{diffInsert, "after\n"})

	obj := renderDiff(lines)
	override := obj.(*container.ThemeOverride)
	vbox := override.Content.(*fyne.Container)

	var summaryText *canvas.Text
	for _, bandObj := range vbox.Objects {
		band := bandObj.(*fyne.Container)
		rect := band.Objects[0].(*canvas.Rectangle)
		lineBox := band.Objects[1].(*fyne.Container)
		for _, lo := range lineBox.Objects {
			text := lo.(*canvas.Text)
			if strings.Contains(text.Text, "unchanged lines") {
				summaryText = text
				if rect.FillColor != color.Transparent {
					t.Errorf("collapsed-line band has a non-transparent fill: %v", rect.FillColor)
				}
			}
		}
	}
	if summaryText == nil {
		t.Fatalf("no collapsed summary line found in rendered diff")
	}
	if strings.HasPrefix(summaryText.Text, "- ") || strings.HasPrefix(summaryText.Text, "+ ") || strings.HasPrefix(summaryText.Text, "  ") {
		t.Errorf("collapsed summary text = %q, want no diff-line prefix", summaryText.Text)
	}
}
