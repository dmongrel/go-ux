// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

package terminal

import (
	"errors"
	"os"
	"testing"
)

func fakeLookup(found map[string]string, dirs map[string]bool, env map[string]string) lookup {
	return lookup{
		lookPath: func(file string) (string, error) {
			if p, ok := found[file]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		stat: func(name string) (os.FileInfo, error) {
			if dirs[name] {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
		getenv: func(key string) string {
			return env[key]
		},
	}
}

func TestDetectPowerShellPrefersPwsh(t *testing.T) {
	l := fakeLookup(map[string]string{
		"pwsh":       `C:\Program Files\PowerShell\7\pwsh.exe`,
		"powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
	}, nil, nil)

	def, ok := detectPowerShell(l)
	if !ok {
		t.Fatal("expected PowerShell to be detected")
	}
	if def.Name != "PowerShell" {
		t.Errorf("Name = %q, want %q", def.Name, "PowerShell")
	}
	if def.Path != `C:\Program Files\PowerShell\7\pwsh.exe` {
		t.Errorf("Path = %q, want pwsh.exe path (pwsh should be preferred over powershell)", def.Path)
	}
}

func TestDetectPowerShellFallsBackToWindowsPowerShell(t *testing.T) {
	l := fakeLookup(map[string]string{
		"powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
	}, nil, nil)

	def, ok := detectPowerShell(l)
	if !ok {
		t.Fatal("expected PowerShell to be detected via fallback")
	}
	if def.Path != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` {
		t.Errorf("Path = %q, want powershell.exe fallback path", def.Path)
	}
}

func TestDetectPowerShellNotFound(t *testing.T) {
	l := fakeLookup(nil, nil, nil)

	if _, ok := detectPowerShell(l); ok {
		t.Error("expected no PowerShell detected when neither pwsh nor powershell is on PATH")
	}
}

func TestDetectGitBashFromKnownInstallPath(t *testing.T) {
	l := fakeLookup(nil, map[string]bool{
		`C:\Program Files\Git\bin\bash.exe`: true,
	}, nil)

	def, ok := detectGitBash(l)
	if !ok {
		t.Fatal("expected Git Bash to be detected")
	}
	if def.Name != "Git Bash" {
		t.Errorf("Name = %q, want %q", def.Name, "Git Bash")
	}
	if def.Path != `C:\Program Files\Git\bin\bash.exe` {
		t.Errorf("Path = %q, want the known install path", def.Path)
	}
}

func TestDetectGitBashFallsBackToPath(t *testing.T) {
	l := fakeLookup(map[string]string{"bash": `D:\tools\git\bin\bash.exe`}, nil, nil)

	def, ok := detectGitBash(l)
	if !ok {
		t.Fatal("expected Git Bash to be detected via PATH fallback")
	}
	if def.Path != `D:\tools\git\bin\bash.exe` {
		t.Errorf("Path = %q, want PATH-resolved bash.exe", def.Path)
	}
}

func TestDetectGitBashNotFound(t *testing.T) {
	l := fakeLookup(nil, nil, nil)

	if _, ok := detectGitBash(l); ok {
		t.Error("expected no Git Bash detected when neither a known path nor PATH has bash.exe")
	}
}

func TestDetectCmdUsesSystemRoot(t *testing.T) {
	l := fakeLookup(nil, map[string]bool{
		`C:\Windows\System32\cmd.exe`: true,
	}, map[string]string{"SystemRoot": `C:\Windows`})

	def, ok := detectCmd(l)
	if !ok {
		t.Fatal("expected cmd.exe to be detected")
	}
	if def.Path != `C:\Windows\System32\cmd.exe` {
		t.Errorf("Path = %q, want SystemRoot-derived cmd.exe path", def.Path)
	}
}

func TestDetectShellsWithComposesAllDetectors(t *testing.T) {
	l := fakeLookup(
		map[string]string{"pwsh": `C:\pwsh.exe`},
		map[string]bool{`C:\Windows\System32\cmd.exe`: true},
		map[string]string{"SystemRoot": `C:\Windows`},
	)

	shells := detectShellsWith(l)

	if len(shells) != 2 {
		t.Fatalf("shells = %#v, want 2 (PowerShell + cmd.exe, no Git Bash)", shells)
	}
	names := map[string]bool{}
	for _, s := range shells {
		names[s.Name] = true
	}
	if !names["PowerShell"] || !names["cmd.exe"] {
		t.Errorf("names = %#v, want PowerShell and cmd.exe present", names)
	}
	if names["Git Bash"] {
		t.Error("Git Bash should not be present when it can't be detected")
	}
}

