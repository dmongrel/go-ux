package editors

import (
	"os"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

// These tests call handleFileChanged directly rather than relying on a
// genuine fsnotify event to arrive asynchronously from the real
// background watcher goroutine — Fyne's test driver's fyne.Do runs its
// callback inline, synchronously, on whatever goroutine called it (see
// fyne.io/fyne/v2/test.driver.DoFromGoroutine), so a real event racing
// with the test's own goroutine is a genuine, observed concurrent-widget-
// mutation crash (Fyne's internal font-shaping cache is not safe for
// that), not just a hypothetical one. Every test below calls g.Close()
// immediately after opening the file and BEFORE writing to it, so the
// real watcher's goroutine has already exited (no queued event, nothing
// left to race) by the time the test does its own write + synchronous
// handleFileChanged call. The real watchLoop→genuine-fsnotify-event path
// itself is accepted as manual/visual-only verification, same as this
// repo's other OS-integration behavior (e.g. terminal's PTY backend).

func TestStartWatchingSkipsTabsWithNoFilePath(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	tab := NewTab("t1", "untitled", "", "some text")
	g.startWatching(tab)

	if g.watcher != nil {
		t.Errorf("watcher was created for a tab with no FilePath")
	}
}

func TestStartWatchingDedupsSamePath(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	path := writeTempFile(t, "chapter1.txt", "hello")
	tabA := NewTab("a", "chapter1.txt", path, "hello")
	tabB := NewTab("b", "chapter1.txt", path, "hello")

	g.startWatching(tabA)
	g.startWatching(tabB)
	g.Close() // stop the real watcher goroutine now — nothing else in this test touches the file

	if len(g.watchedFiles) != 1 {
		t.Errorf("watchedFiles has %d entries, want 1 (same path watched twice)", len(g.watchedFiles))
	}
}

func TestStartWatchingNonExistentFileDoesNotError(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	// A demo-style placeholder path that doesn't exist on disk — must not
	// panic or otherwise break anything; it's simply left unwatched.
	tab := NewTab("t1", "tab-1.txt", "tab-1.txt", "placeholder")
	g.startWatching(tab)

	if g.watchedFiles["tab-1.txt"] {
		t.Errorf("a non-existent path was recorded as watched")
	}
}

func TestHandleFileChangedAutoModeReloadsCleanDocument(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	g.fileWatchMode = FileWatchModeAuto

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close() // stop the real watcher before the write below — see the file doc comment

	if err := os.WriteFile(path, []byte("changed on disk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g.handleFileChanged(path)

	if tab.Doc.Text() != "changed on disk" {
		t.Errorf("Doc.Text() = %q, want %q", tab.Doc.Text(), "changed on disk")
	}
	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden (auto mode reloads silently)", g.primary.southBar.Mode())
	}
}

func TestHandleFileChangedAutoModeFallsBackToNotifyWhenDirty(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	g.fileWatchMode = FileWatchModeAuto

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close()
	tab.Doc.SetText("unsaved local edit") // dirties the Document

	if err := os.WriteFile(path, []byte("changed on disk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g.handleFileChanged(path)

	if tab.Doc.Text() != "unsaved local edit" {
		t.Errorf("Doc.Text() = %q, want unchanged %q (dirty must not be silently clobbered)", tab.Doc.Text(), "unsaved local edit")
	}
	if g.primary.southBar.Mode() != SouthBarFileChanged {
		t.Errorf("southBar.Mode() = %v, want SouthBarFileChanged (auto-but-dirty falls back to notify)", g.primary.southBar.Mode())
	}
}

func TestHandleFileChangedNotifyModeShowsSouthBar(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	g.fileWatchMode = FileWatchModeNotify

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close()

	if err := os.WriteFile(path, []byte("changed on disk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g.handleFileChanged(path)

	if tab.Doc.Text() != "original" {
		t.Errorf("Doc.Text() = %q, want unchanged %q (notify mode never auto-reloads)", tab.Doc.Text(), "original")
	}
	if g.primary.southBar.Mode() != SouthBarFileChanged {
		t.Errorf("southBar.Mode() = %v, want SouthBarFileChanged", g.primary.southBar.Mode())
	}
}

func TestFileChangedNoticeLoadFromDiskAppliesNewText(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	g.fileWatchMode = FileWatchModeNotify

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close()
	if err := os.WriteFile(path, []byte("changed on disk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g.handleFileChanged(path)

	g.primary.southBar.onPrimary() // "Load from Disk"

	if tab.Doc.Text() != "changed on disk" {
		t.Errorf("Doc.Text() = %q, want %q after Load from Disk", tab.Doc.Text(), "changed on disk")
	}
	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden after Load from Disk", g.primary.southBar.Mode())
	}
}

func TestFileChangedNoticeKeepFromMemoryLeavesDocumentUntouched(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	g.fileWatchMode = FileWatchModeNotify

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close()
	if err := os.WriteFile(path, []byte("changed on disk"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g.handleFileChanged(path)

	g.primary.southBar.onSecondary() // "Keep from Memory"

	if tab.Doc.Text() != "original" {
		t.Errorf("Doc.Text() = %q, want unchanged %q after Keep from Memory", tab.Doc.Text(), "original")
	}
	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden after Keep from Memory", g.primary.southBar.Mode())
	}
}

func TestHandleFileChangedUnknownPathIsNoOp(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	// Must not panic even though no tab has this path.
	g.handleFileChanged("/no/such/tab/path.txt")

	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden (nothing should have changed)", g.primary.southBar.Mode())
	}
}
