package editors

// Document is the shared text buffer behind one or more Tabs — the "same
// underlying document, synced live" half of split.go's split semantics
// (see the design plan): when two Panes each show a Tab pointing at the
// same Document, an edit typed into either one's content widget must show
// up in the other immediately. RegisterListener/UnregisterListener is how
// content.go's per-Pane editable widget wires that up, keyed by whichever
// Pane displays it (see pane.go's setActive) so a widget that's no longer
// shown stops receiving updates.
type Document struct {
	text      string
	dirty     bool
	listeners map[any]func(text string)
}

// NewDocument builds a Document with initial content text and no unsaved
// changes yet.
func NewDocument(text string) *Document {
	return &Document{text: text, listeners: make(map[any]func(string))}
}

// Text returns the Document's current content.
func (d *Document) Text() string { return d.text }

// SetText updates the Document's content and notifies every registered
// listener except when text is already the current content — the no-op
// guard is what keeps a listener's own SetText-triggered OnChanged from
// looping back into another round of notifications (see content.go).
func (d *Document) SetText(text string) {
	if text == d.text {
		return
	}
	d.text = text
	d.dirty = true
	for _, fn := range d.listeners {
		fn(text)
	}
}

// Dirty reports whether the Document has unsaved changes.
func (d *Document) Dirty() bool { return d.dirty }

// MarkClean clears the Dirty flag — called after a successful save
// (Group.SaveTab) so the Document stops reporting unsaved changes for
// text that's now actually on disk.
func (d *Document) MarkClean() { d.dirty = false }

// RegisterListener registers fn to be called with the new text every time
// SetText changes it. key identifies the caller (typically the *Pane
// displaying this Document) so a later UnregisterListener call can remove
// exactly this registration without affecting others.
func (d *Document) RegisterListener(key any, fn func(text string)) {
	d.listeners[key] = fn
}

// UnregisterListener removes the listener previously registered under key,
// if any.
func (d *Document) UnregisterListener(key any) {
	delete(d.listeners, key)
}
