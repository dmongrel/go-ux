// Package test holds fixtures for go-ux's own test suite: an in-memory SQLite
// db.DB plus example data seeding. It is internal to this repo, not exported
// for consumers of the ux library, and is unrelated to the internal/sqlite
// implementation used by the real db package.
package test

import "go-ux/db"

// NewDB opens an in-memory db.DB for use in tests. Callers should Close it
// when done.
func NewDB() (*db.DB, error) {
	return db.Open(":memory:")
}

// SeedExample populates d with the example Terminal and Version Control
// settings used by test_settings.go and package tests.
func SeedExample(d *db.DB) error {
	terminalID, err := d.AddNode(nil, "Terminal", 0)
	if err != nil {
		return err
	}
	if err := d.AddProperty(terminalID, "shell_path", "Shell path", db.PropertyString, "/bin/bash", nil); err != nil {
		return err
	}
	if err := d.AddProperty(terminalID, "close_on_exit", "Close on exit", db.PropertyBool, "true", nil); err != nil {
		return err
	}
	if err := d.AddProperty(terminalID, "tab_width", "Tab width", db.PropertyInt, "4", nil); err != nil {
		return err
	}

	vcsID, err := d.AddNode(nil, "Version Control", 1)
	if err != nil {
		return err
	}
	if err := d.AddProperty(vcsID, "vcs_type", "VCS", db.PropertyEnum, "Git", []string{"Git", "Mercurial", "None"}); err != nil {
		return err
	}
	if err := d.AddProperty(vcsID, "confirm_add", "Confirm on add", db.PropertyBool, "true", nil); err != nil {
		return err
	}

	gitID, err := d.AddNode(&vcsID, "Git", 0)
	if err != nil {
		return err
	}
	if err := d.AddProperty(gitID, "auto_update", "Auto-update on branch switch", db.PropertyBool, "false", nil); err != nil {
		return err
	}

	return nil
}
