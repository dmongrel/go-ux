# Reusable Tree-State Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new, reusable `go-ux/treestate` package that persists a `*widget.Tree`'s expand/collapse state and selected node live (on every toggle/selection) to a `go-ux/db.DB`, then wire it into `settings.Window`'s properties tree as its first consumer, so reopening Settings restores the previously-expanded branches and automatically re-renders the last-selected node's properties page.

**Architecture:** `treestate.Track(database, id, tree, opts)` takes over a `*widget.Tree`'s `OnSelected`/`OnBranchOpened`/`OnBranchClosed` callback fields, persisting the whole current expand-set + selection as one JSON blob via `db`'s existing generic `SaveUIState`/`LoadUIState` (already keyed by an arbitrary string ID — no `db` changes needed) after every event, then calling through to caller-supplied `Options` callbacks so existing business logic (e.g. rendering a properties pane) is unaffected. `Tracker.Restore()` reads the blob back and replays it (`OpenBranch`/`Select`), filtered through a caller-supplied `Exists` predicate so stale references from a since-changed tree are silently skipped, guarded so the replay doesn't itself trigger another save.

**Tech Stack:** Go 1.26, Fyne v2.8.0 (`widget.Tree`), `go-ux/db` (existing `SaveUIState`/`LoadUIState`), `encoding/json` (same blob-marshaling pattern `settings.uiState` already uses).

## Global Constraints

- `db/db.go` is not modified — `SaveUIState`/`LoadUIState` are already generic enough; this plan adds no new `db` API.
- `treestate` depends only on `fyne.io/fyne/v2/widget` and `go-ux/db` — no dependency on `go-ux/settings` (the reusability requirement: a future non-settings tree, e.g. a project/files tree, must be able to use this package without depending on `settings`).
- Every persist event writes the *entire* current state (expand set + selection) in one `SaveUIState` call — no debounce, no partial/incremental writes (per the approved design).
- `settings.Window`'s existing window-size/splitter-offset persistence (`uiState`/`saveUIState`/`restoreUIState`, keyed by `componentID`) is unchanged — tree state uses a separate key (`componentID + ".tree"`), never merged into the same blob.
- `terminal.Window` gets no changes in this plan — window-size/position persistence for `terminal` is explicitly out of scope (see design spec).
- Design spec: `docs/superpowers/specs/2026-07-22-tree-state-tracking-design.md` — read it before starting if anything below is unclear on intent.

---

### Task 1: `treestate` package

**Files:**
- Create: `treestate/treestate.go`
- Test: `treestate/treestate_test.go`

**Interfaces:**
- Consumes: `db.DB.SaveUIState(componentID string, blob []byte) error`, `db.DB.LoadUIState(componentID string) ([]byte, error)` (both already exist, unchanged).
- Produces:
  - `type Options struct { Exists func(uid string) bool; OnSelected, OnBranchOpened, OnBranchClosed func(uid string) }`
  - `type Tracker struct { ... }` (all fields unexported)
  - `func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker`
  - `func (t *Tracker) Restore()`

- [ ] **Step 1: Write the failing tests**

Create `treestate/treestate_test.go`:

