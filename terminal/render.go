package terminal

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"github.com/hinshun/vt10x"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// defaultCellWidth and defaultCellHeight are the fallback per-cell pixel box
// used when a real font face can't report metrics — they match
// basicfont.Face7x13's fixed advance/height so the grid stays aligned even on
// the fallback path. render.go treats one grid cell as a fixed rectangle
// (monospace assumption); proportional fonts are explicitly out of scope for
// a terminal grid.
const (
	defaultCellWidth  = 7
	defaultCellHeight = 13
)

// gridRenderer turns a vtState's grid snapshot into pixels. It owns the
// canvas.Raster shown by the widget and a reusable *image.RGBA it repaints
// into on every refresh — reusing one image (rather than allocating per
// frame) matters because refreshes are driven by PTY output that can arrive
// hundreds of times a second (see widget.go's debounce).
//
// Rationale for hand-rasterizing into a single raster, rather than one Fyne
// object per cell or per row, lives in the design doc (reflective-fern.md,
// "Rendering approach") and is deliberately not re-argued here.
type gridRenderer struct {
	state *vtState

	face   xfont.Face
	cellW  int
	cellH  int
	ascent int // baseline offset from the cell's top, in pixels

	mu  sync.Mutex // guards img against concurrent refresh + raster generate
	img *image.RGBA

	raster *canvas.Raster
}

// newGridRenderer builds a renderer over state, loading the monospace font
// face used to draw glyphs. The raster's generator hands back the cached
// image; because the generator is called on the Fyne UI goroutine while
// refresh() may run there too, both take the same mutex.
func newGridRenderer(state *vtState) *gridRenderer {
	r := &gridRenderer{state: state}
	r.face, r.cellW, r.cellH, r.ascent = loadMonospaceFace(float64(defaultCellHeight))

	r.raster = canvas.NewRaster(func(w, h int) image.Image {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.img == nil {
			// Refresh hasn't run yet; hand back a 1x1 transparent image
			// rather than nil so canvas.Raster never panics on first paint.
			return image.NewRGBA(image.Rect(0, 0, 1, 1))
		}
		return r.img
	})
	// Nearest-neighbor keeps the hand-rasterized glyphs crisp when Fyne
	// scales the raster to the widget's pixel size instead of blurring them.
	r.raster.ScaleMode = canvas.ImageScalePixels
	r.refresh()
	return r
}

// refresh repaints the whole grid into the cached image from a fresh
// snapshot. It is safe to call from the UI goroutine (and must be called
// there once the raster is on-screen, per Fyne's threading model); it takes
// no Fyne locks itself beyond guarding its own image buffer.
func (r *gridRenderer) refresh() {
	snap := r.state.snapshot()

	r.mu.Lock()
	defer r.mu.Unlock()

	wantW := max(1, snap.Cols*r.cellW)
	wantH := max(1, snap.Rows*r.cellH)
	if r.img == nil || r.img.Rect.Dx() != wantW || r.img.Rect.Dy() != wantH {
		r.img = image.NewRGBA(image.Rect(0, 0, wantW, wantH))
	}

	drawer := &xfont.Drawer{Dst: r.img, Face: r.face}
	for y := 0; y < snap.Rows; y++ {
		for x := 0; x < snap.Cols; x++ {
			r.drawCell(drawer, snap.Cells[y][x], x, y)
		}
	}
}

// drawCell paints one cell's background then its glyph. Background is a solid
// fill; the glyph is drawn in the foreground color at the cell baseline. The
// reverse attribute is already baked into fg/bg by vt10x at parse time, so no
// swap happens here — doing it again would double-invert.
func (r *gridRenderer) drawCell(drawer *xfont.Drawer, c snapCell, col, row int) {
	x0 := col * r.cellW
	y0 := row * r.cellH
	rect := image.Rect(x0, y0, x0+r.cellW, y0+r.cellH)

	bg := paletteColor(c.BG)
	draw.Draw(r.img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)

	if c.Rune == 0 || c.Rune == ' ' {
		return
	}
	drawer.Src = &image.Uniform{C: paletteColor(c.FG)}
	drawer.Dot = fixed.P(x0, y0+r.ascent)
	drawer.DrawString(string(c.Rune))
}

// canvasObject exposes the raster for the widget renderer to place. Kept as a
// method (not a field read) so gridRenderer's internals stay unexported.
func (r *gridRenderer) canvasObject() *canvas.Raster { return r.raster }

// resize is the renderer's leg of the PTY/vt10x/raster size sync. It resizes
// the underlying vt10x grid, then repaints so the cached image's cell count
// matches the new dimensions immediately. widget.go calls this together with
// ptySession.Resize so all three stay consistent.
func (r *gridRenderer) resize(cols, rows int) {
	r.state.resize(cols, rows)
	r.refresh()
}

// pixelSize reports the natural pixel size of the current grid, used by the
// widget renderer's MinSize/Layout so the on-screen raster maps 1:1 to the
// rasterized cells before any scaling.
func (r *gridRenderer) pixelSize() (w, h int) {
	cols, rows := r.state.size()
	return cols * r.cellW, rows * r.cellH
}

