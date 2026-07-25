package dialog

import "testing"

func TestNormalizeSpecDefaults(t *testing.T) {
	got := normalizeSpec(CustomDialogSpec{Title: "t"})
	if len(got.Buttons) != 1 || got.Buttons[0] != ButtonOK {
		t.Fatalf("Buttons = %v, want [OK]", got.Buttons)
	}
	if got.Width != defaultWidth || got.Height != defaultHeight {
		t.Fatalf("size = %dx%d, want %dx%d", got.Width, got.Height, defaultWidth, defaultHeight)
	}
}

func TestNormalizeSpecPreservesExplicitValues(t *testing.T) {
	got := normalizeSpec(CustomDialogSpec{
		Buttons: []ButtonKind{ButtonOK, ButtonCancel},
		Width:   400,
		Height:  300,
	})
	if len(got.Buttons) != 2 || got.Buttons[1] != ButtonCancel {
		t.Fatalf("Buttons = %v, want [OK Cancel]", got.Buttons)
	}
	if got.Width != 400 || got.Height != 300 {
		t.Fatalf("size = %dx%d, want 400x300", got.Width, got.Height)
	}
}

func TestGetSpecReturnsRegisteredSpec(t *testing.T) {
	s := NewService(nil)
	spec := CustomDialogSpec{Title: "Rename", Properties: []Property{{Key: "name", Kind: PropertyTextField}}}
	s.specs["1"] = spec

	got := s.GetSpec("1")
	if got.Title != "Rename" || len(got.Properties) != 1 {
		t.Fatalf("GetSpec = %+v, want %+v", got, spec)
	}
}

func TestGetSpecUnknownIDReturnsZeroValue(t *testing.T) {
	s := NewService(nil)
	got := s.GetSpec("missing")
	if got.Title != "" || got.Properties != nil {
		t.Fatalf("GetSpec(missing) = %+v, want zero value", got)
	}
}

func TestSubmitDeliversResultToPendingCall(t *testing.T) {
	s := NewService(nil)
	ch := make(chan map[string]any, 1)
	s.pending["1"] = ch

	want := map[string]any{"name": "Alice"}
	s.Submit("1", want)

	select {
	case got := <-ch:
		if got["name"] != "Alice" {
			t.Fatalf("result = %v, want %v", got, want)
		}
	default:
		t.Fatal("Submit did not deliver a result")
	}
	if _, stillPending := s.pending["1"]; stillPending {
		t.Fatal("Submit left the call pending")
	}
}

func TestCancelDialogDeliversNil(t *testing.T) {
	s := NewService(nil)
	ch := make(chan map[string]any, 1)
	s.pending["1"] = ch

	s.CancelDialog("1")

	select {
	case got := <-ch:
		if got != nil {
			t.Fatalf("result = %v, want nil", got)
		}
	default:
		t.Fatal("CancelDialog did not deliver a result")
	}
}

// TestResolveIsIdempotent guards the real race ShowCustom's window-closing
// hook and an explicit Submit/CancelDialog click can hit simultaneously —
// e.g. Submit closes the window as a side effect, which also fires
// WindowClosing. Only the first resolution must win; the second must be a
// silent no-op, not a panic on send-to-closed-channel or a block on a
// channel nothing is reading anymore.
func TestResolveIsIdempotent(t *testing.T) {
	s := NewService(nil)
	ch := make(chan map[string]any, 1)
	s.pending["1"] = ch

	s.resolve("1", map[string]any{"name": "Alice"})
	s.resolve("1", nil) // must not panic or block

	got := <-ch
	if got["name"] != "Alice" {
		t.Fatalf("result = %v, want the first resolution to win", got)
	}
}
