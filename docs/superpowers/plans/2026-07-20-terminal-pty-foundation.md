# `terminal` Package — PTY Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and prove the ConPTY-backed process layer for `go-ux/terminal` — spawn a real Windows shell through a pseudo-console, read/write it, detect available shells — with zero third-party PTY dependency and zero cgo.

**Architecture:** A `ptySession` interface (`terminal/session.go`) backed by a direct `golang.org/x/sys/windows` ConPTY implementation (`terminal/conpty_windows.go`), plus a standalone shell-detection module (`terminal/shell.go`). No Fyne widget code, no VT parsing, no rendering — this plan is scoped to the layer everything else in the design gets built on top of.

**Tech Stack:** Go 1.26, `golang.org/x/sys/windows` v0.46.0 (already an indirect dependency via Fyne; this plan promotes it to direct), Windows ConPTY (`CreatePseudoConsole`), no cgo.

## Scope note (read before starting)

The full design (`docs/superpowers/plans/2026-07-20-dialog-list-dropdown-properties.md`'s sibling design doc — see `C:\Users\jcaesar\.claude\plans\create-a-new-plan-reflective-fern.md`) lays out 10 phases: PTY+VT+raster spike, shell detection, session management, VT-rendering widget, keyboard input, tabs/window, settings/DB integration, advanced features, shell integration, demo/docs. **This plan covers only the PTY foundation** — the design's Phase 1 narrowed to its PTY-only portion (no VT parsing, no `canvas.Raster` rendering — those need a human at a Windows GUI to visually verify and are not automatable the way process I/O is), plus Phases 2 and 3 in full.

This is a deliberate scope cut, not an oversight: the VT-parsing and rendering work (design Phases 1's visual half, and Phase 4) depends on facts only this foundation can surface — whether `vt10x`'s actual API matches what the design assumed, whether `canvas.Raster` performs acceptably once real PTY output is flowing into it. Plan that work in a follow-up pass once this lands, using whatever this plan's Task 1 spike and Task 3 integration tests reveal.

What this plan produces, concretely: `DetectShells()` finds PowerShell/Git Bash/cmd.exe on the dev machine; a `ptySession` can spawn any of them through a real ConPTY, have text written to its stdin, have its stdout read back, be resized, and be closed cleanly — all proven by an automated `go test` that spawns a real Windows process, no GUI required.

## Global Constraints

- Go 1.26, module `go-ux` (see `go.mod`).
- `golang.org/x/sys v0.46.0` — already present as an indirect dependency (via Fyne); this plan's first task promotes it to a direct `require` line by importing `golang.org/x/sys/windows` directly. Do not bump the version.
- **No cgo, no C compiler, anywhere.** `CGO_ENABLED=0` must build the whole repo including this new code. All Windows API calls go through `golang.org/x/sys/windows`'s existing bindings or `syscall.NewLazyDLL`/`NewProc` for the handful of functions that package doesn't wrap (see Task 1) — never cgo.
- All new files under `terminal/` that touch ConPTY carry `//go:build windows` — this plan targets Windows only; no unix stub is written here (that's future work per the design's "extensible" requirement, not this plan's job).
- Follow existing repo doc-comment style (see `dialog/dialog.go`, `settings/settings.go`): godoc on every exported symbol, no filler comments, comments explain *why* not *what*.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` clean after every task — this dev machine is Windows, so these commands genuinely exercise the `//go:build windows` code, not just compile-check it on a different OS.

---

### Task 1: ConPTY spike — prove the raw Win32 plumbing works

**Files:**
- Create (scratch, not committed): a temp Go module/program under the scratchpad directory — **do not put this under `terminal/` or anywhere in the repo**; this is throwaway exploratory code per the design plan, its only job is to catch syscall-arity/type mistakes empirically before Task 3 writes the real, tested package code.

**Interfaces:**
- Produces: empirical confirmation (or corrected syscall signatures) that feeds directly into Task 3's `conpty_windows.go`. Nothing here is imported by later tasks — only the *validated code shape* carries forward.

This task has no traditional "failing test" step — there is no way to unit-test "does this Win32 syscall sequence actually create a working pseudo-console" without actually calling it on real Windows. That's the nature of a spike; treat `go run` + reading the terminal output as the verification step.

- [ ] **Step 1: Write the scratch spike program**

Create `spike_main.go` in a scratch directory (e.g. `C:\Users\jcaesar\AppData\Local\Temp\claude\...\scratchpad\conpty-spike\`) with its own `go.mod` (`go mod init conpty-spike`, then `go get golang.org/x/sys/windows@v0.46.0`):

```go
package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                               = syscall.NewLazyDLL("kernel32.dll")
	procInitializeProcThreadAttributeList  = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute          = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList      = kernel32.NewProc("DeleteProcThreadAttributeList")
)

// newAttributeList builds a PROC_THREAD_ATTRIBUTE_LIST with a single
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE entry pointing at hpc. Neither
// InitializeProcThreadAttributeList nor UpdateProcThreadAttribute nor
// DeleteProcThreadAttributeList is wrapped by golang.org/x/sys/windows, so
// this calls kernel32.dll directly via syscall.NewLazyDLL — still no cgo,
// just raw syscalls for the handful of functions that package doesn't cover.
func newAttributeList(hpc windows.Handle) (ptr unsafe.Pointer, buf []byte, err error) {
	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, nil, fmt.Errorf("could not determine attribute list size")
	}

	buf = make([]byte, size)
	listPtr := unsafe.Pointer(&buf[0])
	r1, _, callErr := procInitializeProcThreadAttributeList.Call(
		uintptr(listPtr), 1, 0, uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return nil, nil, fmt.Errorf("InitializeProcThreadAttributeList: %w", callErr)
	}

	r1, _, callErr = procUpdateProcThreadAttribute.Call(
		uintptr(listPtr),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0, 0,
	)
	if r1 == 0 {
		return nil, nil, fmt.Errorf("UpdateProcThreadAttribute: %w", callErr)
	}
	return listPtr, buf, nil
}

func deleteAttributeList(ptr unsafe.Pointer) {
	procDeleteProcThreadAttributeList.Call(uintptr(ptr))
}

func main() {
	var stdinRead, stdinWrite, stdoutRead, stdoutWrite windows.Handle
	must(windows.CreatePipe(&stdinRead, &stdinWrite, nil, 0))
	must(windows.CreatePipe(&stdoutRead, &stdoutWrite, nil, 0))

	var hpc windows.Handle
	must(windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, stdinRead, stdoutWrite, 0, &hpc))
	windows.CloseHandle(stdinRead)
	windows.CloseHandle(stdoutWrite)

	attrPtr, attrBuf, err := newAttributeList(hpc)
	must(err)
	_ = attrBuf
	defer deleteAttributeList(attrPtr)

	var si windows.StartupInfoEx
	si.ProcThreadAttributeList = (*windows.ProcThreadAttributeList)(attrPtr)
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))

	cmdLine, err := syscall.UTF16PtrFromString(`powershell.exe -NoLogo`)
	must(err)

	var pi windows.ProcessInformation
	must(windows.CreateProcess(
		nil, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		nil, nil, &si.StartupInfo, &pi,
	))

	// Write a command, then read output for a few seconds and print it to
	// this program's own stdout so we can eyeball it.
	go func() {
		time.Sleep(500 * time.Millisecond)
		windows.WriteFile(stdinWrite, []byte("Write-Output SPIKE_MARKER_12345\r"), nil, nil)
	}()

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			var n uint32
			readErr := windows.ReadFile(stdoutRead, buf, &n, nil)
			if n > 0 {
				os.Stdout.Write(buf[:n])
			}
			if readErr != nil && readErr != io.EOF {
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
	}

	windows.ClosePseudoConsole(hpc)
	windows.CloseHandle(stdinWrite)
	windows.CloseHandle(stdoutRead)
	windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
