package terminal

import (
	"testing"

	"github.com/dmongrel/go-ux/db"
)

// newTestDB opens an in-memory db.DB for this file's tests. terminal can't
// import go-ux/test (that package exists for the repo's own top-level tests
// and importing it here would be a cross-package dependency in the wrong
// direction), so it opens its own in-memory database directly the same way
// test.NewDB does.
func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestRegisterSettingsCreatesTerminalNodeWithProperties confirms
// RegisterSettings seeds a root "Terminal" node with default_shell (enum)
// and close_on_exit (bool, default "true"), among the full 22-row set (see
// TestRegisterSettingsSeedsAll22Rows for the complete inventory).
func TestRegisterSettingsCreatesTerminalNodeWithProperties(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodes, "Terminal")
	if !ok {
		t.Fatalf("no root Terminal node found in %+v", nodes)
	}

	props, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 22 {
		t.Fatalf("len(props) = %d, want 22 (%+v)", len(props), props)
	}

	byKey := make(map[string]db.Property, len(props))
	for _, p := range props {
		byKey[p.Key] = p
	}

	shellProp, ok := byKey[KeyDefaultShell]
	if !ok {
		t.Fatal("no default_shell property")
	}
	if shellProp.Type != db.PropertyEnum {
		t.Errorf("default_shell.Type = %v, want PropertyEnum", shellProp.Type)
	}
	if shellProp.Value == "" {
		t.Error("default_shell.Value is empty, want a non-empty default")
	}
	wantDefault := "PowerShell"
	if shells := DetectShells(); len(shells) > 0 {
		wantDefault = shells[0].Name
	}
	if shellProp.Value != wantDefault {
		t.Errorf("default_shell.Value = %q, want %q (first DetectShells() entry, or PowerShell if none)", shellProp.Value, wantDefault)
	}

	closeProp, ok := byKey[KeyCloseOnExit]
	if !ok {
		t.Fatal("no close_on_exit property")
	}
	if closeProp.Type != db.PropertyBool {
		t.Errorf("close_on_exit.Type = %v, want PropertyBool", closeProp.Type)
	}
	if closeProp.Value != "true" {
		t.Errorf("close_on_exit.Value = %q, want \"true\"", closeProp.Value)
	}
}

// TestRegisterSettingsIsIdempotent confirms a second call doesn't duplicate
// the node or its properties — the brief's explicit done-bar, verified by
// comparing ListSettings()'s contents before and after, not just checking
// for a nil error.
func TestRegisterSettingsIsIdempotent(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings (1st call): %v", err)
	}
	nodesBefore, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodesBefore, "Terminal")
	if !ok {
		t.Fatalf("no root Terminal node found after first call")
	}
	propsBefore, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings (2nd call): %v", err)
	}

	nodesAfter, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if len(nodesAfter) != len(nodesBefore) {
		t.Fatalf("len(nodes) after 2nd call = %d, want unchanged %d", len(nodesAfter), len(nodesBefore))
	}

	propsAfter, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(propsAfter) != len(propsBefore) {
		t.Fatalf("len(props) after 2nd call = %d, want unchanged %d", len(propsAfter), len(propsBefore))
	}
}

func TestRegisterSettingsSeedsFontProperties(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		t.Fatal("Terminal node not found")
	}
	props, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}

	byKey := make(map[string]db.Property)
	for _, p := range props {
		byKey[p.Key] = p
	}

	fontFamily, ok := byKey[KeyFontFamily]
	if !ok {
		t.Fatal("font_family property not seeded")
	}
	if fontFamily.Value != "(default)" {
		t.Errorf("font_family default = %q, want %q", fontFamily.Value, "(default)")
	}
	if fontFamily.Type != db.PropertyEnum {
		t.Errorf("font_family type = %q, want %q", fontFamily.Type, db.PropertyEnum)
	}
	found := false
	for _, opt := range fontFamily.EnumOptions {
		if opt == "(default)" {
			found = true
		}
	}
	if !found {
		t.Error("font_family enum options missing \"(default)\" sentinel")
	}

	fontSize, ok := byKey[KeyFontSize]
	if !ok || fontSize.Value != "13" || fontSize.Type != db.PropertyInt {
		t.Errorf("font_size = %+v, want value \"13\" type PropertyInt", fontSize)
	}

	lineHeight, ok := byKey[KeyLineHeight]
	if !ok || lineHeight.Value != "1.0" || lineHeight.Type != db.PropertyFloat {
		t.Errorf("line_height = %+v, want value \"1.0\" type PropertyFloat", lineHeight)
	}

	columnWidth, ok := byKey[KeyColumnWidth]
	if !ok || columnWidth.Value != "1.0" || columnWidth.Type != db.PropertyFloat {
		t.Errorf("column_width = %+v, want value \"1.0\" type PropertyFloat", columnWidth)
	}
}

