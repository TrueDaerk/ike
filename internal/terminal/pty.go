package terminal

import "io"

// ptyMaster is the parent's side of a session's pseudo-terminal: the stream
// the read loop drains and the write loop feeds, plus the window-size
// control the pane resize applies. Unix wraps the creack/pty master
// (spawn_unix.go); Windows wraps a ConPTY pseudo console and its two pipes
// (spawn_windows.go, #3005). Close releases every handle the implementation
// owns and must be safe to call more than once.
type ptyMaster interface {
	io.Reader
	io.Writer
	io.Closer
	// Resize tells the terminal its new size in cells; the child sees it as
	// SIGWINCH (unix) or a console resize event (Windows).
	Resize(w, h int) error
}
