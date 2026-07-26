// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

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

func TestNormalizeImageGridSpecDefaults(t *testing.T) {
	got := normalizeImageGridSpec(ImageGridSpec{Title: "t"})
	if got.Width != defaultImageGridWidth || got.Height != defaultImageGridHeight {
		t.Fatalf("size = %dx%d, want %dx%d", got.Width, got.Height, defaultImageGridWidth, defaultImageGridHeight)
	}
}

func TestNormalizeImageGridSpecPreservesExplicitValues(t *testing.T) {
	got := normalizeImageGridSpec(ImageGridSpec{Width: 200, Height: 150})
	if got.Width != 200 || got.Height != 150 {
		t.Fatalf("size = %dx%d, want 200x150", got.Width, got.Height)
	}
}

func TestGetImageGridSpecReturnsRegisteredSpec(t *testing.T) {
	s := NewService(nil)
	spec := ImageGridSpec{Title: "Choose", Options: []ImageOption{{Key: "a", ImageData: []byte{1, 2, 3}}}}
	s.imageSpecs["1"] = spec

	got := s.GetImageGridSpec("1")
	if got.Title != "Choose" || len(got.Options) != 1 {
		t.Fatalf("GetImageGridSpec = %+v, want %+v", got, spec)
	}
}

func TestGetImageGridSpecUnknownIDReturnsZeroValue(t *testing.T) {
	s := NewService(nil)
	got := s.GetImageGridSpec("missing")
	if got.Title != "" || got.Options != nil {
		t.Fatalf("GetImageGridSpec(missing) = %+v, want zero value", got)
	}
}

func TestSelectImageDeliversKeyToPendingCall(t *testing.T) {
	s := NewService(nil)
	ch := make(chan string, 1)
	s.imagePending["1"] = ch

	s.SelectImage("1", "kiwihug-shadow")

	select {
	case got := <-ch:
		if got != "kiwihug-shadow" {
			t.Fatalf("result = %q, want %q", got, "kiwihug-shadow")
		}
	default:
		t.Fatal("SelectImage did not deliver a result")
	}
	if _, stillPending := s.imagePending["1"]; stillPending {
		t.Fatal("SelectImage left the call pending")
	}
}

func TestCancelImageGridDeliversEmptyString(t *testing.T) {
	s := NewService(nil)
	ch := make(chan string, 1)
	s.imagePending["1"] = ch

	s.CancelImageGrid("1")

	select {
	case got := <-ch:
		if got != "" {
			t.Fatalf("result = %q, want empty", got)
		}
	default:
		t.Fatal("CancelImageGrid did not deliver a result")
	}
}

// TestResolveImageGridIsIdempotent mirrors TestResolveIsIdempotent's race
// guard for the image-grid path: a click and the window-closing hook can
// fire concurrently, and only the first must win.
func TestResolveImageGridIsIdempotent(t *testing.T) {
	s := NewService(nil)
	ch := make(chan string, 1)
	s.imagePending["1"] = ch

	s.resolveImageGrid("1", "picked")
	s.resolveImageGrid("1", "") // must not panic or block

	got := <-ch
	if got != "picked" {
		t.Fatalf("result = %q, want the first resolution to win", got)
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

