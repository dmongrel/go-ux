package editors

import (
	"go-ux/db"
	"go-ux/fontsettings"
)

// editorsSettingsLabel returns groupID's settings-tree root node
// description. Scoped per groupID — not one shared "Editors" node the way
// terminal has a single "Terminal" node for every Session — because font
// size and file-watch mode are independent per Service instance, the same
// way layout persistence (Service.SaveLayout/LoadLayout, mcptooling.go) is
// already scoped per groupID.
func editorsSettingsLabel(groupID string) string {
	return "Editors: " + groupID
}

// KeyFileWatchMode is the settings-tree property key for a Group's
// file-watch mode (see watch.go): FileWatchModeAuto or FileWatchModeNotify.
const KeyFileWatchMode = "file_watch_mode"

const (
	// FileWatchModeAuto silently reloads a tab's Document from disk when
	// its file changes externally, unless the Document has unsaved edits
	// (Dirty), in which case it falls back to FileWatchModeNotify behavior
	// rather than clobbering them.
	FileWatchModeAuto = "auto"
	// FileWatchModeNotify shows the south bar's Load-from-Disk/
	// Keep-from-Memory choice instead of reloading automatically.
	FileWatchModeNotify = "notify"
)

// RegisterSettings seeds a root "Editors: <groupID>" node with font
// settings (family/size/line height/column width, via
// fontsettings.SeedFontProperties) and file_watch_mode (PropertyEnum,
// default FileWatchModeNotify) in database's settings registry, if one is
// not already present for groupID. Idempotent — safe to call on every app
// startup, mirroring terminal.RegisterSettings.
func RegisterSettings(database *db.DB, groupID string) error {
	nodes, err := database.ListSettings()
	if err != nil {
		return err
	}
	label := editorsSettingsLabel(groupID)
	if _, ok := findRootNode(nodes, label); ok {
		return nil // already registered; touch nothing
	}

	nodeID, err := database.AddNode(nil, label, 0)
	if err != nil {
		return err
	}
	if err := fontsettings.SeedFontProperties(database, nodeID, fontsettings.DetectMonospaceFonts(), fontsettings.DefaultFontSettings); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyFileWatchMode, "File watch mode", db.PropertyEnum, FileWatchModeNotify, []string{FileWatchModeAuto, FileWatchModeNotify}); err != nil {
		return err
	}
	return nil
}

// readEditorSettings looks up groupID's Editors node's font and
// file-watch-mode values. found is false if RegisterSettings has never
// been called for groupID — callers should fall back to their own
// defaults in that case rather than treating it as an error.
func readEditorSettings(database *db.DB, groupID string) (font fontsettings.FontSettings, fileWatchMode string, found bool, err error) {
	nodes, err := database.ListSettings()
	if err != nil {
		return fontsettings.FontSettings{}, "", false, err
	}

	node, ok := findRootNode(nodes, editorsSettingsLabel(groupID))
	if !ok {
		return fontsettings.FontSettings{}, "", false, nil
	}

	props, err := database.GetProperties(node.ID)
	if err != nil {
		return fontsettings.FontSettings{}, "", false, err
	}

	fileWatchMode = FileWatchModeNotify // matches RegisterSettings' seeded default
	for _, p := range props {
		if p.Key == KeyFileWatchMode {
			fileWatchMode = p.Value
		}
	}
	font = fontsettings.ReadFontProperties(props, fontsettings.DefaultFontSettings)
	return font, fileWatchMode, true, nil
}

// ApplyEditorSettings re-reads groupID's Editors node from database and
// pushes its font value into svc.fonts live — every frontend view showing
// svc's tabs picks up the change via CurrentFontSettings — and updates
// svc.fileWatchMode. A host app calls this after a Settings-window
// OK/Apply commits new values, the same way terminal.ApplyFontSettings
// does for terminal Sessions. A database with no Editors node for groupID
// yet is not an error — ApplyEditorSettings simply leaves svc untouched,
// same graceful-fallback contract readEditorSettings itself already has.
//
// Changing fileWatchMode takes effect for the next file-change event; a
// pending diff review or file-changed notification already showing is
// left as-is (there's nothing to "re-apply" retroactively to something
// already resolved down to a user decision).
func ApplyEditorSettings(database *db.DB, groupID string, svc *Service) error {
	font, fileWatchMode, found, err := readEditorSettings(database, groupID)
	if err != nil {
		return err
	}
	if found {
		svc.fonts.Set(font)
		svc.fileWatchMode = fileWatchMode
	}
	return nil
}

// findRootNode returns the node with the given description among nodes
// whose ParentID is nil (a root node), or false if none matches.
func findRootNode(nodes []db.Node, description string) (db.Node, bool) {
	for _, n := range nodes {
		if n.ParentID == nil && n.Description == description {
			return n, true
		}
	}
	return db.Node{}, false
}
