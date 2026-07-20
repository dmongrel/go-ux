package dialog

import (
	"testing"
	"time"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// tapAndWait taps btn on its own goroutine (mirroring real usage, where the
// click happens on Fyne's UI goroutine while the caller blocks reading
// resultCh) and returns the dialog's result. Tapping synchronously on the
// test goroutine would deadlock: the OK/Cancel handler sends on an
// unbuffered channel that only this same goroutine is meant to read.
func tapAndWait(t *testing.T, btn *widget.Button, ch <-chan map[string]any) map[string]any {
	t.Helper()
	go fynetest.Tap(btn)
	select {
	case res := <-ch:
		return res
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dialog result")
		return nil
	}
}

func TestInfoDialogOKReturnsNil(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewInfo("hello world")
	b := d.build(app)

	if b.win.Title() != "Info" {
		t.Errorf("title = %q, want %q", b.win.Title(), "Info")
	}
	if b.okButton == nil {
		t.Fatal("expected an OK button")
	}
	if b.cancelButton != nil {
		t.Error("info dialog should not have a Cancel button")
	}

	if res := tapAndWait(t, b.okButton, b.resultCh); res != nil {
		t.Errorf("info dialog OK result = %#v, want nil", res)
	}
}

func TestErrorDialogTitle(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewError("boom")
	b := d.build(app)

	if b.win.Title() != "Error" {
		t.Errorf("title = %q, want %q", b.win.Title(), "Error")
	}

	tapAndWait(t, b.okButton, b.resultCh)
}

func TestSetTitleOverridesDefault(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewInfo("hello").SetTitle("Heads up")
	b := d.build(app)

	if b.win.Title() != "Heads up" {
		t.Errorf("title = %q, want %q", b.win.Title(), "Heads up")
	}
	tapAndWait(t, b.okButton, b.resultCh)
}

func TestCustomDialogOKCollectsTypedResult(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddProperty("message", "A custom dialog", PropertyLabel).
		AddProperty("boolean", "boolean", PropertyBool).
		AddProperty("textField", "textField", PropertyTextField).
		AddProperty("int", "int", PropertyInt)
	b := d.build(app)

	if b.okButton == nil || b.cancelButton == nil {
		t.Fatal("expected both OK and Cancel buttons")
	}
	if _, ok := b.fields["message"]; ok {
		t.Error("a label-only property should not produce an input field")
	}

	b.fields["boolean"].(*widget.Check).SetChecked(true)
	b.fields["textField"].(*widget.Entry).SetText("hello")
	b.fields["int"].(*widget.Entry).SetText("42")

	res := tapAndWait(t, b.okButton, b.resultCh)

	if res["boolean"] != true {
		t.Errorf("boolean = %#v, want true", res["boolean"])
	}
	if res["textField"] != "hello" {
		t.Errorf("textField = %#v, want %q", res["textField"], "hello")
	}
	if res["int"] != 42 {
		t.Errorf("int = %#v, want 42", res["int"])
	}
	if _, ok := res["message"]; ok {
		t.Error("label-only property should not appear in the result")
	}
}

func TestCustomDialogCancelReturnsNil(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddProperty("int", "int", PropertyInt)
	b := d.build(app)

	b.fields["int"].(*widget.Entry).SetText("7")

	if res := tapAndWait(t, b.cancelButton, b.resultCh); res != nil {
		t.Errorf("cancel result = %#v, want nil", res)
	}
}

func TestCustomDialogDefaultButtonsIsSingleOK(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().AddProperty("int", "int", PropertyInt)
	b := d.build(app)

	if b.okButton == nil {
		t.Fatal("expected a default OK button")
	}
	if b.cancelButton != nil {
		t.Error("default custom dialog should not have a Cancel button")
	}
}

func TestSetButtonsClampsToTwo(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().SetButtons(ButtonOK, ButtonCancel, ButtonOK)
	if len(d.buttons) != 2 {
		t.Fatalf("buttons = %v, want length 2", d.buttons)
	}
}

func TestSetButtonsAndAddPropertyNoOpOnNonCustom(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewInfo("hello").
		SetButtons(ButtonOK, ButtonCancel).
		AddProperty("x", "x", PropertyBool)
	b := d.build(app)

	if b.cancelButton != nil {
		t.Error("SetButtons should have no effect on an info dialog")
	}
	if len(b.fields) != 0 {
		t.Error("AddProperty should have no effect on an info dialog")
	}

	tapAndWait(t, b.okButton, b.resultCh)
}
