package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// watchedfiles_test.go covers the workspace/didChangeWatchedFiles path (#1144):
// glob matching, registration round-trip, the language-match fallback, and the
// debounce/merge discipline.

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.php", "src/Foo.php", true},
		{"**/*.php", "Foo.php", true}, // `**/` matches zero directories
		{"**/*.php", "a/b/c/Foo.php", true},
		{"**/*.php", "src/Foo.txt", false},
		{"**", "anything/at/all.go", true},
		{"**/composer.json", "composer.json", true},
		{"**/composer.json", "vendor/pkg/composer.json", true},
		{"**/composer.json", "composer.lock", false},
		{"src/**/*.go", "src/a/b/x.go", true},
		{"src/**/*.go", "src/x.go", true},
		{"src/**/*.go", "lib/x.go", false},
		{"*.go", "main.go", true},
		{"*.go", "sub/main.go", false}, // `*` never crosses a separator
		{"main.?o", "main.go", true},
		{"main.?o", "main.gxo", false},
		{"**/*.{ts,tsx}", "app/x.ts", true},
		{"**/*.{ts,tsx}", "app/x.tsx", true},
		{"**/*.{ts,tsx}", "app/x.tso", false},
		{"[a-c].go", "b.go", true},
		{"[a-c].go", "d.go", false},
		{"[!a-c].go", "d.go", true},
		{"a/**", "a/b/c", true},
		{"a/**", "b/c", false},
		{"/abs/**/*.go", "/abs/pkg/x.go", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMergeChangeTypes(t *testing.T) {
	cases := []struct {
		old, next, want int
		keep            bool
	}{
		{protocol.FileChangeCreated, protocol.FileChangeChanged, protocol.FileChangeCreated, true},
		{protocol.FileChangeCreated, protocol.FileChangeDeleted, 0, false},
		{protocol.FileChangeDeleted, protocol.FileChangeCreated, protocol.FileChangeChanged, true},
		{protocol.FileChangeChanged, protocol.FileChangeDeleted, protocol.FileChangeDeleted, true},
		{protocol.FileChangeChanged, protocol.FileChangeChanged, protocol.FileChangeChanged, true},
	}
	for _, c := range cases {
		got, keep := mergeChangeTypes(c.old, c.next)
		if keep != c.keep || (keep && got != c.want) {
			t.Errorf("mergeChangeTypes(%d, %d) = (%d, %v), want (%d, %v)", c.old, c.next, got, keep, c.want, c.keep)
		}
	}
}

// TestRegisterCapabilityStoresAndUnregisterDrops covers the registration
// round-trip in isolation: a client/registerCapability payload lands in the
// server's watcher set, unregisterCapability removes it by id.
func TestRegisterCapabilityStoresAndUnregisterDrops(t *testing.T) {
	m := New(nil, fakeConnector(), Callbacks{})
	srv := &server{lang: "php", root: "/proj"}
	regs := []protocol.Registration{{
		ID:              "w1",
		Method:          "workspace/didChangeWatchedFiles",
		RegisterOptions: json.RawMessage(`{"watchers":[{"globPattern":"**/*.php","kind":7},{"globPattern":{"baseUri":"file:///proj/src","pattern":"**/*.inc"}}]}`),
	}, {
		ID:     "other",
		Method: "workspace/didChangeConfiguration",
	}}
	m.registerWatchers(srv, regs)
	if got := len(srv.watchers["w1"]); got != 2 {
		t.Fatalf("stored watchers = %d, want 2", got)
	}
	if w := srv.watchers["w1"][1]; w.GlobPattern.Pattern != "**/*.inc" || w.GlobPattern.BaseURI != "file:///proj/src" {
		t.Fatalf("RelativePattern decoded wrong: %+v", w.GlobPattern)
	}
	if _, ok := srv.watchers["other"]; ok {
		t.Fatal("non-watched-files registration must be ignored")
	}
	m.unregisterWatchers(srv, []protocol.Unregistration{{ID: "w1", Method: "workspace/didChangeWatchedFiles"}})
	if len(srv.watchers) != 0 {
		t.Fatalf("unregister left %d sets", len(srv.watchers))
	}
}

// waitForWatchers polls until the (single) running server has registered
// watcher globs — the fake sends client/registerCapability asynchronously
// after "initialized".
func waitForWatchers(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, srv := range m.servers {
			if len(srv.watchers) > 0 {
				m.mu.Unlock()
				return
			}
		}
		m.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server never registered watchers")
}

// TestWatchedFilesRegisteredGlobsFilter is the wire-level round trip: the fake
// server registers `**/*.php`, external events flow through FileEvent, and the
// notification carries exactly the matching files — the Intelephense repro
// (a class file created externally) in miniature.
func TestWatchedFilesRegisteredGlobsFilter(t *testing.T) {
	watched := make(chan protocol.DidChangeWatchedFilesParams, 4)
	spec := lsp.ServerSpec{Language: "php", Command: "fake"}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind: protocol.SyncFull,
		watched:  watched,
		registerWatchers: json.RawMessage(
			`[{"id":"w1","method":"workspace/didChangeWatchedFiles","registerOptions":{"watchers":[{"globPattern":"**/*.php"}]}}]`),
	}), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 10 * time.Millisecond

	dir := t.TempDir()
	open := filepath.Join(dir, "index.php")
	if err := m.Open(open, "php", "<?php"); err != nil {
		t.Fatal(err)
	}
	waitForWatchers(t, m)

	newClass := filepath.Join(dir, "src", "Foo.php")
	m.FileEvent(newClass, protocol.FileChangeCreated)
	m.FileEvent(filepath.Join(dir, "notes.txt"), protocol.FileChangeCreated)

	select {
	case p := <-watched:
		if len(p.Changes) != 1 {
			t.Fatalf("changes = %+v, want just the php file", p.Changes)
		}
		if p.Changes[0].URI != protocol.PathToURI(newClass) || p.Changes[0].Type != protocol.FileChangeCreated {
			t.Fatalf("change = %+v", p.Changes[0])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no didChangeWatchedFiles notification")
	}
}

