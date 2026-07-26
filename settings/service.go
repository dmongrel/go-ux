// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

// Package settings is the Wails v3 replacement for go-ux's Fyne settings
// control panel: a tree of settings nodes and a generated properties form,
// modeled on IntelliJ Community Edition's Settings dialog. Settings data
// comes from a registry (go-ux/db); no SQLite is touched directly by this
// package. Unlike the original Fyne Window, the tree and form are rendered
// entirely by the frontend (frontend/src/views/settings.ts) — Service only
// serves data and persists staged edits + tree expand/collapse state
// (go-ux/treestate).
package settings

import (
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dmongrel/go-ux/db"
	"github.com/dmongrel/go-ux/treestate"
)

// componentID identifies this settings instance for tree-state persistence
// (go-ux/treestate). Hardcoded per component, as any go-ux window/dialog is.
const componentID = "b6f6c9d1-3f2b-4b8a-9e3a-2f1c7a5d9e10"

// Service is the Wails-bound replacement for go-ux/settings.Window.
// Register it with app.RegisterService(application.NewService(settings.NewService(app, database))).
type Service struct {
	app      *application.App
	db       *db.DB
	tree     *treestate.Tracker
	validIDs map[string]bool

	mu     sync.Mutex
	staged map[int64]map[string]string
}

// NewService builds a settings Service backed by database. It indexes the
// current settings tree immediately (used to filter stale tree-state UIDs
// — see InitialTreeState); a settings instance with an entirely static
// node set is the expected case, matching how go-ux/settings.Window's own
// Fyne widget.Tree data source was likewise fixed for a window's lifetime.
func NewService(app *application.App, database *db.DB) (*Service, error) {
	s := &Service{
		app:    app,
		db:     database,
		tree:   treestate.New(database, componentID+".tree"),
		staged: make(map[int64]map[string]string),
	}
	nodes, err := database.ListSettings()
	if err != nil {
		return nil, err
	}
	s.validIDs = make(map[string]bool, len(nodes))
	for _, n := range nodes {
		s.validIDs[strconv.FormatInt(n.ID, 10)] = true
	}
	return s, nil
}

// ListNodes returns every node in the settings tree. The frontend assembles
// the nested tree itself using Node.ParentID (nil for root nodes).
func (s *Service) ListNodes() ([]db.Node, error) {
	return s.db.ListSettings()
}

// GetProperties returns the properties page contents for one node.
func (s *Service) GetProperties(nodeID int64) ([]db.Property, error) {
	return s.db.GetProperties(nodeID)
}

// AllProperties returns every node's properties, keyed by node ID (as a
// string — Wails/JSON map keys must be strings). Lets the frontend
// implement instant, no-round-trip search across every node's property
// labels, matching go-ux/settings.Window's original applySearch behavior
// (which likewise prefetched every node's properties up front).
func (s *Service) AllProperties() (map[string][]db.Property, error) {
	nodes, err := s.db.ListSettings()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]db.Property, len(nodes))
	for _, n := range nodes {
		props, err := s.db.GetProperties(n.ID)
		if err != nil {
			return nil, err
		}
		out[strconv.FormatInt(n.ID, 10)] = props
	}
	return out, nil
}

// StageProperty records an in-memory edit for nodeID/key, not yet written
// to the db — the Wails equivalent of go-ux/settings.Window's stage,
// called on every form-field change. Apply/Cancel flush or discard it.
func (s *Service) StageProperty(nodeID int64, key string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.staged[nodeID] == nil {
		s.staged[nodeID] = make(map[string]string)
	}
	s.staged[nodeID][key] = value
}

// Apply writes every staged edit to the db (one SaveProperties call per
// node) and clears the staged set — the Wails equivalent of
// go-ux/settings.Window's handleApply/handleOK.
func (s *Service) Apply() error {
	s.mu.Lock()
	staged := s.staged
	s.staged = make(map[int64]map[string]string)
	s.mu.Unlock()

	for nodeID, values := range staged {
		if err := s.db.SaveProperties(nodeID, values); err != nil {
			return err
		}
	}
	return nil
}

// Cancel discards every staged edit without writing it — the Wails
// equivalent of go-ux/settings.Window's handleCancel.
func (s *Service) Cancel() {
	s.mu.Lock()
	s.staged = make(map[int64]map[string]string)
	s.mu.Unlock()
}

// TreeState is the persisted expand/collapse + selection state for this
// settings instance's tree, already filtered against currently valid node
// IDs — the Wails equivalent of go-ux/treestate.Tracker.Restore's
// stale-UID filtering, which this package now does itself since Tracker
// no longer has any notion of tree shape.
type TreeState struct {
	Expanded []string
	Selected string
}

// InitialTreeState returns the persisted tree-state, for the frontend to
// replay (open the given nodes, select the given node) when the tree first
// mounts.
func (s *Service) InitialTreeState() TreeState {
	expanded := make([]string, 0)
	for _, uid := range s.tree.Expanded() {
		if s.validIDs[uid] {
			expanded = append(expanded, uid)
		}
	}
	selected := s.tree.Selected()
	if !s.validIDs[selected] {
		selected = ""
	}
	return TreeState{Expanded: expanded, Selected: selected}
}

// SetExpanded persists uid's expand/collapse state, called on every tree
// toggle.
func (s *Service) SetExpanded(uid string, expanded bool) {
	s.tree.SetExpanded(uid, expanded)
}

// SetSelected persists the selected node, called on every tree selection.
func (s *Service) SetSelected(uid string) {
	s.tree.SetSelected(uid)
}

// OpenWindow opens the settings UI in its own window.
func (s *Service) OpenWindow() {
	s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Settings",
		Width:            1024,
		Height:           800,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#settings",
	})
}

