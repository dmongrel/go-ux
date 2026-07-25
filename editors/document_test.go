package editors

import "testing"

func TestNewDocumentStartsCleanWithGivenText(t *testing.T) {
	d := NewDocument("hello")

	if d.Text() != "hello" {
		t.Errorf("Text() = %q, want %q", d.Text(), "hello")
	}
	if d.Dirty() {
		t.Errorf("Dirty() = true, want false for a freshly created Document")
	}
}

func TestMarkCleanClearsDirty(t *testing.T) {
	d := NewDocument("hello")
	d.SetText("world")

	d.MarkClean()

	if d.Dirty() {
		t.Errorf("Dirty() = true after MarkClean, want false")
	}
	if d.Text() != "world" {
		t.Errorf("Text() = %q, want unchanged %q — MarkClean must not touch the text itself", d.Text(), "world")
	}
}

func TestMarkCleanNotifiesListenersWithUnchangedText(t *testing.T) {
	d := NewDocument("hello")

	var got string
	called := false
	d.RegisterListener("key1", func(text string) { called = true; got = text })

	d.MarkClean()

	if !called {
		t.Errorf("listener did not fire for MarkClean — anything watching purely for Dirty transitions (e.g. a tab bar's unsaved-changes indicator) would never find out a save happened")
	}
	if got != "hello" {
		t.Errorf("listener received %q, want unchanged %q", got, "hello")
	}
}

func TestSetTextUpdatesTextAndMarksDirty(t *testing.T) {
	d := NewDocument("hello")

	d.SetText("world")

	if d.Text() != "world" {
		t.Errorf("Text() = %q, want %q", d.Text(), "world")
	}
	if !d.Dirty() {
		t.Errorf("Dirty() = false, want true after SetText")
	}
}

func TestSetTextNotifiesRegisteredListeners(t *testing.T) {
	d := NewDocument("hello")

	var got string
	d.RegisterListener("key1", func(text string) { got = text })

	d.SetText("world")

	if got != "world" {
		t.Errorf("listener received %q, want %q", got, "world")
	}
}

func TestSetTextWithSameTextDoesNotNotifyListeners(t *testing.T) {
	d := NewDocument("hello")

	called := false
	d.RegisterListener("key1", func(string) { called = true })

	d.SetText("hello")

	if called {
		t.Errorf("listener was called for a SetText that didn't change the text")
	}
	if d.Dirty() {
		t.Errorf("Dirty() = true, want false — SetText with unchanged text should be a no-op")
	}
}

func TestUnregisterListenerStopsFutureNotifications(t *testing.T) {
	d := NewDocument("hello")

	called := false
	d.RegisterListener("key1", func(string) { called = true })
	d.UnregisterListener("key1")

	d.SetText("world")

	if called {
		t.Errorf("listener fired after being unregistered")
	}
}

func TestMultipleListenersAllNotified(t *testing.T) {
	d := NewDocument("hello")

	var gotA, gotB string
	d.RegisterListener("a", func(text string) { gotA = text })
	d.RegisterListener("b", func(text string) { gotB = text })

	d.SetText("world")

	if gotA != "world" || gotB != "world" {
		t.Errorf("gotA=%q gotB=%q, want both %q", gotA, gotB, "world")
	}
}
