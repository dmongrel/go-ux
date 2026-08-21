// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

// newPtySession is the ptySession-selecting entry point session.go's
// documented interface expects. winpty (newWinPTYSession) is the default —
// see docs/superpowers/specs/2026-08-20-terminal-conpty-default-design.md
// for the ConPTY-as-default attempt this reverts: on real hardware
// (go-strider.exe itself, not just the nested-pty test harness that first
// caught a milder version of this), ConPTY sessions spawned successfully
// but never produced a working shell at all — every tab sat on a bare
// blinking cursor with no prompt and no interactivity, not the
// output-occasionally-unreliable failure mode the spawn-time-only fallback
// was designed to tolerate. newConPTYSession (conpty_windows.go) is kept
// for a future attempt, but is not wired in here — it needs a real
// mid-session liveness check (e.g. a timeout waiting for the shell's first
// byte of output) before it can safely be the default again, which this
// revert does not attempt.
func newPtySession(def ShellDef, cols, rows int) (ptySession, error) {
	return newWinPTYSession(def, cols, rows)
}
