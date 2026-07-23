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

	face xfont.Face
	// cellW/cellH are the natural per-cell size from the loaded face,
	// before lineHeight/columnWidth multipliers — see pixelSize and paint.
	cellW  int
	cellH  int
	ascent int // baseline offset from the cell's top, in pixels

	// lineHeight/columnWidth are the multipliers applied on top of
	// cellW/cellH — see paint's use of them and applyFontSettings.
	lineHeight  float64
	columnWidth float64

	mu  sync.Mutex // guards img against concurrent refresh + raster generate
	img *image.RGBA

	raster *canvas.Raster
}

// newGridRenderer builds a renderer over state, loading the monospace font
// face used to draw glyphs. The raster's generator (paint) does the actual
// per-frame rasterization; because it's called on the Fyne UI goroutine
// while resize()/refresh() may run there too, all three take the same
// mutex.
func newGridRenderer(state *vtState) *gridRenderer {
	r := &gridRenderer{state: state, lineHeight: 1.0, columnWidth: 1.0}
	s := currentFontSettings()
	r.face, r.cellW, r.cellH, r.ascent = loadMonospaceFace(s.Family, float64(s.Size))
	r.lineHeight, r.columnWidth = s.LineHeight, s.ColumnWidth

	r.raster = canvas.NewRaster(r.paint)
	// Nearest-neighbor keeps the hand-rasterized glyphs crisp rather than
	// blurring them — paint (below) now already renders at the exact (w, h)
	// Fyne requests, so this no longer does any real scaling in the common
	// case; it only matters for the rare frame where the generator is asked
	// to redraw before Fyne has caught up to a just-applied resize.
	r.raster.ScaleMode = canvas.ImageScalePixels
	return r
}

// paint is canvas.Raster's Generator callback: Fyne calls it with the
// widget's actual current on-screen pixel size and uses the returned image
// directly. NewRaster's own doc comment says as much — "Images returned
// from this method should draw dynamically to fill the width and height
// parameters passed" — but this used to ignore w/h entirely and always
// rasterize at a fixed "natural" size (cols*cellW x rows*cellH, from font
// metrics alone). Whenever that natural size didn't exactly match what Fyne
// actually wanted — any device scale factor other than 1, or simply a
// layout giving the widget more room than its MinSize — Fyne had to
// nearest-neighbor-scale the mismatched image to fit, which is what
// produced visibly inconsistent per-row pixel heights ("some lines render
// at 2px where they should be 1px"): a non-integer scale ratio rounds
// differently row to row. Rendering directly at the requested (w, h), with
// per-cell boxes sized proportionally by float division rather than the
// fixed integer cellW/cellH, removes that second scaling step entirely —
// any remaining natural-vs-actual mismatch (usually at most a fraction of
// one cell, from gridDims' own floor division) now shows up as a small,
// uniform effect from one consistent calculation, not per-row noise from
// an unrelated general-purpose image-scaling algorithm.
func (r *gridRenderer) paint(w, h int) image.Image {
	snap := r.state.snapshot()

	r.mu.Lock()
	defer r.mu.Unlock()

	w, h = max(w, 1), max(h, 1)
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	cols, rows := max(snap.Cols, 1), max(snap.Rows, 1)
	cellW := float64(w) / float64(cols)
	cellH := float64(h) / float64(rows)

	drawer := &xfont.Drawer{Dst: r.img, Face: r.face}
	for y := 0; y < snap.Rows; y++ {
		for x := 0; x < snap.Cols; x++ {
			r.drawCell(drawer, snap.Cells[y][x], x, y, cellW, cellH)
		}
	}
	return r.img
}

// refresh requests a repaint at whatever size the raster is actually
// showing at — the real rasterization now happens in paint (Fyne's own
// Generator callback, invoked with the current on-screen size), not here.
func (r *gridRenderer) refresh() {
	canvas.Refresh(r.raster)
}

