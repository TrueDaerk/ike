package app

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/deeplink"
	"ike/internal/netlink"
	"ike/internal/palette"
)

// writeGitConfig makes dir look like a checkout with one origin remote.
func writeGitConfig(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = " + remote + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNetStatusHolderRootChange: the holder fills the remote from the root's
// git config, keeps it while only the file and cursor move, and re-reads it
// when the project root changes (a project switch).
func TestNetStatusHolderRootChange(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "ike")
	writeGitConfig(t, repo, "git@github.com:TrueDaerk/ike.git")
	plain := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	var h netStatusHolder
	if got := h.get(); got != (netlink.Status{}) {
		t.Fatalf("a fresh holder must be empty: %+v", got)
	}
	h.set(netlink.Status{Project: "ike", Root: repo, File: "a.go", Line: 1, Col: 1})
	got := h.get()
	if got.Remote != "github.com/truedaerk/ike" {
		t.Fatalf("remote %q", got.Remote)
	}
	// A cursor move within the same root keeps the remote.
	h.set(netlink.Status{Project: "ike", Root: repo, File: "b.go", Line: 7, Col: 3})
	got = h.get()
	if got.Remote != "github.com/truedaerk/ike" || got.File != "b.go" || got.Line != 7 || got.Col != 3 {
		t.Fatalf("after a cursor move %+v", got)
	}
	// A project without a remote drops it again.
	h.set(netlink.Status{Project: "scratch", Root: plain})
	got = h.get()
	if got.Remote != "" || got.Project != "scratch" || got.Root != plain || got.File != "" {
		t.Fatalf("after the switch %+v", got)
	}
	// Coming back re-reads it.
	h.set(netlink.Status{Project: "ike", Root: repo})
	if got = h.get(); got.Remote != "github.com/truedaerk/ike" {
		t.Fatalf("back in the repo %+v", got)
	}
}

// TestNetStatusHolderConcurrent: the getter the server calls on connection
// goroutines runs while the update loop writes (go test -race).
func TestNetStatusHolderConcurrent(t *testing.T) {
	var h netStatusHolder
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if s := h.get(); s.Root != "" && s.Project == "" {
						t.Error("a snapshot with a root must name its project")
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 500; i++ {
		h.set(netlink.Status{Project: "ike", Root: "/r/ike", File: "a.go", Line: i + 1, Col: 1})
	}
	close(stop)
	wg.Wait()
}

// TestNetLinkStatusOverTheWire: the snapshot the settled Update pass keeps
// is what a paired client reads — the active workspace's root and its
// remote, the focused editor's file and cursor — and it follows both a
// project switch and a cursor move.
func TestNetLinkStatusOverTheWire(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	repo := filepath.Join(t.TempDir(), "ike")
	writeGitConfig(t, repo, "git@github.com:TrueDaerk/ike.git")
	file := filepath.Join(repo, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Get()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", 0
	if err := m.startNetLink(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.nlServer.Close)
	token, _, err := m.nlServer.Store().Issue("test", "127.0.0.1:1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ask := func() netlink.Response {
		t.Helper()
		conn, err := net.Dial("tcp", m.nlServer.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		req, _ := json.Marshal(netlink.Request{Cmd: "status", Token: token})
		_, _ = conn.Write(append(req, '\n'))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var resp netlink.Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		return resp
	}

	// The project switch: the workspace root is the authoritative source.
	m.activeWS().Root = repo
	out, _ := m.Update(palette.OpenFileMsg{Path: file})
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || ed.Path() != file {
		t.Fatalf("the file should be open in an editor")
	}
	r := ask()
	if r.Type != "status" || r.Root != repo || r.Project != "ike" {
		t.Fatalf("status %+v (want root %q)", r, repo)
	}
	if r.Remote != "github.com/truedaerk/ike" {
		t.Errorf("remote %q", r.Remote)
	}
	if r.File != "main.go" || r.Line != 1 || r.Col != 1 {
		t.Errorf("file/cursor %+v", r)
	}

	// A cursor move is reflected on the next status.
	m.activeEditor().SetCursor(2, 3)
	out, _ = m.Update(nil)
	m = out.(Model)
	r = ask()
	if r.File != "main.go" || r.Line != 3 || r.Col != 4 {
		t.Fatalf("after the cursor move %+v", r)
	}
	l, err := deeplink.Parse(r.Link)
	if err != nil || l.RemoteKey != r.Remote || l.File != "main.go" || l.Line != 3 {
		t.Fatalf("link %q parsed to %+v err %v", r.Link, l, err)
	}
}

// TestNetRelFile: a file inside the root is reported relative to it with
// forward slashes; anything outside — or no root at all — is no part of the
// project a link can address.
func TestNetRelFile(t *testing.T) {
	root := filepath.Join("/home", "dev", "ike")
	for _, tc := range []struct {
		name, root, path, want string
		ok                     bool
	}{
		{"inside", root, filepath.Join(root, "internal", "app", "app.go"), "internal/app/app.go", true},
		{"the root itself", root, root, "", false},
		{"outside", root, filepath.Join("/home", "dev", "other", "x.go"), "", false},
		{"no root", "", filepath.Join(root, "x.go"), "", false},
		{"no path", root, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := netRelFile(tc.root, tc.path)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("netRelFile(%q, %q) = %q, %v; want %q, %v", tc.root, tc.path, got, ok, tc.want, tc.ok)
			}
		})
	}
}
