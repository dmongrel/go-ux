package terminal

import (
	"github.com/hinshun/vt10x"
)

// vtState wraps a vt10x virtual terminal so the rest of the package never
// touches vt10x's API directly. The wrapper exists (rather than exposing
// vt10x.Terminal raw) for two reasons the raw type doesn't serve well:
//
//  1. Byte input and grid readout must be race-free across the readLoop
//     goroutine (which feeds PTY output in) and the Fyne UI goroutine (which
//     reads a snapshot out to repaint). vt10x locks its state during Write
//     and exposes Lock/Unlock, but leaves correct pairing to the caller;
//     concentrating that discipline here means widget.go/render.go can't get
//     it wrong.
//  2. render.go needs a *consistent* view of the whole grid at one instant,
//     not per-cell reads that could interleave with a parse. snapshot()
//     copies the entire grid under one lock hold, decoding vt10x's private
//     attribute bitfield into plain booleans so render.go depends on this
//     wrapper's stable shape, not on vt10x internals.
type vtState struct {
	term vt10x.Terminal
}

// vt10x stores per-glyph attributes as a private bitfield in Glyph.Mode.
// Those bit values are not exported by the library, so they are mirrored
// here to decode a snapshot. They must stay in lockstep with vt10x's own
// iota order (state.go): a version bump of vt10x is the one thing that could
// silently break this mapping, so it is called out rather than buried.
const (
	attrReverse = 1 << iota
	attrUnderline
	attrBold
	attrGfx
	attrItalic
	attrBlink
	attrWrap
)

// snapCell is one grid cell as render.go consumes it: the rune to draw plus
// its colors and the decoded attributes that affect drawing. Colors are kept
// as raw vt10x.Color values (not pre-converted to RGBA) so this type stays
// independent of the palette choice, which is render.go's concern — and so
// tests can assert on semantic colors (e.g. vt10x.Red) rather than on pixel
// values.
type snapCell struct {
	Rune      rune
	FG, BG    vt10x.Color
	Bold      bool
	Underline bool
	Italic    bool
	Reverse   bool
}

// gridSnapshot is a complete, point-in-time copy of the terminal grid plus
// cursor state. It is a value type owning its own cell storage so the UI
// goroutine can walk it after the lock is released without racing the next
// parse.
type gridSnapshot struct {
	Cols, Rows    int
	Cells         [][]snapCell // [row][col]
	CursorX       int
	CursorY       int
	CursorVisible bool
	Title         string
}

// newVTState creates a virtual terminal of the given character dimensions.
// It parallels the PTY's initial size; keeping the two in sync afterward is
// resize()'s job (see widget.go's resize handling).
func newVTState(cols, rows int) *vtState {
	return &vtState{term: vt10x.New(vt10x.WithSize(cols, rows))}
}

// write feeds raw PTY output bytes to the parser. It is called only from the
// readLoop goroutine, never the UI goroutine; vt10x locks its own state for
// the duration of the parse, so a concurrent snapshot() blocks until the
// parse completes rather than observing a half-applied sequence.
func (v *vtState) write(p []byte) {
	// vt10x.terminal.Write never returns a non-nil error for a byte slice
	// source (it only surfaces reader errors, and a bytes reader has none),
	// so there is nothing actionable to propagate upward here.
	_, _ = v.term.Write(p)
}

// resize changes the virtual grid dimensions. It must be kept consistent with
// both the PTY (ptySession.Resize) and the renderer's cell count; widget.go
// owns that three-way synchronization and calls this as one leg of it.
func (v *vtState) resize(cols, rows int) {
	v.term.Resize(cols, rows)
}

// size reports the current grid dimensions, the authoritative count the
// renderer sizes its image against.
func (v *vtState) size() (cols, rows int) {
	return v.term.Size()
}

// snapshot copies the entire grid and cursor state under a single lock hold.
// Taking one consistent copy (rather than letting render.go read cells
// live) is what lets the repaint run on the UI goroutine without a lock and
// without tearing against an in-flight parse on the readLoop goroutine.
func (v *vtState) snapshot() gridSnapshot {
	v.term.Lock()
	defer v.term.Unlock()

	cols, rows := v.term.Size()
	cells := make([][]snapCell, rows)
	for y := range rows {
		row := make([]snapCell, cols)
		for x := range cols {
			g := v.term.Cell(x, y)
			row[x] = snapCell{
				Rune:      g.Char,
				FG:        g.FG,
				BG:        g.BG,
				Bold:      g.Mode&attrBold != 0,
				Underline: g.Mode&attrUnderline != 0,
				Italic:    g.Mode&attrItalic != 0,
				Reverse:   g.Mode&attrReverse != 0,
			}
		}
		cells[y] = row
	}

	cur := v.term.Cursor()
	return gridSnapshot{
		Cols:          cols,
		Rows:          rows,
		Cells:         cells,
		CursorX:       cur.X,
		CursorY:       cur.Y,
		CursorVisible: v.term.CursorVisible(),
		Title:         v.term.Title(),
	}
}
