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
// PTY library, no cgo. golang.org/x/sys/windows does expose
// InitializeProcThreadAttributeList/UpdateProcThreadAttribute wrapped as
// windows.NewProcThreadAttributeList and its Update/Delete methods, but that
// wrapper's Update takes value as unsafe.Pointer, and passing a Windows
// HANDLE (an integer reinterpreted as a pointer, per the Win32 convention
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE requires) through unsafe.Pointer
// unavoidably trips `go vet`'s unsafeptr check ("possible misuse of
// unsafe.Pointer"), regardless of how the conversion is phrased. Going
// through syscall.NewLazyDLL directly avoids that: LazyProc.Call takes
// uintptr arguments natively, so no unsafe.Pointer conversion is needed at
// all. Still cgo-free — this is a raw syscall, not a C compile.
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

	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE's value is the HPCON handle
	// itself (per the Win32 API contract), passed here as a plain uintptr
	// argument to LazyProc.Call — not a pointer to a variable holding the
	// handle. An earlier version of this code went through
	// windows.ProcThreadAttributeListContainer.Update instead, which
	// requires the value as unsafe.Pointer; converting a handle to
	// unsafe.Pointer that way is what forced the syscall.NewLazyDLL
	// approach here in the first place (see the package doc comment).
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

// Close terminates the session: closes all handles and the spawned process.
// Safe to call more than once.
//
// Task 1's spike found that ClosePseudoConsole alone does not kill the
// spawned shell — it only tears down the pseudo-console, leaving the shell
// running as an orphaned process. TerminateProcess is called explicitly here
// to actually end the child process; this was an observed problem (orphaned
// cmd.exe processes surviving Close), not a speculative precaution.
func (s *conPTYSession) Close() error {
	s.closeOnce.Do(func() {
		windows.TerminateProcess(s.process, 0)
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
