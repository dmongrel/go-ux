// Command test_dialog is a manual/visual entry point: it shows one of each
// go-ux/dialog kind (info, error, custom) in turn. Run with
// `go run ./dialogdemo`.
//
// It lives in its own directory rather than at the repo root next to
// test_settings.go because a directory can only have one `package main`
// with one func main; go-ux's own build (`go build ./...`) would otherwise
// fail with a duplicate main.
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

	"go-ux/dialog"
)

func main() {
	fyneApp := app.NewWithID("go-ux.test-dialog")

	// Show blocks until the user responds, so it must run off the
	// goroutine that calls fyneApp.Run().
	go func() {
		dialog.NewInfo("This is an informational message.").Show(fyneApp)

		dialog.NewError("Something went wrong.").Show(fyneApp)

		result := dialog.NewCustom().
			SetTitle("Custom").
			SetButtons(dialog.ButtonOK, dialog.ButtonCancel).
			AddProperty("message", "A custom dialog", dialog.PropertyLabel).
			AddProperty("boolean", "boolean", dialog.PropertyBool).
			AddProperty("textField", "textField", dialog.PropertyTextField).
			AddProperty("int", "int", dialog.PropertyInt).
			AddPropertyList("items", "items", []string{"a", "b"}).
			AddPropertyOptions("dropdown", "dropdown", dialog.PropertyDropdown, []string{"x", "y", "z"}, []string{"y"}).
			AddPropertyOptions("tags", "tags", dialog.PropertyMultiSelect, []string{"a", "b", "c"}, []string{"a"}).
			Show(fyneApp)
		log.Printf("custom dialog result: %#v", result)

		fyneApp.Quit()
	}()

	fyneApp.Run()
}