```go
package treestate_test

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"go-ux/test"
	"go-ux/treestate"
)

// fixtureTree builds a small three-level tree ("" -> a,b ; a -> a1,a2) for
// tests, plus its matching Exists predicate. Each call returns a fresh
// *widget.Tree instance (tests that simulate "reopening" a tree build a
// second one over the same shape) but the same shared expected node set.
func fixtureTree() (*widget.Tree, func(uid string) bool) {
	children := map[string][]string{
		"":  {"a", "b"},
		"a": {"a1", "a2"},
	}
	exists := func(uid string) bool {
		if uid == "" {
			return true
		}
		for _, kids := range children {
			for _, k := range kids {
				if k == uid {
					return true
				}
			}
		}
		return false
	}
	tree := widget.NewTree(
		func(uid string) []string { return children[uid] },
		func(uid string) bool { return len(children[uid]) > 0 },
		func(bool) fyne.CanvasObject { return widget.NewLabel("") },
		func(uid string, branch bool, obj fyne.CanvasObject) { obj.(*widget.Label).SetText(uid) },
	)
	return tree, exists
}

// TestTrackPersistsAndRestoreReopensExpandedAndSelected is the core
// round-trip: state saved by one Tracker (simulating a session) must be
// readable and replayable by a second Tracker over a fresh *widget.Tree
// instance (simulating reopening the window) against the same db.
func TestTrackPersistsAndRestoreReopensExpandedAndSelected(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})

	tree1.OpenBranch("a")
	tree1.Select("a1")

	tree2, _ := fixtureTree()
	var gotSelected string
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{
		Exists:     exists,
		OnSelected: func(uid string) { gotSelected = uid },
	})
	tracker2.Restore()

	if !tree2.IsBranchOpen("a") {
		t.Error("branch \"a\" not restored open")
	}
	if gotSelected != "a1" {
		t.Errorf("OnSelected pass-through fired with %q, want \"a1\"", gotSelected)
	}
}

// TestRestoreSkipsStaleUIDs proves a persisted UID no longer present in the
// current tree (per Exists) is silently dropped — no panic, no fallback
// selection.
func TestRestoreSkipsStaleUIDs(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})
	tree1.OpenBranch("a")
	tree1.Select("a1")

	// A second tree/tracker pair whose Exists rejects "a" and "a1" entirely
	// (as if those nodes were deleted since the state above was saved).
	tree2, _ := fixtureTree()
	goneExists := func(uid string) bool { return uid != "a" && uid != "a1" }
	var gotSelected string
	selectedCalled := false
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{
		Exists:     goneExists,
		OnSelected: func(uid string) { gotSelected = uid; selectedCalled = true },
	})
	tracker2.Restore()

	if tree2.IsBranchOpen("a") {
		t.Error("stale branch \"a\" must not be opened")
	}
	if selectedCalled {
		t.Errorf("OnSelected must not fire for a stale selection, got %q", gotSelected)
	}
}

// TestRestoreDoesNotReSave proves Restore's own OpenBranch/Select calls
// don't themselves trigger another persist — the blob written before
// Restore must be byte-identical to the blob after it.
func TestRestoreDoesNotReSave(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree1, exists := fixtureTree()
	treestate.Track(d, "test.tree", tree1, treestate.Options{Exists: exists})
	tree1.OpenBranch("a")
	tree1.Select("a1")

	before, err := d.LoadUIState("test.tree")
	if err != nil {
		t.Fatalf("LoadUIState (before): %v", err)
	}

	tree2, _ := fixtureTree()
	tracker2 := treestate.Track(d, "test.tree", tree2, treestate.Options{Exists: exists})
	tracker2.Restore()

	after, err := d.LoadUIState("test.tree")
	if err != nil {
		t.Fatalf("LoadUIState (after): %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("Restore changed the persisted blob:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestTrackPassesThroughBranchCallbacksAfterPersisting proves
// OnBranchOpened/OnBranchClosed pass-throughs fire on live (non-restore)
// events, in addition to OnSelected's coverage in the round-trip test
// above.
func TestTrackPassesThroughBranchCallbacksAfterPersisting(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	tree, exists := fixtureTree()
	var opened, closed string
	treestate.Track(d, "test.tree", tree, treestate.Options{
		Exists:         exists,
		OnBranchOpened: func(uid string) { opened = uid },
		OnBranchClosed: func(uid string) { closed = uid },
	})

	tree.OpenBranch("a")
	if opened != "a" {
		t.Errorf("OnBranchOpened pass-through = %q, want \"a\"", opened)
	}
	tree.CloseBranch("a")
	if closed != "a" {
		t.Errorf("OnBranchClosed pass-through = %q, want \"a\"", closed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./treestate/... -v`
Expected: FAIL to build — `treestate` package doesn't exist yet.

- [ ] **Step 3: Implement the package**

Create `treestate/treestate.go`:

