// Command editorsdemo is a manual/visual entry point for go-ux/editors'
// Phase 1 layout shell: a window embedding one editors.Group, pre-opened
// with 3 placeholder tabs, so the tab bar, right-click split/move menus,
// resize-bar dragging, and persisted layout can all be exercised by hand
// before the deeper backend (real text editing, diff review, markdown
// preview, font settings, file watching) exists. Run with
// `go run ./editorsdemo`.
//
// It lives in its own directory rather than at the repo root, for the
// same one-`package main`-per-directory reason as dialogdemo/terminaldemo
// (test_settings.go already occupies that slot at the repo root).
//
// Uses a file-based db (not :memory:) specifically so layout persistence
// can be verified by running this command twice in a row: split/move tabs
// around, close the window, run it again — the same layout should come
// back exactly as it was left.
package main

import (
	"log"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"go-ux/db"
	"go-ux/editors"
)

// dbPath is fixed (not a fresh temp file per run) so two consecutive
// `go run ./editorsdemo` invocations share the same persisted layout —
// see the package doc comment.
var dbPath = filepath.Join(os.TempDir(), "go-ux-editorsdemo.sqlite")

// groupID identifies this demo's one Group instance for editors' own
// layout persistence (editors.NewGroupFromSettings' groupID parameter) —
// analogous to settings.Window's hardcoded componentID or treestate.Track's
// caller-chosen id.
const groupID = "editorsdemo.main"

func main() {
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db at %s: %v", dbPath, err)
	}
	defer database.Close()

	// See terminaldemo's identical call for why this is here: declares
	// this app as fully using the fyne.Do threading model, avoiding a
	// "not migrated" warning on every launch. editors' Phase 1 code has no
	// background goroutines of its own yet, but this is cheap insurance
	// against Phase 2 (file watching, etc.) needing it later without
	// anyone remembering to add it then.
	app.SetMetadata(fyne.AppMetadata{Migrations: map[string]bool{"fyneDo": true}})
	fyneApp := app.NewWithID("go-ux.editorsdemo")

	group := editors.NewGroupFromSettings(fyneApp, database, groupID)

	// Seed 3 placeholder tabs into the primary pane only on a genuinely
	// fresh db (nothing persisted yet for groupID) — on a second run
	// against the same dbPath, whatever layout was left last time (however
	// many tabs, however split) is what NewGroupFromSettings already
	// restored, and re-seeding here would just add 3 more on top of that.
	if existingPanes, _, loadErr := database.LoadEditorLayout(groupID); loadErr == nil && len(existingPanes) == 0 {
		group.AddTab(editors.NewTab("tab-1", "Tab 1", "tab-1.txt", "First placeholder tab.\n\nRight-click the tab bar to try Split Right / Split Down / Move Right / Move Down."))
		group.AddTab(editors.NewTab("tab-2", "Tab 2", "tab-2.txt", "Second placeholder tab — some dummy body text.\n\nThis is a novel-writing-style paragraph of placeholder prose, long enough to show word wrap in the (currently read-only) content area."))
		group.AddTab(editors.NewTab("tab-3", "Tab 3", "tab-3.txt", "Third placeholder tab."))
	}

	win := fyneApp.NewWindow("Editors Demo")
	win.SetContent(group)
	win.Resize(fyne.NewSize(1024, 700))
	win.Show()

	fyneApp.Run()
}
