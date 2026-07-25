package terminal

import (
	"testing"

	"go-ux/db"
)

func TestWithDefaultFirstReordersShellList(t *testing.T) {
	a := ShellDef{Name: "a"}
	b := ShellDef{Name: "b"}
	c := ShellDef{Name: "c"}

	got := withDefaultFirst([]ShellDef{a, b, c}, "c")
	if len(got) != 3 || got[0].Name != "c" {
		t.Fatalf("withDefaultFirst = %v, want c first", got)
	}
}

func TestWithDefaultFirstNoMatchIsUnchanged(t *testing.T) {
	a := ShellDef{Name: "a"}
	b := ShellDef{Name: "b"}

	got := withDefaultFirst([]ShellDef{a, b}, "z")
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("withDefaultFirst(no match) = %v, want unchanged", got)
	}
}

func TestServiceResolveShellUsesFirstDetectedWhenNameEmpty(t *testing.T) {
	shells := DetectShells()
	if len(shells) == 0 {
		t.Skip("no shells detected on this machine")
	}

	s := &Service{}
	got, err := s.resolveShell("")
	if err != nil {
		t.Fatalf("resolveShell(\"\"): %v", err)
	}
	if got.Name != shells[0].Name {
		t.Errorf("resolveShell(\"\") = %q, want first detected shell %q", got.Name, shells[0].Name)
	}
}

func TestServiceResolveShellFallsBackOnUnknownName(t *testing.T) {
	shells := DetectShells()
	if len(shells) == 0 {
		t.Skip("no shells detected on this machine")
	}

	s := &Service{}
	got, err := s.resolveShell("not-a-real-shell")
	if err != nil {
		t.Fatalf("resolveShell(unknown): %v", err)
	}
	if got.Name != shells[0].Name {
		t.Errorf("resolveShell(unknown) = %q, want fallback to first detected shell %q", got.Name, shells[0].Name)
	}
}

func TestServiceWriteInputUnknownSessionReturnsError(t *testing.T) {
	s := &Service{sessions: make(map[string]*session)}
	if err := s.WriteInput("no-such-id", "x"); err != errUnknownSession {
		t.Errorf("WriteInput(unknown) = %v, want errUnknownSession", err)
	}
}

func TestServiceCloseSessionUnknownIDIsNoOp(t *testing.T) {
	s := &Service{sessions: make(map[string]*session)}
	if err := s.CloseSession("no-such-id"); err != nil {
		t.Errorf("CloseSession(unknown) = %v, want nil", err)
	}
}

func TestSetFontSettingsPersistsToDBWhenTerminalNodeExists(t *testing.T) {
	defer setFontSettings(defaultFontSettings) // restore for other tests

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	s := &Service{db: d, sessions: make(map[string]*session)}
	if err := s.SetFontSettings(FontSettings{Family: "", Size: 18, LineHeight: 1.2, ColumnWidth: 1.0}); err != nil {
		t.Fatalf("SetFontSettings: %v", err)
	}

	if got := currentFontSettings().Size; got != 18 {
		t.Errorf("currentFontSettings().Size = %d, want 18", got)
	}

	_, _, font, found, err := readTerminalSettings(d)
	if err != nil {
		t.Fatalf("readTerminalSettings: %v", err)
	}
	if !found {
		t.Fatal("readTerminalSettings: Terminal node not found after SetFontSettings")
	}
	if font.Size != 18 || font.LineHeight != 1.2 {
		t.Errorf("persisted font = %+v, want Size=18 LineHeight=1.2", font)
	}
}

func TestCloseOnExitReflectsRegisteredSetting(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	s := &Service{db: d, sessions: make(map[string]*session)}
	if got := s.CloseOnExit(); got != true {
		t.Errorf("CloseOnExit() = %v, want true (RegisterSettings' seeded default)", got)
	}

	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, terminalSettingsLabel)
	if err := d.SaveProperties(node.ID, map[string]string{KeyCloseOnExit: "false"}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}
	if got := s.CloseOnExit(); got != false {
		t.Errorf("CloseOnExit() = %v, want false after SaveProperties", got)
	}
}

func TestCloseOnExitFalseWhenNeverRegistered(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	s := &Service{db: d, sessions: make(map[string]*session)}
	if got := s.CloseOnExit(); got != false {
		t.Errorf("CloseOnExit() = %v, want false when RegisterSettings was never called", got)
	}
}

func TestSetFontSettingsWithNoTerminalNodeIsNotAnError(t *testing.T) {
	defer setFontSettings(defaultFontSettings)

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	s := &Service{db: d, sessions: make(map[string]*session)}
	if err := s.SetFontSettings(FontSettings{Family: "", Size: 20, LineHeight: 1.0, ColumnWidth: 1.0}); err != nil {
		t.Errorf("SetFontSettings (no Terminal node): %v, want nil", err)
	}
}
