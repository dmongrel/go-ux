package editors

import (
	"testing"

	"fyne.io/fyne/v2"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// findButton recursively walks obj looking for a *widget.Button with the
// given text, returning nil if none is found.
func findButton(obj fyne.CanvasObject, text string) *widget.Button {
	if btn, ok := obj.(*widget.Button); ok && btn.Text == text {
		return btn
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, o := range c.Objects {
			if found := findButton(o, text); found != nil {
				return found
			}
		}
	}
	return nil
}

// allButtons recursively collects every *widget.Button found under obj.
func allButtons(obj fyne.CanvasObject) []*widget.Button {
	var out []*widget.Button
	if btn, ok := obj.(*widget.Button); ok {
		out = append(out, btn)
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, o := range c.Objects {
			out = append(out, allButtons(o)...)
		}
	}
	return out
}

func TestSouthBarStartsHidden(t *testing.T) {
	fynetest.NewApp()

	s := NewSouthBar()
	if s.Mode() != SouthBarHidden {
		t.Fatalf("Mode() = %v, want SouthBarHidden", s.Mode())
	}
}

func TestSouthBarDiffReviewButtonsFireCorrectCallback(t *testing.T) {
	fynetest.NewApp()

	s := NewSouthBar()
	var acceptCalled, cancelCalled bool
	s.SetMode(SouthBarDiffReview,
		func() { acceptCalled = true },
		func() { cancelCalled = true },
	)

	r := s.CreateRenderer()
	objs := r.Objects()
	if len(objs) == 0 {
		t.Fatal("renderer has no objects")
	}

	accept := findButton(objs[0], "Accept")
	if accept == nil {
		t.Fatal("Accept button not found")
	}
	accept.OnTapped()
	if !acceptCalled || cancelCalled {
		t.Fatalf("after tapping Accept: acceptCalled=%v cancelCalled=%v, want true/false", acceptCalled, cancelCalled)
	}

	acceptCalled, cancelCalled = false, false
	cancel := findButton(objs[0], "Cancel")
	if cancel == nil {
		t.Fatal("Cancel button not found")
	}
	cancel.OnTapped()
	if acceptCalled || !cancelCalled {
		t.Fatalf("after tapping Cancel: acceptCalled=%v cancelCalled=%v, want false/true", acceptCalled, cancelCalled)
	}
}

func TestSouthBarFileChangedButtonsFireCorrectCallback(t *testing.T) {
	fynetest.NewApp()

	s := NewSouthBar()
	var loadCalled, keepCalled bool
	s.SetMode(SouthBarFileChanged,
		func() { loadCalled = true },
		func() { keepCalled = true },
	)

	r := s.CreateRenderer()
	objs := r.Objects()
	if len(objs) == 0 {
		t.Fatal("renderer has no objects")
	}

	load := findButton(objs[0], "Load from Disk")
	if load == nil {
		t.Fatal("Load from Disk button not found")
	}
	load.OnTapped()
	if !loadCalled || keepCalled {
		t.Fatalf("after tapping Load from Disk: loadCalled=%v keepCalled=%v, want true/false", loadCalled, keepCalled)
	}

	loadCalled, keepCalled = false, false
	keep := findButton(objs[0], "Keep from Memory")
	if keep == nil {
		t.Fatal("Keep from Memory button not found")
	}
	keep.OnTapped()
	if loadCalled || !keepCalled {
		t.Fatalf("after tapping Keep from Memory: loadCalled=%v keepCalled=%v, want false/true", loadCalled, keepCalled)
	}
}

func TestSouthBarSwitchingModeDropsStaleCallbacks(t *testing.T) {
	fynetest.NewApp()

	s := NewSouthBar()
	var f1Called, f2Called, f3Called, f4Called bool
	s.SetMode(SouthBarDiffReview,
		func() { f1Called = true },
		func() { f2Called = true },
	)
	s.SetMode(SouthBarFileChanged,
		func() { f3Called = true },
		func() { f4Called = true },
	)

	r := s.CreateRenderer()
	objs := r.Objects()
	if len(objs) == 0 {
		t.Fatal("renderer has no objects")
	}

	load := findButton(objs[0], "Load from Disk")
	if load == nil {
		t.Fatal("Load from Disk button not found")
	}
	load.OnTapped()

	if !f3Called {
		t.Fatal("f3 (new primary callback) was not called")
	}
	if f1Called || f2Called || f4Called {
		t.Fatalf("stale/unrelated callback fired: f1=%v f2=%v f4=%v", f1Called, f2Called, f4Called)
	}
}

func TestSouthBarHiddenHasNoButtons(t *testing.T) {
	fynetest.NewApp()

	s := NewSouthBar()
	r := s.CreateRenderer()
	for _, obj := range r.Objects() {
		if btns := allButtons(obj); len(btns) != 0 {
			t.Fatalf("hidden SouthBar has %d button(s), want 0", len(btns))
		}
	}
}