// drawCell paints one cell's background then its glyph, in a box cellW x
// cellH pixels wide/tall (the caller's per-cell size, proportional to the
// raster's actual current pixel dimensions — see paint's doc comment, not
// necessarily equal to the font's own natural cellW/cellH). Background is a
// solid fill; the glyph is drawn in the foreground color at the cell
// baseline. The reverse attribute is already baked into fg/bg by vt10x at
// parse time, so no swap happens here — doing it again would double-invert.
func (r *gridRenderer) drawCell(drawer *xfont.Drawer, c snapCell, col, row int, cellW, cellH float64) {
	x0 := int(float64(col) * cellW)
	y0 := int(float64(row) * cellH)
	x1 := int(float64(col+1) * cellW)
	y1 := int(float64(row+1) * cellH)
	rect := image.Rect(x0, y0, x1, y1)

	bg := paletteColor(c.BG)
	draw.Draw(r.img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)

	if c.Rune == 0 || c.Rune == ' ' {
		return
	}
	drawer.Src = &image.Uniform{C: paletteColor(c.FG)}
	// Scale the font's own natural ascent by how much this cell's actual
	// height differs from the font's natural cellH, so the glyph baseline
	// stays roughly cell-centered even when cellH (derived from the
	// widget's real size) isn't exactly the font's own line height.
	ascent := int(float64(r.ascent) * cellH / float64(r.cellH))
	drawer.Dot = fixed.P(x0, y0+ascent)
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

// applyFontSettings reloads the font face (if Family/Size changed) and
// updates the line-height/column-width multipliers, then repaints. Called
// once at construction (via currentFontSettings(), above) and again
// whenever the shared FontSettings changes.
func (r *gridRenderer) applyFontSettings(s FontSettings) {
	r.mu.Lock()
	r.face, r.cellW, r.cellH, r.ascent = loadMonospaceFace(s.Family, float64(s.Size))
	r.lineHeight, r.columnWidth = s.LineHeight, s.ColumnWidth
	r.mu.Unlock()

	r.refresh()
}

// pixelSize reports the natural pixel size of the current grid — cols/rows
// at the current per-cell size, including the LineHeight/ColumnWidth
// multipliers (applyFontSettings). Exposed for tests that want to verify
// the multiplier math directly; production code deliberately does not
// derive the widget's MinSize from this (see sessionRenderer.MinSize's
// doc comment in widget.go) so a font-size change can't force the window
// frame itself to resize.
func (r *gridRenderer) pixelSize() (w, h int) {
	r.mu.Lock()
	cellW := float64(r.cellW) * r.columnWidth
	cellH := float64(r.cellH) * r.lineHeight
	r.mu.Unlock()

	cols, rows := r.state.size()
	return int(float64(cols) * cellW), int(float64(rows) * cellH)
}

// loadMonospaceFace loads the named font family at the given pixel size and
// reports the resulting fixed cell box and baseline. family == "" loads
// Fyne's bundled monospace font (this package's original, still-default
// behavior); a non-empty family is looked up the same way
// DetectMonospaceFonts (font_windows.go) found it, by scanning the Windows
// font registry for a matching display name. Any failure at any step —
// unknown family, a file that no longer exists, a parse error — falls back
// to the bundled font, then to golang.org/x/image/font/basicfont if even
// that fails to parse, so rendering can never fail to produce *some*
// legible grid.
func loadMonospaceFace(family string, sizePx float64) (face xfont.Face, cellW, cellH, ascent int) {
	if family != "" {
		if data, ok := loadSystemFontFile(family); ok {
			if fnt, err := opentype.Parse(data); err == nil {
				if f, cw, ch, asc, ok := faceMetrics(fnt, sizePx); ok {
					return f, cw, ch, asc
				}
			}
		}
	}

	res := theme.DefaultTextMonospaceFont()
	fnt, err := opentype.Parse(res.Content())
	if err == nil {
		if f, cw, ch, asc, ok := faceMetrics(fnt, sizePx); ok {
			return f, cw, ch, asc
		}
	}

	// Fallback: fixed bitmap face with known metrics.
	bf := basicfont.Face7x13
	return bf, defaultCellWidth, defaultCellHeight, bf.Ascent
}

// faceMetrics builds a face from fnt at sizePx and reports its cell box —
// factored out of loadMonospaceFace so both the system-font and
// bundled-font paths share the same "measure via GlyphAdvance('M')" logic.
// ok is false if the face can't be built or can't report a usable advance.
func faceMetrics(fnt *opentype.Font, sizePx float64) (face xfont.Face, cellW, cellH, ascent int, ok bool) {
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     72, // 1 point == 1 pixel, so Size is effectively in pixels
		Hinting: xfont.HintingFull,
	})
	if err != nil {
		return nil, 0, 0, 0, false
	}
	m := f.Metrics()
	adv, advOK := f.GlyphAdvance('M')
	if !advOK || adv <= 0 {
		return nil, 0, 0, 0, false
	}
	cellW = adv.Ceil()
	cellH = (m.Ascent + m.Descent).Ceil()
	return f, max(cellW, 1), max(cellH, 1), m.Ascent.Ceil(), true
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