```

- [ ] **Step 2: Run it and confirm PowerShell actually started and echoed the marker**

Run: `go run .` (from the scratch module directory)
Expected: the program's own stdout shows real PowerShell output — a banner/prompt, and after about half a second, `SPIKE_MARKER_12345` printed back (from the `Write-Output` command written to its stdin). Raw ANSI/VT escape bytes will likely be visible/garbled in the console since nothing is parsing them yet — that's expected and out of scope for this task; the only thing being verified is that bytes are flowing both directions through a real ConPTY-spawned process.

- [ ] **Step 3: Fix any syscall errors that surface**

If `CreatePseudoConsole`, `CreateProcess`, or the attribute-list calls fail, the error return (from `must`'s panic) will name which call and Windows' `GetLastError`-derived message. Common issues to check if something fails: `EXTENDED_STARTUPINFO_PRESENT` must be OR'd into the creation flags (missing it is the most common mistake with this API); `si.StartupInfo.Cb` must be `sizeof(StartupInfoEx)`, not `sizeof(StartupInfo)`; the attribute list buffer (`attrBuf`) must stay alive (not garbage collected) until after `CreateProcess` returns — Go's GC has no visibility into the raw pointer stored in `si.ProcThreadAttributeList`, which is why `attrBuf` is kept as a named variable in scope through the `CreateProcess` call. Iterate until Step 2's expected output is observed.

- [ ] **Step 4: Record findings**

No commit — this code is not part of the repo. Note anything that had to change from the code above (so Task 3 starts from the corrected version, not the draft) as a one-line summary for whoever picks up Task 3 next (append to the task's report file if running under subagent-driven-development, or just keep it in scratch notes otherwise).

---

### Task 2: Shell detection

**Files:**
- Create: `terminal/terminal.go` (package doc comment only — see Step 3)
- Create: `terminal/shell.go`
- Test: `terminal/shell_test.go`

**Interfaces:**
- Produces: `type ShellDef struct { Name, Path string; Args []string; WorkDir string; Env map[string]string }`; `func DetectShells() []ShellDef`.
- Consumes: nothing from Task 1 (independent, no PTY involved) — can be done before or in parallel with Task 1/3, listed second only because it's the more approachable starting point.

Detection logic is injectable (a small `lookup` struct of function fields) so tests never touch the real filesystem or `PATH` — deterministic, no OS flakiness, consistent with this repo's existing avoidance of "mocking what you don't own" (here we're not mocking `os`/`exec` themselves, just injecting the specific three lookup operations this package needs).

- [ ] **Step 1: Write the failing tests**

Create `terminal/shell_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./terminal/... -v`
Expected: FAIL — `terminal` package doesn't exist yet / `lookup`, `detectPowerShell`, `detectGitBash`, `detectCmd`, `detectShellsWith` undefined.

- [ ] **Step 3: Create the package doc comment file**

Create `terminal/terminal.go`:

```go
// Package terminal is an embeddable, tabbed, PTY-backed terminal widget for
// Fyne: GitBash, PowerShell, and cmd.exe on Windows to start. Unlike
// go-ux/dialog and go-ux/settings, this package spawns real OS processes
// through a pseudo-console (ConPTY on Windows — see conpty_windows.go) and
// reads their output on a background goroutine, so it is the first package
// in this repo that must actually use fyne.Do/fyne.DoAndWait rather than
// mutating widgets directly.
//
// This file intentionally holds only the package doc comment; see shell.go,
// session.go, and conpty_windows.go for the actual PTY foundation.
package terminal
```

- [ ] **Step 4: Implement shell detection**

Create `terminal/shell.go`:

```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./terminal/... -v`
Expected: PASS (7 tests)

- [ ] **Step 6: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add terminal/terminal.go terminal/shell.go terminal/shell_test.go
git commit -m "Add terminal package skeleton and shell detection"
```

