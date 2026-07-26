// SPDX-FileCopyrightText: 2026 jcaesar
// SPDX-License-Identifier: MIT

// Package terminal is a Wails v3 replacement for go-ux's Fyne-based
// embeddable, tabbed, PTY-backed terminal widget: Git Bash, PowerShell, and
// cmd.exe on Windows to start. It spawns real OS processes through a
// pseudo-console (winpty on Windows — see winpty_windows.go) and streams
// their raw output to the frontend as Wails events; unlike the old Fyne
// widget, VT100/xterm parsing and rendering is no longer done in Go at
// all — it's owned entirely by xterm.js in the frontend
// (frontend/src/views/terminal.ts), the same architecture terminal-poc
// proved out. Go only owns the PTY process lifecycle and shuttles bytes.
//
// This file intentionally holds only the package doc comment; see shell.go,
// session.go, winpty_windows.go, and service.go for the actual
// implementation.
package terminal

