# Terminal backend: ConPTY default, winpty fallback

Date: 2026-08-20

**Status: reverted the same day.** See "Outcome" at the bottom — ConPTY
is implemented (`conpty_windows.go`) but not wired into `newPtySession`.
The rest of this document describes the attempt as designed, for the
record and for whoever picks this up next.

## Goal

Make ConPTY (`CreatePseudoConsole`) the default Windows PTY backend again,
with winpty kept as an automatic fallback rather than removed. Motivation:
two distinct symptoms traced back to winpty's nature as a screen-scraping
emulator (it polls a hidden real console via `GetConsoleScreenBufferInfo`
and re-synthesizes VT escape codes for whatever changed) rather than a true
pass-through PTY — a resize-event-delivery gap (mitigated separately, see
`winPTYSession.Resize`'s doc comment) and a cursor-visibility/position bug
specific to full-screen Node/Ink TUIs (Claude Code's CLI), which winpty's
scraping approach can't represent as faithfully as ConPTY's native
pseudo-console semantics can.

This reverses `2026-07-22-terminal-winpty-backend-design.md`'s "full
replacement, not a fallback" decision — that switch was itself motivated by
ConPTY's own documented output-delivery unreliability on some Windows
builds/environments. Both concerns are real; this design keeps both
backends rather than re-relitigating which one is "right."

## Scope

- Windows-only, as before.
- `terminal/conpty_windows.go` (revived from git history at `8e6390e^`,
  hardened — see below) implements `newConPTYSession`.
- `terminal/winpty_windows.go` (unchanged apart from the rename below)
  implements `newWinPTYSession` — the fallback.
- `terminal/pty_windows.go` is the new `newPtySession` entry point
  `session.go`'s `ptySession` interface expects: try ConPTY, fall back to
  winpty on any failure.
- `terminal/session_windows.go` is new: helpers shared by both backends
  that were previously duplicated or winpty-only —
  `overlappedIO`/`errSessionClosed`, `uintptrToPointer`/`utf16PtrToString`,
  `commandLine`/`hasSpace`, `buildEnvBlock` (renamed from
  `buildWinptyEnvBlock`, since ConPTY needs the identical UTF-16
  double-null-terminated block shape for `CreateProcess`'s
  `lpEnvironment`).
- No new user-facing setting. Fully automatic, no `pty_backend` config row
  added to `settings_schema.go`.

## Fallback trigger — spawn-time only

`newPtySession` tries `newConPTYSession`; on *any* error (including
`errConPTYUnavailable`, returned before ever calling a ConPTY API when
`CreatePseudoConsole` isn't exported by this Windows build — pre-1809),
it logs and falls through to `newWinPTYSession`.

Explicitly out of scope: detecting a ConPTY session that spawns
successfully but then delivers output unreliably mid-session, and
transparently respawning it under winpty. That would need a "how long is
too long to wait for output" heuristic that risks false-positiving on a
legitimately idle shell. If that turns out to be a real problem in
practice, it needs its own design with real evidence behind it — see
"Known risk" below, which is exactly that scenario.

## `conPTYAvailable`

`golang.org/x/sys/windows` resolves `CreatePseudoConsole` as a
`LazyProc` too, but `LazyProc.Addr()` **panics** (not: returns an error) if
the DLL doesn't export the proc — calling `windows.CreatePseudoConsole`
directly on pre-1809 Windows would crash the whole process instead of
falling back. `conPTYAvailable` (a `sync.OnceValue`) probes for the proc
via a separate `syscall.NewLazyDLL("kernel32.dll").NewProc(...).Find()`
first; `newConPTYSession` checks it before calling any ConPTY API.

## Hardening vs. the original (pre-July-22) ConPTY implementation

The version in git history used plain synchronous `CreatePipe` (anonymous
pipe) handles for its own end of stdin/stdout. That's the same
close-races-a-blocking-syscall hazard winpty's rewrite (`8e6390e`) fixed
with overlapped I/O plus a shutdown event — generic to any pipe-backed
`ptySession`, not winpty-specific, so the revived version needed the same
fix. `CreatePipe`'s anonymous pipes can never be overlapped (a Windows
limitation), so `conpty_windows.go` instead creates a unique **named**
pipe per direction (`createOverlappedNamedPipe`): an overlapped handle for
our own side, a plain synchronous one for ConPTY's side (which manages its
end internally and never needs it overlapped). `conPTYSession` otherwise
mirrors `winPTYSession`'s field/method shape (`stdinEvent`/`stdoutEvent`/
`shutdownEvent`, per-direction mutexes, `closed`), reusing the exact same
`overlappedIO` helper (moved to `session_windows.go`) rather than
reimplementing it.

`Resize` needs no nudge — see `winPTYSession.Resize`'s doc comment for why
winpty's does and ConPTY's doesn't (ConPTY has genuine OS resize
semantics, no synthesized event to worry about missing). `Close` keeps the
same explicit `TerminateProcess` precaution winpty's version documented:
`ClosePseudoConsole`/`winpty_free` alone don't reliably kill the spawned
shell, an observed problem in both backends' histories, not a speculative
one.

## Known risk (predicted correctly, see Outcome)

Automated testing during this change reproduced, in a nested-pty test
harness (`go test` invoked from within Git Bash), the exact
"never-attached, banner leaked to the real console" ConPTY quirk this
package's own historical test comments already documented as the real,
observed reason for the original winpty switch. That symptom did not
reproduce in that same form when testing the actual deployed path
(`go-strider.exe` itself) — but a related failure did, at full severity.
See "Outcome".

## Outcome

Tested live in `go-strider.exe` (not just the test harness): every new
terminal tab, PowerShell and Git Bash alike, sat on a bare blinking
cursor in the top-left corner — no shell prompt, no interactivity, and
the frontend couldn't switch tabs afterward either. This is not the
"spawns fine, then output is occasionally unreliable" failure mode this
design's fallback logic was built to tolerate (see "Fallback trigger —
spawn-time only" above) — `CreatePseudoConsole`/`CreateProcess` reported
success, so the spawn-time-only fallback never triggered, but the
session never actually produced a usable shell at all. Confirms the
concern in the (now-superseded) "Known risk" section above: this
machine's real ConPTY problem is broader than the narrow nested-pty
quirk that section named.

**Reverted same-day**: `pty_windows.go`'s `newPtySession` now calls
`newWinPTYSession` unconditionally — see that file's doc comment.
`conpty_windows.go`, `session_windows.go`, and their tests are kept
as-is (all still compile and pass; `conpty_windows_test.go`'s own tests
exercise `newConPTYSession` directly, not through the dispatcher) for a
future attempt, but ConPTY is not reachable through the normal
`newPtySession` path this app actually uses.

**What a future attempt needs that this one didn't have**: a genuine
mid-session liveness check — e.g. a bounded wait for the shell's first
byte of real output (not just a mode-set escape sequence) after spawn,
falling back to winpty if it doesn't arrive — since `CreateProcess`
succeeding is not evidence the session actually works on this class of
machine/environment. The spawn-time-only fallback this design chose
specifically to avoid that complexity turned out to be the wrong
trade-off here.
