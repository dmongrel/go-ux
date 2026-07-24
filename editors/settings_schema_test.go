package editors

import (
	"testing"

	fynetest "fyne.io/fyne/v2/test"

	"go-ux/fontsettings"
	"go-ux/test"
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

func TestApplyEditorSettingsPushesFontIntoLiveGroup(t *testing.T) {
	app := fynetest.NewApp()
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

	g := NewGroup(app)
	t.Cleanup(g.Close)

	if err := ApplyEditorSettings(d, "g1", g); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if got := g.fonts.Current().Size; got != 24 {
		t.Errorf("fonts.Current().Size = %d, want 24", got)
	}
}

// TestApplyEditorSettingsUpdatesFileWatchModeLive is a regression test:
// ApplyEditorSettings previously only pushed the font value into a live
// Group, leaving fileWatchMode frozen at whatever NewGroupFromSettings
// read once at construction — so changing the setting later (e.g. via a
// settings window) had no effect until the app restarted.
func TestApplyEditorSettingsUpdatesFileWatchModeLive(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	if err := RegisterSettings(d, "g1"); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	g := NewGroup(app)
	t.Cleanup(g.Close)
	if g.fileWatchMode != FileWatchModeNotify {
		t.Fatalf("precondition: fileWatchMode = %q, want %q", g.fileWatchMode, FileWatchModeNotify)
	}

	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, editorsSettingsLabel("g1"))
	if err := d.SaveProperties(node.ID, map[string]string{KeyFileWatchMode: FileWatchModeAuto}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	if err := ApplyEditorSettings(d, "g1", g); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if g.fileWatchMode != FileWatchModeAuto {
		t.Errorf("fileWatchMode = %q after ApplyEditorSettings, want %q", g.fileWatchMode, FileWatchModeAuto)
	}
}

func TestApplyEditorSettingsNoOpWhenNeverRegistered(t *testing.T) {
	app := fynetest.NewApp()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("test.NewDB: %v", err)
	}
	defer d.Close()

	g := NewGroup(app)
	t.Cleanup(g.Close)
	before := g.fonts.Current()
	beforeMode := g.fileWatchMode

	if err := ApplyEditorSettings(d, "never-registered", g); err != nil {
		t.Fatalf("ApplyEditorSettings: %v", err)
	}

	if g.fonts.Current() != before {
		t.Errorf("fonts changed despite no registered settings: %+v -> %+v", before, g.fonts.Current())
	}
	if g.fileWatchMode != beforeMode {
		t.Errorf("fileWatchMode changed despite no registered settings: %q -> %q", beforeMode, g.fileWatchMode)
	}
}
