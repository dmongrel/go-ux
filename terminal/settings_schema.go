package terminal

import (
	"strconv"

	"go-ux/db"
)

// terminalSettingsLabel is the settings-tree node description for this
// package's registry entry. RegisterSettings checks for an existing root
// node with this description before creating one (idempotency), and
// readTerminalSettings looks it up the same way to read the properties
// back.
const terminalSettingsLabel = "Terminal"

// Property keys for the two settings this package registers. Exported so a
// caller building its own UI around this schema — or a future task
// expanding it toward the full 22-row design referenced by the plan this
// task is part of — has the exact strings to match against rather than
// guessing at what RegisterSettings used.
const (
	KeyDefaultShell = "default_shell"
	KeyCloseOnExit  = "close_on_exit"
	KeyFontFamily   = "font_family"
	KeyFontSize     = "font_size"
	KeyLineHeight   = "line_height"
	KeyColumnWidth  = "column_width"
)

// fontFamilyDefault is the sentinel font_family value meaning "use the
// bundled font" — DetectMonospaceFonts() never returns this string itself
// (it only lists real installed font names), so it can't collide with a
// genuine family name.
const fontFamilyDefault = "(default)"

// RegisterSettings seeds a root "Terminal" node with default_shell
// (PropertyEnum, options from DetectShells()) and close_on_exit
// (PropertyBool, default "true") in database's settings registry, if one is
// not already present. Idempotent — safe to call on every app startup,
// mirroring how a caller would seed example data via test.SeedExample
// before opening settings.NewWindow.
//
// This is intentionally a minimal two-property slice for the demo, not the
// full design's 22-row table — that expansion is later, out-of-scope work.
func RegisterSettings(database *db.DB) error {
	nodes, err := database.ListSettings()
	if err != nil {
		return err
	}
	if _, ok := findRootNode(nodes, terminalSettingsLabel); ok {
		return nil // already registered; touch nothing
	}

	shells := DetectShells()
	options := make([]string, 0, len(shells))
	for _, s := range shells {
		options = append(options, s.Name)
	}
	defaultShell := "PowerShell"
	if len(shells) > 0 {
		defaultShell = shells[0].Name
	} else {
		options = []string{defaultShell}
	}

	nodeID, err := database.AddNode(nil, terminalSettingsLabel, 0)
	if err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyDefaultShell, "Default shell", db.PropertyEnum, defaultShell, options); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyCloseOnExit, "Close tab on shell exit", db.PropertyBool, "true", nil); err != nil {
		return err
	}

	fontOptions := append([]string{fontFamilyDefault}, DetectMonospaceFonts()...)
	if err := database.AddProperty(nodeID, KeyFontFamily, "Font", db.PropertyEnum, fontFamilyDefault, fontOptions); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyFontSize, "Font size", db.PropertyInt, "13", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyLineHeight, "Line height", db.PropertyFloat, "1.0", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyColumnWidth, "Column width", db.PropertyFloat, "1.0", nil); err != nil {
		return err
	}
	return nil
}

// readTerminalSettings looks up the Terminal node's default_shell,
// close_on_exit, and font values. found is false if RegisterSettings has
// never been called against database (no Terminal node yet) — callers
// should fall back to their own defaults in that case rather than treating
// it as an error.
func readTerminalSettings(database *db.DB) (defaultShell string, closeOnExit bool, font FontSettings, found bool, err error) {
	nodes, err := database.ListSettings()
	if err != nil {
		return "", false, FontSettings{}, false, err
	}

	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		return "", false, FontSettings{}, false, nil
	}

	props, err := database.GetProperties(node.ID)
	if err != nil {
		return "", false, FontSettings{}, false, err
	}

	closeOnExit = true // matches RegisterSettings' seeded default
	font = defaultFontSettings
	for _, p := range props {
		switch p.Key {
		case KeyDefaultShell:
			defaultShell = p.Value
		case KeyCloseOnExit:
			closeOnExit = p.Value == "true"
		case KeyFontFamily:
			if p.Value != fontFamilyDefault {
				font.Family = p.Value
			}
		case KeyFontSize:
			if v, err := strconv.Atoi(p.Value); err == nil {
				font.Size = v
			}
		case KeyLineHeight:
			if v, err := strconv.ParseFloat(p.Value, 64); err == nil {
				font.LineHeight = v
			}
		case KeyColumnWidth:
			if v, err := strconv.ParseFloat(p.Value, 64); err == nil {
				font.ColumnWidth = v
			}
		}
	}
	font = clampFontSettings(font)
	return defaultShell, closeOnExit, font, true, nil
}

// ApplyFontSettings re-reads font_family/font_size/line_height/column_width
// from database's Terminal node and pushes them into the live shared
// FontSettings (font.go) — every open Session re-renders against the new
// values immediately. A host app calls this after a Settings-window
// OK/Apply commits new font values, the same way NewWindowFromSettings
// applies them once at window-construction time. A database with no
// Terminal node yet (RegisterSettings never called) is not an error —
// ApplyFontSettings simply leaves the live state untouched, same
// graceful-fallback contract readTerminalSettings itself already has.
func ApplyFontSettings(database *db.DB) error {
	_, _, font, found, err := readTerminalSettings(database)
	if err != nil {
		return err
	}
	if found {
		setFontSettings(font)
	}
	return nil
}

// findRootNode returns the node with the given description among nodes
// whose ParentID is nil (a root node), or false if none matches. Root-only
// because "Terminal" is meant to live at the top level of the tree, same as
// test.SeedExample's own Terminal/Version Control nodes.
func findRootNode(nodes []db.Node, description string) (db.Node, bool) {
	for _, n := range nodes {
		if n.ParentID == nil && n.Description == description {
			return n, true
		}
	}
	return db.Node{}, false
}
