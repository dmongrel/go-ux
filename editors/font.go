package editors

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyne.io/fyne/v2/driver/desktop"

	"go-ux/fontsettings"
)

// fontSizeScrollStep is how many points the content font size changes per
// mouse-wheel tick while Ctrl is held — matches
// terminal/widget.go's fontSizeScrollStep so Ctrl+scroll feels the same in
// both packages.
const fontSizeScrollStep = 1

// sizeOverrideTheme wraps a base fyne.Theme, overriding only
// theme.SizeNameText to reflect a live *fontsettings.State — every other
// name (colors, icons, other sizes) delegates to base unchanged. This is
// what makes the content area's font size independently adjustable per
// Group (via container.NewThemeOverride in pane.go) rather than a single
// value shared by the whole app the way changing Fyne's global theme
// would be.
type sizeOverrideTheme struct {
	base  fyne.Theme
	fonts *fontsettings.State
}

func (t *sizeOverrideTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}

func (t *sizeOverrideTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *sizeOverrideTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *sizeOverrideTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return float32(t.fonts.Current().Size)
	}
	return t.base.Size(name)
}

// newContentThemeOverride wraps content in a container.NewThemeOverride
// using fonts' current Size for theme.SizeNameText, and registers a
// listener (keyed by key, typically the *Pane) so future fonts.Set calls
// (from Ctrl+scroll, see editorEntry below, or a future settings window)
// refresh the override live. Callers (pane.go) must call the returned
// cleanup func when the content is torn down, mirroring
// newDocumentContent's own cleanup contract.
func newContentThemeOverride(content fyne.CanvasObject, fonts *fontsettings.State, key any) (wrapped fyne.CanvasObject, cleanup func()) {
	override := container.NewThemeOverride(content, &sizeOverrideTheme{base: theme.DefaultTheme(), fonts: fonts})
	fonts.RegisterListener(key, func(fontsettings.FontSettings) {
		override.Refresh()
	})
	return override, func() { fonts.UnregisterListener(key) }
}

// editorEntry wraps widget.Entry solely to intercept Ctrl+scroll for
// live font-size adjustment (mirrors terminal/widget.go's
// Session.KeyDown/KeyUp/Scrolled). Implementing Scrolled ourselves means
// Fyne routes every wheel event over this widget to us instead of
// bubbling it to the ancestor container.Scroll that actually scrolls the
// content — so a plain (non-Ctrl) scroll must be forwarded to that
// ancestor manually via forwardScroll, or scrolling the document would
// silently stop working.
type editorEntry struct {
	*widget.Entry
	ctrlHeld      bool
	fonts         *fontsettings.State
	forwardScroll func(*fyne.ScrollEvent)
	onSave        func()
	onNextTab     func()
	onPrevTab     func()
}

func newEditorEntry(fonts *fontsettings.State, forwardScroll func(*fyne.ScrollEvent)) *editorEntry {
	// Deliberately NOT widget.NewMultiLineEntry(): that constructor calls
	// entry.ExtendBaseWidget(entry) itself, and BaseWidget.ExtendBaseWidget
	// is a one-shot — it silently no-ops if the widget's "impl" is already
	// set (see its own source). Calling e.ExtendBaseWidget(e) below would
	// then do nothing, leaving impl pointed at the bare inner *widget.Entry
	// instead of e — and *widget.Entry.requestFocus() (used by every tap,
	// and thus by every keystroke) calls fyne.CurrentApp().Driver().
	// CanvasForObject(impl): since only e (not the inner bare Entry) is
	// ever actually placed in the canvas tree, that lookup returns nil and
	// focus is never acquired, silently breaking all typing/paste — a real
	// bug this exact construction produced, caught by manual testing
	// (automated tests all drove Entry via SetText/OnChanged directly,
	// never through a real tap-to-focus-then-type flow, so they never
	// exercised requestFocus at all). Building the *widget.Entry value by
	// hand and extending e first, before anything else has a chance to
	// extend the inner Entry on its own, fixes it.
	e := &editorEntry{
		Entry:         &widget.Entry{MultiLine: true, Wrapping: fyne.TextWrap(fyne.TextTruncateClip)},
		fonts:         fonts,
		forwardScroll: forwardScroll,
	}
	e.ExtendBaseWidget(e)
	return e
}

// KeyDown/KeyUp track Ctrl's held state, then delegate to Entry's own
// KeyDown/KeyUp so its existing shortcut handling (shift-select, etc.)
// still works.
func (e *editorEntry) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyControlLeft || ev.Name == desktop.KeyControlRight {
		e.ctrlHeld = true
	}
	e.Entry.KeyDown(ev)
}

func (e *editorEntry) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyControlLeft || ev.Name == desktop.KeyControlRight {
		e.ctrlHeld = false
	}
	e.Entry.KeyUp(ev)
}

// Scrolled (fyne.Scrollable) adjusts fonts' live Size when Ctrl is held
// (one fontSizeScrollStep per wheel tick, clamped by fontsettings.State.Set
// itself); otherwise forwards the event to the ancestor Scroll so normal
// scrolling keeps working (see the type doc comment).
func (e *editorEntry) Scrolled(ev *fyne.ScrollEvent) {
	if !e.ctrlHeld {
		if e.forwardScroll != nil {
			e.forwardScroll(ev)
		}
		return
	}

	current := e.fonts.Current()
	if ev.Scrolled.DY < 0 {
		current.Size -= fontSizeScrollStep
	} else {
		current.Size += fontSizeScrollStep
	}
	e.fonts.Set(current)
}

// TypedShortcut intercepts Ctrl+S/Ctrl+PageDown/Ctrl+PageUp (Fyne has no
// built-in shortcut constants for any of these — the driver reports any
// unrecognized modifier+key combo as a *desktop.CustomShortcut, which
// widget.Entry's own TypedShortcut just ignores since nothing is
// registered for them) to call onSave/onNextTab/onPrevTab respectively,
// otherwise delegates to Entry's own TypedShortcut so
// Undo/Redo/Cut/Copy/Paste/Select-all keep working unchanged.
func (e *editorEntry) TypedShortcut(shortcut fyne.Shortcut) {
	if cs, ok := shortcut.(*desktop.CustomShortcut); ok && cs.Modifier == fyne.KeyModifierControl {
		switch cs.KeyName {
		case fyne.KeyS:
			if e.onSave != nil {
				e.onSave()
			}
			return
		case fyne.KeyPageDown:
			if e.onNextTab != nil {
				e.onNextTab()
			}
			return
		case fyne.KeyPageUp:
			if e.onPrevTab != nil {
				e.onPrevTab()
			}
			return
		}
	}
	e.Entry.TypedShortcut(shortcut)
}

// FocusLost clears ctrlHeld — mirrors terminal/widget.go's Session.FocusLost:
// Alt-Tabbing away while Ctrl is physically held can happen without a
// matching KeyUp ever being delivered, and without this a later plain
// scroll after refocusing would be misread as a Ctrl+scroll font change.
func (e *editorEntry) FocusLost() {
	e.ctrlHeld = false
	e.Entry.FocusLost()
}
