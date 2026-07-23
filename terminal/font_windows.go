//go:build windows

package terminal

import (
	"os"
	"path/filepath"

	"go-ux/fontsettings"

	"golang.org/x/sys/windows/registry"
)

// loadSystemFontFile finds family among the system/per-user font registry
// keys (the same ones go-ux/fontsettings' DetectMonospaceFonts enumerates)
// and returns its raw file bytes. ok is false if no matching display name
// is found or its file can't be read — loadMonospaceFace (render.go) treats
// that as "fall back to the bundled font", not an error.
//
// This stays in terminal (not moved to fontsettings alongside
// DetectMonospaceFonts) because it's tied to terminal's own custom canvas
// rasterization — reading raw font bytes to build an x/image/font face —
// which editors has no equivalent of (its content area uses Fyne's native
// widget.Entry, themed via container.NewThemeOverride instead).
func loadSystemFontFile(family string) (data []byte, ok bool) {
	winDir := os.Getenv("SystemRoot")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")

	find := func(root registry.Key, path string, resolvePath func(string) string) (string, bool) {
		key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
		if err != nil {
			return "", false
		}
		defer key.Close()

		valueNames, err := key.ReadValueNames(-1)
		if err != nil {
			return "", false
		}
		for _, regName := range valueNames {
			if fontsettings.FontDisplayName(regName) != family {
				continue
			}
			file, _, err := key.GetStringValue(regName)
			if err != nil || file == "" {
				continue
			}
			return resolvePath(file), true
		}
		return "", false
	}

	if path, found := find(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		func(file string) string {
			if filepath.IsAbs(file) {
				return file
			}
			return filepath.Join(fontsDir, file)
		}); found {
		if data, err := os.ReadFile(path); err == nil {
			return data, true
		}
	}
	if path, found := find(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Fonts`,
		func(file string) string { return file }); found {
		if data, err := os.ReadFile(path); err == nil {
			return data, true
		}
	}
	return nil, false
}
