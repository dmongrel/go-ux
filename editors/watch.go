package editors

import (
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
)

// startWatching adds tab's FilePath to s's shared *fsnotify.Watcher
// (creating it and its background read-loop on first use), so external
// changes to that file drive s.fileWatchMode's auto-reload/notify
// behavior (see handleFileChanged). A no-op for a tab with no FilePath
// (an in-memory-only tab), a path already being watched (watchedFiles
// dedups), or a path that doesn't exist on disk yet (fsnotify.Add's own
// error is logged and otherwise ignored, not fatal).
//
// One *fsnotify.Watcher per Service (not one per Document) — matches the
// original Group's own simplification (see its doc comment, preserved
// here): fsnotify's Watcher already supports watching multiple paths and
// delivers all their events on one shared channel, so a single Watcher
// for the Service's lifetime is simpler than per-Document lifecycle
// management for the same result.
func (s *Service) startWatching(tab *Tab) {
	if tab.FilePath == "" {
		return
	}
	if s.watchedFiles[tab.FilePath] {
		return
	}

	if s.watcher == nil {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("editors: create file watcher: %v", err)
			return
		}
		s.watcher = w
		go s.watchLoop(w)
	}

	if err := s.watcher.Add(tab.FilePath); err != nil {
		return // file doesn't exist yet, or some other non-fatal issue — not watched, not an error to the caller
	}
	s.watchedFiles[tab.FilePath] = true
}

// watchLoop runs on its own goroutine for the lifetime of w, dispatching
// write/create events to handleFileChanged. Returns once w's channels are
// closed (Close).
func (s *Service) watchLoop(w *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			s.handleFileChanged(event.Name)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("editors: file watcher: %v", err)
		}
	}
}

// handleFileChanged reacts to path having changed on disk: in
// FileWatchModeAuto, silently reloads the matching tab's Document unless
// it has unsaved edits (Dirty) — matching edits are never silently
// clobbered, falling back to the notify behavior below instead. In
// FileWatchModeNotify (or the auto-but-dirty fallback), emits the
// "editors:filechanged" event with the tab's ID so every frontend window
// showing it can offer Load-from-Disk/Keep-from-Memory — the Wails
// equivalent of the old south bar's SouthBarFileChanged mode.
func (s *Service) handleFileChanged(path string) {
	tab, ok := s.findTabByPath(path)
	if !ok {
		return
	}

	if s.fileWatchMode == FileWatchModeAuto && !tab.Dirty() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		tab.Doc.SetText(string(data))
		return
	}

	if s.app != nil {
		s.app.Event.Emit("editors:filechanged", tab.ID)
	}
}

// Close stops s's background file watcher, if one was ever started. Safe
// to call even if no file was ever watched.
func (s *Service) Close() {
	if s.watcher != nil {
		s.watcher.Close()
	}
}
