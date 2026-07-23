// Package editors provides a Fyne text-editor-with-tabs component,
// embeddable in a host Go app, with a novel-writing / prose-editing focus
// and a Go API surface for AI-assistant-driven diff review.
package editors

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// splitAxis identifies which way a split node divides its two children.
type splitAxis int

const (
	axisNone       splitAxis = iota // this node is a leaf (pane != nil)
	axisHorizontal                  // left/right split (container.NewHSplit)
	axisVertical                    // top/bottom split (container.NewVSplit)
)

// node is the pure-logic shape of a Group's layout: at most one level of
// split nesting (see package doc / design notes above — a pane inside an
// inner split can never be split again). pane != nil iff this is a leaf;
// a and b are non-nil iff this is a split node (pane == nil). Nodes are
// treated as immutable values: every mutating operation below (split,
// removePane, movePane's tree-shape-changing sibling) returns a NEW root
// rather than mutating the receiver, matching this package's "full
// rebuild, not incremental patch" persistence philosophy.
type node struct {
	pane   *Pane
	axis   splitAxis
	a, b   *node
	offset float64 // meaningful iff axis != axisNone; 0.5 is the sane default

	// live is the *container.Split rebuild most recently created for this
	// node, if it's a split node — a cache of the on-screen widget the user
	// actually drags, kept so syncOffsets can read back its current
	// Offset. This is a deliberate, narrow exception to "nodes are
	// immutable values" (see the type doc comment): Fyne's container.Split
	// has no drag/change callback in v2.8.0 (confirmed against the
	// vendored source — only an unexported divider.DragEnd exists,
	// inaccessible from this package), so there is no push notification
	// when the user finishes dragging a divider. Reading .Offset back out
	// of the live widget on demand (syncOffsets, called before every
	// persisted-layout save) is the only way to capture a resize without
	// either polling on a timer (a background goroutine Phase 1
	// deliberately has none of) or forking container.Split. The practical
	// consequence: a resize is captured the next time anything else
	// changes (a tab open/close/split/move), not the instant the drag
	// ends — see editors.md's persistence section for the same caveat
	// spelled out for API consumers.
	live *container.Split
}

func leaf(p *Pane) *node { return &node{pane: p} }

func (n *node) isLeaf() bool { return n != nil && n.pane != nil }

// findLeaf returns the node wrapping target, and whether target's node is
// itself a direct child of the tree's OUTER split (depth 1) versus nested
// inside an inner split (depth 2) — canSplit uses this to enforce the
// one-level-of-nesting rule. depth 0 means target IS the root (a
// single-pane tree with no split at all yet).
func findLeaf(root *node, target *Pane) (found *node, depth int, ok bool) {
	if root == nil {
		return nil, 0, false
	}
	if root.isLeaf() {
		if root.pane == target {
			return root, 0, true
		}
		return nil, 0, false
	}
	if root.a.isLeaf() && root.a.pane == target {
		return root.a, 1, true
	}
	if root.b.isLeaf() && root.b.pane == target {
		return root.b, 1, true
	}
	// Depth 2: target might be a leaf inside root.a or root.b's own inner
	// split (only possible if root.a/root.b are themselves split nodes).
	if !root.a.isLeaf() {
		if found, _, ok := findLeaf(root.a, target); ok {
			return found, 2, true
		}
	}
	if !root.b.isLeaf() {
		if found, _, ok := findLeaf(root.b, target); ok {
			return found, 2, true
		}
	}
	return nil, 0, false
}

// canSplit reports whether target is eligible to be split further — false
// if target is not present in root at all, or is already nested inside an
// inner split (depth 2).
func canSplit(root *node, target *Pane) bool {
	_, depth, ok := findLeaf(root, target)
	return ok && depth < 2
}

// split replaces the leaf wrapping target within root with a new split
// node of the given axis, whose two children are target's own pane (side
// "a") and newPane (side "b"). Returns the new root and true, or the
// original root and false if target isn't eligible (see canSplit).
func split(root *node, target *Pane, axis splitAxis, newPane *Pane) (newRoot *node, ok bool) {
	if !canSplit(root, target) {
		return root, false
	}
	return replaceLeaf(root, target, &node{axis: axis, a: leaf(target), b: leaf(newPane), offset: 0.5}), true
}

// replaceLeaf returns a new tree identical to root except the leaf
// wrapping target is replaced with replacement. Used by both split and
// (inversely, conceptually) removePane's promotion step.
func replaceLeaf(root *node, target *Pane, replacement *node) *node {
	if root.isLeaf() {
		if root.pane == target {
			return replacement
		}
		return root
	}
	return &node{axis: root.axis, offset: root.offset, a: replaceLeaf(root.a, target, replacement), b: replaceLeaf(root.b, target, replacement)}
}

