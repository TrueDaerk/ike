package deeplink

import (
	"bufio"
	"errors"
	"fmt"
	"ike/internal/safego"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ipc.go is the hand-off between the OS URL handler and a running IKE: one
// unix domain socket per instance under the user state directory. The handler
// (or a second `ike ike://…` invocation) writes the URL to the most recently
// focused instance's socket; no instance answering means "start one yourself".
//
// The socket is a trust boundary: created 0600 in a 0700 directory, it accepts
// exactly two message forms — "open ike://…" and "event {json}" (#2843, see
// event.go) — with a hard length cap. Anything else is answered with an error
// and the connection dropped. The URL is re-parsed by the receiver before
// anything acts on it.

// maxLinkLen bounds an incoming message; a legitimate link is a few hundred
// bytes, so anything larger is garbage or an attack.
const maxLinkLen = 8 * 1024

// ipcTimeout bounds every client-side dial/write/read: the handler must never
// hang on a dead instance's leftover socket.
const ipcTimeout = 2 * time.Second

// DefaultDir is the socket directory shared by every instance of this user:
// $IKE_CONFIG_DIR/deeplink when the override is set (tests, portable setups —
// the same seam every other user-scoped state uses), else ~/.ike/deeplink.
func DefaultDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "deeplink")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "deeplink")
}

// Server is one instance's deep-link endpoint.
type Server struct {
	dir  string
	sock string
	ln   net.Listener
}

// Handlers receives the validated messages of one endpoint. Open gets every
// ike:// link, Event every agent lifecycle push; a nil handler refuses its
// message form.
type Handlers struct {
	Open  func(url string)
	Event func(Event)
}

// Serve opens this instance's socket under dir (created 0700) and delivers
// every valid incoming link to deliver, each on its own goroutine. The socket
// is named by pid so parallel instances never collide.
func Serve(dir string, deliver func(url string)) (*Server, error) {
	return ServeHandlers(dir, Handlers{Open: deliver})
}

// ServeHandlers is Serve with a handler per message form.
func ServeHandlers(dir string, h Handlers) (*Server, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, fmt.Sprintf("ike-%d.sock", os.Getpid()))
	// A crashed previous run with the same pid left a dead socket: remove it,
	// the pid is ours now.
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(sock, 0o600)
	s := &Server{dir: dir, sock: sock, ln: ln}
	s.Touch()
	safego.Go("deeplink.Serve", func() { s.accept(h) })
	return s, nil
}

// accept handles connections until the listener closes.
func (s *Server) accept(h Handlers) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		safego.Go("deeplink.Server.accept", func() { handleConn(conn, h) })
	}
}

// handleConn reads and validates the single message of a connection. The
// connection answers "ok" only after the message parsed, so the client can
// fall back to starting a fresh instance when it hit a wedged or foreign
// socket.
func handleConn(conn net.Conn, h Handlers) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ipcTimeout))
	r := bufio.NewReaderSize(limitConn(conn), maxLinkLen)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return
	}
	line = strings.TrimSpace(line)
	if raw, ok := strings.CutPrefix(line, "open "); ok && h.Open != nil {
		if _, err := Parse(raw); err != nil {
			fmt.Fprintf(conn, "err %v\n", err)
			return
		}
		fmt.Fprintln(conn, "ok")
		h.Open(raw)
		return
	}
	if raw, ok := strings.CutPrefix(line, "event "); ok && h.Event != nil {
		ev, err := ParseEvent(raw)
		if err != nil {
			fmt.Fprintf(conn, "err %v\n", err)
			return
		}
		fmt.Fprintln(conn, "ok")
		h.Event(ev)
		return
	}
	fmt.Fprintln(conn, "err unsupported message")
}

// limitConn caps what a connection may send — ReadString would otherwise
// buffer an unbounded line.
func limitConn(conn net.Conn) *limitedConn { return &limitedConn{conn: conn, left: maxLinkLen} }

type limitedConn struct {
	conn net.Conn
	left int
}

func (l *limitedConn) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, errors.New("message too long")
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	n, err := l.conn.Read(p)
	l.left -= n
	return n, err
}

// Touch stamps this instance as the most recently focused one: the sidecar
// .focus file's mtime is what Send orders candidate sockets by. Called on
// every terminal focus gain; cheap enough to be unconditional.
func (s *Server) Touch() {
	if s == nil {
		return
	}
	focus := strings.TrimSuffix(s.sock, ".sock") + ".focus"
	if f, err := os.OpenFile(focus, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Close()
	}
	now := time.Now()
	_ = os.Chtimes(focus, now, now)
}

// Close shuts the endpoint and removes its files.
func (s *Server) Close() {
	if s == nil {
		return
	}
	_ = s.ln.Close()
	_ = os.Remove(s.sock)
	_ = os.Remove(strings.TrimSuffix(s.sock, ".sock") + ".focus")
}

// Send delivers one ike:// URL to the most recently focused running instance.
// It tries every socket under dir, newest focus stamp first, removes the
// leftovers of dead instances as it goes, and reports ErrNoInstance when
// nobody answered — the caller then starts IKE itself.
var ErrNoInstance = errors.New("no running ike instance")

func Send(dir, url string) error {
	return deliverLine(sockets(dir), "open "+url)
}

// sockets lists the instance sockets under dir, newest focus stamp first.
func sockets(dir string) []string {
	socks, err := filepath.Glob(filepath.Join(dir, "ike-*.sock"))
	if err != nil {
		return nil
	}
	sort.Slice(socks, func(i, j int) bool { return focusTime(socks[i]).After(focusTime(socks[j])) })
	return socks
}

// deliverLine tries socks in order until one acknowledges line. A socket
// nobody answers belongs to a dead instance: its files are removed on the way
// so the directory never accumulates corpses.
func deliverLine(socks []string, line string) error {
	for _, sock := range socks {
		if sendLine(sock, line) {
			return nil
		}
		removeDead(sock)
	}
	return ErrNoInstance
}

// focusTime reads a socket's focus stamp, falling back to the socket's own
// mtime for an instance that never gained focus.
func focusTime(sock string) time.Time {
	if fi, err := os.Stat(strings.TrimSuffix(sock, ".sock") + ".focus"); err == nil {
		return fi.ModTime()
	}
	if fi, err := os.Stat(sock); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}
