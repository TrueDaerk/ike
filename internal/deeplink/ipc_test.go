package deeplink

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sockDir returns a short-lived directory short enough for a unix socket path
// (macOS caps sun_path at 104 bytes; t.TempDir's name-derived path blows it).
func sockDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "dl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "deeplink")
}

func TestServeAndSend(t *testing.T) {
	dir := sockDir(t)
	got := make(chan string, 1)
	s, err := Serve(dir, func(url string) { got <- url })
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer s.Close()

	url := "ike://open?project=ike"
	if err := Send(dir, url); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case u := <-got:
		if u != url {
			t.Errorf("delivered %q, want %q", u, url)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("link never delivered")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("socket dir perm = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
}

func TestSendNoInstance(t *testing.T) {
	if err := Send(sockDir(t), "ike://open?project=x"); err != ErrNoInstance {
		t.Errorf("Send into empty dir = %v, want ErrNoInstance", err)
	}
}

func TestServerRejectsGarbage(t *testing.T) {
	dir := sockDir(t)
	delivered := make(chan string, 1)
	s, err := Serve(dir, func(url string) { delivered <- url })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	sock, _ := filepath.Glob(filepath.Join(dir, "ike-*.sock"))
	if len(sock) != 1 {
		t.Fatalf("sockets = %v", sock)
	}
	for _, msg := range []string{"exec rm -rf /\n", "open not-a-link\n", "open https://x/y\n"} {
		conn, err := net.Dial("unix", sock[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	select {
	case u := <-delivered:
		t.Errorf("garbage %q was delivered", u)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSendPicksMostRecentlyFocused(t *testing.T) {
	dir := sockDir(t)
	// Two servers in one process share the pid, so fake the second endpoint by
	// hand: a dead socket file with a fresher focus stamp must be skipped (and
	// cleaned up) in favour of the live server.
	got := make(chan string, 1)
	s, err := Serve(dir, func(url string) { got <- url })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dead := filepath.Join(dir, "ike-99999999.sock")
	if err := os.WriteFile(dead, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	focus := filepath.Join(dir, "ike-99999999.focus")
	if err := os.WriteFile(focus, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(focus, future, future)

	if err := Send(dir, "ike://open?project=ike"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("live instance never received the link")
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("dead socket not cleaned up: %v", err)
	}
}
