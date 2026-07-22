// Command test_terminal is a manual/visual entry point: a small master
// window with two buttons exercising go-ux/terminal — "Open Settings" (the
// Terminal node seeded by terminal.RegisterSettings, shown in a
// go-ux/settings window) and "Open Terminal" (a terminal.Window with one tab
// per detected shell, its default shell/close-on-exit sourced from that same
// registry). Run with `go run ./terminaldemo`.
//
// The master window exists for the same reason dialogdemo's does: Fyne's
// driver quits (and tears down its windowing library) once the last open
// window closes. Both buttons here open non-blocking windows the user can
// close independently (unlike go-ux/dialog's blocking Show), but without a
// window that stays open the whole time, closing every terminal/settings
// window at once would still tear the app down — the master window anchors
// it regardless of what the user opens or closes.
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

	"go-ux/db"
	"go-ux/settings"
	"go-ux/terminal"
)

func main() {
	database, err := db.Open(":memory:")
	if err != nil {
		log.Fatalf("open in-memory db: %v", err)
	}
	defer database.Close()

	// Declares this app (and everything it calls into, including go-ux/
	// terminal's own background goroutines) as fully using the fyne.Do
	// threading model — without it, Run prints a "not migrated" warning on
	// every launch even though widget.go's readLoop/refreshLoop/blinkLoop/
	// waitLoop already only ever touch CanvasObjects via fyne.Do (see
	// widget.go's uiMu/doUI). See https://docs.fyne.io/started/goroutines.
	app.SetMetadata(fyne.AppMetadata{Migrations: map[string]bool{"fyneDo": true}})
	fyneApp := app.NewWithID("go-ux.test-terminal")

	master := fyneApp.NewWindow("Terminal Demo")

	// Both settings.NewWindow and terminal.NewWindowFromSettings are
	// non-blocking (Show just displays the window and returns), so — unlike
	// dialogdemo's Show calls — these run directly on the button-callback
	// goroutine (the Fyne UI goroutine) with no `go func() {...}()` wrapper.
	showSettings := widget.NewButton("Open Settings", func() {
		if err := terminal.RegisterSettings(database); err != nil {
			log.Printf("register terminal settings: %v", err)
			return
		}
		win, err := settings.NewWindow(fyneApp, database)
		if err != nil {
			log.Printf("build settings window: %v", err)
			return
		}
		win.Show()
	})

	showTerminal := widget.NewButton("Open Terminal", func() {
		// NewWindowFromSettings (not the plain NewWindow) so the terminal
		// picks up default_shell/close_on_exit from the registry when
		// "Open Settings" has seeded/edited them — falling back to
		// DetectShells()'s own ordering and close-on-exit off if
		// RegisterSettings was never called (e.g. this button clicked
		// before "Open Settings").
		win, err := terminal.NewWindowFromSettings(fyneApp, database)
		if err != nil {
			log.Printf("build terminal window: %v", err)
			return
		}
		win.Show()
	})

	master.SetContent(container.NewVBox(showSettings, showTerminal))
	master.Show()

	fyneApp.Run()
}
