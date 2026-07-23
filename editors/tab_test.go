package editors

import "testing"

func TestNewTabSetsFields(t *testing.T) {
	tab := NewTab("id-1", "chapter1.md", "/path/chapter1.md", "Once upon a time...")

	if tab.ID != "id-1" {
		t.Errorf("ID = %q, want %q", tab.ID, "id-1")
	}
	if tab.Title != "chapter1.md" {
		t.Errorf("Title = %q, want %q", tab.Title, "chapter1.md")
	}
	if tab.FilePath != "/path/chapter1.md" {
		t.Errorf("FilePath = %q, want %q", tab.FilePath, "/path/chapter1.md")
	}
	if tab.Text != "Once upon a time..." {
		t.Errorf("Text = %q, want %q", tab.Text, "Once upon a time...")
	}
	if tab.Dirty {
		t.Errorf("Dirty = true, want false for a freshly created tab")
	}
}
