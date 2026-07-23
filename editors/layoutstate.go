package editors

import (
	"fmt"
	"path/filepath"
	"sort"

	"fyne.io/fyne/v2"

	"go-ux/db"
)

// NewGroupFromSettings builds a Group like NewGroup, but sources its
// initial layout (panes, splits, open tabs) from database's persisted
// state for groupID (as previously written by this same function's own
// live-save hook — see notifyChanged), and keeps writing to it live
// thereafter: every tab open/close/split/move/resize persists
// immediately, not just on some periodic or close-time save — matching
// this package's design decision to persist live, not settings.Window's
// on-close-only pattern.
//
// If database has no saved layout yet for groupID (a fresh groupID, or
// database itself is nil), this falls back to NewGroup's default: one
// empty primary pane — a caller that forgets to persist, or is using this
// package without a db at all, still gets a working Group.
func NewGroupFromSettings(app fyne.App, database *db.DB, groupID string) *Group {
	g := NewGroup(app)
	g.database = database
	g.groupID = groupID

	if database == nil {
		return g
	}

	if font, fileWatchMode, found, err := readEditorSettings(database, groupID); err == nil && found {
		g.fonts.Set(font)
		g.fileWatchMode = fileWatchMode
	}

	panes, tabs, err := database.LoadEditorLayout(groupID)
	if err != nil || len(panes) == 0 {
		// Nothing saved yet (or a load error, treated the same as "nothing
		// saved" — matches this repo's existing graceful-fallback
		// convention elsewhere) — keep the fresh single-pane default
		// NewGroup already built.
		return g
	}

	g.restoring = true
	root, primary, ok := rebuildTreeFromPersisted(g, panes, tabs)
	g.restoring = false
	if !ok {
		// Persisted data was malformed/inconsistent in some way this
		// function's own writes should never produce — fall back to the
		// safe default rather than leaving Group in a partially-built
		// state.
		return g
	}
	g.root = root
	g.primary = primary
	g.rebuildContent()

	// Re-persist unconditionally (not just when pruneEmpty actually
	// changed something) — idempotent given SaveEditorLayout's
	// full-replace design, and it's what makes a self-heal from stale
	// (e.g. pre-bugfix) persisted state stick: without this, the exact
	// same bad layout would just get loaded, pruned in memory, shown
	// correctly once, and then reappear on the next restart, since
	// nothing ever wrote the healed shape back to database.
	g.notifyChanged()

	return g
}

// buildPersistedLayout walks g.root (pre-order — parent before children,
// satisfying SaveEditorLayout's own ordering requirement) and produces
// the flat []db.EditorPane/[]db.EditorTab shape SaveEditorLayout expects.
// Temporary int64 IDs are assigned sequentially during this walk purely
// to let child rows reference their parent row within the SAME call
// (SaveEditorLayout itself reassigns real IDs on insert and rewrites
// these references — see its doc comment) — they have no meaning beyond
// this one function call and are never compared against previously
// persisted IDs.
func (g *Group) buildPersistedLayout() (panes []db.EditorPane, tabs []db.EditorTab) {
	var nextID int64 = 1
	var walk func(n *node, parent *int64, sortOrder int)
	walk = func(n *node, parent *int64, sortOrder int) {
		id := nextID
		nextID++
		if n.isLeaf() {
			p := n.pane
			panes = append(panes, db.EditorPane{
				ID: id, GroupID: g.groupID, ParentPane: parent,
				IsPane: true, SortOrder: sortOrder, IsPrimary: p == g.primary,
			})
			for i, tab := range p.tabs {
				tabs = append(tabs, db.EditorTab{
					GroupID: g.groupID, PaneID: id, FilePath: tab.FilePath,
					TabOrder: i, IsActive: tab == p.active,
				})
			}
			return
		}
		axisStr := "h"
		if n.axis == axisVertical {
			axisStr = "v"
		}
		panes = append(panes, db.EditorPane{
			ID: id, GroupID: g.groupID, ParentPane: parent,
			IsPane: false, Axis: axisStr, SplitOffset: n.offset, SortOrder: sortOrder,
		})
		walk(n.a, &id, 0)
		walk(n.b, &id, 1)
	}
	walk(g.root, nil, 0)
	return panes, tabs
}

