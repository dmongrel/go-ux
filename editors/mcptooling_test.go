package editors

import (
	"os"
	"path/filepath"
	"testing"

	fynetest "fyne.io/fyne/v2/test"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestOpenFileReadsFromDisk(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "hello from disk")

	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if tab.Doc.Text() != "hello from disk" {
		t.Errorf("Doc.Text() = %q, want %q", tab.Doc.Text(), "hello from disk")
	}
	if tab.FilePath != path {
		t.Errorf("FilePath = %q, want %q", tab.FilePath, path)
	}
	if tab.Title != "chapter1.txt" {
		t.Errorf("Title = %q, want %q", tab.Title, "chapter1.txt")
	}
}

func TestOpenFileReturnsExistingTabWithoutDuplicating(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "hello")

	tab1, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile (1st): %v", err)
	}
	tab2, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile (2nd): %v", err)
	}

	if tab1 != tab2 {
		t.Errorf("second OpenFile returned a different *Tab, want the same one")
	}
	if len(g.primary.tabs) != 1 {
		t.Errorf("primary pane has %d tabs, want 1 (no duplicate)", len(g.primary.tabs))
	}
}

func TestOpenFileMissingFileReturnsError(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	_, err := g.OpenFile(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err == nil {
		t.Fatalf("OpenFile(missing file) returned nil error")
	}
}

func TestSaveTabWritesCurrentTextToDiskAndMarksClean(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	path := writeTempFile(t, "chapter1.txt", "original")
	tab, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	g.Close() // stop the real watcher before the write below — see watch_test.go's package doc comment

	tab.Doc.SetText("edited text")
	if err := g.SaveTab(tab); err != nil {
		t.Fatalf("SaveTab: %v", err)
	}

	if tab.Dirty() {
		t.Errorf("Dirty() = true after SaveTab, want false")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if string(onDisk) != "edited text" {
		t.Errorf("file on disk = %q, want %q", string(onDisk), "edited text")
	}
}

func TestSaveTabWithNoFilePathReturnsError(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)

	tab := NewTab("t1", "untitled", "", "some text")

	if err := g.SaveTab(tab); err == nil {
		t.Fatalf("SaveTab(tab with no FilePath) returned nil error")
	}
}

func TestProposeDiffSwitchesActivePaneIntoDiffReview(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "old text")

	if err := g.ProposeDiff(path, "new text"); err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}

	if g.primary.southBar.Mode() != SouthBarDiffReview {
		t.Fatalf("southBar.Mode() = %v, want SouthBarDiffReview", g.primary.southBar.Mode())
	}
	if g.primary.active.pendingDiff == nil {
		t.Fatalf("active tab has no pendingDiff after ProposeDiff")
	}
	if g.primary.active.pendingDiff.newText != "new text" {
		t.Errorf("pendingDiff.newText = %q, want %q", g.primary.active.pendingDiff.newText, "new text")
	}
	// Document itself must stay untouched until Accept.
	if g.primary.active.Doc.Text() != "old text" {
		t.Errorf("Doc.Text() = %q, want unchanged %q before Accept", g.primary.active.Doc.Text(), "old text")
	}
}

func TestAcceptDiffAppliesTextAndWritesToDisk(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "old text")

	if err := g.ProposeDiff(path, "new text"); err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}
	tab := g.primary.active

	// Stop the real file watcher before Accept writes to path below — a
	// genuine fsnotify event racing with this test's own goroutine is a
	// real, observed concurrent-widget-mutation crash in Fyne's test
	// driver (see watch_test.go's package doc comment for the full
	// explanation); the watcher isn't needed for Accept's own logic.
	g.Close()

	g.acceptDiff(tab)

	if tab.Doc.Text() != "new text" {
		t.Errorf("Doc.Text() = %q, want %q", tab.Doc.Text(), "new text")
	}
	if tab.Dirty() {
		t.Errorf("Dirty() = true after a successful Accept, want false — the write to disk succeeded")
	}
	if tab.pendingDiff != nil {
		t.Errorf("pendingDiff is non-nil after Accept, want nil")
	}
	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden after Accept", g.primary.southBar.Mode())
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if string(onDisk) != "new text" {
		t.Errorf("file on disk = %q, want %q", string(onDisk), "new text")
	}
}

func TestCancelDiffLeavesDocumentAndDiskUntouched(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "old text")

	if err := g.ProposeDiff(path, "new text"); err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}
	tab := g.primary.active

	g.cancelDiff(tab)

	if tab.Doc.Text() != "old text" {
		t.Errorf("Doc.Text() = %q, want unchanged %q after Cancel", tab.Doc.Text(), "old text")
	}
	if tab.pendingDiff != nil {
		t.Errorf("pendingDiff is non-nil after Cancel, want nil")
	}
	if g.primary.southBar.Mode() != SouthBarHidden {
		t.Errorf("southBar.Mode() = %v, want SouthBarHidden after Cancel", g.primary.southBar.Mode())
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back %s: %v", path, err)
	}
	if string(onDisk) != "old text" {
		t.Errorf("file on disk = %q, want unchanged %q", string(onDisk), "old text")
	}
}

func TestProposeDiffOnAlreadyOpenTabReusesIt(t *testing.T) {
	app := fynetest.NewApp()
	g := NewGroup(app)
	t.Cleanup(g.Close)
	path := writeTempFile(t, "chapter1.txt", "original")

	opened, err := g.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := g.ProposeDiff(path, "proposed"); err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}

	if g.primary.active != opened {
		t.Errorf("ProposeDiff opened a second tab instead of reusing the already-open one")
	}
	if len(g.primary.tabs) != 1 {
		t.Errorf("primary pane has %d tabs, want 1", len(g.primary.tabs))
	}
}
