package editors

import (
	"testing"

	"github.com/dmongrel/go-ux/fontsettings"
	"github.com/dmongrel/go-ux/test"
)

func TestRegisterSettingsSeedsFontAndFileWatchMode(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	font, fileWatchMode, found, err := readEditorSettings(d, "g1")
	if err != nil {
		t.Fatalf("readEditorSettings: %v", err)
	}
	if !found {
		t.Fatalf("found = false after RegisterSettings")
	}
	if font != fontsettings.DefaultFontSettings {
		t.Errorf("font = %+v, want defaults %+v", font, fontsettings.DefaultFontSettings)
	}
	if fileWatchMode != FileWatchModeNotify {
		t.Errorf("fileWatchMode = %q, want %q", fileWatchMode, FileWatchModeNotify)
	}
}

func TestRegisterSettingsIsIdempotent(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings (1st): %v", err)
	}
	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings (2nd): %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	count := 0
	for _, n := range nodes {
		if n.ParentID == nil && n.Description == editorsSettingsLabel("g1") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("found %d root nodes for g1, want 1 (RegisterSettings should be idempotent)", count)
	}
}

func TestReadEditorSettingsNotFoundWhenNeverRegistered(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	_, _, found, err := readEditorSettings(d, "never-registered")
	if err != nil {
		t.Fatalf("readEditorSettings: %v", err)
	}
	if found {
		t.Errorf("found = true for a groupID that was never registered")
	}
}

func TestApplyEditorSettingsPushesFontIntoLiveService(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, editorsSettingsLabel("g1"))
	if err := d.SaveProperties(node.ID, map[string]string{fontsettings.KeyFontSize: "24"}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	s := NewService(nil, d, "g1")
	t.Cleanup(s.Close)

	if err := ApplyEditorSettings(d, "g1", s); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if got := s.CurrentFontSettings().Size; got != 24 {
		t.Errorf("CurrentFontSettings().Size = %d, want 24", got)
	}
}

// TestApplyEditorSettingsUpdatesFileWatchModeLive is a regression test:
// ApplyEditorSettings previously only pushed the font value into a live
// Group/Service, leaving fileWatchMode frozen at whatever construction
// time read once — so changing the setting later (e.g. via a settings
// window) had no effect until the app restarted.
func TestApplyEditorSettingsUpdatesFileWatchModeLive(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	s := NewService(nil, d, "g1")
	t.Cleanup(s.Close)
	if s.fileWatchMode != FileWatchModeNotify {
		t.Fatalf("precondition: fileWatchMode = %q, want %q", s.fileWatchMode, FileWatchModeNotify)
	}

	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, editorsSettingsLabel("g1"))
	if err := d.SaveProperties(node.ID, map[string]string{KeyFileWatchMode: FileWatchModeAuto}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	if err := ApplyEditorSettings(d, "g1", s); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if s.fileWatchMode != FileWatchModeAuto {
		t.Errorf("fileWatchMode = %q after ApplyEditorSettings, want %q", s.fileWatchMode, FileWatchModeAuto)
	}
}

func TestApplyEditorSettingsNoOpWhenNeverRegistered(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	s := NewService(nil, d, "never-registered")
	t.Cleanup(s.Close)
	before := s.CurrentFontSettings()
	beforeMode := s.fileWatchMode

	if err := ApplyEditorSettings(d, "never-registered", s); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if s.CurrentFontSettings() != before {
		t.Errorf("fonts changed despite no registered settings: %+v -> %+v", before, s.CurrentFontSettings())
	}
	if s.fileWatchMode != beforeMode {
		t.Errorf("fileWatchMode changed despite no registered settings: %q -> %q", beforeMode, s.fileWatchMode)
	}
}
