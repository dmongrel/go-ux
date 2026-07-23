// Command editorsdemo is a manual/visual entry point for go-ux/editors: a
// window embedding one editors.Group, pre-opened with 4 tabs (3 plain-text
// placeholders plus one real Markdown file), plus an "Open File..." button
// (this demo's own, since editors.Group.OpenFile has no picker UI of its
// own) so the tab bar, right-click split/move menus, resize-bar dragging,
// live editing, Ctrl+scroll font sizing, Ctrl+S save, Markdown preview
// toggle, opening arbitrary files, file watching, and persisted layout can
// all be exercised by hand. Diff review (ProposeDiff) isn't wired to any
// UI here yet — it's meant to be driven by a host app's own AI tooling,
// not a manual demo action. Run with `go run ./editorsdemo`.
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
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

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

// markdownDemoText is seeded into tab-4.md (a real .md FilePath) so the
// tab bar's Preview/Edit toggle button — hidden for the other, plain-text
// tabs — has something to actually toggle in this demo.
const markdownDemoText = `# Chapter One

This is a **bold** claim, and this is *italicized* prose. Here is some ` + "`inline code`" + ` for good measure.

## A Sub-Heading

- first item
- second item
- third item

> A blockquote, for a character's aside.

` + "```" + `
fmt.Println("a fenced code block")
` + "```" + `

See [the go-ux repo](https://github.com/) for more.

---

Text after a thematic break.
`

func main() {
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db at %s: %v", dbPath, err)
	}
	defer database.Close()

	// See terminaldemo's identical call for why this is here: declares this
	// app as fully using the fyne.Do threading model, avoiding a "not
	// migrated" warning on every launch — file watching's background
	// goroutine (watch.go) is exactly the kind of off-main-goroutine UI
	// work this exists for.
	app.SetMetadata(fyne.AppMetadata{Migrations: map[string]bool{"fyneDo": true}})
	fyneApp := app.NewWithID("go-ux.editorsdemo")

	// Idempotent — seeds the "Editors: <groupID>" settings node (font
	// size, file_watch_mode) on a fresh db, no-ops if already present.
	if err := editors.RegisterSettings(database, groupID); err != nil {
		log.Printf("editors.RegisterSettings: %v", err)
	}

	group := editors.NewGroupFromSettings(fyneApp, database, groupID)
	defer group.Close() // stops the file-watching goroutine on exit

	// Seed 3 placeholder tabs into the primary pane only on a genuinely
	// fresh db (nothing persisted yet for groupID) — on a second run
	// against the same dbPath, whatever layout was left last time (however
	// many tabs, however split) is what NewGroupFromSettings already
	// restored, and re-seeding here would just add 3 more on top of that.
	if existingPanes, _, loadErr := database.LoadEditorLayout(groupID); loadErr == nil && len(existingPanes) == 0 {
		group.AddTab(editors.NewTab("tab-1", "Tab 1", "tab-1.txt", loremIpsum))
		group.AddTab(editors.NewTab("tab-2", "Tab 2", "tab-2.txt", loremIpsum))
		group.AddTab(editors.NewTab("tab-3", "Tab 3", "tab-3.txt", loremIpsum))
		group.AddTab(editors.NewTab("tab-4", "Chapter One.md", "tab-4.md", markdownDemoText))
	}

	win := fyneApp.NewWindow("Editors Demo")

	// A file-open button — group.OpenFile itself is a plain Go API with no
	// picker UI of its own (by design: the editors package leaves file
	// choosing to the host app, see editors.md's "Diff review
	// (mcp_tooling)" section), so this demo needs to supply one to let
	// OpenFile actually be exercised by hand rather than only from Go code.
	openBtn := widget.NewButton("Open File...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, win)
				return
			}
			if reader == nil {
				return // user cancelled
			}
			defer reader.Close()

			path := reader.URI().Path()
			if _, err := group.OpenFile(path); err != nil {
				dialog.ShowError(err, win)
			}
		}, win)
	})

	win.SetContent(container.NewBorder(openBtn, nil, nil, nil, group))
	win.Resize(fyne.NewSize(1024, 700))
	win.Show()

	fyneApp.Run()
}
