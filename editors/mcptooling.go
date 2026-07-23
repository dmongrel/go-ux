package editors

import (
	"errors"
	"log"
	"os"
	"path/filepath"
)

// OpenFile is the host app's "sidebar click-to-open" entry point (and the
// first half of ProposeDiff, below): if path is already open somewhere in
// this Group (any Pane, matched by Tab.FilePath), returns that existing
// Tab rather than opening a duplicate; otherwise reads path from disk and
// opens it as a new Tab in the primary Pane (same destination AddTab
// itself uses).
//
// This is where file I/O first enters the editors package — every prior
// phase seeded Tab text purely in memory.
func (g *Group) OpenFile(path string) (*Tab, error) {
	if tab, ok := g.findTabByPath(path); ok {
		return tab, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tab := NewTab(path, filepath.Base(path), path, string(data))
	g.AddTab(tab)
	return tab, nil
}

// SaveTab writes tab's current Document text to tab.FilePath, marking it
// clean on success. This is the plain, non-diff-review "just save my
// edit" path (Ctrl+S — see font.go's editorEntry.TypedShortcut) — until
// now the only way to persist an edited Tab's content to disk at all was
// via a host-tooling-proposed ProposeDiff/Accept, which is the wrong
// entry point for a normal user edit. Returns an error if tab has no
// FilePath (nothing to save to — e.g. a Tab that was never opened from,
// or associated with, a real file).
func (g *Group) SaveTab(tab *Tab) error {
	if tab.FilePath == "" {
		return errors.New("editors: tab has no FilePath to save to")
	}
	if err := os.WriteFile(tab.FilePath, []byte(tab.Doc.Text()), 0o644); err != nil {
		return err
	}
	tab.Doc.MarkClean()
	return nil
}

// findTabByPath searches every Pane's tabs for one whose FilePath matches
// path.
func (g *Group) findTabByPath(path string) (*Tab, bool) {
	var found *Tab
	walkPanes(g.root, func(p *Pane) {
		if found != nil {
			return
		}
		for _, t := range p.tabs {
			if t.FilePath == path {
				found = t
				return
			}
		}
	})
	return found, found != nil
}

// ProposeDiff is the entry point a host app's own AI-assistant tooling
// calls to propose replacing path's content with newText: opens path
// (via OpenFile) if it isn't already, then switches every Pane currently
// showing that Tab into diff-review mode (a read-only red/green line
// diff in the content area, Accept/Cancel in the south bar — see
// diff.go/pane.go's showDiffReview). Accepting applies newText to the
// Tab's Document and writes it to disk; Cancelling discards the proposal,
// leaving the Document untouched. Both are plain synchronous methods, no
// goroutine/channel API — same documented UI-goroutine-only contract as
// AddTab/SplitRight/etc. (see group.go's doc comment).
func (g *Group) ProposeDiff(path string, newText string) error {
	tab, err := g.OpenFile(path)
	if err != nil {
		return err
	}
	tab.pendingDiff = &pendingDiff{newText: newText}
	walkPanes(g.root, func(p *Pane) {
		if p.active == tab {
			p.showDiffReview(tab)
		}
	})
	return nil
}

// acceptDiff is the south bar's Accept-button handler for a Pane in
// diff-review mode over tab: applies tab.pendingDiff's newText to the
// Document (so every other Pane showing the same Tab picks it up live,
// via Document's existing listener mechanism — see document.go), writes
// it to disk, then takes every Pane showing tab back out of diff-review
// mode. A write failure is logged, not propagated — SouthBar's Accept
// button has no error-reporting path today (matches this package's other
// best-effort persistence, e.g. notifyChanged's own log.Printf on a save
// error), but the Document change and diff-review exit still happen
// either way, since the in-memory edit itself succeeded regardless of
// whether the disk write did.
func (g *Group) acceptDiff(tab *Tab) {
	if tab.pendingDiff == nil {
		return
	}
	newText := tab.pendingDiff.newText
	tab.Doc.SetText(newText)
	if err := os.WriteFile(tab.FilePath, []byte(newText), 0o644); err != nil {
		log.Printf("editors: write %s: %v", tab.FilePath, err)
	} else {
		tab.Doc.MarkClean()
	}
	tab.pendingDiff = nil
	g.exitDiffReview(tab)
}

// cancelDiff is the south bar's Cancel-button handler: discards
// tab.pendingDiff without touching the Document, then takes every Pane
// showing tab back out of diff-review mode.
func (g *Group) cancelDiff(tab *Tab) {
	if tab.pendingDiff == nil {
		return
	}
	tab.pendingDiff = nil
	g.exitDiffReview(tab)
}

// exitDiffReview takes every Pane currently showing tab as its active tab
// back to normal (non-diff-review) display.
func (g *Group) exitDiffReview(tab *Tab) {
	walkPanes(g.root, func(p *Pane) {
		if p.active == tab {
			p.exitDiffReview()
		}
	})
}
