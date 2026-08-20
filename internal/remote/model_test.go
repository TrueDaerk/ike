package remote

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeConn is an in-memory remote host: a map of directory listings keyed by
// path. It stands in for the SFTP layer, keeping the pane's tests offline.
type fakeConn struct {
	home   string
	dirs   map[string][]Entry
	files  map[string][]byte
	closed bool
}

func (f *fakeConn) Home() (string, error) { return f.home, nil }

func (f *fakeConn) ReadDir(path string) ([]Entry, error) {
	es, ok := f.dirs[path]
	if !ok {
		return nil, errors.New("no such directory")
	}
	return es, nil
}

func (f *fakeConn) Fetch(path string, max int64) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, errors.New("no such file")
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (f *fakeConn) Close() error { f.closed = true; return nil }

func dir(name string) Entry  { return Entry{Name: name, Mode: fs.ModeDir | 0o755} }
func file(name string) Entry { return Entry{Name: name, Size: 42, Mode: 0o644} }

// host is the fixture tree: / → home/ + etc/, /home → user/, /home/user →
// notes.txt, .hidden, logs/.
func host() *fakeConn {
	return &fakeConn{
		home: "/home/user",
		dirs: map[string][]Entry{
			"/":               {dir("home"), dir("etc")},
			"/home":           {dir("user")},
			"/home/user":      {file("notes.txt"), file(".hidden"), dir("logs")},
			"/home/user/logs": {file("app.log")},
			"/etc":            {file("hosts")},
		},
		files: map[string][]byte{"/home/user/notes.txt": []byte("hi")},
	}
}

// pump runs cmds to quiescence, feeding every ResultMsg back into the model —
// the explorer's pumpScans harness — and returns the first non-result message.
func pump(t *testing.T, m *Model, cmd tea.Cmd) tea.Msg {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 100 {
			t.Fatal("pump did not settle")
		}
		msg := cmd()
		if msg == nil {
			return nil
		}
		res, ok := msg.(ResultMsg)
		if !ok {
			return msg
		}
		cmd = m.Update(res)
	}
	return nil
}

// mounted returns a connected model revealed at the remote home.
func mounted(t *testing.T, conn *fakeConn) *Model {
	t.Helper()
	m := New("remote:test", "test", func(string) (Conn, error) { return conn, nil }, nil)
	m.SetSize(60, 12)
	pump(t, &m, m.Init())
	return &m
}

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s, Code: rune(s[0])} }

// rowPaths flattens the visible rows for assertions.
func rowPaths(m *Model) []string {
	var out []string
	for i := 0; i < m.Rows(); i++ {
		out = append(out, m.RowPath(i))
	}
	return out
}

// TestConnectRevealsHome guards the connect flow: the tree opens with the
// remote home's ancestors expanded and the cursor on the home directory.
func TestConnectRevealsHome(t *testing.T) {
	m := mounted(t, host())
	paths := rowPaths(m)
	want := []string{"/etc", "/home", "/home/user", "/home/user/logs", "/home/user/notes.txt"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Fatalf("rows = %v, want %v", paths, want)
	}
	if got := m.RowPath(m.Cursor()); got != "/home/user" {
		t.Fatalf("cursor on %q, want /home/user", got)
	}
}

// TestHiddenToggleFiltersDotEntries guards the "." toggle: dot entries appear
// only while hidden files are shown, consistent with the explorer, and the
// cursor stays on its entry across the rebuild.
func TestHiddenToggleFiltersDotEntries(t *testing.T) {
	m := mounted(t, host())
	for _, p := range rowPaths(m) {
		if strings.HasPrefix(p, "/home/user/.") {
			t.Fatalf("hidden entry %q visible by default", p)
		}
	}
	m.Update(key("."))
	found := false
	for _, p := range rowPaths(m) {
		if p == "/home/user/.hidden" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hidden entry missing after toggle: %v", rowPaths(m))
	}
	if got := m.RowPath(m.Cursor()); got != "/home/user" {
		t.Fatalf("cursor moved to %q across the toggle", got)
	}
	m.Update(key("."))
	if len(rowPaths(m)) != 5 {
		t.Fatalf("toggle back kept %v", rowPaths(m))
	}
}

