// Package terminal is an embeddable, tabbed, PTY-backed terminal widget for
// Fyne: Git Bash, PowerShell, and cmd.exe on Windows to start. Unlike
// go-ux/dialog and go-ux/settings, this package spawns real OS processes
// through a pseudo-console (winpty on Windows — see winpty_windows.go) and
// reads their output on a background goroutine, so it is the first package
// in this repo that must actually use fyne.Do/fyne.DoAndWait rather than
// mutating widgets directly.
//
// This file intentionally holds only the package doc comment; see shell.go,
// session.go, and winpty_windows.go for the actual PTY foundation.
package terminal