// rebuildTreeFromPersisted turns LoadEditorLayout's flat rows back into a
// live node tree of real *Pane objects (each with its Tabs restored,
// active tab set from IsActive), for g (whose Pane constructor,
// newPane(g, id, isPrimary), needs g as the back-reference every real
// Pane requires — see pane.go). ok is false if panes/tabs don't form a
// well-formed tree (e.g. no root found, a dangling ParentPane reference)
// — NewGroupFromSettings treats that as "fall back to the safe default"
// rather than panicking on data this function itself should never
// actually have been asked to reconstruct, in practice, from data this
// same package wrote.
func rebuildTreeFromPersisted(g *Group, panes []db.EditorPane, tabs []db.EditorTab) (root *node, primary *Pane, ok bool) {
	byID := make(map[int64]db.EditorPane, len(panes))
	var rootRow *db.EditorPane
	for i := range panes {
		byID[panes[i].ID] = panes[i]
		if panes[i].ParentPane == nil {
			if rootRow != nil {
				return nil, nil, false // more than one root — malformed
			}
			rootRow = &panes[i]
		}
	}
	if rootRow == nil {
		return nil, nil, false
	}

	tabsByPane := make(map[int64][]db.EditorTab)
	for _, t := range tabs {
		tabsByPane[t.PaneID] = append(tabsByPane[t.PaneID], t)
	}

	children := make(map[int64][]db.EditorPane) // parent ID -> its children, in SortOrder
	for _, p := range panes {
		if p.ParentPane != nil {
			children[*p.ParentPane] = append(children[*p.ParentPane], p)
		}
	}
	for k := range children {
		sortByOrder(children[k])
	}

	nextPaneNum := 0
	var build func(row db.EditorPane) *node
	build = func(row db.EditorPane) *node {
		if row.IsPane {
			nextPaneNum++
			p := newPane(g, fmt.Sprintf("pane-%d", nextPaneNum), row.IsPrimary)
			if row.IsPrimary {
				primary = p
			}
			rowTabs := tabsByPane[row.ID]
			sortTabsByOrder(rowTabs)
			var activeTab *Tab
			for _, t := range rowTabs {
				tab := NewTab(t.FilePath, filepath.Base(t.FilePath), t.FilePath, "(restored placeholder text for "+t.FilePath+")")
				p.AddTab(tab)
				if t.IsActive {
					activeTab = tab
				}
			}
			if activeTab != nil {
				p.setActive(activeTab)
			}
			return leaf(p)
		}
		kids := children[row.ID]
		if len(kids) != 2 {
			return nil // malformed — a split node must have exactly 2 children
		}
		axis := axisHorizontal
		if row.Axis == "v" {
			axis = axisVertical
		}
		a := build(kids[0])
		b := build(kids[1])
		if a == nil || b == nil {
			return nil
		}
		return &node{axis: axis, offset: row.SplitOffset, a: a, b: b}
	}

	root = build(*rootRow)
	if root == nil || primary == nil {
		return nil, nil, false
	}
	// Self-heal any empty non-primary pane a previous, buggier build might
	// have persisted (see pruneEmpty's doc comment) — a live Group never
	// produces one itself, but old saved state might still have one.
	root = pruneEmpty(root, primary)
	return root, primary, true
}

func sortByOrder(panes []db.EditorPane) {
	sort.Slice(panes, func(i, j int) bool { return panes[i].SortOrder < panes[j].SortOrder })
}

func sortTabsByOrder(tabs []db.EditorTab) {
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].TabOrder < tabs[j].TabOrder })
}
