package terminal

import (
	"github.com/dmongrel/go-ux/db"
	"github.com/dmongrel/go-ux/fontsettings"
)

// terminalSettingsLabel is the settings-tree node description for this
// package's registry entry. RegisterSettings checks for an existing root
// node with this description before creating one (idempotency), and
// readTerminalSettings looks it up the same way to read the properties
// back.
const terminalSettingsLabel = "Terminal"

// Property keys for the full 22-row settings surface referenced by this
// package's original design plan (docs/superpowers/plans/2026-07-21-
// terminal-vt-rendering-demo.md's Task 4, and the design doc it links to).
// Exported so a caller building its own UI around this schema has the
// exact strings to match against rather than guessing at what
// RegisterSettings used.
//
// Per that design's explicit intent, RegisterSettings seeds every row
// below regardless of whether the behavior it drives has been built yet
// — "so the settings UI is complete even while later phases are still
// catching up." Only KeyDefaultShell, KeyCloseOnExit, and the four
// KeyFont*/KeyLineHeight/KeyColumnWidth keys actually drive live behavior
// today (window.go/font.go); the rest are seeded placeholders for
// not-yet-implemented features (shell integration, mouse reporting,
// cursor shape, hyperlink highlighting, etc.) — readTerminalSettings
// deliberately does not read them back yet, since nothing consumes them.
const (
	KeyDefaultShell          = "default_shell"
	KeyDefaultTabName        = "default_tab_name"
	KeyUseAppTitleAsTabName  = "use_app_title_as_tab_name"
	KeyStartDirectory        = "start_directory"
	KeyCloseOnExit           = "close_on_exit"
	KeyFontFamily            = fontsettings.KeyFontFamily
	KeyFontSize              = fontsettings.KeyFontSize
	KeyLineHeight            = fontsettings.KeyLineHeight
	KeyColumnWidth           = fontsettings.KeyColumnWidth
	KeyScrollbackLines       = "scrollback_lines"
	KeyCursorBlink           = "cursor_blink"
	KeyCursorShape           = "cursor_shape"
	KeyMinContrastRatio      = "min_contrast_ratio"
	KeyAudibleBell           = "audible_bell"
	KeyMouseReporting        = "mouse_reporting"
	KeyCopyOnSelection       = "copy_on_selection"
	KeyPasteOnMiddleClick    = "paste_on_middle_click"
	KeyOverrideHostShortcuts = "override_host_shortcuts"
	KeyFocusEscapeKey        = "focus_escape_key"
	KeyHighlightHyperlinks   = "highlight_hyperlinks"
	KeyShellIntegration      = "shell_integration"
	KeyShowCommandSeparators = "show_command_separators"
)

// cursorShapeOptions is KeyCursorShape's fixed enum option set — matches
// the design plan's table exactly ("block/underline/bar"), not derived
// from any runtime detection the way default_shell's options are.
var cursorShapeOptions = []string{"block", "underline", "bar"}

// fontFamilyDefault is the sentinel font_family value meaning "use the
// bundled font" — DetectMonospaceFonts() never returns this string itself
// (it only lists real installed font names), so it can't collide with a
// genuine family name.
const fontFamilyDefault = fontsettings.FamilyDefault

// RegisterSettings seeds a root "Terminal" node with the full 22-row
// property set from the package's original design plan in database's
// settings registry, if one is not already present. Idempotent — safe to
// call on every app startup, mirroring how a caller would seed example
// data via test.SeedExample before opening settings.NewWindow.
//
// Only default_shell, close_on_exit, and the font properties
// (fontsettings.SeedFontProperties) currently drive live behavior
// (window.go/font.go read them back via readTerminalSettings/
// ApplyFontSettings) — the remaining rows are seeded placeholders for
// features this package hasn't implemented yet (shell integration, mouse
// reporting, cursor shape/blink, hyperlink highlighting, etc.), matching
// the design's explicit intent that the settings UI be complete even
// while those phases are still catching up. A settings.Window pointed at
// this registry shows every row; changing one with no consuming behavior
// yet simply has no visible effect until that feature lands.
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
	if err := database.AddProperty(nodeID, KeyDefaultTabName, "Default tab name", db.PropertyString, "Shell", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyUseAppTitleAsTabName, "Use app title as tab name", db.PropertyBool, "false", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyStartDirectory, "Start directory", db.PropertyString, "", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyCloseOnExit, "Close tab on shell exit", db.PropertyBool, "true", nil); err != nil {
		return err
	}

	if err := fontsettings.SeedFontProperties(database, nodeID, fontsettings.DetectMonospaceFonts(), defaultFontSettings); err != nil {
		return err
	}

	if err := database.AddProperty(nodeID, KeyScrollbackLines, "Scrollback lines", db.PropertyInt, "1000", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyCursorBlink, "Cursor blink", db.PropertyBool, "true", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyCursorShape, "Cursor shape", db.PropertyEnum, "block", cursorShapeOptions); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyMinContrastRatio, "Minimum contrast ratio", db.PropertyInt, "1", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyAudibleBell, "Audible bell", db.PropertyBool, "true", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyMouseReporting, "Mouse reporting", db.PropertyBool, "true", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyCopyOnSelection, "Copy on selection", db.PropertyBool, "false", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyPasteOnMiddleClick, "Paste on middle click", db.PropertyBool, "false", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyOverrideHostShortcuts, "Override host shortcuts", db.PropertyBool, "false", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyFocusEscapeKey, "Focus escape key", db.PropertyString, "Escape", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyHighlightHyperlinks, "Highlight hyperlinks", db.PropertyBool, "true", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyShellIntegration, "Shell integration", db.PropertyBool, "false", nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyShowCommandSeparators, "Show command separators", db.PropertyBool, "false", nil); err != nil {
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
	for _, p := range props {
		switch p.Key {
		case KeyDefaultShell:
			defaultShell = p.Value
		case KeyCloseOnExit:
			closeOnExit = p.Value == "true"
		}
	}
	font = fontsettings.ReadFontProperties(props, defaultFontSettings)
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
