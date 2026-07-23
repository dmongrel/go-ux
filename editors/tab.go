package editors

// Tab is one open document's presence in one Pane: a title, a dirty
// (unsaved-changes) flag, and — Phase 1 only — static placeholder text
// (Phase 2 replaces this with a reference to a shared Document buffer;
// see the design plan's "shared document, split panes stay synced"
// decision — Tab itself is deliberately kept this simple in Phase 1 so
// that swap-in later doesn't require reshaping anything that depends on
// Tab's other fields).
type Tab struct {
	ID       string // stable identifier — used by menus/persistence to refer to a specific tab without relying on slice position
	Title    string
	FilePath string
	Dirty    bool
	Text     string // Phase 1 placeholder content shown by content.go
}

// NewTab builds a Tab with the given identity/content fields; Dirty starts
// false (a freshly opened tab has no unsaved changes yet).
func NewTab(id, title, filePath, text string) *Tab {
	return &Tab{
		ID:       id,
		Title:    title,
		FilePath: filePath,
		Text:     text,
	}
}
