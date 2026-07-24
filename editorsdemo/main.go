// Command editorsdemo is a manual/visual entry point for go-ux/editors: a
// window embedding one editors.Group, pre-opened with 4 tabs (3 plain-text
// placeholders plus one real Markdown file), plus "Open File..."/"Propose
// Diff..." buttons so the tab bar, right-click split/move menus,
// resize-bar dragging, live editing, Ctrl+scroll font sizing, Ctrl+S save
// (including "Save As" for a tab with no FilePath yet), Markdown preview
// toggle, opening arbitrary files, file watching, persisted layout, and
// diff review can all be exercised by hand. Run with `go run ./editorsdemo`.
//
// "Open File..." and "Propose Diff..." are purely this test harness's own
// stand-ins, not part of editors' real API surface (editors.Group.OpenFile/
// ProposeDiff have no picker UI of their own by design — see mcptooling.go).
// The intended real host app triggers these through its own menu (e.g.
// File -> Open), not a demo button; this file exists only to exercise the
// underlying Group methods by hand.
//
// File/save pickers use github.com/ncruces/zenity — the platform's native
// system dialog — rather than Fyne's own built-in (non-native) file
// dialog, so what a host app's real File -> Open would show is exercised
// here too. zenity's calls block the calling goroutine (they run a real
// native dialog process/window), so they're always called via `go func()`
// off the Fyne UI goroutine, with the result marshaled back via fyne.Do
// per this repo's threading convention (CLAUDE.md).
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
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/ncruces/zenity"

	"go-ux/db"
	"go-ux/editors"
)

// pickFileOpen runs zenity's native "open file" dialog on its own
// goroutine (see the package doc comment on why) and, if the user picked a
// file, calls onPicked back on the Fyne UI goroutine via fyne.Do. A silent
// no-op on cancel; any other error shows in a Fyne error dialog (an error
// dialog isn't a *file* dialog, so it stays Fyne-native here).
func pickFileOpen(win fyne.Window, title string, onPicked func(path string)) {
	go func() {
		path, err := zenity.SelectFile(zenity.Title(title))
		if err != nil {
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() { dialog.ShowError(err, win) })
			return
		}
		fyne.Do(func() { onPicked(path) })
	}()
}

// pickFileSave mirrors pickFileOpen for zenity's native "save file" dialog,
// pre-filling defaultName as the suggested filename and confirming before
// overwriting an existing file.
func pickFileSave(win fyne.Window, title, defaultName string, onPicked func(path string)) {
	go func() {
		path, err := zenity.SelectFileSave(zenity.Title(title), zenity.Filename(defaultName), zenity.ConfirmOverwrite())
		if err != nil {
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() { dialog.ShowError(err, win) })
			return
		}
		fyne.Do(func() { onPicked(path) })
	}()
}

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

	// A file-open button standing in for a host app's real File -> Open
	// menu item (see the package doc comment) — group.OpenFile itself is a
	// plain Go API with no picker UI of its own.
	openBtn := widget.NewButton("Open File...", func() {
		pickFileOpen(win, "Open File", func(path string) {
			if _, err := group.OpenFile(path); err != nil {
				dialog.ShowError(err, win)
			}
		})
	})

	// Ctrl+S on a tab with no FilePath (e.g. one seeded in memory, never
	// opened from disk) calls this instead of writing silently nowhere —
	// same "host supplies the picker" reasoning as openBtn above.
	group.OnSaveAsRequested = func(tab *editors.Tab) {
		pickFileSave(win, "Save As", tab.Title, func(path string) {
			if err := group.SaveTabAs(tab, path); err != nil {
				dialog.ShowError(err, win)
			}
		})
	}

	// Stands in for a host app's AI-assistant tooling: picks a file (same
	// native picker as openBtn), reads its current content, and lets the
	// person running the demo type replacement text — then calls
	// ProposeDiff so the resulting Accept/Cancel south-bar flow can be
	// exercised by hand. A real caller (e.g. Claude Code's own /ide-style
	// integration in the host app) would supply newText itself and skip
	// this button/dialog entirely, calling group.ProposeDiff directly. The
	// replacement-text entry itself stays a Fyne dialog (zenity has no
	// editable multi-line text widget) — only the file *picker* switched
	// to the native one.
	diffBtn := widget.NewButton("Propose Diff...", func() {
		pickFileOpen(win, "Propose Diff: choose a file", func(path string) {
			oldText, err := os.ReadFile(path)
			if err != nil {
				dialog.ShowError(err, win)
				return
			}

			entry := widget.NewMultiLineEntry()
			entry.SetText(string(oldText))
			entry.Wrapping = fyne.TextWrapWord

			editDialog := dialog.NewCustomConfirm("Propose Diff: "+filepath.Base(path),
				"Propose", "Cancel", entry, func(confirmed bool) {
					if !confirmed {
						return
					}
					if err := group.ProposeDiff(path, entry.Text); err != nil {
						dialog.ShowError(err, win)
					}
				}, win)
			editDialog.Resize(fyne.NewSize(600, 500))
			editDialog.Show()
		})
	})

	win.SetContent(container.NewBorder(container.NewHBox(openBtn, diffBtn), nil, nil, nil, group))
	win.Resize(fyne.NewSize(1024, 700))
	win.Show()

	fyneApp.Run()
}
