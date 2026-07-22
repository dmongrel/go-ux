// Command test_dialog is a manual/visual entry point: a small master window
// with one button per go-ux/dialog kind (info, error, custom). Run with
// `go run ./dialogdemo`.
//
// The master window exists because Fyne's driver quits (and tears down its
// windowing library) once the last open window closes; since dialog.Show
// opens and closes its own window per call, running dialogs back-to-back
// with no window ever open in between crashes without a persistent window
// to anchor the app.
//
// It lives in its own directory rather than at the repo root next to
// test_settings.go because a directory can only have one `package main`
// with one func main; go-ux's own build (`go build ./...`) would otherwise
// fail with a duplicate main.
package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"go-ux/dialog"
)

func main() {
	// See terminaldemo/test_terminal.go's matching call for why: declares
	// this app as fully using the fyne.Do threading model, silencing Run's
	// "not migrated" warning.
	app.SetMetadata(fyne.AppMetadata{Migrations: map[string]bool{"fyneDo": true}})
	fyneApp := app.NewWithID("go-ux.test-dialog")

	master := fyneApp.NewWindow("Dialog Demo")

	// Show blocks until the user responds, so each must run off the
	// goroutine that calls fyneApp.Run() — including here, off the button
	// callback, which itself runs on that same goroutine.
	showInfo := widget.NewButton("Show Info Dialog", func() {
		go dialog.NewInfo("This is an informational message.").Show(fyneApp)
	})
	showError := widget.NewButton("Show Error Dialog", func() {
		go dialog.NewError("Something went wrong.").Show(fyneApp)
	})
	showCustom := widget.NewButton("Show Custom Dialog", func() {
		go func() {
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
		}()
	})

	master.SetContent(container.NewVBox(showInfo, showError, showCustom))
	master.Show()

	fyneApp.Run()
}
