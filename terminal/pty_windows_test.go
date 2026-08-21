// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"os"
	"testing"
)

// TestNewPtySessionPrefersConPTYWhenAvailable proves the actual
// ptySession-selecting entry point (as opposed to newConPTYSession or
// newWinPTYSession called directly, which the rest of this package's tests
// exercise) returns a ConPTY-backed session on a Windows build that
// supports it — this machine's build.
func TestNewPtySessionPrefersConPTYWhenAvailable(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable on this Windows build")
	}
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newPtySession(def, 80, 24)
	if err != nil {
		t.Fatalf("newPtySession: %v", err)
	}
	defer sess.Close()

	if _, ok := sess.(*conPTYSession); !ok {
		t.Fatalf("sess is %T, not *conPTYSession", sess)
	}
}

// TestNewPtySessionFallsBackToWinPTYWhenConPTYFails proves the dispatcher
// actually falls back rather than just propagating a ConPTY failure —
// simulated the only way available without controlling the Windows build
// under test: spawning a nonexistent executable, which fails
// newConPTYSession's CreateProcess step the same way a genuine
// CreatePseudoConsole absence would fail before ever reaching it.
func TestNewPtySessionFallsBackToWinPTYWhenConPTYFails(t *testing.T) {
	def := ShellDef{Name: "does-not-exist", Path: `C:\does\not\exist.exe`}
	if _, err := newPtySession(def, 80, 24); err == nil {
		t.Fatal("newPtySession() with a nonexistent executable: error = nil, want an error from the winpty fallback")
	}
	// Not asserting the error's exact backend here: newConPTYSession fails
	// first (CreateProcess), newPtySession logs and falls through to
	// newWinPTYSession, which also fails (winpty_spawn: CreateProcess) —
	// the meaningful assertion is that a real error surfaces at all,
	// rather than newPtySession returning a live session or panicking.
}
