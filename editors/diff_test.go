package editors

import (
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
