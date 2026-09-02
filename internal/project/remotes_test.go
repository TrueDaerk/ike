package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordOpenPersistsRemotes (#2396): recording an open of a git checkout
// stores the repository's canonical remote keys in the history entry, and the
// round trip through the config pipeline keeps them.
func TestRecordOpenPersistsRemotes(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[remote \"origin\"]\n\turl = git@github.com:TrueDaerk/ike.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RecordOpen(opts, root, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries := readHistory(t, opts)
	if len(entries) != 1 {
		t.Fatalf("history = %v", entries)
	}
	if len(entries[0].Remotes) != 1 || entries[0].Remotes[0] != "github.com/truedaerk/ike" {
		t.Fatalf("remotes = %v, want the canonical key", entries[0].Remotes)
	}
}

// TestRecordOpenNoRepoNoRemotes: a project without a git checkout records a
// plain entry — no remotes key at all.
func TestRecordOpenNoRepoNoRemotes(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()
	if err := RecordOpen(opts, root, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries := readHistory(t, opts)
	if len(entries) != 1 || len(entries[0].Remotes) != 0 {
		t.Fatalf("history = %+v, want one entry without remotes", entries)
	}
}
