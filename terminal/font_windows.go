//go:build windows

package terminal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/sys/windows/registry"
)

// monospaceFontsOnce/monospaceFonts cache DetectMonospaceFonts' result for
// the process lifetime — font installs don't change while this app is
// running, so there's no reason to re-scan (and re-parse every candidate
// font file) on every call.
var (
	monospaceFontsOnce sync.Once
	monospaceFonts     []string
)

// DetectMonospaceFonts scans fonts installed on this machine (system-wide
// and per-user) and returns the names of the ones that are genuinely
// monospace (fixed glyph advance width across a representative ASCII
// sample) — the property that makes a font usable for a terminal grid.
// Mirrors DetectShells()'s contract: never returns an error, an empty
// result on any enumeration failure is handled gracefully by callers
// (font_family's registered enum then only offers "(default)", the bundled
// font).
func DetectMonospaceFonts() []string {
	monospaceFontsOnce.Do(func() {
		monospaceFonts = scanMonospaceFonts()
	})
	return monospaceFonts
}

func scanMonospaceFonts() []string {
	winDir := os.Getenv("SystemRoot")
	if winDir == "" {
		winDir = `C:\Windows`
	}
	fontsDir := filepath.Join(winDir, "Fonts")

	seen := make(map[string]bool)
	var names []string

	// System-installed fonts: HKLM's registry values are filenames relative
	// to %WINDIR%\Fonts.
	scanFontRegistryKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`,
		func(file string) string {
			if filepath.IsAbs(file) {
				return file
			}
			return filepath.Join(fontsDir, file)
		}, seen, &names)

	// Per-user installed fonts: HKCU's registry values are already absolute
	// paths.
	scanFontRegistryKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Fonts`,
		func(file string) string { return file }, seen, &names)

	sort.Strings(names)
	return names
}

// scanFontRegistryKey enumerates one font registry key's values, resolves
// each to a file path via resolvePath, and appends the display name to
// *names (deduplicated via seen) for every file that passes
// isMonospaceFontFile. Any failure to open/read the key is silent — the
// other key (system vs. per-user) may still yield results, and
// DetectMonospaceFonts' own contract is "never error, possibly empty".
func scanFontRegistryKey(root registry.Key, path string, resolvePath func(string) string, seen map[string]bool, names *[]string) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer key.Close()

	valueNames, err := key.ReadValueNames(-1)
	if err != nil {
		return
	}
	for _, regName := range valueNames {
		file, _, err := key.GetStringValue(regName)
		if err != nil || file == "" {
			continue
		}
		display := fontDisplayName(regName)
		if seen[display] {
			continue
		}
		if isMonospaceFontFile(resolvePath(file)) {
			seen[display] = true
			*names = append(*names, display)
		}
	}
}

// fontDisplayName strips the "(TrueType)"/"(OpenType)" suffix Windows adds
// to font registry value names, leaving the plain family name a user would
// recognize (e.g. "Consolas" not "Consolas (TrueType)").
func fontDisplayName(regName string) string {
	if i := strings.LastIndex(regName, " ("); i >= 0 {
		return regName[:i]
	}
	return regName
}

// loadSystemFontFile finds family among the system/per-user font registry
// keys (the same ones scanFontRegistryKey enumerates) and returns its raw
// file bytes. ok is false if no matching display name is found or its file
// can't be read — loadMonospaceFace (render.go) treats that as "fall back
// to the bundled font", not an error.
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
			if fontDisplayName(regName) != family {
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

// isMonospaceFontFile reports whether the font file at path has a fixed
// glyph advance width across a representative sample of ASCII letters,
// digits, and punctuation. Any parse failure (missing file, unsupported
// format, a face that can't report advances) is treated as "not
// monospace" rather than propagated as an error — a single bad font
// shouldn't break the whole scan.
func isMonospaceFontFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	fnt, err := opentype.Parse(data)
	if err != nil {
		return false
	}
	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{Size: 13, DPI: 72, Hinting: xfont.HintingNone})
	if err != nil {
		return false
	}
	defer f.Close()

	var want int32
	for i, r := range "ABCabc012.,;" {
		adv, ok := f.GlyphAdvance(r)
		if !ok {
			return false
		}
		if i == 0 {
			want = int32(adv)
			continue
		}
		if int32(adv) != want {
			return false
		}
	}
	return true
}
