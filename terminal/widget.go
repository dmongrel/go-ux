package terminal

import (
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Default grid dimensions a session starts at before the widget is laid out
// and Resize recomputes them from the actual on-screen size. 80x24 is the
// historical terminal default and what most shells assume until told
// otherwise.
const (
	defaultCols = 80
	defaultRows = 24

	// refreshInterval caps repaint frequency. PTY output from a chatty
	// process (a build log, `yes`, a progress bar) can arrive thousands of
	// times a second; repainting once per read would flood the Fyne UI
	// goroutine with fyne.Do calls. Instead readLoop only *marks* the grid
	// dirty and this interval bounds how often that mark turns into an actual
	// repaint — ~50Hz, comfortably smooth without melting a core. This
	// debounce is a first-class design concern, not an afterthought (see the
	// plan's threading model, item 3).
	refreshInterval = 20 * time.Millisecond

	// cursorBlinkInterval is the on/off half-period of the block cursor.
	cursorBlinkInterval = 530 * time.Millisecond
)

// Session is the embeddable terminal widget: one shell process attached to a
// pseudo-console, its output parsed into a VT grid and drawn via a
// canvas.Raster, with a blinking cursor overlay. It embeds widget.BaseWidget
// and satisfies fyne.CanvasObject, so a host app can drop it straight into
// any Fyne container. It also implements fyne.Focusable, fyne.Tappable, and
// fyne.Shortcutable (see keymap.go for the byte-translation tables and the
// TypedRune/TypedKey/TypedShortcut methods below) so it can receive and
// forward keyboard input to the shell.
//
// Threading is the crux of this type and is called out at each goroutine's
// definition below: PTY output arrives on a background goroutine and must
// cross onto the Fyne UI goroutine (via fyne.Do) before touching any
// CanvasObject. Keyboard input (TypedRune/TypedKey/TypedShortcut/
// FocusGained/FocusLost/Tapped) is a different regime entirely: Fyne's input
// dispatch already calls these on the UI goroutine, so they need NO fyne.Do
// — see the doc comment on TypedRune below for the full reasoning. Don't
// assume every PTY-adjacent method needs fyne.Do just because readLoop does;
// the direction of data flow (input to the PTY vs. output from it) is what
// decides that, not proximity to the PTY.
type Session struct {
	widget.BaseWidget

	def    ShellDef
	pty    ptySession
	vt     *vtState
	render *gridRenderer
	cursor *canvas.Rectangle

	// cellW/cellH are cached from the renderer so Resize can convert the
	// widget's pixel size into a grid cell count without reaching through
	// render every time.
	cellW, cellH int

	// cols/rows is the current grid size, only mutated on the UI goroutine
	// (in Resize), so it needs no lock.
	cols, rows int

	// refreshReq is the debounce channel: readLoop does a non-blocking send
	// to mark the grid dirty; the refresh loop drains it at refreshInterval.
	// Buffered depth 1 is all that's needed — many reads collapse into one
	// pending mark.
	refreshReq chan struct{}
	done       chan struct{}
	closeOnce  sync.Once

	mu       sync.Mutex // guards onExit, blinkOn, and focused
	onExit   func()
	exitDone bool
	blinkOn  bool

	// focused tracks Focusable's gained/lost lifecycle. Nothing in this task
	// visually depends on it yet (that's the Phase 8 cursor-contrast work,
	// out of scope here), but it's tracked now so a later task doesn't need
	// to thread focus state through from scratch, and so tests can assert on
	// it independent of any visual behavior.
	focused bool
}

// NewSession spawns def's shell attached to a fresh pseudo-console and returns
// a ready-to-embed widget. It starts the background goroutines (PTY read
// loop, debounced repaint, cursor blink, process-exit watch) immediately, so
// output begins rendering as soon as the widget is shown. The returned error
// is non-nil only if the shell process could not be spawned.
func NewSession(def ShellDef) (*Session, error) {
	pty, err := newPtySession(def, defaultCols, defaultRows)
	if err != nil {
		return nil, err
	}

	vt := newVTState(defaultCols, defaultRows)
	render := newGridRenderer(vt)

	s := &Session{
		def:        def,
		pty:        pty,
		vt:         vt,
		render:     render,
		cellW:      render.cellW,
		cellH:      render.cellH,
		cols:       defaultCols,
		rows:       defaultRows,
		refreshReq: make(chan struct{}, 1),
		done:       make(chan struct{}),
		blinkOn:    true,
	}
	s.cursor = canvas.NewRectangle(color.RGBA{0xd0, 0xd0, 0xd0, 0xff})
	s.ExtendBaseWidget(s)

	go s.readLoop()
	go s.refreshLoop()
	go s.blinkLoop()
	go s.waitLoop()

	return s, nil
}

// readLoop runs on a BACKGROUND goroutine (never the Fyne UI goroutine). It
// blocking-reads PTY output and feeds it to the VT parser, which is a plain
// data structure, not a CanvasObject — so no fyne.Do is needed here. It only
// *marks* the grid dirty (a non-blocking send); the actual CanvasObject touch
// happens on the UI goroutine in refreshLoop.
func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.vt.write(buf[:n])
			s.markDirty()
		}
		if err != nil {
			return // pipe closed (session ending) or a read error; stop.
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

// markDirty records that the grid changed, coalescing storms of reads into a
// single pending repaint. The non-blocking send means a full channel (a
// repaint already pending) simply drops the extra mark — exactly the
// debounce behavior wanted.
func (s *Session) markDirty() {
	select {
	case s.refreshReq <- struct{}{}:
	default:
	}
}

// refreshLoop runs on a BACKGROUND goroutine but performs all its
// CanvasObject work inside fyne.Do, hopping onto the UI goroutine. It ticks at
// refreshInterval and repaints only when a dirty mark is pending, capping
// redraw frequency regardless of how fast PTY output arrives.
func (s *Session) refreshLoop() {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			select {
			case <-s.refreshReq:
				// Cross onto the UI goroutine before touching the widget.
				fyne.Do(s.Refresh)
			default:
			}
		}
	}
}

