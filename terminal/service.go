package terminal

import (
	"errors"
	"log"
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"go-ux/db"
)

var (
	errNoShellsDetected = errors.New("terminal: no shells detected on this machine")
	errUnknownSession   = errors.New("terminal: unknown session id")
)

func init() {
	application.RegisterEvent[SessionOutput]("terminal:data")
	application.RegisterEvent[string]("terminal:exit")
	application.RegisterEvent[FontSettings]("terminal:font")
}

// SessionOutput is one chunk of raw PTY output, emitted on the "terminal:data"
// event — the frontend routes it to the matching xterm.js instance by
// SessionID (multiple sessions/tabs share the one event channel; a single
// unqualified event, as terminal-poc's single-session POC used, isn't
// enough once more than one session can be open at a time).
type SessionOutput struct {
	SessionID string
	Data      string
}

// session is one running shell process this Service owns.
type session struct {
	pty ptySession
}

// Service is the Wails-bound replacement for go-ux/terminal.Window/TabView:
// it owns PTY session lifecycles and streams their raw output to the
// frontend, which renders via xterm.js and owns all VT100 parsing/tabs UI
// itself (see terminal.go's package doc comment).
type Service struct {
	app *application.App
	db  *db.DB

	mu       sync.Mutex
	sessions map[string]*session
	nextID   int
}

// NewService builds a terminal Service backed by database — RegisterSettings
// should already have been called against it (see settings_schema.go), same
// precondition go-ux/terminal.NewWindowFromSettings originally documented.
// Applies the persisted font settings (if any) as the live shared value
// immediately, matching NewWindowFromSettings' original behavior.
func NewService(app *application.App, database *db.DB) *Service {
	s := &Service{app: app, db: database, sessions: make(map[string]*session)}

	if err := ApplyFontSettings(database); err != nil {
		log.Printf("terminal: apply font settings: %v", err)
	}

	// Broadcast every font change (from this or any other terminal window)
	// to every open terminal window, matching the original Fyne widget's
	// "every open Session, in every open Window, renders against the same
	// shared value" behavior — there, a listener per live *Session widget
	// achieved that; here, one listener per Service (there is only ever one
	// Service instance, shared by every window) rebroadcasts as a Wails
	// event every frontend terminal view subscribes to.
	registerFontListener(s, func(f FontSettings) {
		app.Event.Emit("terminal:font", f)
	})

	return s
}

// DetectShells returns the shells this machine has, reordered so the
// configured default_shell (if RegisterSettings has been called and a
// Terminal node exists) is first — the Wails equivalent of
// NewWindowFromSettings' shell-list handling, now exposed for a frontend
// shell picker instead of driving TabView's initial tab order directly.
func (s *Service) DetectShells() []ShellDef {
	shells := DetectShells()
	defaultShell, _, _, found, err := readTerminalSettings(s.db)
	if err != nil {
		log.Printf("terminal: read settings: %v", err)
		return shells
	}
	if found {
		shells = withDefaultFirst(shells, defaultShell)
	}
	return shells
}

// CloseOnExit reports whether a tab whose shell process exits on its own
// should be closed automatically — the frontend's terminal:exit handler
// calls this to decide, mirroring the original Fyne TabView's
// close_on_exit-driven auto-close behavior (readTerminalSettings' seeded
// default is true, matching RegisterSettings; false if RegisterSettings
// was never called at all).
func (s *Service) CloseOnExit() bool {
	_, closeOnExit, _, found, err := readTerminalSettings(s.db)
	if err != nil {
		log.Printf("terminal: read settings: %v", err)
		return false
	}
	return found && closeOnExit
}

// withDefaultFirst reorders shells so the entry named name is first (which
// becomes the frontend shell picker's default), leaving the relative order
// of the rest unchanged. Returns shells unmodified if no entry matches name
// (e.g. the configured default_shell is no longer detected on this
// machine).
func withDefaultFirst(shells []ShellDef, name string) []ShellDef {
	for i, sh := range shells {
		if sh.Name != name {
			continue
		}
		if i == 0 {
			return shells
		}
		reordered := make([]ShellDef, 0, len(shells))
		reordered = append(reordered, sh)
		reordered = append(reordered, shells[:i]...)
		reordered = append(reordered, shells[i+1:]...)
		return reordered
	}
	return shells
}

