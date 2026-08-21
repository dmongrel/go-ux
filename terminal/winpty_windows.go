// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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

// winPTYSession is the Windows ptySession implementation backed by winpty
// (github.com/rprichard/winpty, vendored under terminal/winpty/) — the
// fallback backend newPtySession (pty_windows.go) uses when ConPTY is
// unavailable or fails to spawn. See
// docs/superpowers/specs/2026-08-20-terminal-conpty-default-design.md for
// why ConPTY is preferred when it works: unlike winpty (which reads a
// hidden console's screen buffer on a timer and re-synthesizes VT escape
// codes for whatever changed), ConPTY is a first-class Windows API with
// genuine pseudo-console semantics — no scraping, no synthesized resize
// events, no cursor-state reconstruction. Kept as the fallback rather than
// removed because ConPTY has its own documented output-delivery
// unreliability on some Windows builds/environments.
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

// newWinPTYSession spawns a shell under winpty. newPtySession
// (pty_windows.go) is the actual ptySession-selecting entry point this
// package exposes; this is its fallback path.
func newWinPTYSession(def ShellDef, cols, rows int) (ptySession, error) {
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

	envPtr, err := buildEnvBlock(def.Env)
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

// Resize changes the pseudo-console's dimensions, nudging through a
// briefly-held intermediate size first when one is available.
//
// winpty has to synthesize a WINDOW_BUFFER_SIZE_EVENT for the console
// after winpty_set_size, rather than relying on genuine OS resize
// semantics the way ConPTY does — and that synthesized event isn't
// always observed by every nested console child process. A plain shell
// prompt (PowerShell/Git Bash) never notices a missed event, since it
// re-queries the current size on every redraw regardless; a Node.js TUI
// like Claude Code's CLI only redraws in response to the event itself,
// so a missed one leaves its frame stuck at the stale size. Requesting
// a genuinely different intermediate size before the real target forces
// two distinct events instead of one, improving the odds a nested
// reader observes at least the second. Skipped when cols and rows are
// both already 1 — there's no smaller size to nudge through.
func (s *winPTYSession) Resize(cols, rows int) error {
	winptyMu.Lock()
	defer winptyMu.Unlock()
	if s.wpClosed {
		return errSessionClosed
	}

	if cols > 1 || rows > 1 {
		nudgeCols, nudgeRows := cols, rows
		if cols > 1 {
			nudgeCols--
		} else {
			nudgeRows--
		}
		_ = s.setSizeLocked(nudgeCols, nudgeRows) // best-effort; the real resize below is what must succeed
		time.Sleep(20 * time.Millisecond)
	}

	return s.setSizeLocked(cols, rows)
}

// setSizeLocked calls winpty_set_size. Callers must hold winptyMu.
func (s *winPTYSession) setSizeLocked(cols, rows int) error {
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

