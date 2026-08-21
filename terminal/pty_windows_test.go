// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"os"
	"testing"
)

// TestNewPtySessionUsesWinPTY proves the actual ptySession-selecting entry
// point (as opposed to newWinPTYSession/newConPTYSession called directly,
// which the rest of this package's tests exercise) currently dispatches to
// winpty — see pty_windows.go's doc comment for why ConPTY isn't wired in
// as the default despite conpty_windows.go existing: it spawned but never
// produced a working shell on real hardware.
func TestNewPtySessionUsesWinPTY(t *testing.T) {
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newPtySession(def, 80, 24)
	if err != nil {
		t.Fatalf("newPtySession: %v", err)
	}
	defer sess.Close()

	if _, ok := sess.(*winPTYSession); !ok {
		t.Fatalf("sess is %T, not *winPTYSession", sess)
	}
}
