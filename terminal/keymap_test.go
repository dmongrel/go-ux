package terminal

import (
	"bytes"
	"testing"

	"fyne.io/fyne/v2"
)

// TestRuneBytes covers the printable-character path: UTF-8 verbatim,
// including a multi-byte rune, since a terminal must pass through non-ASCII
// input (e.g. accented characters, CJK) unmangled.
func TestRuneBytes(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want []byte
	}{
		{"ascii letter", 'a', []byte("a")},
		{"digit", '5', []byte("5")},
		{"multibyte rune", 'é', []byte("é")},
		{"cjk rune", '中', []byte("中")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runeBytes(c.r)
			if !bytes.Equal(got, c.want) {
				t.Errorf("runeBytes(%q) = %v, want %v", c.r, got, c.want)
			}
		})
	}
}

// TestKeyEventBytes covers the fixed-key mapping table from the task brief
// verbatim: Enter, Backspace, Tab, Escape, arrows, Home/End, Delete, Page
// Up/Down. Keys outside this deliberately pragmatic subset (F-keys, etc.)
// must report ok=false so callers know to do nothing.
func TestKeyEventBytes(t *testing.T) {
	cases := []struct {
		name   string
		key    fyne.KeyName
		want   []byte
		wantOK bool
	}{
		{"enter/return", fyne.KeyReturn, []byte("\r"), true},
		{"enter/keypad", fyne.KeyEnter, []byte("\r"), true},
		{"backspace", fyne.KeyBackspace, []byte("\x7f"), true},
		{"tab", fyne.KeyTab, []byte("\t"), true},
		{"escape", fyne.KeyEscape, []byte("\x1b"), true},
		{"up", fyne.KeyUp, []byte("\x1b[A"), true},
		{"down", fyne.KeyDown, []byte("\x1b[B"), true},
		{"right", fyne.KeyRight, []byte("\x1b[C"), true},
		{"left", fyne.KeyLeft, []byte("\x1b[D"), true},
		{"home", fyne.KeyHome, []byte("\x1b[H"), true},
		{"end", fyne.KeyEnd, []byte("\x1b[F"), true},
		{"delete", fyne.KeyDelete, []byte("\x1b[3~"), true},
		{"page up", fyne.KeyPageUp, []byte("\x1b[5~"), true},
		{"page down", fyne.KeyPageDown, []byte("\x1b[6~"), true},
		{"unmapped F1", fyne.KeyF1, nil, false},
		{"unmapped letter (handled via TypedRune, not TypedKey)", fyne.KeyA, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := keyEventBytes(&fyne.KeyEvent{Name: c.key})
			if ok != c.wantOK {
				t.Fatalf("keyEventBytes(%v) ok = %v, want %v", c.key, ok, c.wantOK)
			}
			if ok && !bytes.Equal(got, c.want) {
				t.Errorf("keyEventBytes(%v) = %v, want %v", c.key, got, c.want)
			}
		})
	}
}

// TestCtrlKeyBytes covers Ctrl+letter (rune - 'a' + 1, per the brief,
// verified against the well-known Ctrl+C -> 0x03) and the three punctuation
// keys the brief calls out by exact byte. These arrive at Session via
// TypedShortcut, not TypedKey/TypedRune — see the doc comment on
// Session.TypedShortcut in widget.go for why.
func TestCtrlKeyBytes(t *testing.T) {
	cases := []struct {
		name   string
		key    fyne.KeyName
		want   byte
		wantOK bool
	}{
		{"ctrl+a", fyne.KeyA, 0x01, true},
		{"ctrl+c (SIGINT)", fyne.KeyC, 0x03, true},
		{"ctrl+z", fyne.KeyZ, 0x1a, true},
		{"ctrl+[", fyne.KeyLeftBracket, 0x1b, true},
		{"ctrl+backslash", fyne.KeyBackslash, 0x1c, true},
		{"ctrl+]", fyne.KeyRightBracket, 0x1d, true},
		{"unmapped digit", fyne.Key5, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ctrlKeyBytes(c.key)
			if ok != c.wantOK {
				t.Fatalf("ctrlKeyBytes(%v) ok = %v, want %v", c.key, ok, c.wantOK)
			}
			if ok && (len(got) != 1 || got[0] != c.want) {
				t.Errorf("ctrlKeyBytes(%v) = %v, want [%#x]", c.key, got, c.want)
			}
		})
	}
}