```go
// Package treestate persists a *widget.Tree's expand/collapse state and
// selected node, live, reusable across any tree-based UI backed by a
// go-ux/db.DB. It is the Fyne-widget-wiring layer on top of db's already-
// generic UI-state blob store (db.SaveUIState/LoadUIState); db itself
// gains no new API and stays free of any Fyne dependency.
package treestate

import (
	"encoding/json"
	"log"
	"sync"

	"fyne.io/fyne/v2/widget"

	"go-ux/db"
)

// Options configures Track.
type Options struct {
	// Exists reports whether uid is still valid in the tree's current
	// data. Required — Restore uses it to filter stale persisted
	// references (a node that existed in a previous session but was since
	// removed) out of the branches it opens and the node it selects.
	Exists func(uid string) bool

	// OnSelected/OnBranchOpened/OnBranchClosed are optional pass-throughs:
	// Track takes over the tree's corresponding callback field, persists
	// first, then calls these (if non-nil) so the caller's own reaction
	// (e.g. rendering a properties pane) still runs — including during
	// Restore's replay, so a caller like settings.Window still renders
	// the restored selection's properties page.
	OnSelected     func(uid string)
	OnBranchOpened func(uid string)
	OnBranchClosed func(uid string)
}

// Tracker is the handle Track returns.
type Tracker struct {
	database *db.DB
	id       string
	tree     *widget.Tree
	opts     Options

	mu        sync.Mutex
	expanded  map[string]bool
	selected  string
	restoring bool
}

// state is the persisted shape, marshaled as JSON into the db's opaque
// UI-state blob — an implementation detail, never exposed.
type state struct {
	Expanded []string
	Selected string
}

// Track wires database-backed persistence for tree's expand/collapse state
// and selection, keyed by id (caller-chosen, unique per tree instance —
// e.g. "settings.tree"). It takes over tree.OnSelected/OnBranchOpened/
// OnBranchClosed. Every toggle/selection writes the entire current state
// to database immediately — no explicit Save call, no debounce.
//
// Call Track once, after tree's data-provider callbacks are set but
// before it's shown; call the returned Tracker's Restore once the tree is
// attached to its window/container.
func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker {
	t := &Tracker{
		database: database,
		id:       id,
		tree:     tree,
		opts:     opts,
		expanded: make(map[string]bool),
	}

	tree.OnBranchOpened = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		t.expanded[uid] = true
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnBranchOpened != nil {
			opts.OnBranchOpened(uid)
		}
	}
	tree.OnBranchClosed = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		delete(t.expanded, uid)
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnBranchClosed != nil {
			opts.OnBranchClosed(uid)
		}
	}
	tree.OnSelected = func(uid widget.TreeNodeID) {
		t.mu.Lock()
		t.selected = uid
		restoring := t.restoring
		t.mu.Unlock()
		if !restoring {
			t.save()
		}
		if opts.OnSelected != nil {
			opts.OnSelected(uid)
		}
	}

	return t
}

// save writes the tracker's current in-memory state to database under id.
// Errors are logged and swallowed — a UI-state persistence failure must
// never block the tree from working, matching settings.saveUIState's
// existing error-handling convention.
func (t *Tracker) save() {
	t.mu.Lock()
	expanded := make([]string, 0, len(t.expanded))
	for uid := range t.expanded {
		expanded = append(expanded, uid)
	}
	selected := t.selected
	t.mu.Unlock()

	blob, err := json.Marshal(state{Expanded: expanded, Selected: selected})
	if err != nil {
		log.Printf("treestate: marshal state: %v", err)
		return
	}
	if err := t.database.SaveUIState(t.id, blob); err != nil {
		log.Printf("treestate: save state: %v", err)
	}
}

// Restore loads the persisted state (if any) and opens every persisted
// branch / selects the persisted node, each filtered through
// Options.Exists — a stale UID is silently skipped, with no fallback
// selection. Call once, after tree is populated and visible.
//
// The replay (OpenBranch/Select) does not itself trigger another persist
// — restoring is held true for its duration, checked by the wrapped
// callbacks above, so this is idempotent to call against an unmodified
// tree.
func (t *Tracker) Restore() {
	blob, err := t.database.LoadUIState(t.id)
	if err != nil {
		log.Printf("treestate: load state: %v", err)
		return
	}
	if blob == nil {
		return
	}

	var s state
	if err := json.Unmarshal(blob, &s); err != nil {
		log.Printf("treestate: unmarshal state: %v", err)
		return
	}

	t.mu.Lock()
	t.restoring = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.restoring = false
		t.mu.Unlock()
	}()

	for _, uid := range s.Expanded {
		if t.opts.Exists(uid) {
			t.tree.OpenBranch(uid)
		}
	}
	if s.Selected != "" && t.opts.Exists(s.Selected) {
		t.tree.Select(s.Selected)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./treestate/... -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Run with race detector**

Run: `go test ./treestate/... -race -v`
Expected: PASS — `Tracker.mu` guards every field the wrapped callbacks and `Restore` touch; this run is what proves that guarding is actually sufficient, not just present.

- [ ] **Step 6: Run go vet**

Run: `go vet ./treestate/...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add treestate/treestate.go treestate/treestate_test.go
git commit -m "treestate: add reusable tree expand/selection persistence"
```

---

### Task 2: Wire `treestate` into `settings.Window`

**Files:**
- Modify: `settings/settings.go`
- Test: `settings/settings_test.go`

**Interfaces:**
- Consumes: `treestate.Track`/`Options`/`Tracker.Restore` (Task 1).
- Produces: four thin test-only exports — `func (w *Window) TreeForTest() *widget.Tree`, `func (w *Window) SelectedNodeForTest() string`, `func (w *Window) ApplySearchForTest(query string)` — plus a package-level `func TreeComponentIDForTest() string`, all for `settings_test`'s external test package (mirrors the existing `PropertyWidgetForTest`/`HandleOKForTest` pattern from prior work in this file). No change to `settings.Window`'s real public API (`NewWindow`/`SetSize`/`Show` are untouched). Also fixes a latent bug in `applySearch`: it called `tree.OpenAllBranches()`, which bypasses `OnBranchOpened` entirely (see Step 3) — replaced with a per-branch `OpenBranch` loop so search-driven expansion is visible to `treestate.Track`.

- [ ] **Step 1: Write the failing tests**

Add to `settings/settings_test.go` (package `settings_test`, already imports `fynetest "fyne.io/fyne/v2/test"`, `"go-ux/settings"`, `"go-ux/test"` — this step additionally needs `"strconv"` imported, not currently present):

```go
func TestTreeStateRestoresExpandedAndSelectedNodeAcrossReopen(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w1, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var vcsID, gitID int64
	for _, n := range nodes {
		switch n.Description {
		case "Version Control":
			vcsID = n.ID
		case "Git":
			gitID = n.ID
		}
	}
	if vcsID == 0 || gitID == 0 {
		t.Fatal("expected \"Version Control\" and \"Git\" nodes from SeedExample")
	}
	vcsUID := strconv.FormatInt(vcsID, 10)
	gitUID := strconv.FormatInt(gitID, 10)

	w1.TreeForTest().OpenBranch(vcsUID)
	w1.TreeForTest().Select(gitUID)

	if got := w1.SelectedNodeForTest(); got != gitUID {
		t.Fatalf("after selecting Git, SelectedNodeForTest() = %q, want %q", got, gitUID)
	}

	// A fresh Window against the same db must restore both the expanded
	// branch and the selection on its own, with no help from this test.
	w2, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow (reopen): %v", err)
	}
	if !w2.TreeForTest().IsBranchOpen(vcsUID) {
		t.Error("\"Version Control\" branch not restored open on reopen")
	}
	if got := w2.SelectedNodeForTest(); got != gitUID {
		t.Errorf("SelectedNodeForTest() after reopen = %q, want %q (Git)", got, gitUID)
	}
}

