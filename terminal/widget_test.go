package terminal

import "testing"

// TestGridDims covers the pure pixel-to-cell math that drives resize, with no
// PTY involved — the "resize-sync logic is testable as pure state-tracking
// logic" the task's TDD guidance asks for. The clamp-to-1 cases matter
// because Fyne can issue a zero-size layout pass before the widget has real
// bounds, and a 0-column grid would break every downstream size computation.
func TestGridDims(t *testing.T) {
	cases := []struct {
		name               string
		w, h, cellW, cellH int
		wantCols, wantRows int
	}{
		{"exact fit", 560, 312, 7, 13, 80, 24},
		{"rounds down", 565, 320, 7, 13, 80, 24},
		{"tiny area clamps to 1x1", 3, 3, 7, 13, 1, 1},
		{"zero size clamps to 1x1", 0, 0, 7, 13, 1, 1},
		{"zero cell size clamps to 1x1", 560, 312, 0, 0, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cols, rows := gridDims(c.w, c.h, c.cellW, c.cellH)
			if cols != c.wantCols || rows != c.wantRows {
				t.Errorf("gridDims(%d,%d,%d,%d) = %dx%d, want %dx%d",
					c.w, c.h, c.cellW, c.cellH, cols, rows, c.wantCols, c.wantRows)
			}
		})
	}
}
