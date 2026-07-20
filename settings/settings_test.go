package settings_test

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"

	"go-ux/settings"
	"go-ux/test"
)

func TestNewWindowBuildsTreeFromRegistry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	win, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if win == nil {
		t.Fatal("NewWindow returned nil window")
	}
}

func TestSetSizeIsChainableAndReturnsWindow(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	app := fynetest.NewApp()
	defer app.Quit()

	win, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	if got := win.SetSize(640, 480); got != win {
		t.Errorf("SetSize returned %#v, want the same *Window for chaining", got)
	}
	// Non-positive values must not panic and must remain no-ops.
	win.SetSize(0, 480).SetSize(640, 0).SetSize(-1, -1)
}

func TestNewWindowWithEmptyRegistry(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	app := fynetest.NewApp()
	defer app.Quit()

	if _, err := settings.NewWindow(app, d); err != nil {
		t.Fatalf("NewWindow with empty registry: %v", err)
	}
}