func TestTreeStateStaleReferenceSkippedOnRestore(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	// Simulate a previous session that selected/expanded a node which no
	// longer exists (e.g. removed by an app upgrade) by writing a
	// tree-state blob directly, referencing a UID no current node has.
	// settings has no node-deletion API to exercise this more directly.
	if err := d.SaveUIState(settings.TreeComponentIDForTest(), []byte(`{"Expanded":["999999"],"Selected":"999999"}`)); err != nil {
		t.Fatalf("SaveUIState: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w, err := settings.NewWindow(app, d) // must not panic
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	if got := w.SelectedNodeForTest(); got != "" {
		t.Errorf("SelectedNodeForTest() = %q, want empty (stale UID must be skipped, no fallback)", got)
	}
	if w.TreeForTest().IsBranchOpen("999999") {
		t.Error("stale branch UID must not be opened")
	}
}

// TestSearchExpandedBranchesArePersisted covers the applySearch fix this
// task makes: search-driven branch opens (via the per-branch OpenBranch
// loop replacing OpenAllBranches) must persist just like a manual click,
// per the design's "persist whatever's expanded, regardless of cause"
// decision.
func TestSearchExpandedBranchesArePersisted(t *testing.T) {
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()

	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}

	app := fynetest.NewApp()
	defer app.Quit()

	w1, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}

	nodes, err := d.ListSettings()
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	var vcsID int64
	for _, n := range nodes {
		if n.Description == "Version Control" {
			vcsID = n.ID
		}
	}
	if vcsID == 0 {
		t.Fatal("expected a \"Version Control\" node from SeedExample")
	}
	vcsUID := strconv.FormatInt(vcsID, 10)

	// "auto" matches Git's "Auto-update on branch switch" property label,
	// which should reveal and auto-expand its parent, "Version Control".
	w1.ApplySearchForTest("auto")
	if !w1.TreeForTest().IsBranchOpen(vcsUID) {
		t.Fatal("search for \"auto\" did not open \"Version Control\" branch — precondition for this test failed")
	}

	w2, err := settings.NewWindow(app, d)
	if err != nil {
		t.Fatalf("NewWindow (reopen): %v", err)
	}
	if !w2.TreeForTest().IsBranchOpen(vcsUID) {
		t.Error("search-driven branch expansion was not persisted across reopen")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./settings/... -run TestTreeState -v`
Expected: FAIL to build — `TreeForTest`/`SelectedNodeForTest`/`TreeComponentIDForTest`/`ApplySearchForTest` undefined.

- [ ] **Step 3: Wire `treestate` into `settings.go`**

Add `"go-ux/treestate"` to the import block in `settings/settings.go`:

```go
import (
	"encoding/json"
	"image/color"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go-ux/db"
	"go-ux/treestate"
)
```

Remove the line `t.OnSelected = w.selectNode` from the end of `buildTree` — `treestate.Track` takes over `tree.OnSelected` in the next step, so leaving that assignment in `buildTree` would just be silently overwritten (harmless, but confusing to a future reader):

```go
func (w *Window) buildTree() *widget.Tree {
	t := widget.NewTree(
		// ... existing four arguments, unchanged ...
	)
	return t
}
```

In `NewWindow`, right after `w.tree = w.buildTree()`, wire tracking:

```go
	w.tree = w.buildTree()
	tracker := treestate.Track(database, componentID+".tree", w.tree, treestate.Options{
		Exists: func(uid string) bool {
			_, ok := w.byID[uid]
			return ok
		},
		OnSelected: w.selectNode,
	})
```

Then, right after the existing `w.restoreUIState()` call (same place window size/splitter restore already runs), add:

```go
	w.restoreUIState()
	tracker.Restore()
	win.SetCloseIntercept(func() {
```

(`tracker` is a local variable in `NewWindow` — no new `Window` struct field needed, since `Restore` is only ever called once, in this same function, matching the plan's "call Restore once, right after the tree is attached" contract. Do not store it on `Window`.)

Fyne's `widget.Tree.OpenAllBranches()` mutates the tree's internal open-set directly and does **not** call `OnBranchOpened` (confirmed by reading `widget/tree.go` in the Fyne module) — so `applySearch`'s existing `w.tree.OpenAllBranches()` call would silently bypass `treestate.Track`'s persistence, meaning a search-driven expansion would never be saved. Replace that call with a loop that opens each branch node individually, which *does* fire `OnBranchOpened`:

```go
	w.tree.Refresh()
	for uid := range w.byID {
		if len(w.byParent[uid]) > 0 {
			w.tree.OpenBranch(uid)
		}
	}
	if w.selectedUID != "" {
		w.renderProperties(w.selectedUID)
	}
```

(This replaces the single `w.tree.OpenAllBranches()` line in `applySearch`, right after `w.tree.Refresh()` — same resulting UI behavior, every branch node ends up open, but each individual open now goes through `Track`'s wrapped `OnBranchOpened` and gets persisted, per the design's "persist whatever's expanded, regardless of cause" decision.)

- [ ] **Step 4: Add the test-only exports**

Add near the end of `settings/settings.go` (alongside the file's existing test-only exports, if any from other in-flight work — otherwise anywhere after `buildTree`/`selectNode` is fine):

```go
// TreeForTest exposes the underlying *widget.Tree for settings_test's
// external test package (e.g. to check IsBranchOpen after a restore).
func (w *Window) TreeForTest() *widget.Tree {
	return w.tree
}

// SelectedNodeForTest exposes the currently-selected tree node's UID, for
// settings_test's external test package.
func (w *Window) SelectedNodeForTest() string {
	return w.selectedUID
}

// TreeComponentIDForTest exposes the db component ID used for tree-state
// persistence, for settings_test's external test package to write a
// synthetic stale blob against.
func TreeComponentIDForTest() string {
	return componentID + ".tree"
}

// ApplySearchForTest exposes applySearch for settings_test's external test
// package (e.g. to verify search-driven branch expansion persists).
func (w *Window) ApplySearchForTest(query string) {
	w.applySearch(query)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./settings/... -run TestTreeState -v`
Expected: PASS (both new tests).

- [ ] **Step 6: Run the full settings package suite, plain and race**

Run: `go test ./settings/... -v` then `go test ./settings/... -race -v`
Expected: PASS both — confirms no regression in the existing search/staging/OK/Cancel/Apply tests, and that the new live-persistence path (triggered from Fyne callbacks, potentially interleaved with other UI-goroutine work in a real app) is race-clean.

- [ ] **Step 7: Commit**

```bash
git add settings/settings.go settings/settings_test.go
git commit -m "settings: persist and restore tree expand/selection state via treestate"
```

---

### Task 3: Verification and docs

**Files:**
- Create: `treestate.md`
- Modify: `settings.md`, `CLAUDE.md`
- No further source changes.

**Interfaces:** none new — this task is verification + documentation only.

- [ ] **Step 1: Full build/vet across the whole module**

Run: `go build ./...` then `go vet ./...`
Expected: both clean.

- [ ] **Step 2: Full test suite, every package, plain and race**

Run:
```
go test ./... -count=1 -timeout 120s -v
go test ./... -race -count=1 -timeout 120s -v
```
Expected: PASS every package (`db`, `dialog`, `settings`, `treestate`, plus anything else in the module). This branch does not include the (separate, unmerged) terminal font-settings branch's work, so `terminal`'s test run here reflects `master`'s current state, not that branch's.

- [ ] **Step 3: Create `treestate.md`**

```markdown
# `treestate` package

Import path: `go-ux/treestate`

Persists a Fyne `*widget.Tree`'s expand/collapse state and selected node,
live, reusable by any tree-based UI backed by a `*go-ux/db.DB` — not tied
to `go-ux/settings`, whose properties tree is simply its first consumer.

## Public API

```go
type Options struct {
    Exists         func(uid string) bool
    OnSelected     func(uid string)
    OnBranchOpened func(uid string)
    OnBranchClosed func(uid string)
}

func Track(database *db.DB, id string, tree *widget.Tree, opts Options) *Tracker
func (t *Tracker) Restore()
```

`Track` takes over `tree`'s `OnSelected`/`OnBranchOpened`/`OnBranchClosed`
callback fields. Every toggle or selection writes the tree's *entire*
current expand-set and selection to `database`, immediately, as one JSON
blob via `database.SaveUIState(id, blob)` — the same generic, arbitrary-ID
opaque-blob store any `go-ux` component's own UI state uses (see `db.md`).
There's no explicit `Save` call and no debounce.

`opts.Exists` is required: it reports whether a given tree node UID is
still valid in the tree's *current* data. `Restore` uses it to filter out
stale references — a UID that was expanded/selected in a previous session
but no longer exists (e.g. the underlying data changed) is silently
skipped, with no fallback selection.

`opts.OnSelected`/`OnBranchOpened`/`OnBranchClosed` are optional
pass-throughs: `Track` persists first, then calls these (if set), so a
caller's own reaction to tree events (e.g. rendering a properties pane)
keeps working exactly as if it had set the tree's callbacks directly. These
also fire during `Restore`'s replay — e.g. a caller using `OnSelected` to
render a properties pane will see it render for whatever node `Restore`
found persisted as selected.

`id` is caller-chosen and must be unique per tree instance you want tracked
independently — e.g. `"myapp.settings.tree"` for one tree,
`"myapp.projectExplorer.tree"` for another; nothing enforces uniqueness,
duplicate IDs simply share (and clobber each other's) persisted state.

## Minimal usage

```go
tree := widget.NewTree(childUIDsFn, isBranchFn, createFn, updateFn)

tracker := treestate.Track(database, "myapp.explorer.tree", tree, treestate.Options{
    Exists: func(uid string) bool {
        _, ok := myNodes[uid]
        return ok
    },
    OnSelected: func(uid string) {
        renderDetails(uid)
    },
})

// ... attach tree to your window/container, show it ...

tracker.Restore() // after the tree is populated and visible
```

## Restore does not re-persist

`Restore`'s own `OpenBranch`/`Select` calls do not themselves trigger
another `SaveUIState` write — internally guarded for the duration of
`Restore`, so calling it against an already-persisted, unmodified tree is
a no-op from the db's perspective (aside from the one `LoadUIState` read).

## Error handling

A `SaveUIState`/`LoadUIState`/JSON marshal/unmarshal failure is logged
(`log.Printf`) and otherwise swallowed — a UI-state persistence failure
must never block the tree (or its host window) from working. This matches
`go-ux/settings`'s own established convention for its window-level UI
state.
```

- [ ] **Step 4: Update `settings.md`**

In the "Own UI state" section, after the existing paragraph about window size/splitter persistence, add:

```markdown
The properties tree's own expand/collapse state and last-selected node are
persisted separately, via `go-ux/treestate` (see `treestate.md`) — its own
blob, keyed by `componentID + ".tree"`, written live on every branch
toggle/selection rather than only on window close. This is what makes
reopening Settings automatically show the same tree shape and the same
selected node's properties page as when it was last closed.
```

- [ ] **Step 5: Update `CLAUDE.md`'s package layout list**

Add a line for the new package, alphabetically/logically placed among the other one-line package descriptions (find the existing list — `settings/`, `dialog/`, `terminal/`, `db/`, `internal/sqlite/`, `test/` — and add):

```markdown
- `treestate/` — reusable live persistence of a Fyne `*widget.Tree`'s expand/collapse state and selected node, backed by `db`'s generic UI-state blob store; used by `settings`'s properties tree, not tied to it
```

Also add `treestate.md` to the "Docs for downstream consumers" line:

```markdown
Docs for downstream consumers: `settings.md`, `db.md`, `dialog.md`, `terminal.md`, `treestate.md` (project root) — one per package with a public API meant for consumers.
```

- [ ] **Step 6: Commit**

```bash
git add treestate.md settings.md CLAUDE.md
git commit -m "docs: document the treestate package and its settings.Window integration"
```

- [ ] **Step 7: Push**

Only if the user has explicitly asked for a push in this session (check before running) — per this repo's established convention, pushing/PRs happen only on explicit request.

## Out of scope (per design spec)

- `terminal.Window` window size/position persistence.
- A second real-world tree consumer beyond `settings.Window` — this plan only builds the reusable mechanism and its one consumer.
- Debouncing/batching writes.
