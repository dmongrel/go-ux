// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package terminal

import "github.com/dmongrel/go-ux/fontsettings"

// FontSettings is the live, shared terminal font configuration: every open
// session, across every open terminal window, renders against the same
// shared value (see registerFontListener) — this is what makes Ctrl+scroll
// or a Settings-window Apply affect every tab at once, without threading a
// shared object through every constructor. Family "" means "use the
// browser/xterm.js default font" (frontend's own fallback, mirroring the
// old bundled-font fallback).
//
// A type alias (not a redeclaration) to go-ux/fontsettings.FontSettings,
// which owns the actual struct definition.
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
// listeners, delegating to go-ux/fontsettings.State — the same mechanism
// editors.Group uses via its own independent instance.
var fontState = fontsettings.NewState(defaultFontSettings)

// currentFontSettings returns the live shared font configuration.
func currentFontSettings() FontSettings {
	return fontState.Current()
}

// setFontSettings replaces the shared font configuration (clamped) and
// notifies every registered listener.
func setFontSettings(s FontSettings) {
	fontState.Set(s)
}

// registerFontListener subscribes key to future setFontSettings calls,
// applying the change via fn. key just needs to be comparable — unlike the
// original Fyne version (keyed by *Session, a live widget), the Wails
// Service registers exactly one listener for its own lifetime, keyed by
// itself, and broadcasts every change to every open terminal window as a
// Wails event (see Service.NewService).
func registerFontListener(key any, fn func(FontSettings)) {
	fontState.RegisterListener(key, fn)
}

// unregisterFontListener removes key's subscription.
func unregisterFontListener(key any) {
	fontState.UnregisterListener(key)
}

// registerFontListenerFunc is a test-only seam — see font_test.go.
func registerFontListenerFunc(fn func(FontSettings)) (unregister func()) {
	key := new(int)
	registerFontListener(key, fn)
	return func() { unregisterFontListener(key) }
}

