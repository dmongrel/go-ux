// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

//go:build windows

package terminal

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewConPTYSessionSpawnsShellAndProducesOutput(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable on this Windows build")
	}
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newConPTYSession(def, 80, 24)
	if err != nil {
		t.Fatalf("newConPTYSession: %v", err)
	}
	defer sess.Close()

	if _, ok := sess.(*conPTYSession); !ok {
		t.Fatalf("sess is %T, not *conPTYSession", sess)
	}

	if _, err := sess.Write([]byte("echo SESSION_MARKER_98765\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := readForDuration(t, sess, 3*time.Second)
	if !strings.Contains(out, "SESSION_MARKER_98765") {
		t.Fatalf("captured output %q does not contain the echoed marker", out)
	}
}

func TestConPTYSessionCloseIsIdempotent(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable on this Windows build")
	}
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newConPTYSession(def, 80, 24)
	if err != nil {
		t.Fatalf("newConPTYSession: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close should also succeed (idempotent), got: %v", err)
	}
}

func TestConPTYSessionResize(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable on this Windows build")
	}
	def := ShellDef{Name: "cmd.exe", Path: os.Getenv("SystemRoot") + `\System32\cmd.exe`}
	sess, err := newConPTYSession(def, 80, 24)
	if err != nil {
		t.Fatalf("newConPTYSession: %v", err)
	}
	defer sess.Close()

	if err := sess.Resize(120, 40); err != nil {
		t.Errorf("Resize: %v", err)
	}
}

func TestNewConPTYSessionHonorsWorkDirAndEnv(t *testing.T) {
	if !conPTYAvailable() {
		t.Skip("ConPTY unavailable on this Windows build")
	}
	workDir := os.Getenv("SystemRoot") // e.g. C:\Windows; always exists
	def := ShellDef{
		Name:    "cmd.exe",
		Path:    os.Getenv("SystemRoot") + `\System32\cmd.exe`,
		Args:    []string{"/C", "cd", "&", "echo", "%GOUX_TEST_VAR%"},
		WorkDir: workDir,
		Env:     map[string]string{"GOUX_TEST_VAR": "hello123"},
	}
	sess, err := newConPTYSession(def, 80, 24)
	if err != nil {
		t.Fatalf("newConPTYSession: %v", err)
	}
	defer sess.Close()

	out := readForDuration(t, sess, 3*time.Second)
	if !strings.Contains(out, workDir) {
		t.Errorf("captured output %q does not contain WorkDir %q", out, workDir)
	}
	if !strings.Contains(out, "hello123") {
		t.Errorf("captured output %q does not contain env var value %q", out, "hello123")
	}
}
