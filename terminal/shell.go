package terminal

import (
	"os"
	"os/exec"
)

// ShellDef describes one runnable shell: its display name, the executable
// to spawn, default arguments, and optional working directory / environment
// overrides for the session it starts.
type ShellDef struct {
	Name    string            // display name: "PowerShell", "Git Bash", "cmd.exe"
	Path    string            // absolute or PATH-resolved executable
	Args    []string
	WorkDir string            // optional; empty = inherit host process's cwd
	Env     map[string]string // optional extra/override env vars for this session
}

// lookup is the set of OS-touching operations shell detection needs,
// injected so detection logic is testable without touching the real
// filesystem or PATH.
type lookup struct {
	lookPath func(file string) (string, error)
	stat     func(name string) (os.FileInfo, error)
	getenv   func(key string) string
}

// DetectShells returns the shells this package can find on the current
// machine. On Windows: PowerShell (pwsh.exe preferred, else powershell.exe),
// Git Bash, and cmd.exe. Only shells actually found are returned — callers
// should handle a PowerShell-only (or even empty) result gracefully.
func DetectShells() []ShellDef {
	return detectShellsWith(lookup{lookPath: exec.LookPath, stat: os.Stat, getenv: os.Getenv})
}

func detectShellsWith(l lookup) []ShellDef {
	var shells []ShellDef
	if def, ok := detectPowerShell(l); ok {
		shells = append(shells, def)
	}
	if def, ok := detectGitBash(l); ok {
		shells = append(shells, def)
	}
	if def, ok := detectCmd(l); ok {
		shells = append(shells, def)
	}
	return shells
}

// detectPowerShell prefers pwsh.exe (PowerShell 7+) over the legacy
// powershell.exe when both are on PATH.
//
// A future unix equivalent would look for "pwsh" the same way (PowerShell 7
// is cross-platform); there is no legacy-powershell.exe equivalent to fall
// back to outside Windows.
func detectPowerShell(l lookup) (ShellDef, bool) {
	if path, err := l.lookPath("pwsh"); err == nil {
		return ShellDef{Name: "PowerShell", Path: path}, true
	}
	if path, err := l.lookPath("powershell"); err == nil {
		return ShellDef{Name: "PowerShell", Path: path}, true
	}
	return ShellDef{}, false
}

// detectGitBash checks the standard Git-for-Windows install locations before
// falling back to whatever "bash" resolves to on PATH (which, on a machine
// with WSL installed, may not be Git Bash — the explicit paths are checked
// first specifically to avoid that ambiguity).
//
// A future unix equivalent would just be l.lookPath("bash") — there's no
// install-location ambiguity to resolve outside Windows.
func detectGitBash(l lookup) (ShellDef, bool) {
	for _, candidate := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if _, err := l.stat(candidate); err == nil {
			return ShellDef{Name: "Git Bash", Path: candidate}, true
		}
	}
	if path, err := l.lookPath("bash"); err == nil {
		return ShellDef{Name: "Git Bash", Path: path}, true
	}
	return ShellDef{}, false
}

// detectCmd resolves cmd.exe via %SystemRoot%, which is always set on
// Windows. cmd.exe has no non-Windows equivalent, so there is nothing for a
// future unix detector to mirror here.
func detectCmd(l lookup) (ShellDef, bool) {
	root := l.getenv("SystemRoot")
	if root == "" {
		return ShellDef{}, false
	}
	path := root + `\System32\cmd.exe`
	if _, err := l.stat(path); err != nil {
		return ShellDef{}, false
	}
	return ShellDef{Name: "cmd.exe", Path: path}, true
}
