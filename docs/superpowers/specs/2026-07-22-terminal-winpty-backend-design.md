# Terminal backend: switch from ConPTY to winpty

Date: 2026-07-22

## Goal

Replace `terminal`'s ConPTY-based pseudo-console backend with one backed by
[winpty](https://github.com/rprichard/winpty), matching how IntelliJ bundles
and drives its own copy of winpty rather than relying on the host's ConPTY
support. Motivation: ConPTY's documented unreliability on some Windows
builds/environments (see `terminal.md`'s "Constraints for callers").

## Scope

- Windows-only. Replaces `terminal/conpty_windows.go` with
  `terminal/winpty_windows.go`.
- No change to the `ptySession` interface (`terminal/session.go`) or anything
  above it (`widget.go`, `tabs.go`, `window.go`, `settings_schema.go`) — they
  depend only on that interface.
- x64 only. No 32-bit support.
- Full replacement, not a fallback: ConPTY code is deleted, not kept behind a
  flag.

## Binaries

Vendor the official winpty 0.4.3 release's x64 binaries (MIT-licensed, from
`github.com/rprichard/winpty`) at `terminal/winpty/winpty.dll` and
`terminal/winpty/winpty-agent.exe`. Embed both via `go:embed`. At first use,
extract them together into a per-run temp directory (e.g. under
`os.UserCacheDir()` or `os.TempDir()`, content-addressed or version-tagged so
repeat runs can reuse an existing extraction rather than re-writing it every
process start) — winpty.dll locates winpty-agent.exe relative to its own
on-disk directory, so both must land in the same real directory before
`winpty.dll` is loaded.

## Bindings

New file `terminal/winpty_windows.go`, raw syscalls via
`syscall.NewLazyDLL(extractedDLLPath)` + `NewProc`, no cgo, following the
existing `conpty_windows.go` file's precedent (including its documented
workaround for `unsafe.Pointer`/`go vet` friction with process attribute
lists, which still applies to spawning the shell under winpty the same way it
did under ConPTY).

Functions bound, per `winpty.h`:
- `winpty_config_new`, `winpty_config_set_initial_size`, `winpty_config_free`
- `winpty_open`
- `winpty_conin_name`, `winpty_conout_name`
- `winpty_spawn_config_new`, `winpty_spawn`, `winpty_spawn_config_free`
- `winpty_set_size`
- `winpty_agent_process`
- `winpty_free`
- `winpty_error_code`, `winpty_error_msg`, `winpty_error_free`

## ptySession mapping

- `newPtySession`: `winpty_config_new` → `winpty_config_set_initial_size` →
  `winpty_open` → open `winpty_conin_name`/`winpty_conout_name` named pipes
  via `windows.CreateFile` → `winpty_spawn_config_new` (command line, cwd,
  env block — same construction helpers as today: `commandLine`,
  `buildEnvBlock`) → `winpty_spawn`.
- `Write`: `WriteFile` to the CONIN pipe handle.
- `Read`: `ReadFile` from the CONOUT pipe handle.
- `Resize`: `winpty_set_size`.
- `Close`: `TerminateProcess` on the spawned shell (same orphan-process
  precaution as the current ConPTY code), then `winpty_free`, then close the
  pipe/process/thread handles.
- `Wait`: unchanged — `WaitForSingleObject` + `GetExitCodeProcess` on the
  spawned process handle.

Errors from any winpty call are read via the optional `winpty_error_ptr_t`
out-param (`winpty_error_msg` for text, `winpty_error_free` after use) and
wrapped as `fmt.Errorf("terminal: winpty_xxx: %w", ...)`, matching the
existing error style.

## Renames

`conPTYSession` → `winPTYSession`. Existing white-box tests asserting
`sess.(*conPTYSession)` (`session_windows_test.go`, `tabs_windows_test.go`)
update to `*winPTYSession`; their behavior-level assertions (echo, resize,
exit, close) are unchanged since they go through the `ptySession` interface.

## Docs

Update `terminal.md`:
- "Rendering" section's mention of ConPTY → winpty.
- "Constraints for callers" ConPTY-reliability caveat → replaced with
  whatever winpty's equivalent caveat is (winpty is generally more reliable
  than ConPTY for output delivery, but depends on bundled binaries being
  extractable/loadable — note that instead).

## Out of scope

- No Unix PTY work (still a documented future item, unaffected by this
  change).
- No settings/UI changes.
- No attempt to support 32-bit Windows or non-bundled/system-installed
  winpty.