func TestApplyFontSettingsPushesDbValuesIntoLiveState(t *testing.T) {
	defer setFontSettings(defaultFontSettings) // restore for other tests

	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	nodes, _ := d.ListSettings()
	node, _ := findRootNode(nodes, terminalSettingsLabel)

	if err := d.SaveProperties(node.ID, map[string]string{
		KeyFontSize:   "18",
		KeyLineHeight: "1.4",
	}); err != nil {
		t.Fatalf("SaveProperties: %v", err)
	}

	if err := ApplyFontSettings(d); err != nil {
		t.Fatalf("ApplyFontSettings: %v", err)
	}

	got := currentFontSettings()
	if got.Size != 18 {
		t.Errorf("currentFontSettings().Size = %d, want 18", got.Size)
	}
	if got.LineHeight != 1.4 {
		t.Errorf("currentFontSettings().LineHeight = %v, want 1.4", got.LineHeight)
	}
}

func TestApplyFontSettingsNoTerminalNodeIsNotAnError(t *testing.T) {
	d := newTestDB(t)

	if err := ApplyFontSettings(d); err != nil {
		t.Errorf("ApplyFontSettings (no Terminal node): %v, want nil", err)
	}
}

// TestWithDefaultFirstReorders is a pure unit test over withDefaultFirst's
// reordering logic (window.go) — no db, no Fyne, no PTY involved, so it
// runs on any platform and needs no windows build tag.
func TestWithDefaultFirstReorders(t *testing.T) {
	a := ShellDef{Name: "a"}
	b := ShellDef{Name: "b"}
	c := ShellDef{Name: "c"}

	tests := []struct {
		name  string
		in    []ShellDef
		match string
		want  []ShellDef
	}{
		{"already first", []ShellDef{a, b, c}, "a", []ShellDef{a, b, c}},
		{"middle moves to front", []ShellDef{a, b, c}, "b", []ShellDef{b, a, c}},
		{"last moves to front", []ShellDef{a, b, c}, "c", []ShellDef{c, a, b}},
		{"no match is unchanged", []ShellDef{a, b, c}, "z", []ShellDef{a, b, c}},
		{"empty input", nil, "a", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withDefaultFirst(tt.in, tt.match)
			if len(got) != len(tt.want) {
				t.Fatalf("withDefaultFirst(%v, %q) = %v, want %v", tt.in, tt.match, got, tt.want)
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Errorf("withDefaultFirst(%v, %q)[%d] = %q, want %q", tt.in, tt.match, i, got[i].Name, tt.want[i].Name)
				}
			}
		})
	}
}

// TestRegisterSettingsDoesNotTouchOtherNodes confirms RegisterSettings on a
// db that already has unrelated settings (e.g. a Version Control node from
// test.SeedExample-style seeding) only adds its own Terminal node, leaving
// everything else alone — idempotency should be scoped to "Terminal", not
// "the registry has anything in it at all".
func TestRegisterSettingsDoesNotTouchOtherNodes(t *testing.T) {
	d := newTestDB(t)

	vcsID, err := d.AddNode(nil, "Version Control", 0)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := d.AddProperty(vcsID, "vcs_type", "VCS", db.PropertyEnum, "Git", []string{"Git", "None"}); err != nil {
		t.Fatalf("AddProperty: %v", err)
	}

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if _, ok := findRootNode(nodes, "Version Control"); !ok {
		t.Error("Version Control node was removed/lost")
	}
	if _, ok := findRootNode(nodes, "Terminal"); !ok {
		t.Error("Terminal node was not created")
	}
	if len(nodes) != 2 {
		t.Errorf("len(nodes) = %d, want 2", len(nodes))
	}
}

