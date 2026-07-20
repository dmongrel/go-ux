package dialog

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// findAll recursively walks a fyne.CanvasObject tree (through *fyne.Container
// and *container.Scroll, the two container kinds used by this package's
// dialog layouts) and collects every object for which match returns true.
func findAll(root fyne.CanvasObject, match func(fyne.CanvasObject) bool) []fyne.CanvasObject {
	var found []fyne.CanvasObject
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil {
			return
		}
		if match(o) {
			found = append(found, o)
		}
		switch c := o.(type) {
		case *fyne.Container:
			for _, child := range c.Objects {
				walk(child)
			}
		case *container.Scroll:
			walk(c.Content)
		}
	}
	walk(root)
	return found
}

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

func TestAddPropertyListNoOpOnNonCustom(t *testing.T) {
	d := NewInfo("hello").AddPropertyList("items", "Items", []string{"a", "b"})
	if len(d.props) != 0 {
		t.Errorf("AddPropertyList should have no effect on an info dialog, got %d props", len(d.props))
	}
}

func TestAddPropertyOptionsNoOpOnNonCustom(t *testing.T) {
	d := NewInfo("hello").AddPropertyOptions("choice", "Choice", PropertyDropdown, []string{"x", "y"}, nil)
	if len(d.props) != 0 {
		t.Errorf("AddPropertyOptions should have no effect on an info dialog, got %d props", len(d.props))
	}
}

func TestAddPropertyListStoresInitialItems(t *testing.T) {
	d := NewCustom().AddPropertyList("items", "Items", []string{"a", "b"})
	if len(d.props) != 1 {
		t.Fatalf("expected 1 prop, got %d", len(d.props))
	}
	p := d.props[0]
	if p.key != "items" || p.label != "Items" || p.kind != PropertyList {
		t.Errorf("unexpected property: %#v", p)
	}
	if len(p.initial) != 2 || p.initial[0] != "a" || p.initial[1] != "b" {
		t.Errorf("initial = %#v, want [a b]", p.initial)
	}
}

func TestAddPropertyOptionsStoresOptionsAndSelected(t *testing.T) {
	d := NewCustom().AddPropertyOptions("choice", "Choice", PropertyDropdown, []string{"x", "y"}, []string{"y"})
	if len(d.props) != 1 {
		t.Fatalf("expected 1 prop, got %d", len(d.props))
	}
	p := d.props[0]
	if p.key != "choice" || p.label != "Choice" || p.kind != PropertyDropdown {
		t.Errorf("unexpected property: %#v", p)
	}
	if len(p.options) != 2 || p.options[0] != "x" || p.options[1] != "y" {
		t.Errorf("options = %#v, want [x y]", p.options)
	}
	if len(p.selected) != 1 || p.selected[0] != "y" {
		t.Errorf("selected = %#v, want [y]", p.selected)
	}
}

func TestCustomDialogListDefaultsToInitialItems(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyList("items", "Items", []string{"a", "b"})
	b := d.build(app)

	res := tapAndWait(t, b.okButton, b.resultCh)

	items, ok := res["items"].([]string)
	if !ok {
		t.Fatalf("items = %#v, want []string", res["items"])
	}
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Errorf("items = %#v, want [a b]", items)
	}
}

func TestCustomDialogListReflectsLiveEdits(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyList("items", "Items", []string{"a"})
	b := d.build(app)

	data, ok := b.fields["items"].(binding.StringList)
	if !ok {
		t.Fatalf("fields[items] = %#v, want binding.StringList", b.fields["items"])
	}
	if err := data.Append("c"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := data.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	res := tapAndWait(t, b.okButton, b.resultCh)

	items, ok := res["items"].([]string)
	if !ok {
		t.Fatalf("items = %#v, want []string", res["items"])
	}
	if len(items) != 1 || items[0] != "c" {
		t.Errorf("items = %#v, want [c]", items)
	}
}

// TestCustomDialogListRemoveByIndexHandlesDuplicates covers the bug where
// removing by value (data.Remove(items[selected])) deleted the first
// matching occurrence instead of the selected one. The Remove button
// handler now removes by index (get items, splice out the selected index,
// data.Set the result). This test drives the real widgets: it locates the
// actual *widget.List and "Remove" *widget.Button inside the built dialog's
// window content, selects an item in the real list, and taps the real
// button, so it would fail against the old by-value implementation.
//
// Seed data ["a", "b", "a"] with selection at index 2 (the trailing "a") is
// chosen deliberately: the old and new implementations disagree on the
// result.
//   - Old (buggy, by value): data.Remove(items[2]) == data.Remove("a")
//     removes the FIRST "a", at index 0, leaving ["b", "a"].
//   - New (fixed, by index): splicing out index 2 directly leaves ["a", "b"].
//
// The two results differ in both order and which element survives, so this
// seed/selection pair is a real regression guard, unlike ["a", "a", "b"]
// selecting index 1, where by-value and by-index removal coincidentally
// produce the same ["a", "b"] result.
func TestCustomDialogListRemoveByIndexHandlesDuplicates(t *testing.T) {
	app := fynetest.NewApp()
	defer app.Quit()

	d := NewCustom().
		SetButtons(ButtonOK, ButtonCancel).
		AddPropertyList("items", "Items", []string{"a", "b", "a"})
	b := d.build(app)

	var list *widget.List
	var removeButton *widget.Button
	for _, o := range findAll(b.win.Content(), func(fyne.CanvasObject) bool { return true }) {
		switch v := o.(type) {
		case *widget.List:
			list = v
		case *widget.Button:
			if v.Text == "Remove" {
				removeButton = v
			}
		}
	}
	if list == nil {
		t.Fatal("could not find the list widget in the built dialog")
	}
	if removeButton == nil {
		t.Fatal("could not find the Remove button in the built dialog")
	}

	list.Select(2) // the trailing "a", at index 2
	fynetest.Tap(removeButton)

	res := tapAndWait(t, b.okButton, b.resultCh)

	items, ok := res["items"].([]string)
	if !ok {
		t.Fatalf("items = %#v, want []string", res["items"])
	}
	if len(items) != 2 || items[0] != "a" || items[1] != "b" {
		t.Errorf("items = %#v, want [a b] (trailing \"a\" removed by index, not the first \"a\")", items)
	}
}
