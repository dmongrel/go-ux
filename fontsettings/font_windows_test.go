//go:build windows

package fontsettings

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/theme"
)

func TestIsMonospaceFontFile(t *testing.T) {
	dir := t.TempDir()

	monoPath := filepath.Join(dir, "mono.ttf")
	if err := os.WriteFile(monoPath, theme.DefaultTextMonospaceFont().Content(), 0o644); err != nil {
		t.Fatalf("write mono fixture: %v", err)
	}
	if !isMonospaceFontFile(monoPath) {
		t.Error("isMonospaceFontFile(bundled monospace font) = false, want true")
	}

	propPath := filepath.Join(dir, "prop.ttf")
	if err := os.WriteFile(propPath, theme.DefaultTextFont().Content(), 0o644); err != nil {
		t.Fatalf("write proportional fixture: %v", err)
	}
	if isMonospaceFontFile(propPath) {
		t.Error("isMonospaceFontFile(bundled proportional font) = true, want false")
	}

	if isMonospaceFontFile(filepath.Join(dir, "does-not-exist.ttf")) {
		t.Error("isMonospaceFontFile(missing file) = true, want false")
	}
}

func TestDetectMonospaceFontsNeverErrors(t *testing.T) {
	got := DetectMonospaceFonts()
	got2 := DetectMonospaceFonts()
	if len(got) != len(got2) {
		t.Errorf("DetectMonospaceFonts() not stable across calls: %d then %d results", len(got), len(got2))
	}
}

func TestFontDisplayNameStripsSuffix(t *testing.T) {
	if got := FontDisplayName("Consolas (TrueType)"); got != "Consolas" {
		t.Errorf("FontDisplayName(%q) = %q, want %q", "Consolas (TrueType)", got, "Consolas")
	}
	if got := FontDisplayName("Segoe UI"); got != "Segoe UI" {
		t.Errorf("FontDisplayName(%q) = %q, want %q (no suffix to strip)", "Segoe UI", got, "Segoe UI")
	}
}
