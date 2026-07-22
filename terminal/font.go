package terminal

import "sync"

// FontSettings is the live, global terminal font configuration: every open
// Session, in every open Window/TabView, renders against the same shared
// value (see registerFontListener) — this is what makes Ctrl+scroll or a
// Settings-window Apply affect every tab at once, without threading a
// shared object through every constructor. Family "" means "use the
// bundled font" (loadMonospaceFace's existing fallback path).
type FontSettings struct {
	Family      string
	Size        int
	LineHeight  float64
	ColumnWidth float64
}

// defaultFontSettings is FontSettings' zero-configuration value — matches
// RegisterSettings' own seeded defaults so a caller that never touches font
// settings at all sees identical behavior to before this feature existed.
var defaultFontSettings = FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}

const (
	minFontSize = 8
	maxFontSize = 36

	minFontMultiplier = 0.5
	maxFontMultiplier = 3.0
)

// clampFontSettings bounds Size/LineHeight/ColumnWidth to fixed ranges,
// leaving Family untouched. Applied wherever a FontSettings value is about
// to be read for rendering or written to the db — a hand-edited db row or
// a scroll-driven step past the edge must not produce an unusable
// (negative/zero) font size.
func clampFontSettings(s FontSettings) FontSettings {
	s.Size = clampInt(s.Size, minFontSize, maxFontSize)
	s.LineHeight = clampFloat(s.LineHeight, minFontMultiplier, maxFontMultiplier)
	s.ColumnWidth = clampFloat(s.ColumnWidth, minFontMultiplier, maxFontMultiplier)
	return s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// fontState is the package-level shared FontSettings plus its listeners.
// mu guards both fields; current is only ever replaced wholesale (via
// setFontSettings), never mutated in place, so a reader never needs to
// hold mu past the point it copies current out.
var fontState = struct {
	mu        sync.Mutex
	current   FontSettings
	listeners map[*Session]func(FontSettings)
}{
	current:   defaultFontSettings,
	listeners: make(map[*Session]func(FontSettings)),
}

// currentFontSettings returns the live shared font configuration.
func currentFontSettings() FontSettings {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	return fontState.current
}

// setFontSettings replaces the shared font configuration (clamped) and
// notifies every registered listener — every live Session's own reaction
// is what actually recomputes font metrics and re-layouts; setFontSettings
// itself has no Fyne/UI dependency at all, so it's testable in isolation.
func setFontSettings(s FontSettings) {
	s = clampFontSettings(s)

	fontState.mu.Lock()
	fontState.current = s
	fns := make([]func(FontSettings), 0, len(fontState.listeners))
	for _, fn := range fontState.listeners {
		fns = append(fns, fn)
	}
	fontState.mu.Unlock()

	for _, fn := range fns {
		fn(s)
	}
}

// registerFontListener subscribes s to future setFontSettings calls,
// applying the change via fn. Called from NewSession.
func registerFontListener(s *Session, fn func(FontSettings)) {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	fontState.listeners[s] = fn
}

// unregisterFontListener removes s's subscription. Called from Close.
func unregisterFontListener(s *Session) {
	fontState.mu.Lock()
	defer fontState.mu.Unlock()
	delete(fontState.listeners, s)
}

// registerFontListenerFunc is a test-only seam: registerFontListener keys
// its map on *Session (a real PTY-backed widget, expensive to construct
// just to test notification plumbing), but the underlying mechanism doesn't
// actually need a *Session — any comparable key works. Tests use a
// throwaway *Session-shaped key via this helper instead of a real Session.
func registerFontListenerFunc(fn func(FontSettings)) (unregister func()) {
	key := new(Session)
	registerFontListener(key, fn)
	return func() { unregisterFontListener(key) }
}
