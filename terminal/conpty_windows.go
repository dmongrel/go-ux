// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

var (
	kernel32DLL                           = syscall.NewLazyDLL("kernel32.dll")
	procInitializeProcThreadAttributeList = kernel32DLL.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32DLL.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32DLL.NewProc("DeleteProcThreadAttributeList")
)

// errConPTYUnavailable is what newConPTYSession returns when this Windows
// build doesn't export CreatePseudoConsole at all (added in Windows 10
// 1809 / build 17763) — newPtySession (pty_windows.go) treats it as a
// signal to fall back to winpty rather than a real spawn failure worth
// surfacing to the caller.
var errConPTYUnavailable = fmt.Errorf("terminal: ConPTY unavailable on this Windows build")

// conPTYAvailable reports whether kernel32.dll actually exports
// CreatePseudoConsole. golang.org/x/sys/windows resolves that proc lazily
// too (via LazyProc.Addr, which panics — not errors — if the proc can't be
// found), so this must be checked before ever calling windows.
// CreatePseudoConsole/ClosePseudoConsole/ResizePseudoConsole; otherwise a
// pre-1809 Windows build would crash the whole process instead of falling
// back to winpty.
var conPTYAvailable = sync.OnceValue(func() bool {
	return kernel32DLL.NewProc("CreatePseudoConsole").Find() == nil
})

// conPTYSession is the Windows ptySession implementation backed directly by
// ConPTY (CreatePseudoConsole) via golang.org/x/sys/windows — no
// third-party PTY library, no cgo. See winPTYSession's doc comment for why
// this is the preferred backend and winpty is kept as the fallback.
type conPTYSession struct {
	hpc     windows.Handle
	stdin   windows.Handle // ours: overlapped, we Write to it
	stdout  windows.Handle // ours: overlapped, we Read from it
	process windows.Handle
	thread  windows.Handle

	// stdinEvent/stdoutEvent/shutdownEvent/the two mutexes/closed mirror
	// winPTYSession's identical fields — see its doc comments for why each
	// exists; the overlapped-I/O-plus-shutdown-event hazard they guard
	// against is generic to any pipe-backed ptySession, not winpty-specific.
	stdinEvent    windows.Handle
	stdoutEvent   windows.Handle
	shutdownEvent windows.Handle

	stdoutMu sync.Mutex
	stdinMu  sync.Mutex
	closed   atomic.Bool

	// hpcMu/hpcClosed close the same TOCTOU race winPTYSession's
	// winptyMu/wpClosed do for wp, scoped per-session here rather than
	// package-wide since (unlike winpty.dll) ConPTY's API has no
	// documented single-shared-instance concurrency hazard across
	// sessions — each HPCON is independent.
	hpcMu     sync.Mutex
	hpcClosed bool

	closeOnce sync.Once
	closeErr  error
}

