# Terminal backend: ConPTY default, winpty fallback

Date: 2026-08-20

**Status: reverted, root-caused, fixed, re-enabled — all the same day.**
See "Outcome" and "Root cause and fix" at the bottom. ConPTY is the live
default again as of the fix; the sections in between describe the failed
first attempt for the record.

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

**Reverted same-day** (first pass): `pty_windows.go`'s `newPtySession`
briefly called `newWinPTYSession` unconditionally while the actual cause
was investigated.

## Root cause and fix

Compared this package's `conpty_windows.go` line-by-line against
JetBrains' own ConPTY binding — `pty4j` (`com.pty4j.windows.conpty`,
what IntelliJ's terminal actually uses; confirmed via
`LocalPtyOptions.shouldUseWinConPty()`, which defaults to `true` on
Windows — ConPTY genuinely is IntelliJ's default here, not winpty).
`pty4j`'s `ProcessUtils.prepareStartupInformation` sets one thing this
package's `newConPTYSession` didn't:

```java
// according to https://github.com/microsoft/terminal/issues/11276#issuecomment-923210023
startupInfo.StartupInfo.dwFlags = WinBase.STARTF_USESTDHANDLES;
```

— with `hStdInput`/`hStdOutput`/`hStdError` all left `NULL`. That GitHub
issue's own description matches this project's symptom almost exactly:
without `STARTF_USESTDHANDLES`, a ConPTY-spawned child can end up using
the parent's own standard handles (or, for a console-less GUI parent
like `go-strider.exe`, failing to attach to the pseudo-console at all)
instead of respecting `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` — exactly
the "spawns, but the session is dead" failure this design hit.
`newConPTYSession` now sets `si.StartupInfo.Flags =
windows.STARTF_USESTDHANDLES` right alongside the attribute list. With
that one flag, all four `conpty_windows_test.go` tests went from
skipping/failing (garbled or console-leaked output) to passing cleanly
— no other change needed, not the named-pipe rewrite, not the retry
loop winpty needed, nothing.

`newPtySession` now prefers ConPTY again, winpty as the fallback, per
the original goal. The spawn-time-only fallback scope (see "Fallback
trigger" above) stands: this fix addresses a real bug in this project's
own `CreateProcess` call, not a fundamental limit of ConPTY needing a
runtime liveness check to work around. If output-delivery unreliability
still turns out to be real on some other Windows build/environment,
that's a separate, later problem with its own evidence to gather.

**Note for future maintainers**: `pty4j`'s `Pipe` class uses plain
synchronous anonymous `CreatePipe` handles, same as this package's
original pre-July-22 ConPTY code — not the overlapped named pipes this
rewrite introduced. That hardening (matching winpty's own
Close-races-Read fix) is still justified independently — the underlying
Windows hazard it guards against is real — but it was not what fixed
this particular bug, and is called out here so it isn't mistakenly
credited as the fix if this file's history gets revisited later.