// TestRegisterSettingsSeedsAll22Rows confirms every row of the design
// plan's full property table exists with the right key, type, and default
// value — not just the two/six that actually drive live behavior today.
// Per settings_schema.go's doc comment, the rest are intentional seeded
// placeholders for features this package hasn't built yet.
func TestRegisterSettingsSeedsAll22Rows(t *testing.T) {
	d := newTestDB(t)

	if err := RegisterSettings(d); err != nil {
		t.Fatalf("RegisterSettings: %v", err)
	}
	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	node, ok := findRootNode(nodes, "Terminal")
	if !ok {
		t.Fatalf("no root Terminal node found")
	}
	props, err := d.GetProperties(node.ID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	byKey := make(map[string]db.Property, len(props))
	for _, p := range props {
		byKey[p.Key] = p
	}

	wantDefault := "PowerShell"
	if shells := DetectShells(); len(shells) > 0 {
		wantDefault = shells[0].Name
	}

	cases := []struct {
		key     string
		ptype   db.PropertyType
		wantVal string
	}{
		{KeyDefaultShell, db.PropertyEnum, wantDefault},
		{KeyDefaultTabName, db.PropertyString, "Shell"},
		{KeyUseAppTitleAsTabName, db.PropertyBool, "false"},
		{KeyStartDirectory, db.PropertyString, ""},
		{KeyCloseOnExit, db.PropertyBool, "true"},
		{KeyFontFamily, db.PropertyEnum, fontFamilyDefault},
		{KeyFontSize, db.PropertyInt, "13"},
		{KeyLineHeight, db.PropertyFloat, "1"},
		{KeyColumnWidth, db.PropertyFloat, "1"},
		{KeyScrollbackLines, db.PropertyInt, "1000"},
		{KeyCursorBlink, db.PropertyBool, "true"},
		{KeyCursorShape, db.PropertyEnum, "block"},
		{KeyMinContrastRatio, db.PropertyInt, "1"},
		{KeyAudibleBell, db.PropertyBool, "true"},
		{KeyMouseReporting, db.PropertyBool, "true"},
		{KeyCopyOnSelection, db.PropertyBool, "false"},
		{KeyPasteOnMiddleClick, db.PropertyBool, "false"},
		{KeyOverrideHostShortcuts, db.PropertyBool, "false"},
		{KeyFocusEscapeKey, db.PropertyString, "Escape"},
		{KeyHighlightHyperlinks, db.PropertyBool, "true"},
		{KeyShellIntegration, db.PropertyBool, "false"},
		{KeyShowCommandSeparators, db.PropertyBool, "false"},
	}
	if len(cases) != 22 {
		t.Fatalf("test itself lists %d cases, want 22 — fix the test", len(cases))
	}

	for _, c := range cases {
		p, ok := byKey[c.key]
		if !ok {
			t.Errorf("missing property %q", c.key)
			continue
		}
		if p.Type != c.ptype {
			t.Errorf("%s.Type = %v, want %v", c.key, p.Type, c.ptype)
		}
		if c.key == KeyFontFamily || c.key == KeyLineHeight || c.key == KeyColumnWidth || c.key == KeyFontSize {
			continue // font values are seeded via fontsettings.SeedFontProperties, already covered by TestRegisterSettingsSeedsFontProperties
		}
		if p.Value != c.wantVal {
			t.Errorf("%s.Value = %q, want %q", c.key, p.Value, c.wantVal)
		}
	}

	cursorShapeProp := byKey[KeyCursorShape]
	wantOptions := []string{"block", "underline", "bar"}
	if len(cursorShapeProp.EnumOptions) != len(wantOptions) {
		t.Fatalf("cursor_shape.EnumOptions = %v, want %v", cursorShapeProp.EnumOptions, wantOptions)
	}
	for i, opt := range wantOptions {
		if cursorShapeProp.EnumOptions[i] != opt {
			t.Errorf("cursor_shape.EnumOptions[%d] = %q, want %q", i, cursorShapeProp.EnumOptions[i], opt)
		}
	}
}