func newConPTYSession(def ShellDef, cols, rows int) (ptySession, error) {
	if !conPTYAvailable() {
		return nil, errConPTYUnavailable
	}

	ourIn, theirIn, err := createOverlappedNamedPipe("in", windows.PIPE_ACCESS_OUTBOUND, windows.GENERIC_READ)
	if err != nil {
		return nil, err
	}
	ourOut, theirOut, err := createOverlappedNamedPipe("out", windows.PIPE_ACCESS_INBOUND, windows.GENERIC_WRITE)
	if err != nil {
		windows.CloseHandle(ourIn)
		windows.CloseHandle(theirIn)
		return nil, err
	}

	var hpc windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, theirIn, theirOut, 0, &hpc); err != nil {
		windows.CloseHandle(ourIn)
		windows.CloseHandle(theirIn)
		windows.CloseHandle(ourOut)
		windows.CloseHandle(theirOut)
		return nil, fmt.Errorf("terminal: CreatePseudoConsole: %w", err)
	}
	// The pseudoconsole duplicates these internally; our copies aren't
	// needed once it's created (matches the pipe-handle pattern
	// newWinPTYSession uses for winpty's own conin/conout).
	windows.CloseHandle(theirIn)
	windows.CloseHandle(theirOut)

	// success gates every deferred cleanup below, added incrementally as
	// each resource is acquired — mirrors newWinPTYSession's identical
	// pattern, for the identical reason: every early return before the
	// final one needs everything opened so far closed on the way out,
	// without repeating the whole handle list at each return site.
	success := false
	defer func() {
		if success {
			return
		}
		windows.ClosePseudoConsole(hpc)
		windows.CloseHandle(ourIn)
		windows.CloseHandle(ourOut)
	}()

	attrPtr, attrBuf, err := newPseudoConsoleAttributeList(hpc)
	if err != nil {
		return nil, err
	}
	defer deleteProcThreadAttributeList(attrPtr)
	_ = attrBuf // kept alive through CreateProcess by staying in scope

	var si windows.StartupInfoEx
	si.ProcThreadAttributeList = (*windows.ProcThreadAttributeList)(attrPtr)
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))

	cmdLine, err := syscall.UTF16PtrFromString(commandLine(def))
	if err != nil {
		return nil, fmt.Errorf("terminal: encode command line: %w", err)
	}

	var curDir *uint16
	if def.WorkDir != "" {
		curDir, err = syscall.UTF16PtrFromString(def.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("terminal: encode work dir: %w", err)
		}
	}

	envBlock, err := buildEnvBlock(def.Env)
	if err != nil {
		return nil, fmt.Errorf("terminal: encode environment: %w", err)
	}

	var pi windows.ProcessInformation
	if err := windows.CreateProcess(
		nil, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		envBlock, curDir, &si.StartupInfo, &pi,
	); err != nil {
		return nil, fmt.Errorf("terminal: CreateProcess: %w", err)
	}
	defer func() {
		if !success {
			windows.TerminateProcess(pi.Process, 0)
			windows.CloseHandle(pi.Process)
			windows.CloseHandle(pi.Thread)
		}
	}()

	stdinEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("terminal: create stdin event: %w", err)
	}
	defer func() {
		if !success {
			windows.CloseHandle(stdinEvent)
		}
	}()
	stdoutEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("terminal: create stdout event: %w", err)
	}
	defer func() {
		if !success {
			windows.CloseHandle(stdoutEvent)
		}
	}()
	shutdownEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("terminal: create shutdown event: %w", err)
	}
	defer func() {
		if !success {
			windows.CloseHandle(shutdownEvent)
		}
	}()

	success = true
	return &conPTYSession{
		hpc:           hpc,
		stdin:         ourIn,
		stdout:        ourOut,
		process:       pi.Process,
		thread:        pi.Thread,
		stdinEvent:    stdinEvent,
		stdoutEvent:   stdoutEvent,
		shutdownEvent: shutdownEvent,
	}, nil
}

// createOverlappedNamedPipe creates a unique named pipe and connects both
// ends: an overlapped handle for our own side (serverAccess:
// windows.PIPE_ACCESS_INBOUND or PIPE_ACCESS_OUTBOUND — CreatePipe's
// anonymous pipes can never be overlapped, which is why this exists rather
// than the simpler CreatePipe ConPTY examples typically show) and a plain
// synchronous handle for ConPTY's side (clientAccess: the complementary
// GENERIC_READ/GENERIC_WRITE) — ConPTY manages its own end internally, so
// it never needs to be overlapped. label distinguishes the pipe name
// ("in"/"out") for diagnostics; the actual uniqueness comes from the
// appended UUID, since a fixed name would collide across concurrent
// sessions.
func createOverlappedNamedPipe(label string, serverAccess, clientAccess uint32) (ours, theirs windows.Handle, err error) {
	namePtr, err := syscall.UTF16PtrFromString(`\\.\pipe\go-ux-conpty-` + label + "-" + uuid.NewString())
	if err != nil {
		return 0, 0, fmt.Errorf("terminal: encode pipe name: %w", err)
	}

	ours, err = windows.CreateNamedPipe(
		namePtr,
		serverAccess|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT,
		1, 4096, 4096, 0, nil,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("terminal: CreateNamedPipe %s: %w", label, err)
	}

	theirs, err = windows.CreateFile(namePtr, clientAccess, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		windows.CloseHandle(ours)
		return 0, 0, fmt.Errorf("terminal: connect conpty pipe %s: %w", label, err)
	}
	return ours, theirs, nil
}

// newPseudoConsoleAttributeList builds a one-attribute PROC_THREAD_ATTRIBUTE_LIST
// with PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE set to hpc, for CreateProcess's
// StartupInfoEx.
//
// This goes through syscall.NewLazyDLL directly rather than
// golang.org/x/sys/windows's own windows.NewProcThreadAttributeList +
// ProcThreadAttributeListContainer.Update wrapper: that wrapper's Update
// takes its value as unsafe.Pointer, and passing a Windows HANDLE (an
// integer reinterpreted as a pointer, per the Win32 convention
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE requires) through unsafe.Pointer
// unavoidably trips go vet's unsafeptr check ("possible misuse of
// unsafe.Pointer"), regardless of how the conversion is phrased. Calling
// the raw syscalls avoids that: LazyProc.Call takes uintptr arguments
// natively, so no unsafe.Pointer conversion is needed at all — still
// cgo-free, just a raw syscall rather than a C compile.
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
	// argument — not a pointer to a variable holding the handle.
	r1, _, callErr = procUpdateProcThreadAttribute.Call(
		uintptr(listPtr),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0, 0,
	)
	if r1 == 0 {
		// InitializeProcThreadAttributeList succeeded but this call didn't,
		// so the caller never gets listPtr back and can't defer the
		// matching cleanup itself — do it here instead.
		deleteProcThreadAttributeList(listPtr)
		return nil, nil, fmt.Errorf("terminal: UpdateProcThreadAttribute: %w", callErr)
	}
	return listPtr, buf, nil
}

