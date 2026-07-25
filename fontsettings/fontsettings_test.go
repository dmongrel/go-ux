package fontsettings

import (
	"sync/atomic"
	"testing"
)

func TestNewStateStartsAtDefaults(t *testing.T) {
	s := NewState(DefaultFontSettings)
	if got := s.Current(); got != DefaultFontSettings {
		t.Errorf("Current() = %+v, want %+v", got, DefaultFontSettings)
	}
}

func TestSetNotifiesRegisteredListeners(t *testing.T) {
	s := NewState(DefaultFontSettings)

	var calls int32
	unregister := s.RegisterListenerFunc(func(FontSettings) {
		atomic.AddInt32(&calls, 1)
	})
	defer unregister()

	s.Set(FontSettings{Family: "", Size: 20, LineHeight: 1.0, ColumnWidth: 1.0})

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("listener called %d times, want 1", got)
	}
	if got := s.Current().Size; got != 20 {
		t.Errorf("Current().Size = %d, want 20", got)
	}
}

func TestUnregisterListenerStopsNotifications(t *testing.T) {
	s := NewState(DefaultFontSettings)

	called := false
	unregister := s.RegisterListenerFunc(func(FontSettings) { called = true })
	unregister()

	s.Set(FontSettings{Size: 20, LineHeight: 1.0, ColumnWidth: 1.0})

	if called {
		t.Errorf("listener fired after being unregistered")
	}
}

func TestTwoStatesAreIndependent(t *testing.T) {
	a := NewState(DefaultFontSettings)
	b := NewState(DefaultFontSettings)

	a.Set(FontSettings{Size: 30, LineHeight: 1.0, ColumnWidth: 1.0})

	if b.Current().Size != DefaultFontSettings.Size {
		t.Errorf("Set on one State affected another independent State: b.Current().Size = %d, want %d", b.Current().Size, DefaultFontSettings.Size)
	}
}

func TestClampFontSettings(t *testing.T) {
	got := ClampFontSettings(FontSettings{Size: 200, LineHeight: 10, ColumnWidth: 0.01})
	if got.Size != MaxFontSize {
		t.Errorf("Size = %d, want %d (clamped)", got.Size, MaxFontSize)
	}
	if got.LineHeight != MaxFontMultiplier {
		t.Errorf("LineHeight = %v, want %v (clamped)", got.LineHeight, MaxFontMultiplier)
	}
	if got.ColumnWidth != MinFontMultiplier {
		t.Errorf("ColumnWidth = %v, want %v (clamped)", got.ColumnWidth, MinFontMultiplier)
	}

	got2 := ClampFontSettings(FontSettings{Size: 1, LineHeight: 0.01, ColumnWidth: 10})
	if got2.Size != MinFontSize {
		t.Errorf("Size = %d, want %d (clamped)", got2.Size, MinFontSize)
	}
	if got2.LineHeight != MinFontMultiplier {
		t.Errorf("LineHeight = %v, want %v (clamped)", got2.LineHeight, MinFontMultiplier)
	}
	if got2.ColumnWidth != MaxFontMultiplier {
		t.Errorf("ColumnWidth = %v, want %v (clamped)", got2.ColumnWidth, MaxFontMultiplier)
	}
}

func TestSetWithSameValueStillNotifies(t *testing.T) {
	// Unlike Document.SetText (editors package), State.Set has no no-op
	// guard — a caller re-applying the same settings (e.g. a settings
	// window's Apply button) expects listeners to still run, since Set's
	// side effects (clamping, re-layout) are cheap and idempotent.
	s := NewState(DefaultFontSettings)

	called := false
	unregister := s.RegisterListenerFunc(func(FontSettings) { called = true })
	defer unregister()

	s.Set(DefaultFontSettings)

	if !called {
		t.Errorf("listener did not fire for a Set with the same value")
	}
}