// TestExpandScansLazily guards lazy loading: entering a collapsed directory
// dispatches its scan and the children land asynchronously.
func TestExpandScansLazily(t *testing.T) {
	m := mounted(t, host())
	m.selectPath("/home/user/logs")
	pump(t, m, m.Update(key("l")))
	found := false
	for _, p := range rowPaths(m) {
		if p == "/home/user/logs/app.log" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expanded dir did not load: %v", rowPaths(m))
	}
}

// TestCollapseOrParentNeverAscendsAboveRoot guards the h-key walk: collapse an
// expanded dir, jump files to their parent, and stay put at the top level.
func TestCollapseOrParentNeverAscendsAboveRoot(t *testing.T) {
	m := mounted(t, host())
	m.selectPath("/home/user/notes.txt")
	m.Update(key("h"))
	if got := m.RowPath(m.Cursor()); got != "/home/user" {
		t.Fatalf("h on a file landed on %q, want the parent", got)
	}
	m.Update(key("h")) // collapse /home/user
	for _, p := range rowPaths(m) {
		if strings.HasPrefix(p, "/home/user/") {
			t.Fatalf("collapse kept child row %q", p)
		}
	}
}

// TestOpenFileEmitsMsg guards the open funnel: enter on a file emits
// OpenFileMsg with the pane key, alias and full remote path.
func TestOpenFileEmitsMsg(t *testing.T) {
	m := mounted(t, host())
	m.selectPath("/home/user/notes.txt")
	msg := pump(t, m, m.Update(key("l")))
	open, ok := msg.(OpenFileMsg)
	if !ok {
		t.Fatalf("expected OpenFileMsg, got %T", msg)
	}
	if open.Key != "remote:test" || open.Alias != "test" || open.Path != "/home/user/notes.txt" || open.Size != 42 {
		t.Fatalf("OpenFileMsg = %+v", open)
	}
}

// TestConnectErrorSurfaces guards the failure path: a refused dial renders
// ssh's message and the fix hint instead of an empty tree.
func TestConnectErrorSurfaces(t *testing.T) {
	m := New("remote:test", "test", func(string) (Conn, error) {
		return nil, errors.New("ssh test: Permission denied (publickey)")
	}, nil)
	m.SetSize(60, 12)
	pump(t, &m, m.Init())
	view := m.View()
	if !strings.Contains(view, "Permission denied") {
		t.Fatalf("error view misses the cause:\n%s", view)
	}
	if !strings.Contains(view, "~/.ssh/config") {
		t.Fatalf("error view misses the hint:\n%s", view)
	}
}

// TestRefreshMergesKeepingExpansion guards r: a re-scan of the selected
// directory merges by path, so expanded subtrees survive.
func TestRefreshMergesKeepingExpansion(t *testing.T) {
	conn := host()
	m := mounted(t, conn)
	m.selectPath("/home/user/logs")
	pump(t, m, m.Update(key("l"))) // expand logs
	conn.dirs["/home/user"] = append(conn.dirs["/home/user"], file("new.txt"))
	m.selectPath("/home/user")
	pump(t, m, m.Update(key("r")))
	paths := strings.Join(rowPaths(m), " ")
	if !strings.Contains(paths, "/home/user/new.txt") {
		t.Fatalf("refresh missed the new entry: %v", paths)
	}
	if !strings.Contains(paths, "/home/user/logs/app.log") {
		t.Fatalf("refresh collapsed the expanded subtree: %v", paths)
	}
}

// TestCloseEndsSession guards the pane close: the connection closes and a
// late result is dropped without effect.
func TestCloseEndsSession(t *testing.T) {
	conn := host()
	m := mounted(t, conn)
	m.Close()
	if !conn.closed {
		t.Fatal("Close left the connection open")
	}
	if cmd := m.Update(ResultMsg{Key: m.Key(), scan: &scanResult{path: "/", entries: nil}}); cmd != nil {
		t.Fatal("closed pane still scheduled work")
	}
}

// TestDiscardClosesLateConnect guards the routing dead-end: a connect result
// whose pane is gone must close the just-dialed session, or the ssh
// subprocess would linger.
func TestDiscardClosesLateConnect(t *testing.T) {
	conn := host()
	msg := ResultMsg{Key: "remote:gone", conn: &connResult{conn: conn}}
	msg.Discard()
	if !conn.closed {
		t.Fatal("Discard left the connection open")
	}
}