// blinkLoop runs on a BACKGROUND goroutine and toggles the cursor visibility,
// again hopping onto the UI goroutine via fyne.Do to touch the cursor
// rectangle. The blink is intentionally independent of PTY output so the
// cursor keeps blinking on an idle prompt.
func (s *Session) blinkLoop() {
	ticker := time.NewTicker(cursorBlinkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.blinkOn = !s.blinkOn
			s.mu.Unlock()
			fyne.Do(s.refreshCursor)
		}
	}
}

// waitLoop runs on a BACKGROUND goroutine, blocking until the shell process
// exits, then delivers the exit notification. Per OnExit's contract the
// callback is invoked on the UI goroutine (fyne.Do), since a host app's exit
// handler will typically touch UI (close a tab, show a message).
func (s *Session) waitLoop() {
	_ = s.pty.Wait()

	s.mu.Lock()
	fn := s.onExit
	s.exitDone = true
	s.mu.Unlock()

	if fn != nil {
		fyne.Do(fn)
	}
}

// OnExit registers a callback fired when the shell process exits. fn is
// invoked via fyne.Do (on the UI goroutine), so it may safely touch widgets.
// If the process has already exited by the time OnExit is called, fn is
// invoked immediately (still via fyne.Do) so no exit is missed.
func (s *Session) OnExit(fn func()) {
	s.mu.Lock()
	already := s.exitDone
	s.onExit = fn
	s.mu.Unlock()

	if already && fn != nil {
		fyne.Do(fn)
	}
}

// Title reports the session's display title: the OSC-set window title if the
// running program has set one, otherwise the shell's configured name. A tab
// UI (Task 3) uses this as the tab label.
func (s *Session) Title() string {
	if t := s.vt.snapshot().Title; t != "" {
		return t
	}
	return s.def.Name
}

// Close terminates the session: stops the background goroutines and kills the
// shell process. Safe to call more than once. It does not itself run on any
// particular goroutine.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		err = s.pty.Close()
	})
	return err
}

// Resize recomputes the grid dimensions from the new widget size and drives
// the three-way size synchronization the design flags as a classic
// terminal-corruption source: the PTY (ptySession.Resize), the VT grid, and
// the raster's cell count must all agree. Fyne calls Resize on the UI
// goroutine, so the CanvasObject work here needs no fyne.Do.
func (s *Session) Resize(size fyne.Size) {
	s.BaseWidget.Resize(size)

	cols, rows := gridDims(int(size.Width), int(size.Height), s.cellW, s.cellH)
	if cols == s.cols && rows == s.rows {
		return
	}
	s.cols, s.rows = cols, rows
	_ = s.pty.Resize(cols, rows) // PTY leg
	s.render.resize(cols, rows)  // VT + raster legs (render.resize does both)
	s.refreshCursor()
}

// gridDims converts a pixel size and per-cell box into a grid cell count,
// clamped to at least 1x1 so a zero-area layout pass can't collapse the grid.
// Extracted as a pure function so the size math is unit-testable without
// spawning a PTY.
func gridDims(width, height, cellW, cellH int) (cols, rows int) {
	if cellW <= 0 || cellH <= 0 {
		return 1, 1
	}
	return max(1, width/cellW), max(1, height/cellH)
}

// refreshCursor repositions and re-shows/hides the cursor overlay from the
// latest grid state and blink phase. Must run on the UI goroutine (its
// callers all arrange that).
func (s *Session) refreshCursor() {
	snap := s.vt.snapshot()

	s.mu.Lock()
	on := s.blinkOn
	s.mu.Unlock()

	visible := on && snap.CursorVisible
	s.cursor.Hidden = !visible
	s.cursor.Move(fyne.NewPos(float32(snap.CursorX*s.cellW), float32(snap.CursorY*s.cellH)))
	s.cursor.Resize(fyne.NewSize(float32(s.cellW), float32(s.cellH)))
	canvas.Refresh(s.cursor)
}

