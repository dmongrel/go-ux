// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package terminal

import (
	"sync/atomic"
	"testing"
)

func TestDefaultFontSettings(t *testing.T) {
	got := currentFontSettings()
	want := FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}
	if got != want {
		t.Errorf("currentFontSettings() = %+v, want %+v", got, want)
	}
}

func TestSetFontSettingsNotifiesRegisteredListeners(t *testing.T) {
	defer setFontSettings(FontSettings{Family: "", Size: 13, LineHeight: 1.0, ColumnWidth: 1.0}) // restore default for other tests

	var calls int32
	unregister := registerFontListenerFunc(func(FontSettings) {
		atomic.AddInt32(&calls, 1)
	})
	defer unregister()

	setFontSettings(FontSettings{Family: "", Size: 20, LineHeight: 1.0, ColumnWidth: 1.0})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("listener called %d times, want 1", got)
	}
	if got := currentFontSettings().Size; got != 20 {
		t.Errorf("currentFontSettings().Size = %d, want 20", got)
	}
}

func TestClampFontSettings(t *testing.T) {
	got := clampFontSettings(FontSettings{Size: 200, LineHeight: 10, ColumnWidth: 0.01})
	if got.Size != 36 {
		t.Errorf("Size = %d, want 36 (clamped)", got.Size)
	}
	if got.LineHeight != 3.0 {
		t.Errorf("LineHeight = %v, want 3.0 (clamped)", got.LineHeight)
	}
	if got.ColumnWidth != 0.5 {
		t.Errorf("ColumnWidth = %v, want 0.5 (clamped)", got.ColumnWidth)
	}

	got2 := clampFontSettings(FontSettings{Size: 1, LineHeight: 0.01, ColumnWidth: 10})
	if got2.Size != 8 {
		t.Errorf("Size = %d, want 8 (clamped)", got2.Size)
	}
	if got2.LineHeight != 0.5 {
		t.Errorf("LineHeight = %v, want 0.5 (clamped)", got2.LineHeight)
	}
	if got2.ColumnWidth != 3.0 {
		t.Errorf("ColumnWidth = %v, want 3.0 (clamped)", got2.ColumnWidth)
	}
}

