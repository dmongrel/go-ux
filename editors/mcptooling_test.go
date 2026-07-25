package editors

import (
	"os"
	"path/filepath"
	"testing"

	"go-ux/fontsettings"
	"go-ux/test"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	s := NewService(nil, d, "test-group")
	t.Cleanup(s.Close)
	return s
}

func TestNewServiceSeedsTwoDemoTabs(t *testing.T) {
	s := newTestService(t)
	tabs := s.ListTabs()
	if len(tabs) != 2 {
		t.Fatalf("len(tabs) = %d, want 2", len(tabs))
	}
	if tabs[0].Title != "Chapter One" || tabs[1].Title != "Notes" {
		t.Errorf("tabs = %+v, want Chapter One, Notes", tabs)
	}
	for _, tab := range tabs {
		if tab.Dirty {
			t.Errorf("seeded tab %q is Dirty, want clean", tab.Title)
		}
	}
}

func TestNewTabAddsBlankUntitledTab(t *testing.T) {
	s := newTestService(t)
	tabs := s.NewTab()
	if len(tabs) != 3 {
		t.Fatalf("len(tabs) = %d, want 3", len(tabs))
	}
	last := tabs[len(tabs)-1]
	if last.Title != "Untitled" || last.Text != "" {
		t.Errorf("new tab = %+v, want Untitled/empty", last)
	}
}

func TestSaveTabWithNoFilePathUpdatesInMemoryOnly(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID

	tabs, err := s.SaveTab(id, "new content")
	if err != nil {
		t.Fatalf("SaveTab: %v", err)
	}
	if tabs[0].Text != "new content" {
		t.Errorf("Text = %q, want %q", tabs[0].Text, "new content")
	}
}

func TestSaveTabUnknownIDReturnsError(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SaveTab("no-such-id", "x"); err != errUnknownTab {
		t.Errorf("SaveTab(unknown) = %v, want errUnknownTab", err)
	}
}

func TestOpenFileReadsRealFileAndAddsTab(t *testing.T) {
	s := newTestService(t)
	path := filepath.Join(t.TempDir(), "chapter.md")
	if err := os.WriteFile(path, []byte("real file content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tabs, err := s.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if len(tabs) != 3 {
		t.Fatalf("len(tabs) = %d, want 3 (2 seeded + 1 opened)", len(tabs))
	}
	opened := tabs[2]
	if opened.Text != "real file content" || opened.FilePath != path {
		t.Errorf("opened tab = %+v, want Text=%q FilePath=%q", opened, "real file content", path)
	}
}

func TestOpenFileTwiceReturnsSameTabNotADuplicate(t *testing.T) {
	s := newTestService(t)
	path := filepath.Join(t.TempDir(), "chapter.md")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := s.OpenFile(path); err != nil {
		t.Fatalf("OpenFile (1st): %v", err)
	}
	tabs, err := s.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile (2nd): %v", err)
	}
	if len(tabs) != 3 {
		t.Fatalf("len(tabs) = %d after opening the same path twice, want 3 (no duplicate)", len(tabs))
	}
}