---

### Task 3: Session/PTY management

**Files:**
- Create: `terminal/session.go`
- Create: `terminal/conpty_windows.go`
- Test: `terminal/session_windows_test.go`

**Interfaces:**
- Consumes: `ShellDef` from Task 2; the validated syscall sequence from Task 1's spike.
- Produces: `type ptySession interface { Write([]byte) (int, error); Read([]byte) (int, error); Resize(cols, rows int) error; Close() error; Wait() error }`; `func newPtySession(def ShellDef, cols, rows int) (ptySession, error)` (Windows-only, defined in `conpty_windows.go`). No Fyne code — the background-goroutine read loop and `fyne.Do` wrapping are explicitly out of scope for this task (that's the design's Phase 4, not this plan).

- [ ] **Step 1: Write the failing test**

Create `terminal/session_windows_test.go`:

```go
//go:build windows

package terminal

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestNewPtySessionSpawnsShellAndEchoesOutput(t *testing.T) {
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newPtySession(def, 80, 24)
	if err != nil {
		t.Fatalf("newPtySession: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Write([]byte("echo SESSION_MARKER_98765\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var out bytes.Buffer
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, readErr := sess.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if bytes.Contains(out.Bytes(), []byte("SESSION_MARKER_98765")) {
				return // success
			}
		}
		if readErr != nil {
			t.Fatalf("Read: %v (output so far: %q)", readErr, out.String())
		}
	}
	t.Fatalf("marker not seen within timeout; output was: %q", out.String())
}

func TestPtySessionCloseIsIdempotent(t *testing.T) {
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newPtySession(def, 80, 24)
	if err != nil {
		t.Fatalf("newPtySession: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close should also succeed (idempotent), got: %v", err)
	}
}

func TestPtySessionResize(t *testing.T) {
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newPtySession(def, 80, 24)
	if err != nil {
		t.Fatalf("newPtySession: %v", err)
	}
	defer sess.Close()

	if err := sess.Resize(120, 40); err != nil {
		t.Errorf("Resize: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./terminal/... -run TestNewPtySession -v`
