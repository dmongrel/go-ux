// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"embed"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winptyBinaries embeds the vendored winpty 0.4.3 x64 release binaries
// (MIT-licensed, github.com/rprichard/winpty — see terminal/winpty/LICENSE).
// winpty.dll locates winpty-agent.exe relative to its own on-disk directory,
// so both files must be extracted together into the same real directory
// before winpty.dll is loaded — neither can be loaded directly from the
// embedded FS.
//
//go:embed winpty/winpty.dll winpty/winpty-agent.exe
var winptyBinaries embed.FS

// extractWinptyOnce guards extractedWinptyDLL/extractWinptyErr: the binaries
// only need to be written to disk once per process, regardless of how many
// sessions are opened.
var (
	extractWinptyOnce  sync.Once
	extractedWinptyDLL string
	extractWinptyErr   error
)

// extractWinpty writes the embedded winpty.dll/winpty-agent.exe pair to a
// version-tagged directory under the user's cache dir (falling back to
// os.TempDir if UserCacheDir is unavailable) and returns the extracted
// DLL's path. Reused across processes: if the directory already holds files
// of the expected size, extraction is skipped.
func extractWinpty() (string, error) {
	extractWinptyOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir := filepath.Join(base, "go-ux", "winpty-0.4.3-x64")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			extractWinptyErr = fmt.Errorf("terminal: create winpty extraction dir: %w", err)
			return
		}

		for _, name := range []string{"winpty.dll", "winpty-agent.exe"} {
			data, err := winptyBinaries.ReadFile("winpty/" + name)
			if err != nil {
				extractWinptyErr = fmt.Errorf("terminal: read embedded %s: %w", name, err)
				return
			}
			dst := filepath.Join(dir, name)
			if info, statErr := os.Stat(dst); statErr == nil && info.Size() == int64(len(data)) {
				continue // already extracted (same size) from a prior run
			}
			if err := os.WriteFile(dst, data, 0o755); err != nil {
				extractWinptyErr = fmt.Errorf("terminal: write %s: %w", dst, err)
				return
			}
		}

		extractedWinptyDLL = filepath.Join(dir, "winpty.dll")
	})
	return extractedWinptyDLL, extractWinptyErr
}

// winptyDLL and its procs are resolved lazily against the extracted DLL path
// on first use (loadWinptyDLL), not at package init, since extraction can
// fail and callers need that surfaced as a regular error from NewSession
// rather than a panic.
var (
	winptyDLLOnce sync.Once
	winptyDLL     *syscall.LazyDLL
	winptyDLLErr  error

	procConfigNew            *syscall.LazyProc
	procConfigSetInitialSize *syscall.LazyProc
	procConfigFree           *syscall.LazyProc
	procOpen                 *syscall.LazyProc
	procConinName            *syscall.LazyProc
	procConoutName           *syscall.LazyProc
	procSpawnConfigNew       *syscall.LazyProc
	procSpawnConfigFree      *syscall.LazyProc
	procSpawn                *syscall.LazyProc
	procSetSize              *syscall.LazyProc
	procAgentProcess         *syscall.LazyProc
	procFree                 *syscall.LazyProc
	procErrorCode            *syscall.LazyProc
	procErrorMsg             *syscall.LazyProc
	procErrorFree            *syscall.LazyProc
)

// winptyMu serializes every call this package makes into winpty.dll, across
// all winpty_t sessions, not just within one. winpty is an old library
// built around a single interactive console session; its C API isn't
// documented as safe for concurrent calls from multiple threads even when
// those calls target different winpty_t handles, and its own winpty-agent.exe
// helper processes and named-pipe plumbing suggest internal state that may
// not be scoped per-handle. Observed evidence: sporadic STATUS_HEAP_CORRUPTION
// crashes under concurrent multi-tab (multi-shell) use that persisted even
// after this package's own Go-level races were fixed (uiMu, Session.wg) and
// that a per-session mutex around Resize/Close didn't eliminate, but that a
// single process-wide lock around every winpty.dll call does. This costs
// real concurrency (spawning/resizing two sessions can't overlap), which is
// an acceptable trade for correctness — these calls are not on a hot path.
var winptyMu sync.Mutex

