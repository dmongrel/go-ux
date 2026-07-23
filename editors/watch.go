package editors

import (
	"log"
	"os"

	"fyne.io/fyne/v2"
	"github.com/fsnotify/fsnotify"
)

// startWatching adds tab's FilePath to g's shared *fsnotify.Watcher
// (creating it and its background read-loop on first use), so external
// changes to that file drive g.fileWatchMode's auto-reload/notify
// behavior (see handleFileChanged). A no-op for a tab with no FilePath
// (Phase 1 placeholder-style tabs with no real backing file), a path
// already being watched (watchedFiles dedups — e.g. after a split copies
// the same Tab into a second Pane), or a path that doesn't exist on disk
// yet (fsnotify.Add's own error is logged at debug-ish severity and
// otherwise ignored, not fatal — a caller opening a not-yet-saved new
// file shouldn't break anything else).
//
// One *fsnotify.Watcher per Group (not one per Document, as an earlier
// draft of the design plan proposed) — fsnotify's own Watcher already
// supports watching multiple paths and delivers all their events on one
// shared channel, so a single Watcher per Group is simpler than manually
// managing per-Document watcher lifecycles for the same result. The
// simplification this trades away: a watch is never explicitly removed
// when a Tab's last Pane reference closes (unlike the plan's stated
// "started 0->1, stopped 1->0" per-Document lifecycle) — the watch just
// lives for the Group's lifetime once started. Acceptable for now: a few
// extra held file handles for the process lifetime, not a resource leak
// that grows unbounded, and Phase 1/2 has no explicit Group.Close to hook
// a real teardown into yet anyway.
func (g *Group) startWatching(tab *Tab) {
	if tab.FilePath == "" {
		return
	}
	if g.watchedFiles == nil {
		g.watchedFiles = make(map[string]bool)
	}
	if g.watchedFiles[tab.FilePath] {
		return
	}

	if g.watcher == nil {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("editors: create file watcher: %v", err)
			return
		}
		g.watcher = w
		go g.watchLoop(w)
	}

	if err := g.watcher.Add(tab.FilePath); err != nil {
		return // file doesn't exist yet, or some other non-fatal issue — not watched, not an error to the caller
	}
	g.watchedFiles[tab.FilePath] = true
}

// watchLoop runs on its own goroutine for the lifetime of w, dispatching
// write/create events back onto the UI goroutine (fyne.Do — CLAUDE.md's
// Fyne conventions mandate this for any UI-affecting code off the main
// goroutine) via handleFileChanged. Returns once w's channels are closed.
func (g *Group) watchLoop(w *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			path := event.Name
			fyne.Do(func() {
				g.handleFileChanged(path)
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("editors: file watcher: %v", err)
		}
	}
}

// handleFileChanged reacts to path having changed on disk: in
// FileWatchModeAuto, silently reloads the matching Tab's Document unless
// it has unsaved edits (Dirty) — matching edits are never silently
// clobbered, falling back to the notify behavior below instead, per the
// design plan. In FileWatchModeNotify (or the auto-but-dirty fallback),
// every Pane currently showing the Tab switches its south bar into
// SouthBarFileChanged mode (pane.go's showFileChangedNotice) instead of
// changing anything automatically.
func (g *Group) handleFileChanged(path string) {
	tab, ok := g.findTabByPath(path)
	if !ok {
		return
	}

	if g.fileWatchMode == FileWatchModeAuto && !tab.Dirty() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		tab.Doc.SetText(string(data))
		return
	}

	walkPanes(g.root, func(p *Pane) {
		if p.active == tab {
			p.showFileChangedNotice(tab)
		}
	})
}
