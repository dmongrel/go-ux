package fontsettings

import (
	"strconv"
	"strings"

	"go-ux/db"
)

// Property keys shared by every consumer's font settings node (terminal's
// and editors' each register their own node, but both use these same key
// strings — no collision since they're scoped to different node IDs).
const (
	KeyFontFamily  = "font_family"
	KeyFontSize    = "font_size"
	KeyLineHeight  = "line_height"
	KeyColumnWidth = "column_width"
)

// FamilyDefault is the sentinel font_family value meaning "use the bundled
// font" — DetectMonospaceFonts never returns this string itself (it only
// lists real installed font names), so it can't collide with a genuine
// family name.
const FamilyDefault = "(default)"

// SeedFontProperties adds the four font properties (family/size/line
// height/column width) to nodeID in database's settings registry, seeded
// from defaults, with fontOptions (typically DetectMonospaceFonts()'s
// result) as the family enum's choices alongside FamilyDefault. Callers
// (terminal/settings_schema.go, editors/settings_schema.go) call this from
// their own RegisterSettings after creating their own root node — it
// handles only the font rows, not the node itself or any other
// package-specific properties.
func SeedFontProperties(database *db.DB, nodeID int64, fontOptions []string, defaults FontSettings) error {
	familyOptions := append([]string{FamilyDefault}, fontOptions...)
	if err := database.AddProperty(nodeID, KeyFontFamily, "Font", db.PropertyEnum, FamilyDefault, familyOptions); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyFontSize, "Font size", db.PropertyInt, strconv.Itoa(defaults.Size), nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyLineHeight, "Line height", db.PropertyFloat, formatFloat(defaults.LineHeight), nil); err != nil {
		return err
	}
	if err := database.AddProperty(nodeID, KeyColumnWidth, "Column width", db.PropertyFloat, formatFloat(defaults.ColumnWidth), nil); err != nil {
		return err
	}
	return nil
}

// formatFloat renders v with at least one decimal place (e.g. "1.0", not
// "1") — matches this repo's existing seeded-defaults convention (see
// terminal/settings_schema_test.go's original literal "1.0"/"1.0" seeds).
func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// ReadFontProperties reads the four font properties out of props (as
// returned by database.GetProperties for a font-settings node), starting
// from defaults for any key that's missing or unparseable, and returns the
// clamped result. Unlike SeedFontProperties, this takes props directly
// rather than database+nodeID — the caller has typically already looked up
// the node for its own other properties (default_shell, close_on_exit,
// etc.) and already has props in hand.
func ReadFontProperties(props []db.Property, defaults FontSettings) FontSettings {
	font := defaults
	for _, p := range props {
		switch p.Key {
		case KeyFontFamily:
			if p.Value != FamilyDefault {
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
	return ClampFontSettings(font)
}
