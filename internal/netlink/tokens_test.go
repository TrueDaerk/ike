package netlink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStoreIssueVerifyPersist: a token verifies, survives a reload, and the
// file never holds it in the clear.
func TestStoreIssueVerifyPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "clients.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	token, c, err := s.Issue("phone", "10.0.0.2:5000", now)
	if err != nil || token == "" || c.ID == "" {
		t.Fatalf("issue: %v %q %+v", err, token, c)
	}
	if _, ok := s.Verify(token, now); !ok {
		t.Fatal("the fresh token must verify")
	}
	if _, ok := s.Verify(token+"x", now); ok {
		t.Fatal("a mangled token must not verify")
	}
	if _, ok := s.Verify("", now); ok {
		t.Fatal("the empty token must not verify")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("the plaintext token must never reach the disk")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("clients file mode %v, want 0600", fi.Mode().Perm())
	}
	again, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Verify(token, now.Add(2*time.Minute))
	if !ok || got.Name != "phone" {
		t.Fatalf("token must survive a reload: %v %+v", ok, got)
	}
	if list := again.Clients(); len(list) != 1 || list[0].LastSeen.Before(now.Add(time.Minute)) {
		t.Fatalf("last-seen must be stamped: %+v", list)
	}
}

// TestStoreRevoke: revoking one client or all of them makes their tokens
// useless; unknown IDs are a no-op.
func TestStoreRevoke(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "clients.json"))
	now := time.Now()
	t1, c1, _ := s.Issue("a", "1:1", now)
	t2, _, _ := s.Issue("b", "2:2", now)
	if ok, err := s.Revoke("nope"); ok || err != nil {
		t.Fatalf("unknown id: %v %v", ok, err)
	}
	if ok, err := s.Revoke(c1.ID); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if _, ok := s.Verify(t1, now); ok {
		t.Fatal("revoked token must fail")
	}
	if _, ok := s.Verify(t2, now); !ok {
		t.Fatal("the other token stays")
	}
	if err := s.RevokeAll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Verify(t2, now); ok || len(s.Clients()) != 0 {
		t.Fatal("RevokeAll must empty the store")
	}
}

// TestStoreInMemory: path "" never touches the disk.
func TestStoreInMemory(t *testing.T) {
	s, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := s.Issue("x", "1:1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Verify(tok, time.Now()); !ok {
		t.Fatal("in-memory token must verify")
	}
}

// TestDefaultStorePathHonoursOverride: IKE_CONFIG_DIR redirects the file.
func TestDefaultStorePathHonoursOverride(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "/x/y")
	if got := DefaultStorePath(); got != filepath.Join("/x/y", "netlink-clients.json") {
		t.Fatalf("got %q", got)
	}
}
