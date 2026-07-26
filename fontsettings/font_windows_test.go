// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package fontsettings

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestIsMonospaceFontFile(t *testing.T) {
	dir := t.TempDir()

	monoPath := filepath.Join(dir, "mono.ttf")
	if err := os.WriteFile(monoPath, gomono.TTF, 0o644); err != nil {
		t.Fatalf("write mono fixture: %v", err)
	}
	if !isMonospaceFontFile(monoPath) {
		t.Error("isMonospaceFontFile(Go Mono) = false, want true")
	}

	propPath := filepath.Join(dir, "prop.ttf")
	if err := os.WriteFile(propPath, goregular.TTF, 0o644); err != nil {
		t.Fatalf("write proportional fixture: %v", err)
	}
	if isMonospaceFontFile(propPath) {
		t.Error("isMonospaceFontFile(Go Regular, proportional) = true, want false")
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