Expected: FAIL — `newPtySession`/`ptySession` undefined (compile error).

- [ ] **Step 3: Define the `ptySession` interface**

Create `terminal/session.go`:

```go
package terminal

// ptySession is one running shell process attached to a pseudo-console. The
// Windows implementation lives in conpty_windows.go; a future unix
// implementation would provide the same interface over a standard PTY,
// letting everything above this layer stay platform-agnostic.
//
// Reading and writing are both synchronous here — the background read-loop
// goroutine and its fyne.Do-wrapped widget updates belong to the rendering
// widget built on top of this interface, not to this layer.
type ptySession interface {
	// Write sends bytes to the shell's stdin (e.g. typed input).
	Write(p []byte) (int, error)
	// Read reads bytes the shell has written to its stdout/stderr (both are
	// merged into one stream by the pseudo-console, matching real terminal
	// behavior).
	Read(p []byte) (int, error)
	// Resize changes the pseudo-console's dimensions, in character cells.
	Resize(cols, rows int) error
	// Close terminates the session: closes all handles and the spawned
	// process. Safe to call more than once.
	Close() error
	// Wait blocks until the shell process exits and returns its exit
	// status as an error (nil for a clean/zero exit).
	Wait() error
}
```

- [ ] **Step 4: Implement the Windows ConPTY session**

Create `terminal/conpty_windows.go`, starting from Task 1's validated spike code, restructured to satisfy `ptySession` and to be safely closable more than once:

