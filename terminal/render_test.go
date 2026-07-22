package terminal

import (
	"image"
	"testing"
)

// TestGridRendererProducesRasterFromNonEmptyGrid is the task's "render.go
// compiles and produces a canvas.Raster (no crash) from a non-empty grid"
// bar: build a renderer over a grid that has real content and confirm it
// yields a non-nil raster whose generated image is correctly sized and
// non-empty. This deliberately does not assert on pixel colors — visual
// correctness needs a human at a GUI (see Global Constraints).
func TestGridRendererProducesRasterFromNonEmptyGrid(t *testing.T) {
	v := newVTState(80, 24)
	v.write([]byte("hello \x1b[31mworld\x1b[0m"))

	r := newGridRenderer(v)
	raster := r.canvasObject()
	if raster == nil {
		t.Fatal("canvasObject() returned nil raster")
	}
	if raster.Generator == nil {
		t.Fatal("raster has no generator")
	}

	// The generator must hand back a real image without panicking.
	img := raster.Generator(160, 48)
	if img == nil {
		t.Fatal("generator returned nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("generated image has empty bounds: %v", b)
	}
}

// TestGridRendererImageMatchesGridPixelSize proves the cached image is sized
// exactly cols*cellW by rows*cellH — the invariant that keeps drawn cells
// aligned to the grid.
func TestGridRendererImageMatchesGridPixelSize(t *testing.T) {
	v := newVTState(20, 10)
	r := newGridRenderer(v)

	wantW, wantH := r.pixelSize()
	img := r.raster.Generator(wantW, wantH)
	got, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("generated image is %T, want *image.RGBA", img)
	}
	if got.Rect.Dx() != wantW || got.Rect.Dy() != wantH {
		t.Errorf("image size = %dx%d, want %dx%d (pixelSize)", got.Rect.Dx(), got.Rect.Dy(), wantW, wantH)
	}
}

// TestGridRendererResizeKeepsSizesConsistent is the pure-state proof of the
// three-way size synchronization the design calls out as a classic
// terminal-corruption source. It exercises the renderer + vt10x legs (the
// PTY leg is a real syscall verified separately in session_windows_test.go's
// TestPtySessionResize); the point here is that after resize(), the vt10x
// grid size, the renderer's reported pixel size, and the actual generated
// image all agree — no leg drifting from the others.
func TestGridRendererResizeKeepsSizesConsistent(t *testing.T) {
	v := newVTState(80, 24)
	r := newGridRenderer(v)

	r.resize(100, 30)

	// vt10x leg.
	if cols, rows := v.size(); cols != 100 || rows != 30 {
		t.Errorf("vt10x size = %dx%d, want 100x30", cols, rows)
	}
	// renderer's reported pixel size leg.
	wantW := 100 * r.cellW
	wantH := 30 * r.cellH
	if w, h := r.pixelSize(); w != wantW || h != wantH {
		t.Errorf("pixelSize = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
	// actual generated image leg.
	img := r.raster.Generator(wantW, wantH)
	if b := img.Bounds(); b.Dx() != wantW || b.Dy() != wantH {
		t.Errorf("generated image = %dx%d, want %dx%d", b.Dx(), b.Dy(), wantW, wantH)
	}
}

// TestGridRendererCellSizePositive guards the metric-loading path: whichever
// font face loads (bundled TTF or the basicfont fallback), the cell box must
// be positive or every downstream size computation collapses to zero.
func TestGridRendererCellSizePositive(t *testing.T) {
	r := newGridRenderer(newVTState(80, 24))
	if r.cellW <= 0 || r.cellH <= 0 {
		t.Errorf("cell size = %dx%d, want both > 0", r.cellW, r.cellH)
	}
	if r.ascent <= 0 || r.ascent > r.cellH {
		t.Errorf("ascent = %d, want in (0, cellH=%d]", r.ascent, r.cellH)
	}
}