// loadMonospaceFace loads Fyne's bundled monospace font at the given pixel
// size and reports the resulting fixed cell box and baseline. It falls back
// to golang.org/x/image/font/basicfont (a fixed 7x13 bitmap face, always
// available, pure Go, already in the module graph) if the bundled resource
// can't be parsed — so rendering can never fail to produce *some* legible
// grid, which matters because a crash here would take down the whole widget.
//
// Using the bundled resource keeps the "no new font dependency" constraint
// from the design doc; opentype parsing lives in golang.org/x/image, already
// pulled in transitively by Fyne, so nothing new is added to go.mod.
func loadMonospaceFace(sizePx float64) (face xfont.Face, cellW, cellH, ascent int) {
	res := theme.DefaultTextMonospaceFont()
	fnt, err := opentype.Parse(res.Content())
	if err == nil {
		f, ferr := opentype.NewFace(fnt, &opentype.FaceOptions{
			Size:    sizePx,
			DPI:     72, // 1 point == 1 pixel, so Size is effectively in pixels
			Hinting: xfont.HintingFull,
		})
		if ferr == nil {
			m := f.Metrics()
			// Advance of a representative glyph gives the monospace cell
			// width; 'M' is a safe wide-ish choice present in any font.
			adv, ok := f.GlyphAdvance('M')
			if ok && adv > 0 {
				cellW = adv.Ceil()
				cellH = (m.Ascent + m.Descent).Ceil()
				return f, max(cellW, 1), max(cellH, 1), m.Ascent.Ceil()
			}
		}
	}

	// Fallback: fixed bitmap face with known metrics.
	bf := basicfont.Face7x13
	return bf, defaultCellWidth, defaultCellHeight, bf.Ascent
}

// paletteColor maps a vt10x.Color to an RGBA. vt10x encodes colors in three
// overlapping ranges (see color.go): [0,16) ANSI, [16,256) the xterm 256-cube,
// packed 24-bit truecolor above that, and sentinel Default* values at the top.
// The Default sentinels resolve to light-grey-on-black — a readable neutral
// when a program sets no explicit color.
func paletteColor(c vt10x.Color) color.RGBA {
	switch {
	case c == vt10x.DefaultFG:
		return color.RGBA{0xd0, 0xd0, 0xd0, 0xff}
	case c == vt10x.DefaultBG:
		return color.RGBA{0x10, 0x10, 0x10, 0xff}
	case c == vt10x.DefaultCursor:
		return color.RGBA{0xff, 0xff, 0xff, 0xff}
	case c < 16:
		return ansi16[c]
	case c < 256:
		return xterm256(c)
	default:
		// 24-bit truecolor packed as r<<16 | g<<8 | b.
		return color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 0xff}
	}
}

// ansi16 is the standard 16-color ANSI palette (VGA-ish), indexed by
// vt10x.Color values 0..15.
var ansi16 = [16]color.RGBA{
	{0x00, 0x00, 0x00, 0xff}, // 0 black
	{0xcd, 0x00, 0x00, 0xff}, // 1 red
	{0x00, 0xcd, 0x00, 0xff}, // 2 green
	{0xcd, 0xcd, 0x00, 0xff}, // 3 yellow
	{0x00, 0x00, 0xee, 0xff}, // 4 blue
	{0xcd, 0x00, 0xcd, 0xff}, // 5 magenta
	{0x00, 0xcd, 0xcd, 0xff}, // 6 cyan
	{0xe5, 0xe5, 0xe5, 0xff}, // 7 light grey
	{0x7f, 0x7f, 0x7f, 0xff}, // 8 dark grey
	{0xff, 0x00, 0x00, 0xff}, // 9 bright red
	{0x00, 0xff, 0x00, 0xff}, // 10 bright green
	{0xff, 0xff, 0x00, 0xff}, // 11 bright yellow
	{0x5c, 0x5c, 0xff, 0xff}, // 12 bright blue
	{0xff, 0x00, 0xff, 0xff}, // 13 bright magenta
	{0x00, 0xff, 0xff, 0xff}, // 14 bright cyan
	{0xff, 0xff, 0xff, 0xff}, // 15 white
}

// xterm256 resolves the xterm 256-color cube and grayscale ramp (indices
// 16..255) to RGBA, matching the de-facto standard xterm palette.
func xterm256(c vt10x.Color) color.RGBA {
	if c < 232 {
		// 6x6x6 color cube starting at index 16.
		i := int(c) - 16
		r := i / 36
		g := (i % 36) / 6
		b := i % 6
		conv := func(v int) uint8 {
			if v == 0 {
				return 0
			}
			return uint8(55 + v*40)
		}
		return color.RGBA{conv(r), conv(g), conv(b), 0xff}
	}
	// Grayscale ramp, indices 232..255.
	level := uint8(8 + (int(c)-232)*10)
	return color.RGBA{level, level, level, 0xff}
}
