package terminal

import (
	"testing"

	"github.com/hinshun/vt10x"
)

// TestVTStateParsesPlainTextIntoCells is the core grid-state proof the task
// requires: bytes fed to the parser land as the expected runes at the
// expected cell positions. It bypasses ConPTY entirely (feeding bytes
// straight into vtState) because this machine's ConPTY does not reliably
// deliver a live shell's output — see Global Constraints in the plan — and
// because parser correctness has nothing to do with the PTY transport
// anyway.
func TestVTStateParsesPlainTextIntoCells(t *testing.T) {
	v := newVTState(80, 24)
	v.write([]byte("Hello"))

	snap := v.snapshot()
	if snap.Cols != 80 || snap.Rows != 24 {
		t.Fatalf("snapshot size = %dx%d, want 80x24", snap.Cols, snap.Rows)
	}
	for i, want := range []rune{'H', 'e', 'l', 'l', 'o'} {
		if got := snap.Cells[0][i].Rune; got != want {
			t.Errorf("cell (0,%d) = %q, want %q", i, got, want)
		}
	}
	// Untouched cells default to a space, not a zero rune.
	if got := snap.Cells[0][5].Rune; got != ' ' {
		t.Errorf("cell (0,5) = %q, want space", got)
	}
}

// TestVTStateAppliesSGRColorAttribute proves an SGR color escape sequence is
// parsed into per-cell color state at exactly the affected positions, and
// that a reset (SGR 0) restores the default foreground — the "at least one
// SGR color-attribute escape sequence" the task's verification approach
// calls for.
func TestVTStateAppliesSGRColorAttribute(t *testing.T) {
	v := newVTState(80, 24)
	// "Hi" default, "AB" red foreground, "Z" back to default.
	v.write([]byte("Hi\x1b[31mAB\x1b[0mZ"))

	snap := v.snapshot()
	if got := snap.Cells[0][0].FG; got != vt10x.DefaultFG {
		t.Errorf("cell (0,0) FG = %d, want DefaultFG %d", got, vt10x.DefaultFG)
	}
	for _, x := range []int{2, 3} {
		if got := snap.Cells[0][x].FG; got != vt10x.Red {
			t.Errorf("cell (0,%d) FG = %d, want Red %d", x, got, vt10x.Red)
		}
		if got := snap.Cells[0][x].Rune; got != []rune{'A', 'B'}[x-2] {
			t.Errorf("cell (0,%d) rune = %q", x, got)
		}
	}
	if got := snap.Cells[0][4].FG; got != vt10x.DefaultFG {
		t.Errorf("cell (0,4) FG = %d, want DefaultFG after reset", got)
	}
}

// TestVTStateDecodesBoldUnderline proves the private vt10x attribute bitfield
// is decoded into the snapshot's boolean flags (the mapping most at risk from
// a vt10x version bump, per attrBold/attrUnderline's doc comment).
func TestVTStateDecodesBoldUnderline(t *testing.T) {
	v := newVTState(80, 24)
	// SGR 1 (bold) + 4 (underline), one char, then reset.
	v.write([]byte("\x1b[1;4mX\x1b[0mY"))

	snap := v.snapshot()
	x := snap.Cells[0][0]
	if !x.Bold || !x.Underline {
		t.Errorf("cell (0,0) bold=%v underline=%v, want both true", x.Bold, x.Underline)
	}
	y := snap.Cells[0][1]
	if y.Bold || y.Underline {
		t.Errorf("cell (0,1) bold=%v underline=%v, want both false after reset", y.Bold, y.Underline)
	}
}

// TestVTStateCursorAdvances confirms the snapshot carries live cursor
// position — the data render.go's cursor overlay is placed from.
func TestVTStateCursorAdvances(t *testing.T) {
	v := newVTState(80, 24)
	v.write([]byte("abc"))

	snap := v.snapshot()
	if snap.CursorX != 3 || snap.CursorY != 0 {
		t.Errorf("cursor = (%d,%d), want (3,0)", snap.CursorX, snap.CursorY)
	}
	if !snap.CursorVisible {
		t.Error("cursor should be visible by default")
	}
}

// TestVTStateNewlineMovesToNextRow proves multi-row layout: a CRLF drops the
// cursor to the next row and text after it lands there, so the snapshot's
// row indexing matches what a renderer would draw.
func TestVTStateNewlineMovesToNextRow(t *testing.T) {
	v := newVTState(80, 24)
	v.write([]byte("top\r\nbottom"))

	snap := v.snapshot()
	for i, want := range []rune{'t', 'o', 'p'} {
		if got := snap.Cells[0][i].Rune; got != want {
			t.Errorf("row 0 cell %d = %q, want %q", i, got, want)
		}
	}
	for i, want := range []rune{'b', 'o', 't', 't', 'o', 'm'} {
		if got := snap.Cells[1][i].Rune; got != want {
			t.Errorf("row 1 cell %d = %q, want %q", i, got, want)
		}
	}
}

// TestVTStateResizeChangesSnapshotDimensions proves resize() actually
// re-dimensions the grid, which the snapshot then reflects — one leg of the
// PTY/vt10x/raster three-way size sync verified more fully in render_test.go.
func TestVTStateResizeChangesSnapshotDimensions(t *testing.T) {
	v := newVTState(80, 24)
	v.resize(100, 30)

	if cols, rows := v.size(); cols != 100 || rows != 30 {
		t.Fatalf("size() = %dx%d, want 100x30", cols, rows)
	}
	snap := v.snapshot()
	if snap.Cols != 100 || snap.Rows != 30 {
		t.Errorf("snapshot = %dx%d, want 100x30", snap.Cols, snap.Rows)
	}
	if len(snap.Cells) != 30 || len(snap.Cells[0]) != 100 {
		t.Errorf("cell grid = %dx%d, want 100x30", len(snap.Cells[0]), len(snap.Cells))
	}
}
