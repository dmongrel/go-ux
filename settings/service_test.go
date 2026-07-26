// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package settings_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/dmongrel/go-ux/db"
	"github.com/dmongrel/go-ux/settings"
	"github.com/dmongrel/go-ux/test"
)

func newTestService(t *testing.T) (*settings.Service, *db.DB) {
	t.Helper()
	d, err := test.NewDB()
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := test.SeedExample(d); err != nil {
		t.Fatalf("SeedExample: %v", err)
	}
	s, err := settings.NewService(nil, d)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return s, d
}

func TestListNodesReturnsSeededTree(t *testing.T) {
	s, _ := newTestService(t)
	nodes, err := s.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 3 { // Terminal, Version Control, and Version Control's child Git
		t.Fatalf("len(nodes) = %d, want 3", len(nodes))
	}
}

func TestGetPropertiesReturnsNodeProperties(t *testing.T) {
	s, _ := newTestService(t)
	nodes, err := s.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	var terminalID int64
	for _, n := range nodes {
		if n.Description == "Terminal" {
			terminalID = n.ID
		}
	}
	props, err := s.GetProperties(terminalID)
	if err != nil {
		t.Fatalf("GetProperties: %v", err)
	}
	if len(props) != 3 {
		t.Fatalf("len(props) = %d, want 3", len(props))
	}
}

func TestAllPropertiesCoversEveryNode(t *testing.T) {
	s, _ := newTestService(t)
	all, err := s.AllProperties()
	if err != nil {
		t.Fatalf("AllProperties: %v", err)
	}
	nodes, _ := s.ListNodes()
	if len(all) != len(nodes) {
		t.Fatalf("len(all) = %d, want %d (one entry per node)", len(all), len(nodes))
	}
}

func TestStagePropertyThenApplyPersists(t *testing.T) {
	s, d := newTestService(t)
	nodes, _ := s.ListNodes()
	var terminalID int64
	for _, n := range nodes {
		if n.Description == "Terminal" {
			terminalID = n.ID
		}
	}

	s.StageProperty(terminalID, "shell_path", "/usr/bin/zsh")
	// Not written yet — Apply hasn't run.
	before, _ := d.GetProperties(terminalID)
	for _, p := range before {
		if p.Key == "shell_path" && p.Value != "/bin/bash" {
			t.Fatalf("StageProperty wrote before Apply: shell_path = %q", p.Value)
		}
	}

	if err := s.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	after, _ := d.GetProperties(terminalID)
	found := false
	for _, p := range after {
		if p.Key == "shell_path" {
			found = true
			if p.Value != "/usr/bin/zsh" {
				t.Errorf("shell_path = %q, want /usr/bin/zsh", p.Value)
			}
		}
	}
	if !found {
		t.Fatal("shell_path property not found after Apply")
	}
}

func TestCancelDiscardsStagedEdits(t *testing.T) {
	s, d := newTestService(t)
	nodes, _ := s.ListNodes()
	var terminalID int64
	for _, n := range nodes {
		if n.Description == "Terminal" {
			terminalID = n.ID
		}
	}

	s.StageProperty(terminalID, "shell_path", "/usr/bin/zsh")
	s.Cancel()
	if err := s.Apply(); err != nil { // Apply after Cancel should be a no-op (nothing staged)
		t.Fatalf("Apply: %v", err)
	}

	after, _ := d.GetProperties(terminalID)
	for _, p := range after {
		if p.Key == "shell_path" && p.Value != "/bin/bash" {
			t.Errorf("Cancel did not discard staged edit: shell_path = %q", p.Value)
		}
	}
}

func TestInitialTreeStateRoundTripsExpandedAndSelected(t *testing.T) {
	s, d := newTestService(t)
	nodes, _ := s.ListNodes()
	var vcsID, gitID int64
	for _, n := range nodes {
		switch n.Description {
		case "Version Control":
			vcsID = n.ID
		case "Git":
			gitID = n.ID
		}
	}

	vcsUID := formatID(vcsID)
	gitUID := formatID(gitID)
	s.SetExpanded(vcsUID, true)
	s.SetSelected(gitUID)

	// A fresh Service over the same db (simulating reopening the window)
	// must see the persisted state.
	s2, err := settings.NewService(nil, d)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	got := s2.InitialTreeState()
	if !slices.Contains(got.Expanded, vcsUID) {
		t.Errorf("Expanded = %v, want to contain %q", got.Expanded, vcsUID)
	}
	if got.Selected != gitUID {
		t.Errorf("Selected = %q, want %q", got.Selected, gitUID)
	}
}

func TestInitialTreeStateFiltersStaleSelection(t *testing.T) {
	s, d := newTestService(t)
	s.SetSelected("999999") // never a real node ID in this db

	s2, err := settings.NewService(nil, d)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	got := s2.InitialTreeState()
	if got.Selected != "" {
		t.Errorf("Selected = %q, want \"\" (stale UID must be filtered)", got.Selected)
	}
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}