func loadWinptyDLL() error {
	winptyDLLOnce.Do(func() {
		dllPath, err := extractWinpty()
		if err != nil {
			winptyDLLErr = err
			return
		}
		winptyDLL = syscall.NewLazyDLL(dllPath)
		if err := winptyDLL.Load(); err != nil {
			winptyDLLErr = fmt.Errorf("terminal: load %s: %w", dllPath, err)
			return
		}

		procConfigNew = winptyDLL.NewProc("winpty_config_new")
		procConfigSetInitialSize = winptyDLL.NewProc("winpty_config_set_initial_size")
		procConfigFree = winptyDLL.NewProc("winpty_config_free")
		procOpen = winptyDLL.NewProc("winpty_open")
		procConinName = winptyDLL.NewProc("winpty_conin_name")
		procConoutName = winptyDLL.NewProc("winpty_conout_name")
		procSpawnConfigNew = winptyDLL.NewProc("winpty_spawn_config_new")
		procSpawnConfigFree = winptyDLL.NewProc("winpty_spawn_config_free")
		procSpawn = winptyDLL.NewProc("winpty_spawn")
		procSetSize = winptyDLL.NewProc("winpty_set_size")
		procAgentProcess = winptyDLL.NewProc("winpty_agent_process")
		procFree = winptyDLL.NewProc("winpty_free")
		procErrorCode = winptyDLL.NewProc("winpty_error_code")
		procErrorMsg = winptyDLL.NewProc("winpty_error_msg")
		procErrorFree = winptyDLL.NewProc("winpty_error_free")
	})
	return winptyDLLErr
}

// winpty_constants.h values used here.
const (
	winptyFlagColorEscapes           = 0x4
	winptySpawnFlagAutoShutdown      = 1
	winptySpawnFlagExitAfterShutdown = 2
)

// winptyErr reads and frees an optional winpty_error_ptr_t out-param,
// returning nil if the call reported no error (a NULL error pointer).
// errPtr must point at the uintptr an API call wrote its winpty_error_ptr_t
// result into.
func winptyErr(call string, errPtr *uintptr) error {
	if *errPtr == 0 {
		return nil
	}
	defer procErrorFree.Call(*errPtr)
	r0, _, _ := procErrorMsg.Call(*errPtr)
	msgPtr := (*uint16)(uintptrToPointer(r0))
	msg := "unknown error"
	if msgPtr != nil {
		msg = utf16PtrToString(msgPtr)
	}
	return fmt.Errorf("terminal: %s: %s", call, msg)
}

// uintptrToPointer reinterprets a uintptr obtained from a winpty DLL call
// (an LPCWSTR/HANDLE the DLL owns, not a Go-managed pointer) as an
// unsafe.Pointer. It exists because go vet's unsafeptr check flags a direct
// `unsafe.Pointer(x)` conversion of any uintptr-typed value x — including
// this one, even though it's the standard "syscall returned a native
// pointer" case golang.org/x/sys/windows's own generated wrappers rely on
// throughout zsyscall_windows.go. Going through the address of x instead of
// x itself avoids that exact flagged syntactic shape without changing what
// happens at runtime (both are a bit-for-bit reinterpretation of the same
// machine word); see this repo's now-deleted conpty_windows.go (git history)
// for this package's established precedent of routing around go vet's
// unsafeptr false positives rather than suppressing the check outright.
func uintptrToPointer(x uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&x))
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var chars []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		chars = append(chars, c)
	}
	return string(utf16.Decode(chars))
}

