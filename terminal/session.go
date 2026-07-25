package terminal

// ptySession is one running shell process attached to a pseudo-console. The
// Windows implementation lives in winpty_windows.go (backed by winpty, not
// the native ConPTY API — see docs/superpowers/specs/
// 2026-07-22-terminal-winpty-backend-design.md); a future unix implementation
// would provide the same interface over a standard PTY, letting everything
// above this layer stay platform-agnostic.
//
// Reading and writing are both synchronous here — the background read-loop
// goroutine that streams output to the frontend (via the terminal:data
// event) belongs to Service, not to this layer.
type ptySession interface {
	// Write sends bytes to the shell's stdin (e.g. typed input).
	Write(p []byte) (int, error)
	// Read reads bytes the shell has written to its stdout/stderr (both are
	// merged into one stream by the pseudo-console, matching real terminal
	// behavior).
	Read(p []byte) (int, error)
	// Resize changes the pseudo-console's dimensions, in character cells.
	Resize(cols, rows int) error
	// Close terminates the session: closes all handles and the spawned
	// process. Safe to call more than once.
	Close() error
	// Wait blocks until the shell process exits and returns its exit
	// status as an error (nil for a clean/zero exit).
	Wait() error
}
