package terminal

import "go-ux/fontsettings"

// FontSettings is the live, global terminal font configuration: every open
// Session, in every open Window/TabView, renders against the same shared
// value (see registerFontListener) — this is what makes Ctrl+scroll or a
// Settings-window Apply affect every tab at once, without threading a
// shared object through every constructor. Family "" means "use the
// bundled font" (loadMonospaceFace's existing fallback path).
//
// A type alias (not a redeclaration) to go-ux/fontsettings.FontSettings,
// which now owns the actual struct definition — kept as an alias so any
// existing caller of terminal.FontSettings stays source/binary compatible
// after the fontsettings extraction.
type FontSettings = fontsettings.FontSettings

// defaultFontSettings is FontSettings' zero-configuration value — matches
// RegisterSettings' own seeded defaults so a caller that never touches font
// settings at all sees identical behavior to before this feature existed.
var defaultFontSettings = fontsettings.DefaultFontSettings

const (
	minFontSize = fontsettings.MinFontSize
	maxFontSize = fontsettings.MaxFontSize

	minFontMultiplier = fontsettings.MinFontMultiplier
	maxFontMultiplier = fontsettings.MaxFontMultiplier
)

// clampFontSettings bounds Size/LineHeight/ColumnWidth to fixed ranges,
// leaving Family untouched. Applied wherever a FontSettings value is about
// to be read for rendering or written to the db — a hand-edited db row or
// a scroll-driven step past the edge must not produce an unusable
// (negative/zero) font size.
func clampFontSettings(s FontSettings) FontSettings {
	return fontsettings.ClampFontSettings(s)
}

// fontState is the package-level shared font configuration plus its
// listeners, now delegating to go-ux/fontsettings.State — the same
// mechanism editors.Group uses via its own independent instance.
var fontState = fontsettings.NewState(defaultFontSettings)

// currentFontSettings returns the live shared font configuration.
func currentFontSettings() FontSettings {
	return fontState.Current()
}

// setFontSettings replaces the shared font configuration (clamped) and
// notifies every registered listener — every live Session's own reaction
// is what actually recomputes font metrics and re-layouts; setFontSettings
// itself has no Fyne/UI dependency at all, so it's testable in isolation.
func setFontSettings(s FontSettings) {
	fontState.Set(s)
}

// registerFontListener subscribes s to future setFontSettings calls,
// applying the change via fn. Called from NewSession.
func registerFontListener(s *Session, fn func(FontSettings)) {
	fontState.RegisterListener(s, fn)
}

// unregisterFontListener removes s's subscription. Called from Close.
func unregisterFontListener(s *Session) {
	fontState.UnregisterListener(s)
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