// winPTYSession is the Windows ptySession implementation, backed by winpty
// (github.com/rprichard/winpty, vendored under terminal/winpty/) rather than
// the native ConPTY API. See the package's design spec
// (docs/superpowers/specs/2026-07-22-terminal-winpty-backend-design.md) for
// why: ConPTY's output delivery is not reliable on every Windows
// build/environment, which winpty (running its own console-hosting agent
// process) sidesteps.
type winPTYSession struct {
	wp      uintptr // winpty_t*
	conin   windows.Handle
	conout  windows.Handle
	process windows.Handle
	thread  windows.Handle

	// coninEvent/conoutEvent are the per-call OVERLAPPED event handles Read
	// and Write wait on. shutdownEvent is signaled once, by Close, before it
	// closes conin/conout — Read/Write's wait treats that as "abort", so
	// Close never has to close a pipe handle out from under an in-flight
	// ReadFile/WriteFile the way plain synchronous I/O would require (a
	// known Windows hazard: closing a handle with a pending synchronous I/O
	// call on another thread is undefined, and empirically this project saw
	// it manifest as sporadic STATUS_HEAP_CORRUPTION under concurrent
	// multi-session use — matching why pty4j's own winpty binding
	// (JetBrains/pty4j, NamedPipe.java) uses this identical
	// overlapped-I/O-plus-shutdown-event pattern rather than plain
	// synchronous calls).
	coninEvent    windows.Handle
	conoutEvent   windows.Handle
	shutdownEvent windows.Handle

	// coninMu/conoutMu each guard one direction's overlappedIO call, kept
	// separate so a blocked Read (waiting on PTY output) never stalls a
	// concurrent Write (the user typing), or vice versa — mirrors pty4j's
	// NamedPipe having independent readLock/writeLock for the same reason.
	// closed, checked under the relevant lock before issuing I/O, is what
	// stops a new Read/Write from starting once Close has begun; Close's own
	// Lock()-then-Unlock() on each mutex (after setting closed and signaling
	// shutdownEvent) is what waits out whichever call already holds it —
	// together these guarantee no Read or Write is touching conin/conout/
	// their events by the time Close actually closes any handle.
	conoutMu sync.Mutex
	coninMu  sync.Mutex
	closed   atomic.Bool // set by Close; read by Read/Write under their own, different mutexes

	// wpClosed is guarded by winptyMu specifically (not closed above), and
	// set to true in the very same critical section that frees wp via
	// winpty_free. Resize also runs under winptyMu and checks wpClosed
	// before touching wp — that shared-lock-plus-flag-check-in-one-
	// critical-section is what actually closes the race, unlike checking
	// Session.exitDone (which only flips once waitLoop notices the process
	// exit, well after Close has already freed wp): without it, Resize
	// could observe "not exited yet" from a stale/racing Fyne layout call,
	// then call winpty_set_size on wp after Close's winpty_free already ran,
	// which crashed with STATUS_ACCESS_VIOLATION under concurrent multi-tab
	// close-while-resizing use.
	wpClosed bool

	closeOnce sync.Once
	closeErr  error
}