func deleteProcThreadAttributeList(ptr unsafe.Pointer) {
	procDeleteProcThreadAttributeList.Call(uintptr(ptr))
}

func (s *conPTYSession) Write(p []byte) (int, error) {
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()
	if s.closed.Load() {
		return 0, errSessionClosed
	}
	return overlappedIO(s.stdin, s.stdinEvent, s.shutdownEvent, func(overlapped *windows.Overlapped) (uint32, error) {
		var n uint32
		err := windows.WriteFile(s.stdin, p, &n, overlapped)
		return n, err
	})
}

func (s *conPTYSession) Read(p []byte) (int, error) {
	s.stdoutMu.Lock()
	defer s.stdoutMu.Unlock()
	if s.closed.Load() {
		return 0, errSessionClosed
	}
	return overlappedIO(s.stdout, s.stdoutEvent, s.shutdownEvent, func(overlapped *windows.Overlapped) (uint32, error) {
		var n uint32
		err := windows.ReadFile(s.stdout, p, &n, overlapped)
		return n, err
	})
}

// Resize changes the pseudo-console's dimensions. Unlike winpty, ConPTY is
// a first-class Windows API with genuine pseudo-console semantics — no
// scraping, no synthesized events to nudge — so a single ResizePseudoConsole
// call is sufficient (see winPTYSession.Resize's doc comment for why
// winpty's equivalent needs the nudge this doesn't).
func (s *conPTYSession) Resize(cols, rows int) error {
	s.hpcMu.Lock()
	defer s.hpcMu.Unlock()
	if s.hpcClosed {
		return errSessionClosed
	}
	if err := windows.ResizePseudoConsole(s.hpc, windows.Coord{X: int16(cols), Y: int16(rows)}); err != nil {
		return fmt.Errorf("terminal: ResizePseudoConsole: %w", err)
	}
	return nil
}

// Close terminates the session: kills the spawned shell (ClosePseudoConsole
// alone does not — it only tears down the pseudo-console, leaving the shell
// running as an orphaned process, an observed problem this precaution
// fixes), then closes the pseudo-console and remaining handles. Safe to
// call more than once.
func (s *conPTYSession) Close() error {
	s.closeOnce.Do(func() {
		recordErr := func(err error, wrap string) {
			if err == nil || s.closeErr != nil {
				return
			}
			s.closeErr = fmt.Errorf("terminal: %s: %w", wrap, err)
		}

		recordErr(windows.TerminateProcess(s.process, 0), "TerminateProcess")

		// Wake any blocked Read/Write, stop any new one from starting, then
		// wait out whichever call was already in flight — see
		// winPTYSession.Close's identical sequencing and its doc comments
		// on shutdownEvent/closed for why all three steps are needed
		// before it's safe to close stdin/stdout/the events below.
		windows.SetEvent(s.shutdownEvent)
		s.closed.Store(true)
		s.stdoutMu.Lock()
		s.stdoutMu.Unlock()
		s.stdinMu.Lock()
		s.stdinMu.Unlock()

		s.hpcMu.Lock()
		windows.ClosePseudoConsole(s.hpc) // no error result to capture
		s.hpcClosed = true
		s.hpcMu.Unlock()
		recordErr(windows.CloseHandle(s.stdin), "CloseHandle stdin")
		recordErr(windows.CloseHandle(s.stdout), "CloseHandle stdout")
		recordErr(windows.CloseHandle(s.process), "CloseHandle process")
		recordErr(windows.CloseHandle(s.thread), "CloseHandle thread")
		recordErr(windows.CloseHandle(s.stdinEvent), "CloseHandle stdinEvent")
		recordErr(windows.CloseHandle(s.stdoutEvent), "CloseHandle stdoutEvent")
		recordErr(windows.CloseHandle(s.shutdownEvent), "CloseHandle shutdownEvent")
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
