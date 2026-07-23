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
	"strings"

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

// loremIpsumParagraphs is placeholder body text for the seeded demo tabs —
// three paragraphs, long enough to exercise wrapping/scrolling behavior in
// the content area once Phase 2 adds a real text backend.
const loremIpsumParagraphs = `Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.

Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Curabitur pretium tincidunt lacus, at velit vehicula viverra. Nullam vitae congue lorem. Aliquam pharetra libero non ipsum congue, in tincidunt neque tincidunt. Vestibulum ante ipsum primis in faucibus orci luctus et ultrices posuere cubilia curae.

Sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium doloremque laudantium, totam rem aperiam, eaque ipsa quae ab illo inventore veritatis et quasi architecto beatae vitae dicta sunt explicabo. Nemo enim ipsam voluptatem quia voluptas sit aspernatur aut odit aut fugit, sed quia consequuntur magni dolores eos qui ratione voluptatem sequi nesciunt.`

// loremIpsum repeats loremIpsumParagraphs several times over — a single
// pass of the three paragraphs turned out too short to reliably overflow
// the content area's viewport, which is the whole point of this
// placeholder text (exercising scrollbars/growth behavior).
var loremIpsum = strings.Repeat(loremIpsumParagraphs+"\n\n", 5)

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
		group.AddTab(editors.NewTab("tab-1", "Tab 1", "tab-1.txt", loremIpsum))
		group.AddTab(editors.NewTab("tab-2", "Tab 2", "tab-2.txt", loremIpsum))
		group.AddTab(editors.NewTab("tab-3", "Tab 3", "tab-3.txt", loremIpsum))
	}

	win := fyneApp.NewWindow("Editors Demo")
	win.SetContent(group)
	win.Resize(fyne.NewSize(1024, 700))
	win.Show()

	fyneApp.Run()
}