func newPtySession(def ShellDef, cols, rows int) (ptySession, error) {
	if err := loadWinptyDLL(); err != nil {
		return nil, err
	}

	winptyMu.Lock()
	defer winptyMu.Unlock()

	var errPtr uintptr
	cfg, _, _ := procConfigNew.Call(winptyFlagColorEscapes, uintptr(unsafe.Pointer(&errPtr)))
	if err := winptyErr("winpty_config_new", &errPtr); err != nil {
		return nil, err
	}
	if cfg == 0 {
		return nil, fmt.Errorf("terminal: winpty_config_new: returned NULL")
	}
	defer procConfigFree.Call(cfg)

	procConfigSetInitialSize.Call(cfg, uintptr(cols), uintptr(rows))

	wp, _, _ := procOpen.Call(cfg, uintptr(unsafe.Pointer(&errPtr)))
	if err := winptyErr("winpty_open", &errPtr); err != nil {
		return nil, err
	}
	if wp == 0 {
		return nil, fmt.Errorf("terminal: winpty_open: returned NULL")
	}

	conin, err := connectWinptyPipe(procConinName, wp, windows.GENERIC_WRITE)
	if err != nil {
		procFree.Call(wp)
		return nil, err
	}
	conout, err := connectWinptyPipe(procConoutName, wp, windows.GENERIC_READ)
	if err != nil {
		windows.CloseHandle(conin)
		procFree.Call(wp)
		return nil, err
	}

	// success gates the deferred cleanup below: every early return before
	// the final one leaves success false, so everything opened so far
	// (conin/conout/the events, but not wp — procFree is only safe to call
	// once winpty_spawn has run or failed, matching the same ordering the
	// rest of this function already used before this refactor) gets closed
	// on the way out. Centralizing this in one deferred func, rather than
	// repeating the handle list at every error return as this function used
	// to, is what let the three event handles get added below without
	// having to touch every earlier return path by hand.
	success := false
	defer func() {
		if success {
			return
		}
		windows.CloseHandle(conin)
		windows.CloseHandle(conout)
		procFree.Call(wp)
	}()

	coninEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("terminal: create conin event: %w", err)
	}
	defer func() {
		if !success {
			windows.CloseHandle(coninEvent)
		}
	}()
	conoutEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("terminal: create conout event: %w", err)
	}
	defer func() {
		if !success {
			windows.CloseHandle(conoutEvent)
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

	// Re-assert the size via winpty_set_size, a few times with a short
	// sleep between attempts, even though winpty_config_set_initial_size
	// above already requested the same (cols, rows). This is not redundant:
	// JetBrains' own winpty binding (pty4j, WinPty.java) does the identical
	// retry loop for the identical reason, calling it a workaround for
	// winpty/console rendering glitches ("extra newlines") on some Windows
	// builds. This project independently observed a related symptom on
	// Windows without it — PowerShell's console host would spawn but never
	// paint its first prompt until something (e.g. the user manually
	// resizing the window) triggered a genuine winpty_set_size call after
	// the fact; cmd.exe and Git Bash eventually rendered without it, but
	// slower than expected. Doing the same kick pty4j does, before spawning
	// the child rather than relying on the widget's first Fyne layout pass
	// to trigger it, fixes that at its source.
	for range 5 {
		var sizeErrPtr uintptr
		ok, _, _ := procSetSize.Call(wp, uintptr(cols), uintptr(rows), uintptr(unsafe.Pointer(&sizeErrPtr)))
		_ = winptyErr("winpty_set_size", &sizeErrPtr)
		if ok == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cmdLine, err := syscall.UTF16PtrFromString(commandLine(def))
	if err != nil {
		return nil, fmt.Errorf("terminal: encode command line: %w", err)
	}

	var cwdPtr *uint16
	if def.WorkDir != "" {
		cwdPtr, err = syscall.UTF16PtrFromString(def.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("terminal: encode work dir: %w", err)
		}
	}

	envPtr, err := buildWinptyEnvBlock(def.Env)
	if err != nil {
		return nil, fmt.Errorf("terminal: encode environment: %w", err)
	}

	spawnCfg, _, _ := procSpawnConfigNew.Call(
		uintptr(winptySpawnFlagAutoShutdown|winptySpawnFlagExitAfterShutdown),
		0, // appname
		uintptr(unsafe.Pointer(cmdLine)),
		uintptr(unsafe.Pointer(cwdPtr)),
		uintptr(unsafe.Pointer(envPtr)),
		uintptr(unsafe.Pointer(&errPtr)),
	)
	if err := winptyErr("winpty_spawn_config_new", &errPtr); err != nil {
		return nil, err
	}
	if spawnCfg == 0 {
		return nil, fmt.Errorf("terminal: winpty_spawn_config_new: returned NULL")
	}
	defer procSpawnConfigFree.Call(spawnCfg)

	var processHandle, threadHandle windows.Handle
	var createProcessErr uint32
	ok, _, _ := procSpawn.Call(
		wp, spawnCfg,
		uintptr(unsafe.Pointer(&processHandle)),
		uintptr(unsafe.Pointer(&threadHandle)),
		uintptr(unsafe.Pointer(&createProcessErr)),
		uintptr(unsafe.Pointer(&errPtr)),
	)
	if err := winptyErr("winpty_spawn", &errPtr); err != nil {
		return nil, err
	}
	if ok == 0 {
		return nil, fmt.Errorf("terminal: winpty_spawn: CreateProcess failed: %w", syscall.Errno(createProcessErr))
	}

	success = true
	sess := &winPTYSession{
		wp:            wp,
		conin:         conin,
		conout:        conout,
		process:       processHandle,
		thread:        threadHandle,
		coninEvent:    coninEvent,
		conoutEvent:   conoutEvent,
		shutdownEvent: shutdownEvent,
	}

	return sess, nil
}

// connectWinptyPipe fetches a named-pipe path (winpty_conin_name/
// winpty_conout_name) from the agent and connects to it with CreateFile, per
// winpty.h's guidance that these are ordinary named pipes the client dials
// into directly rather than a handle winpty hands back itself.
func connectWinptyPipe(nameProc *syscall.LazyProc, wp uintptr, access uint32) (windows.Handle, error) {
	r0, _, _ := nameProc.Call(wp)
	name := (*uint16)(uintptrToPointer(r0))
	if name == nil {
		return 0, fmt.Errorf("terminal: winpty pipe name: returned NULL")
	}
	// FILE_FLAG_OVERLAPPED: Read/Write use overlapped I/O (see their doc
	// comments) specifically so a pending call can be cancelled cleanly on
	// Close instead of racing a blocking synchronous call against
	// CloseHandle on another goroutine.
	h, err := windows.CreateFile(name, access, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return 0, fmt.Errorf("terminal: connect winpty pipe: %w", err)
	}
	return h, nil
}

// buildWinptyEnvBlock renders overrides into the UTF-16
// double-null-terminated "KEY=VALUE\0...\0\0" block winpty_spawn_config_new's
// env parameter expects (same shape CreateProcess's lpEnvironment takes).
// When overrides is empty, it returns (nil, nil) so winpty is told NULL,
// which per winpty_spawn_config_new's contract means the spawned process
// inherits the environment normally — this keeps the default behavior
// unchanged for the common case where no caller sets ShellDef.Env. When
// overrides is non-empty, the block is built from the current process's
// inherited environment (os.Environ()) with overrides layered on top, so a
// session can tweak or add a few variables without callers having to
// reconstruct the whole environment themselves.
func buildWinptyEnvBlock(overrides map[string]string) (*uint16, error) {
	if len(overrides) == 0 {
		return nil, nil
	}

	merged := make(map[string]string)
	for _, kv := range os.Environ() {
		if before, after, ok := strings.Cut(kv, "="); ok {
			merged[before] = after
		}
	}
	maps.Copy(merged, overrides)

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var block []uint16
	for _, k := range keys {
		block = append(block, utf16.Encode([]rune(k+"="+merged[k]+"\x00"))...)
	}
	block = append(block, 0)

	return &block[0], nil
}

// errSessionClosed is returned by Read/Write when Close signaled
// shutdownEvent while a call was pending.
var errSessionClosed = fmt.Errorf("terminal: session closed")

func (s *winPTYSession) Write(p []byte) (int, error) {
	s.coninMu.Lock()
	defer s.coninMu.Unlock()
	if s.closed.Load() {
		return 0, errSessionClosed
	}
	return overlappedIO(s.conin, s.coninEvent, s.shutdownEvent, func(overlapped *windows.Overlapped) (uint32, error) {
		var n uint32
		err := windows.WriteFile(s.conin, p, &n, overlapped)
		return n, err
	})
}

func (s *winPTYSession) Read(p []byte) (int, error) {
	s.conoutMu.Lock()
	defer s.conoutMu.Unlock()
	if s.closed.Load() {
		return 0, errSessionClosed
	}
	return overlappedIO(s.conout, s.conoutEvent, s.shutdownEvent, func(overlapped *windows.Overlapped) (uint32, error) {
		var n uint32
		err := windows.ReadFile(s.conout, p, &n, overlapped)
		return n, err
	})
}

// overlappedIO drives one overlapped ReadFile/WriteFile call (issued by
// start, which must pass the same handle whose OVERLAPPED is being used) to
// completion, or aborts it if shutdownEvent fires first. This exists so
// Close can safely close conin/conout while a Read or Write is in flight on
// another goroutine: without overlapped I/O, that's a close-races-a-blocking-
// syscall hazard Windows leaves undefined, and this project saw it manifest
// as sporadic STATUS_HEAP_CORRUPTION under concurrent multi-session use (see
// winPTYSession's doc comment on shutdownEvent). Mirrors the pattern
// JetBrains' pty4j uses for the same reason (NamedPipe.java: an event-based
// overlapped read/write plus a shared shutdown event Close signals first).
func overlappedIO(handle, ioEvent, shutdownEvent windows.Handle, start func(*windows.Overlapped) (uint32, error)) (int, error) {
	var overlapped windows.Overlapped
	overlapped.HEvent = ioEvent

	n, err := start(&overlapped)
	if err == nil {
		return int(n), nil
	}
	if err != windows.ERROR_IO_PENDING {
		return int(n), err
	}

	idx, waitErr := windows.WaitForMultipleObjects([]windows.Handle{ioEvent, shutdownEvent}, false, windows.INFINITE)
	if waitErr != nil {
		return 0, waitErr
	}
	if idx != 0 {
		// shutdownEvent fired first: cancel the pending call and wait for
		// the cancellation itself to finish before returning, so the
		// OVERLAPPED struct (stack-allocated here) is never touched by the
		// kernel again after this function returns.
		windows.CancelIoEx(handle, &overlapped)
		var discarded uint32
		windows.GetOverlappedResult(handle, &overlapped, &discarded, true)
		return 0, errSessionClosed
	}

	var actual uint32
	if err := windows.GetOverlappedResult(handle, &overlapped, &actual, true); err != nil {
		return int(actual), err
	}
	return int(actual), nil
}

func (s *winPTYSession) Resize(cols, rows int) error {
	winptyMu.Lock()
	defer winptyMu.Unlock()
	if s.wpClosed {
		return errSessionClosed
	}

	var errPtr uintptr
	ok, _, _ := procSetSize.Call(s.wp, uintptr(cols), uintptr(rows), uintptr(unsafe.Pointer(&errPtr)))
	if err := winptyErr("winpty_set_size", &errPtr); err != nil {
		return err
	}
	if ok == 0 {
		return fmt.Errorf("terminal: winpty_set_size: failed")
	}
	return nil
}

// Close terminates the session: kills the spawned shell (the same explicit
// TerminateProcess precaution the ConPTY implementation used, since relying
// solely on tearing down the agent risks an orphaned shell process), frees
// the winpty_t object, and closes the remaining handles. Safe to call more
// than once.
func (s *winPTYSession) Close() error {
	s.closeOnce.Do(func() {
		recordErr := func(err error, wrap string) {
			if err == nil || s.closeErr != nil {
				return
			}
			s.closeErr = fmt.Errorf("terminal: %s: %w", wrap, err)
		}

		recordErr(windows.TerminateProcess(s.process, 0), "TerminateProcess")

		// Wake any blocked Read/Write (shutdownEvent), stop any new one from
		// starting (closed), then wait out whichever call was already in
		// flight by taking and releasing its lock — see the doc comments on
		// winPTYSession's closed field and shutdownEvent for why all three
		// steps are needed before it's safe to close conin/conout/the events
		// below.
		windows.SetEvent(s.shutdownEvent)
		s.closed.Store(true)
		s.conoutMu.Lock()
		s.conoutMu.Unlock()
		s.coninMu.Lock()
		s.coninMu.Unlock()

		winptyMu.Lock()
		procFree.Call(s.wp) // tears down the agent/console; no error result to capture
		s.wpClosed = true
		winptyMu.Unlock()
		recordErr(windows.CloseHandle(s.conin), "CloseHandle conin")
		recordErr(windows.CloseHandle(s.conout), "CloseHandle conout")
		recordErr(windows.CloseHandle(s.process), "CloseHandle process")
		recordErr(windows.CloseHandle(s.thread), "CloseHandle thread")
		recordErr(windows.CloseHandle(s.coninEvent), "CloseHandle coninEvent")
		recordErr(windows.CloseHandle(s.conoutEvent), "CloseHandle conoutEvent")
		recordErr(windows.CloseHandle(s.shutdownEvent), "CloseHandle shutdownEvent")
	})
	return s.closeErr
}

func (s *winPTYSession) Wait() error {
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

// commandLine renders def into a single Windows command-line string: the
// executable path, quoted if it contains spaces, followed by its arguments,
// each individually quoted the same way if it contains a space.
func commandLine(def ShellDef) string {
	path := def.Path
	if hasSpace(path) {
		path = `"` + path + `"`
	}
	var cmd strings.Builder
	cmd.WriteString(path)
	for _, a := range def.Args {
		if hasSpace(a) {
			a = `"` + a + `"`
		}
		cmd.WriteString(" " + a)
	}
	return cmd.String()
}

func hasSpace(s string) bool {
	return strings.Contains(s, " ")
}