// TestWatchedFilesFallbackLanguageMatch: a server that never registers globs
// still hears about files of its own language under its root — and nothing
// else.
func TestWatchedFilesFallbackLanguageMatch(t *testing.T) {
	langreg.Register(langreg.Language{ID: "wfgo", Extensions: []string{"wfgo"}})
	watched := make(chan protocol.DidChangeWatchedFilesParams, 4)
	spec := lsp.ServerSpec{Language: "wfgo", Command: "fake", RootMarkers: []string{"wf.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, watched: watched}), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 10 * time.Millisecond

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "wf.mod"), []byte("module x\n"), 0o644)
	if err := m.Open(filepath.Join(dir, "main.wfgo"), "wfgo", "x"); err != nil {
		t.Fatal(err)
	}

	inRoot := filepath.Join(dir, "new.wfgo")
	m.FileEvent(inRoot, protocol.FileChangeCreated)
	m.FileEvent(filepath.Join(dir, "readme.txt"), protocol.FileChangeChanged)  // wrong language
	m.FileEvent(filepath.Join(os.TempDir(), "far.wfgo"), protocol.FileChangeChanged) // outside root

	select {
	case p := <-watched:
		if len(p.Changes) != 1 || p.Changes[0].URI != protocol.PathToURI(inRoot) {
			t.Fatalf("changes = %+v, want only %s", p.Changes, inRoot)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no didChangeWatchedFiles notification")
	}
}

// TestWatchedFilesDebounceMerges: a burst on one path collapses into a single
// event with the merged change type; a create+delete pair cancels entirely.
func TestWatchedFilesDebounceMerges(t *testing.T) {
	langreg.Register(langreg.Language{ID: "wfmerge", Extensions: []string{"wfm"}})
	watched := make(chan protocol.DidChangeWatchedFilesParams, 4)
	spec := lsp.ServerSpec{Language: "wfmerge", Command: "fake", RootMarkers: []string{"wf.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, watched: watched}), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 20 * time.Millisecond

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "wf.mod"), []byte("module x\n"), 0o644)
	if err := m.Open(filepath.Join(dir, "main.wfm"), "wfmerge", "x"); err != nil {
		t.Fatal(err)
	}

	burst := filepath.Join(dir, "burst.wfm")
	m.FileEvent(burst, protocol.FileChangeCreated)
	m.FileEvent(burst, protocol.FileChangeChanged) // still Created to the server
	ephemeral := filepath.Join(dir, "tmp.wfm")
	m.FileEvent(ephemeral, protocol.FileChangeCreated)
	m.FileEvent(ephemeral, protocol.FileChangeDeleted) // cancels out

	select {
	case p := <-watched:
		if len(p.Changes) != 1 {
			t.Fatalf("changes = %+v, want one merged event", p.Changes)
		}
		if p.Changes[0].URI != protocol.PathToURI(burst) || p.Changes[0].Type != protocol.FileChangeCreated {
			t.Fatalf("merged change = %+v", p.Changes[0])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no didChangeWatchedFiles notification")
	}
}

// TestWatcherAppliesKindBits: a watcher registered for deletes only never
// matches a create.
func TestWatcherAppliesKindBits(t *testing.T) {
	kind := protocol.WatchDelete
	w := protocol.FileSystemWatcher{GlobPattern: protocol.GlobPattern{Pattern: "**/*.go"}, Kind: &kind}
	if watcherApplies(w, "/proj", "/proj/x.go", protocol.FileChangeCreated) {
		t.Fatal("create must not match a delete-only watcher")
	}
	if !watcherApplies(w, "/proj", "/proj/x.go", protocol.FileChangeDeleted) {
		t.Fatal("delete must match a delete-only watcher")
	}
}