```go
//go:build windows

package terminal

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                              = syscall.NewLazyDLL("kernel32.dll")
	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
)

// conPTYSession is the Windows ptySession implementation, backed directly by
// ConPTY (CreatePseudoConsole) via golang.org/x/sys/windows — no third-party
// PTY library, no cgo. InitializeProcThreadAttributeList,
// UpdateProcThreadAttribute, and DeleteProcThreadAttributeList aren't
// wrapped by that package, so those three are called via syscall.NewLazyDLL
// directly (still cgo-free — this is a raw syscall, not a C compile).
type conPTYSession struct {
	hpc        windows.Handle
	stdinWrite windows.Handle
	stdoutRead windows.Handle
	process    windows.Handle
	thread     windows.Handle

	closeOnce sync.Once
	closeErr  error
}

func newPtySession(def ShellDef, cols, rows int) (ptySession, error) {
	var stdinRead, stdinWrite, stdoutRead, stdoutWrite windows.Handle
	if err := windows.CreatePipe(&stdinRead, &stdinWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("terminal: create stdin pipe: %w", err)
	}
	if err := windows.CreatePipe(&stdoutRead, &stdoutWrite, nil, 0); err != nil {
		windows.CloseHandle(stdinRead)
		windows.CloseHandle(stdinWrite)
		return nil, fmt.Errorf("terminal: create stdout pipe: %w", err)
	}

	var hpc windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, stdinRead, stdoutWrite, 0, &hpc); err != nil {
		windows.CloseHandle(stdinRead)
		windows.CloseHandle(stdinWrite)
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stdoutWrite)
		return nil, fmt.Errorf("terminal: CreatePseudoConsole: %w", err)
	}
	// The pseudoconsole duplicates these internally; our copies aren't
	// needed once it's created.
	windows.CloseHandle(stdinRead)
	windows.CloseHandle(stdoutWrite)

	attrPtr, attrBuf, err := newPseudoConsoleAttributeList(hpc)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		windows.CloseHandle(stdinWrite)
		windows.CloseHandle(stdoutRead)
		return nil, err
	}
	defer deleteProcThreadAttributeList(attrPtr)
	_ = attrBuf // kept alive through CreateProcess by staying in scope

	var si windows.StartupInfoEx
	si.ProcThreadAttributeList = (*windows.ProcThreadAttributeList)(attrPtr)
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))

	cmdLine, err := syscall.UTF16PtrFromString(commandLine(def))
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		windows.CloseHandle(stdinWrite)
		windows.CloseHandle(stdoutRead)
		return nil, fmt.Errorf("terminal: encode command line: %w", err)
	}

	var pi windows.ProcessInformation
	err = windows.CreateProcess(
		nil, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		nil, nil, &si.StartupInfo, &pi,
	)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		windows.CloseHandle(stdinWrite)
		windows.CloseHandle(stdoutRead)
		return nil, fmt.Errorf("terminal: CreateProcess: %w", err)
	}

	return &conPTYSession{
		hpc:        hpc,
		stdinWrite: stdinWrite,
		stdoutRead: stdoutRead,
		process:    pi.Process,
		thread:     pi.Thread,
	}, nil
}

// commandLine renders def into a single Windows command-line string: the
// executable path, quoted if it contains spaces, followed by its arguments.
func commandLine(def ShellDef) string {
	path := def.Path
	if hasSpace(path) {
		path = `"` + path + `"`
	}
	cmd := path
	for _, a := range def.Args {
		cmd += " " + a
	}
	return cmd
}

func hasSpace(s string) bool {
	for _, r := range s {
		if r == ' ' {
			return true
		}
	}
	return false
}

func newPseudoConsoleAttributeList(hpc windows.Handle) (ptr unsafe.Pointer, buf []byte, err error) {
	var size uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, nil, fmt.Errorf("terminal: InitializeProcThreadAttributeList: could not determine buffer size")
	}

	buf = make([]byte, size)
	listPtr := unsafe.Pointer(&buf[0])
	r1, _, callErr := procInitializeProcThreadAttributeList.Call(
		uintptr(listPtr), 1, 0, uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return nil, nil, fmt.Errorf("terminal: InitializeProcThreadAttributeList: %w", callErr)
	}

	r1, _, callErr = procUpdateProcThreadAttribute.Call(
		uintptr(listPtr),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0, 0,
	)
	if r1 == 0 {
		return nil, nil, fmt.Errorf("terminal: UpdateProcThreadAttribute: %w", callErr)
	}
	return listPtr, buf, nil
}

func deleteProcThreadAttributeList(ptr unsafe.Pointer) {
	procDeleteProcThreadAttributeList.Call(uintptr(ptr))
}

func (s *conPTYSession) Write(p []byte) (int, error) {
	var n uint32
	err := windows.WriteFile(s.stdinWrite, p, &n, nil)
	return int(n), err
}

func (s *conPTYSession) Read(p []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(s.stdoutRead, p, &n, nil)
	return int(n), err
}

func (s *conPTYSession) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(s.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (s *conPTYSession) Close() error {
	s.closeOnce.Do(func() {
		windows.ClosePseudoConsole(s.hpc)
		windows.CloseHandle(s.stdinWrite)
		windows.CloseHandle(s.stdoutRead)
		windows.CloseHandle(s.process)
		windows.CloseHandle(s.thread)
	})
	return s.closeErr
}

func (s *conPTYSession) Wait() error {
	if _, err := windows.WaitForSingleObject(s.process, windows.INFINITE); err != nil {
		return fmt.Errorf("terminal: WaitForSingleObject: %w", err)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(s.process, &exitCode); err != nil {
		return fmt.Errorf("terminal: GetExitCodeProcess: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("terminal: process exited with code %d", exitCode)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./terminal/... -v`
Expected: PASS — all shell-detection tests from Task 2, plus `TestNewPtySessionSpawnsShellAndEchoesOutput`, `TestPtySessionCloseIsIdempotent`, `TestPtySessionResize`. If a syscall fails here in a way Task 1's spike didn't surface, debug it the same way (check `EXTENDED_STARTUPINFO_PRESENT`, `Cb` size, attribute-buffer lifetime) — this is still expected iteration territory for code this novel to the repo, not a sign the plan is wrong.

- [ ] **Step 6: Build and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: clean across the whole repo, not just `terminal/`.

- [ ] **Step 7: Commit**

```bash
git add terminal/session.go terminal/conpty_windows.go terminal/session_windows_test.go go.mod go.sum
git commit -m "Add ConPTY-backed ptySession: spawn, read/write, resize, close"
```

(`go.mod`/`go.sum` change because this task's `golang.org/x/sys/windows` import promotes that dependency from indirect to direct — run `go mod tidy` before this commit if it doesn't happen automatically.)

---

## After this plan lands

Reopen the design doc and plan the next slice: `vtstate.go` (wrap `github.com/hinshun/vt10x`, feeding it bytes read from `ptySession.Read`), `render.go` (the `canvas.Raster` rendering — needs a human at a Windows GUI to visually validate, unlike everything in this plan), and `widget.go` (the `fyne.Do`-wrapped background read loop that ties `ptySession` to the renderer). Use this plan's `ptySession` interface and `ShellDef`/`DetectShells()` as fixed, already-tested foundations — don't redesign them as part of that follow-up planning pass.