// removePane removes target from root, promoting target's sibling up one
// level if target was one of two children of a split node. primary is the
// Group's designated primary pane, which can never be removed (see
// design notes) — removePane no-ops (returns root, false) if target ==
// primary, or if target is the tree's sole remaining pane (root itself,
// single-pane tree — same "can't remove the last one" rule, since a
// single-pane tree's only leaf is implicitly primary in practice, but
// check target == primary explicitly rather than inferring that).
func removePane(root *node, target *Pane, primary *Pane) (newRoot *node, ok bool) {
	if target == primary {
		return root, false
	}
	if root.isLeaf() {
		return root, false // target must be root, but root == primary was already excluded above... actually if root.pane == target and target != primary, this is a data inconsistency (a non-primary pane can't be the sole pane) — treat as no-op
	}
	if root.a.isLeaf() && root.a.pane == target {
		return root.b, true
	}
	if root.b.isLeaf() && root.b.pane == target {
		return root.a, true
	}
	// target is nested inside root.a or root.b's own inner split.
	if !root.a.isLeaf() {
		if inner, ok := removePane(root.a, target, primary); ok {
			return &node{axis: root.axis, offset: root.offset, a: inner, b: root.b}, true
		}
	}
	if !root.b.isLeaf() {
		if inner, ok := removePane(root.b, target, primary); ok {
			return &node{axis: root.axis, offset: root.offset, a: root.a, b: inner}, true
		}
	}
	return root, false
}

// pruneEmpty removes every non-primary leaf pane with zero tabs from
// root, promoting siblings exactly as removePane does — repeatedly,
// since promoting a sibling up can itself expose another empty leaf that
// also needs pruning (e.g. a 3-pane layout where both non-primary panes
// happen to be empty). The primary pane is never pruned even if empty
// (see removePane's doc comment — "always need one even if empty").
//
// Under normal live operation an empty non-primary pane can't actually
// arise: split.go's split() always copies a tab into the pane it creates
// (see group.go's splitPane), and Group's closePane already removes a
// non-primary pane the moment its last tab closes. This exists as a
// self-healing pass for persisted state saved by an earlier, buggier
// build (see rebuildTreeFromPersisted) — not a substitute for those two
// call sites keeping the invariant live.
func pruneEmpty(root *node, primary *Pane) *node {
	for {
		empty, found := findEmptyNonPrimaryLeaf(root, primary)
		if !found {
			return root
		}
		newRoot, ok := removePane(root, empty, primary)
		if !ok {
			return root // shouldn't happen — avoid looping forever if it somehow does
		}
		root = newRoot
	}
}

// findEmptyNonPrimaryLeaf returns the first non-primary leaf pane in root
// (if any) with zero tabs.
func findEmptyNonPrimaryLeaf(root *node, primary *Pane) (*Pane, bool) {
	if root == nil {
		return nil, false
	}
	if root.isLeaf() {
		if root.pane != primary && len(root.pane.tabs) == 0 {
			return root.pane, true
		}
		return nil, false
	}
	if p, ok := findEmptyNonPrimaryLeaf(root.a, primary); ok {
		return p, true
	}
	return findEmptyNonPrimaryLeaf(root.b, primary)
}

// walkPanes calls fn for every leaf Pane in root, in "a before b" order.
func walkPanes(root *node, fn func(*Pane)) {
	if root == nil {
		return
	}
	if root.isLeaf() {
		fn(root.pane)
		return
	}
	walkPanes(root.a, fn)
	walkPanes(root.b, fn)
}

// adjacentPane returns the Pane that "move right" (axisHorizontal) or
// "move down" (axisVertical) from source should target, if one already
// exists in root — the sibling pane on the given axis, at whatever depth
// source is at. ok is false if source has no such sibling yet (the caller
// — Group's MoveRight/MoveDown — is then expected to call split first to
// create one, then retry).
func adjacentPane(root *node, source *Pane, axis splitAxis) (target *Pane, ok bool) {
	found, _, ok := findLeaf(root, source)
	if !ok {
		return nil, false
	}
	// Walk root looking for the split node whose direct child (a or b) is
	// `found`, and whose axis matches — its OTHER child (if a leaf) is the
	// adjacent pane.
	var search func(n *node) (target *Pane, ok bool)
	search = func(n *node) (*Pane, bool) {
		if n == nil || n.isLeaf() {
			return nil, false
		}
		if n.axis == axis {
			if n.a == found && n.b.isLeaf() {
				return n.b.pane, true
			}
			if n.b == found && n.a.isLeaf() {
				return n.a.pane, true
			}
		}
		if t, ok := search(n.a); ok {
			return t, true
		}
		return search(n.b)
	}
	return search(root)
}

// rebuild walks root and returns the fyne.CanvasObject a Group should show
// as its content: the lone Pane directly (single-pane tree), or a nested
// container.Split tree exactly matching root's shape. Called after every
// structural change (split/remove/move) — always a full reconstruction,
// never an incremental patch (see package doc).
func rebuild(root *node) fyne.CanvasObject {
	if root.isLeaf() {
		return root.pane
	}
	a := rebuild(root.a)
	b := rebuild(root.b)
	var s *container.Split
	if root.axis == axisHorizontal {
		s = container.NewHSplit(a, b)
	} else {
		s = container.NewVSplit(a, b)
	}
	s.Offset = root.offset
	root.live = s
	return s
}

// syncOffsets recursively copies every split node's live *container.Split
// widget's current Offset back into node.offset, so a save triggered by
// some other change (see node.live's doc comment) persists the user's
// most recent drag position rather than whatever offset was last baked
// into the tree at the previous rebuild. A no-op for a node whose live
// widget hasn't been built yet (e.g. immediately after loading persisted
// state, before the first rebuildContent call).
func syncOffsets(root *node) {
	if root == nil || root.isLeaf() {
		return
	}
	if root.live != nil {
		root.offset = root.live.Offset
	}
	syncOffsets(root.a)
	syncOffsets(root.b)
}
