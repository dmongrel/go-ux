package terminal

import "fyne.io/fyne/v2"

// This file translates Fyne keyboard events into the byte sequences a VT
// terminal expects on its stdin. It is a deliberately pragmatic subset of
// VT/xterm input encoding, matching the design doc's scope for this plan:
// printable runes, Enter/Backspace/Tab/Escape, the four arrow keys,
// Home/End, Delete, Page Up/Down, and Ctrl+letter (plus Ctrl+[, Ctrl+\,
// Ctrl+]). It deliberately does NOT implement F-keys, shift-arrow
// selection, or Alt-as-Meta — those are out of scope for this plan (see
// terminal.md, Task 5, once written).

// runeBytes encodes a printable character for TypedRune. Fyne already hands
// TypedRune a decoded Unicode code point, so passing it through as UTF-8 is
// exactly what a UTF-8-speaking shell expects.
func runeBytes(r rune) []byte {
	return []byte(string(r))
}

// keyEventBytes translates the fixed-function keys Fyne delivers via
// TypedKey (Enter, Backspace, Tab, Escape, arrows, Home/End, Delete, Page
// Up/Down) into their VT byte sequences. ok is false for any key outside
// this mapping (e.g. F-keys, plain letters — letters arrive via TypedRune,
// not TypedKey), meaning the caller should do nothing.
func keyEventBytes(ev *fyne.KeyEvent) (data []byte, ok bool) {
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		return []byte("\r"), true
	case fyne.KeyBackspace:
		return []byte("\x7f"), true
	case fyne.KeyTab:
		return []byte("\t"), true
	case fyne.KeyEscape:
		return []byte("\x1b"), true
	case fyne.KeyUp:
		return []byte("\x1b[A"), true
	case fyne.KeyDown:
		return []byte("\x1b[B"), true
	case fyne.KeyRight:
		return []byte("\x1b[C"), true
	case fyne.KeyLeft:
		return []byte("\x1b[D"), true
	case fyne.KeyHome:
		return []byte("\x1b[H"), true
	case fyne.KeyEnd:
		return []byte("\x1b[F"), true
	case fyne.KeyDelete:
		return []byte("\x1b[3~"), true
	case fyne.KeyPageUp:
		return []byte("\x1b[5~"), true
	case fyne.KeyPageDown:
		return []byte("\x1b[6~"), true
	}
	return nil, false
}

// ctrlKeyBytes translates a Ctrl-modified key into its C0 control byte, per
// the standard terminal convention Ctrl+<letter> -> rune('letter') - 'a' + 1
// (Ctrl+C -> 0x03, the SIGINT byte every shell relies on), plus the three
// punctuation keys that also carry well-known control meanings.
//
// This is consulted from Session.TypedShortcut, not TypedKey: Fyne's desktop
// driver intercepts every Ctrl+<key> combination as a keyboard shortcut
// before it would otherwise reach TypedKey (see the doc comment on
// TypedShortcut in widget.go), so keyEventBytes above never sees them.
func ctrlKeyBytes(name fyne.KeyName) (data []byte, ok bool) {
	switch name {
	case fyne.KeyLeftBracket:
		return []byte{0x1b}, true
	case fyne.KeyBackslash:
		return []byte{0x1c}, true
	case fyne.KeyRightBracket:
		return []byte{0x1d}, true
	}
	s := string(name)
	if len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z' {
		return []byte{s[0] - 'A' + 1}, true
	}
	return nil, false
}
