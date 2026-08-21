// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import "log"

// newPtySession is the ptySession-selecting entry point session.go's
// documented interface expects. It prefers ConPTY (newConPTYSession) —
// the native Windows pseudo-console API, with no scraping and no
// synthesized events — and falls back to winpty (newWinPTYSession) when
// ConPTY is unavailable on this Windows build (pre-1809) or fails to spawn
// for any other reason. See docs/superpowers/specs/
// 2026-08-20-terminal-conpty-default-design.md for the full history,
// including a same-day revert-then-fix: the first attempt spawned
// "successfully" but produced a dead session (no prompt, no
// interactivity) on real hardware, traced to CreateProcess needing
// STARTF_USESTDHANDLES set (with all three standard handles left NULL) —
// see newConPTYSession's doc comment — before
// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE is reliably honored. With that fix,
// this is a spawn-time-only fallback — a ConPTY session that spawns
// successfully but then delivers output unreliably mid-session (ConPTY's
// own documented weak spot on some Windows builds/environments) is not
// detected or recovered from here, since "how long is too long to wait"
// would risk false-positiving on a legitimately idle shell.
func newPtySession(def ShellDef, cols, rows int) (ptySession, error) {
	sess, err := newConPTYSession(def, cols, rows)
	if err == nil {
		return sess, nil
	}
	log.Printf("terminal: ConPTY unavailable, falling back to winpty: %v", err)
	return newWinPTYSession(def, cols, rows)
}
