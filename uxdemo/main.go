// Command uxdemo is go-ux's manual/visual verification vehicle for its
// Wails v3 components — the replacement for the four separate Fyne demos
// this repo used to ship (test_settings.go, dialogdemo/, terminaldemo/,
// editorsdemo/). A Wails app is one Go process bound to one compiled
// frontend bundle, so unlike those `go run`-able Fyne demos, every
// component now shares this single entry point: a Hub window opens at
// startup, and each go-ux package registers its own Service here and opens
// itself in a separate window sharing this process's backend/bindings.
//
// Build/run: `wails3 build` from this directory, then run bin/uxdemo.exe.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dmongrel/go-ux/db"
	"github.com/dmongrel/go-ux/dialog"
	"github.com/dmongrel/go-ux/editors"
	"github.com/dmongrel/go-ux/settings"
	"github.com/dmongrel/go-ux/terminal"
	"github.com/dmongrel/go-ux/test"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "uxdemo",
		Description: "go-ux Wails v3 component demo",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	// In-memory db seeded with the same example Terminal/Version Control
	// data test_settings.go used to seed for its Fyne demo — ephemeral per
	// run, matching that original demo's behavior.
	database, err := db.Open(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	if err := test.SeedExample(database); err != nil {
		log.Fatal(err)
	}
	if err := terminal.RegisterSettings(database); err != nil {
		log.Fatal(err)
	}
	if err := editors.RegisterSettings(database, "uxdemo.editor"); err != nil {
		log.Fatal(err)
	}

	settingsService, err := settings.NewService(app, database)
	if err != nil {
		log.Fatal(err)
	}
	terminalService := terminal.NewService(app, database)
	editorService := editors.NewService(app, database, "uxdemo.editor")

	app.RegisterService(application.NewService(dialog.NewService(app)))
	app.RegisterService(application.NewService(settingsService))
	app.RegisterService(application.NewService(terminalService))
	app.RegisterService(application.NewService(editorService))

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "go-ux demo",
		Width:            480,
		Height:           360,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#hub",
	})

	app.OnShutdown(func() {
		terminalService.Close()
		editorService.Close()
		database.Close()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
