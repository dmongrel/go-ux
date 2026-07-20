// Command test_settings is a manual/visual entry point: it seeds an
// in-memory registry with example Terminal and Version Control settings and
// opens the go-ux settings window against it. Run with `go run test_settings.go`.
package main

import (
	"log"

	"fyne.io/fyne/v2/app"

	"go-ux/settings"
	"go-ux/test"
)

func main() {
	d, err := test.NewDB()
	if err != nil {
		log.Fatalf("open test db: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		log.Fatalf("seed example data: %v", err)
	}

	fyneApp := app.NewWithID("go-ux.test-settings")

	win, err := settings.NewWindow(fyneApp, d)
	if err != nil {
		log.Fatalf("build settings window: %v", err)
	}
	win.Show()

	fyneApp.Run()
}
