package editors

// Tab is one open document's presence in one Pane: a title and a
// reference to the shared Document buffer holding its actual text (see
// document.go — split.go's split semantics require the same underlying
// Document to stay in sync across every Pane showing it, which is why the
// text itself lives on Document rather than directly on Tab).
type Tab struct {
	ID       string // stable identifier — used by menus/persistence to refer to a specific tab without relying on slice position
	Title    string
	FilePath string
	Doc      *Document

	// pendingDiff is non-nil (the proposed new text) while an
	// mcp_tooling-proposed diff (see mcptooling.go) is awaiting
	// Accept/Cancel. Unexported — a host app drives this only through
	// Service.ProposeDiff/AcceptDiff/CancelDiff, never by touching Tab
	// directly. The frontend renders the actual diff view itself (via
	// @codemirror/merge, comparing this against the Tab's current text) —
	// unlike the old Fyne version, this package no longer computes or
	// renders a diff at all, just holds the proposed text.
	pendingDiff *string
}

// Text returns the Tab's current content, delegating to its Document.
func (t *Tab) Text() string { return t.Doc.Text() }

// Dirty reports whether the Tab's Document has unsaved changes.
func (t *Tab) Dirty() bool { return t.Doc.Dirty() }

// NewTab builds a Tab with the given identity fields and a fresh Document
// seeded with text.
func NewTab(id, title, filePath, text string) *Tab {
	return &Tab{
		ID:       id,
		Title:    title,
		FilePath: filePath,
		Doc:      NewDocument(text),
	}
}