// Start spawns a new PTY session running shellName (matched against
// DetectShells' Name field; the first detected shell is used if shellName
// is empty or not found) at the given size, and returns its session ID.
// Output streams asynchronously via the "terminal:data" event, keyed by
// this ID.
func (s *Service) Start(shellName string, cols, rows int) (string, error) {
	def, err := s.resolveShell(shellName)
	if err != nil {
		return "", err
	}

	pty, err := newPtySession(def, cols, rows)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.nextID++
	id := strconv.Itoa(s.nextID)
	s.sessions[id] = &session{pty: pty}
	s.mu.Unlock()

	go s.readLoop(id, pty)

	return id, nil
}

func (s *Service) resolveShell(shellName string) (ShellDef, error) {
	shells := DetectShells()
	if len(shells) == 0 {
		return ShellDef{}, errNoShellsDetected
	}
	if shellName == "" {
		return shells[0], nil
	}
	for _, def := range shells {
		if def.Name == shellName {
			return def, nil
		}
	}
	return shells[0], nil
}

// readLoop streams pty's output to the frontend as "terminal:data" events
// until Read returns an error (the process exited or the session was
// closed), then emits "terminal:exit" and removes the session.
func (s *Service) readLoop(id string, pty ptySession) {
	buf := make([]byte, 4096)
	for {
		n, err := pty.Read(buf)
		if n > 0 {
			s.app.Event.Emit("terminal:data", SessionOutput{SessionID: id, Data: string(buf[:n])})
		}
		if err != nil {
			s.mu.Lock()
			delete(s.sessions, id)
			s.mu.Unlock()
			s.app.Event.Emit("terminal:exit", id)
			return
		}
	}
}

// WriteInput sends typed/pasted input to sessionID's shell.
func (s *Service) WriteInput(sessionID string, data string) error {
	sess, err := s.session(sessionID)
	if err != nil {
		return err
	}
	_, err = sess.pty.Write([]byte(data))
	return err
}

// Resize changes sessionID's pseudo-console dimensions.
func (s *Service) Resize(sessionID string, cols int, rows int) error {
	sess, err := s.session(sessionID)
	if err != nil {
		return err
	}
	return sess.pty.Resize(cols, rows)
}

// CloseSession terminates one session (e.g. a closed tab). Safe to call
// more than once or with an already-exited session's ID.
func (s *Service) CloseSession(sessionID string) error {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return sess.pty.Close()
}

func (s *Service) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, errUnknownSession
	}
	return sess, nil
}

// Close terminates every open session — called on app shutdown so a closed
// terminal window never leaves an orphaned shell process running, matching
// terminal-poc's original TerminalService.Close.
func (s *Service) Close() {
	s.mu.Lock()
	sessions := s.sessions
	s.sessions = make(map[string]*session)
	s.mu.Unlock()

	for _, sess := range sessions {
		_ = sess.pty.Close()
	}
}

// CurrentFontSettings returns the live shared font configuration.
func (s *Service) CurrentFontSettings() FontSettings {
	return currentFontSettings()
}

// SetFontSettings replaces the live shared font configuration (clamped),
// notifying every open terminal window via the "terminal:font" event, and
// persists it to the db's Terminal node — the Wails equivalent of the
// original Fyne widget's debounced Ctrl+scroll save (see
// go-ux/fontsettings.SeedFontProperties for the property keys).
func (s *Service) SetFontSettings(f FontSettings) error {
	f = clampFontSettings(f)
	setFontSettings(f)

	nodes, err := s.db.ListSettings()
	if err != nil {
		return err
	}
	node, ok := findRootNode(nodes, terminalSettingsLabel)
	if !ok {
		return nil // RegisterSettings was never called; nothing to persist against
	}
	return s.db.SaveProperties(node.ID, map[string]string{
		KeyFontFamily:  f.Family,
		KeyFontSize:    strconv.Itoa(f.Size),
		KeyLineHeight:  strconv.FormatFloat(f.LineHeight, 'f', -1, 64),
		KeyColumnWidth: strconv.FormatFloat(f.ColumnWidth, 'f', -1, 64),
	})
}

// OpenWindow opens the terminal UI in its own window.
func (s *Service) OpenWindow() {
	s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Terminal",
		Width:            1024,
		Height:           700,
		BackgroundColour: application.NewRGB(30, 30, 30),
		URL:              "/#terminal",
	})
}