// TypedRune handles ordinary printable character input (fyne.Focusable).
//
// Threading note: Fyne's input dispatch calls TypedRune directly on the UI
// goroutine (the driver looks up the focused CanvasObject and invokes this
// method inline, in response to a keyboard event) — the same goroutine
// that's already running the event loop. Writing to the PTY here is a plain
// blocking I/O call, not a CanvasObject touch, so it needs no fyne.Do
// either way. This is the opposite direction from readLoop/refreshLoop
// (PTY output flowing in, which DOES need fyne.Do to cross onto the UI
// goroutine) — don't conflate the two just because both touch s.pty.
func (s *Session) TypedRune(r rune) {
	_, _ = s.pty.Write(runeBytes(r))
}

// TypedKey handles the fixed-function keys in keymap.go's mapping table
// (fyne.Focusable). Same threading note as TypedRune: called on the UI
// goroutine by Fyne's dispatch, no fyne.Do needed for the PTY write.
func (s *Session) TypedKey(ev *fyne.KeyEvent) {
	if data, ok := keyEventBytes(ev); ok {
		_, _ = s.pty.Write(data)
	}
}

// TypedShortcut handles Ctrl-modified key combinations (fyne.Shortcutable).
//
// This is required, not optional, for a terminal: Fyne's desktop driver
// intercepts every Ctrl+<key> combination as a keyboard shortcut before it
// would otherwise reach TypedKey — unconditionally, whether or not the
// focused object implements fyne.Shortcutable (see fyne's glfw driver,
// window.go's triggersShortcut/processKeyPressed). Without this method,
// Ctrl+C would be swallowed as an empty clipboard Copy instead of reaching
// the shell as the 0x03 SIGINT byte every interactive program relies on to
// be interruptible.
//
// fyne.ShortcutCopy/Paste/Cut/SelectAll/Undo/Redo and
// desktop.CustomShortcut all implement fyne.KeyboardShortcut (Key()/Mod()),
// so a single type switch on that interface covers Ctrl+C/V/X/A/Z/Y plus
// every other Ctrl+letter and Ctrl+[ \ ] combo uniformly — no need to
// depend on the desktop package directly. Same UI-goroutine threading note
// as TypedRune/TypedKey applies.
func (s *Session) TypedShortcut(sh fyne.Shortcut) {
	ks, ok := sh.(fyne.KeyboardShortcut)
	if !ok {
		return
	}
	if data, ok := ctrlKeyBytes(ks.Key()); ok {
		_, _ = s.pty.Write(data)
	}
}

// FocusGained is called when the terminal receives keyboard focus
// (fyne.Focusable). Called on the UI goroutine by Fyne's focus-management
// logic, so no fyne.Do is needed to update the guarded focused field.
func (s *Session) FocusGained() {
	s.mu.Lock()
	s.focused = true
	s.mu.Unlock()
}

// FocusLost is called when the terminal loses keyboard focus
// (fyne.Focusable). Same threading note as FocusGained.
func (s *Session) FocusLost() {
	s.mu.Lock()
	s.focused = false
	s.mu.Unlock()
}

// Tapped requests keyboard focus when the terminal is clicked (fyne.
// Tappable) — this is "focus-on-tap". A CanvasObject only receives
// TypedRune/TypedKey/TypedShortcut once it holds canvas focus, and nothing
// focuses it automatically on show; this mirrors the pattern Fyne's own
// widget.Entry uses internally (its unexported requestFocus resolves the
// object's canvas via the driver and calls Canvas.Focus). Called on the UI
// goroutine (tap events are dispatched the same way key events are), so no
// fyne.Do is needed.
func (s *Session) Tapped(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
}

// CreateRenderer wires the raster and cursor overlay into a Fyne widget
// renderer. Implementing fyne.WidgetRenderer directly (rather than composing
// existing widgets) is what lets the cursor sit as a free overlay on top of
// the raster at an arbitrary cell position.
func (s *Session) CreateRenderer() fyne.WidgetRenderer {
	return &sessionRenderer{
		s:       s,
		raster:  s.render.canvasObject(),
		cursor:  s.cursor,
		objects: []fyne.CanvasObject{s.render.canvasObject(), s.cursor},
	}
}

// sessionRenderer is Session's fyne.WidgetRenderer. The raster fills the
// widget; the cursor floats on top.
type sessionRenderer struct {
	s       *Session
	raster  *canvas.Raster
	cursor  *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *sessionRenderer) Layout(size fyne.Size) {
	r.raster.Resize(size)
	r.s.refreshCursor()
}

// MinSize reports the grid's natural pixel size so an unconstrained layout
// shows the whole grid 1:1.
func (r *sessionRenderer) MinSize() fyne.Size {
	w, h := r.s.render.pixelSize()
	return fyne.NewSize(float32(w), float32(h))
}

// Refresh repaints the grid image and cursor. It runs on the UI goroutine
// (refreshLoop calls it via fyne.Do; Fyne calls it directly on user-triggered
// refreshes), so touching CanvasObjects here is safe.
func (r *sessionRenderer) Refresh() {
	r.s.render.refresh()
	canvas.Refresh(r.raster)
	r.s.refreshCursor()
}

func (r *sessionRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *sessionRenderer) Destroy() {}
