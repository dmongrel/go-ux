// Package fontsettings provides a shared, Ctrl+scroll-adjustable font
// configuration mechanism, factored out of go-ux/terminal so go-ux/editors
// can reuse the same clamping/listener plumbing without duplicating it.
//
// Unlike terminal's original package-global FontSettings (one shared value
// for every open Session everywhere), State is an instance type: each
// caller (terminal's package-level fontState, or one editors.Group) owns
// its own State, so their font settings are independent of each other.
package fontsettings

import "sync"

// FontSettings is one live font configuration: Family "" means "use the
// bundled/default font" — callers decide what that means for their own
// rendering.
type FontSettings struct {
	Family      string
	Size        int
	LineHeight  float64
	ColumnWidth float64
}

// DefaultFontSettings is FontSettings' zero-configuration value.
var DefaultFontSettings = FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}

const (
	MinFontSize = 8
	MaxFontSize = 36

	MinFontMultiplier = 0.5
	MaxFontMultiplier = 3.0
)

// ClampFontSettings bounds Size/LineHeight/ColumnWidth to fixed ranges,
// leaving Family untouched. Applied wherever a FontSettings value is about
// to be read for rendering or written to storage — a hand-edited value or
// a scroll-driven step past the edge must not produce an unusable
// (negative/zero) font size.
func ClampFontSettings(s FontSettings) FontSettings {
	s.Size = clampInt(s.Size, MinFontSize, MaxFontSize)
	s.LineHeight = clampFloat(s.LineHeight, MinFontMultiplier, MaxFontMultiplier)
	s.ColumnWidth = clampFloat(s.ColumnWidth, MinFontMultiplier, MaxFontMultiplier)
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

// State is one owner's live FontSettings plus its listeners. Safe for
// concurrent use; current is only ever replaced wholesale (via Set), never
// mutated in place, so a reader never needs to hold mu past the point it
// copies current out.
type State struct {
	mu        sync.Mutex
	current   FontSettings
	listeners map[any]func(FontSettings)
}

// NewState builds a State starting at defaults.
func NewState(defaults FontSettings) *State {
	return &State{current: defaults, listeners: make(map[any]func(FontSettings))}
}

// Current returns the live FontSettings.
func (s *State) Current() FontSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Set replaces the live FontSettings (clamped) and notifies every
// registered listener — each listener's own reaction is what actually
// recomputes font metrics and re-layouts; Set itself has no UI dependency
// at all, so it's testable in isolation.
func (s *State) Set(v FontSettings) {
	v = ClampFontSettings(v)

	s.mu.Lock()
	s.current = v
	fns := make([]func(FontSettings), 0, len(s.listeners))
	for _, fn := range s.listeners {
		fns = append(fns, fn)
	}
	s.mu.Unlock()

	for _, fn := range fns {
		fn(v)
	}
}

// RegisterListener subscribes key to future Set calls, applying the change
// via fn. key is any comparable value identifying the subscriber (e.g. a
// *Session or *Pane) — it need not be a real widget; see
// RegisterListenerFunc for a test-only key-free variant.
func (s *State) RegisterListener(key any, fn func(FontSettings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners[key] = fn
}

// UnregisterListener removes key's subscription, if any.
func (s *State) UnregisterListener(key any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.listeners, key)
}

// RegisterListenerFunc is a test/throwaway-caller seam: it registers fn
// under a fresh, private key so callers that don't have (or don't want to
// construct) a real subscriber object can still listen. Returns an
// unregister func.
func (s *State) RegisterListenerFunc(fn func(FontSettings)) (unregister func()) {
	key := new(int)
	s.RegisterListener(key, fn)
	return func() { s.UnregisterListener(key) }
}