func TestSaveTabWithFilePathWritesToDiskAndMarksClean(t *testing.T) {
	s := newTestService(t)
	path := filepath.Join(t.TempDir(), "chapter.md")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tabs, err := s.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	id := tabs[2].ID

	if _, err := s.SaveTab(id, "v2"); err != nil {
		t.Fatalf("SaveTab: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(onDisk) != "v2" {
		t.Errorf("on-disk content = %q, want %q", onDisk, "v2")
	}
	got, _ := s.findTabByID(id)
	if got.Dirty() {
		t.Error("tab still Dirty after SaveTab wrote to disk")
	}
}

func TestSaveTabAsAdoptsNewPathAndWrites(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID // "Chapter One", no FilePath yet
	path := filepath.Join(t.TempDir(), "saved-as.md")

	tabs, err := s.SaveTabAs(id, path)
	if err != nil {
		t.Fatalf("SaveTabAs: %v", err)
	}
	if tabs[0].FilePath != path || tabs[0].Title != "saved-as.md" {
		t.Errorf("tab = %+v, want FilePath=%q Title=%q", tabs[0], path, "saved-as.md")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(onDisk) != "# Chapter One\n\nIt was a dark and stormy night...\n" {
		t.Errorf("on-disk content = %q, want the tab's original text", onDisk)
	}
}

func TestSaveTabAsEmptyPathIsAnError(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID
	if _, err := s.SaveTabAs(id, ""); err == nil {
		t.Error("SaveTabAs(\"\") = nil error, want an error")
	}
}

func TestProposeAcceptDiffFlow(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID

	tabs, err := s.ProposeDiff(id, "proposed text")
	if err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}
	if tabs[0].PendingDiff == nil || *tabs[0].PendingDiff != "proposed text" {
		t.Fatalf("PendingDiff = %v, want \"proposed text\"", tabs[0].PendingDiff)
	}

	tabs, err = s.AcceptDiff(id, "final text (possibly edited by the user)")
	if err != nil {
		t.Fatalf("AcceptDiff: %v", err)
	}
	if tabs[0].Text != "final text (possibly edited by the user)" {
		t.Errorf("Text = %q, want the accepted finalText", tabs[0].Text)
	}
	if tabs[0].PendingDiff != nil {
		t.Errorf("PendingDiff = %v, want nil after Accept", tabs[0].PendingDiff)
	}
}

func TestProposeCancelDiffDiscardsProposal(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID
	original := s.ListTabs()[0].Text

	if _, err := s.ProposeDiff(id, "proposed text"); err != nil {
		t.Fatalf("ProposeDiff: %v", err)
	}
	tabs, err := s.CancelDiff(id)
	if err != nil {
		t.Fatalf("CancelDiff: %v", err)
	}
	if tabs[0].PendingDiff != nil {
		t.Errorf("PendingDiff = %v, want nil after Cancel", tabs[0].PendingDiff)
	}
	if tabs[0].Text != original {
		t.Errorf("Text = %q, want unchanged original %q", tabs[0].Text, original)
	}
}

func TestCloseTabRemovesFromList(t *testing.T) {
	s := newTestService(t)
	id := s.ListTabs()[0].ID

	tabs := s.CloseTab(id)
	if len(tabs) != 1 {
		t.Fatalf("len(tabs) = %d, want 1", len(tabs))
	}
	if tabs[0].ID == id {
		t.Error("closed tab is still present")
	}
}

func TestLoadLayoutWithNothingSavedReturnsNil(t *testing.T) {
	s := newTestService(t)
	got, err := s.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if got != nil {
		t.Errorf("LoadLayout() = %+v, want nil", got)
	}
}

func TestSaveLayoutThenLoadLayoutRoundTrips(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()
	s := NewService(nil, d, "g1")
	t.Cleanup(s.Close)

	want := LayoutNode{
		Axis:        "row",
		SplitOffset: 0.5,
		A:           &LayoutNode{Tabs: []LayoutTab{{TabID: "1", FilePath: "/a.md"}}, ActiveTabID: "1"},
		B:           &LayoutNode{Tabs: []LayoutTab{{TabID: "2", FilePath: "/b.md"}, {TabID: "3"}}, ActiveTabID: "3"},
	}
	if err := s.SaveLayout(want); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}

	// A fresh Service over the same db (simulating reopening the window)
	// must see the persisted layout.
	s2 := NewService(nil, d, "g1")
	t.Cleanup(s2.Close)
	got, err := s2.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if got == nil {
		t.Fatal("LoadLayout() = nil, want the saved layout")
	}
	if got.Axis != "row" || got.A.Tabs[0].TabID != "1" || got.B.ActiveTabID != "3" || got.B.Tabs[1].FilePath != "" {
		t.Errorf("LoadLayout() = %+v, want %+v", got, want)
	}
}

func TestSetFontSettingsPersistsWhenEditorsNodeExists(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()
	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	s := NewService(nil, d, "g1")
	t.Cleanup(s.Close)

	if err := s.SetFontSettings(fontsettings.FontSettings{Family: "", Size: 22, LineHeight: 1.3, ColumnWidth: 1.0}); err != nil {
		t.Fatalf("SetFontSettings: %v", err)
	}
	if got := s.CurrentFontSettings().Size; got != 22 {
		t.Errorf("CurrentFontSettings().Size = %d, want 22", got)
	}

	_, _, found, err := readEditorSettings(d, "g1")
	if err != nil {
		t.Fatalf("readEditorSettings: %v", err)
	}
	if !found {
		t.Fatal("Editors node not found after SetFontSettings")
	}
}
