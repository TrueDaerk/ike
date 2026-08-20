// Package remote is the SFTP remote file browser (#1997): an explorer-like
// pane listing directories and files on an SSH host from ~/.ssh/config, over
// the sftp subsystem of the user's own ssh. Opening a remote file downloads
// it into a local cache; the app shows it read-only (or hands the local copy
// to a viewer), so nothing here ever writes to the remote side.
//
// The connection is the user's ssh binary speaking the sftp subsystem over
// its stdio (`ssh -s <alias> sftp`), the same stance the SSH terminal (#1938)
// takes: keys, agents, jump hosts, known_hosts and per-host options stay
// ssh's job, IKE never re-implements them. BatchMode keeps a host that would
// need an interactive prompt from hanging a background dial — it fails fast
// and the stderr tail becomes the pane's error notice.
//
// The pane model only ever sees the Conn interface, so tests drive it with a
// fake and the SFTP layer stays mockable.
package remote

import (
	"errors"
	"io/fs"
	"time"
)

// Entry is one remote directory entry, the subset of a FileInfo the browser
// shows. Mode carries the full file mode, so a symlink is distinguishable
// even though the browser does not follow them.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

// IsDir reports whether the entry is a directory.
func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// ErrTooLarge is returned by Conn.Fetch for a remote file over the byte cap
// (remote.max_fetch_mb) — the guard against pulling a huge file over a slow
// link just to preview it.
var ErrTooLarge = errors.New("remote file too large")

// Conn is one live SFTP session. The real implementation (Dial) runs over an
// ssh subprocess; tests substitute a fake. All methods may be called from
// background goroutines — the pane fetches and scans off the update loop.
type Conn interface {
	// Home resolves the session's initial directory (the remote $HOME).
	Home() (string, error)
	// ReadDir lists one remote directory, unsorted.
	ReadDir(path string) ([]Entry, error)
	// Fetch reads one remote file whole, refusing with ErrTooLarge when it
	// exceeds max bytes.
	Fetch(path string, max int64) ([]byte, error)
	// Close ends the session and its transport.
	Close() error
}

// DialFunc opens a connection to one ssh alias; the model takes it as a seam
// so tests connect to a fake host.
type DialFunc func(alias string) (Conn, error)
