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

// depwatch_test.go covers the dependency-update path (#2613): marker events
// reaching the servers past the ordinary filters, the notify-vs-restart
// decision per language, and the debounce that turns one `pip install` into
// one restart.

// TestDepEventBypassesLanguageFallback: a marker file whose *language* maps to
// nothing at all — go.sum, a `.dist-info` directory — still reaches the
// language's server, which the plain FileEvent fallback would drop.
func TestDepEventBypassesLanguageFallback(t *testing.T) {
	langreg.Register(langreg.Language{
		ID: "dwgo", Extensions: []string{"dwgo"},
		Deps: &langreg.DepWatch{Files: []string{"dw.mod", "dw.sum"}},
	})
	watched := make(chan protocol.DidChangeWatchedFilesParams, 4)
	spec := lsp.ServerSpec{Language: "dwgo", Command: "fake", RootMarkers: []string{"dw.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, watched: watched}), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 10 * time.Millisecond

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "dw.mod"), []byte("module x\n"), 0o644)
	if err := m.Open(filepath.Join(dir, "main.dwgo"), "dwgo", "x"); err != nil {
		t.Fatal(err)
	}

	sum := filepath.Join(dir, "dw.sum")
	// Untagged, the fallback drops it: dw.sum resolves to no language.
	m.FileEvent(sum, protocol.FileChangeChanged)
	select {
	case p := <-watched:
		t.Fatalf("an untagged marker must not pass the fallback, got %+v", p.Changes)
	case <-time.After(200 * time.Millisecond):
	}

	m.DepEvent("dwgo", dir, sum, protocol.FileChangeChanged)
	select {
	case p := <-watched:
		if len(p.Changes) != 1 || p.Changes[0].URI != protocol.PathToURI(sum) {
			t.Fatalf("changes = %+v, want exactly %s", p.Changes, sum)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the tagged marker never reached the server")
	}
}

// TestDepEventMarkerBypassesRegisteredGlobs: the bypass also holds for a
// server that *did* register watcher globs — pyright registers `**/*.py` and
// would never name a site-packages `.dist-info` directory.
func TestDepEventMarkerBypassesRegisteredGlobs(t *testing.T) {
	langreg.Register(langreg.Language{
		ID: "dwglob", Extensions: []string{"dwg"},
		Deps: &langreg.DepWatch{Files: []string{"dwglob.toml"}},
	})
	watched := make(chan protocol.DidChangeWatchedFilesParams, 4)
	spec := lsp.ServerSpec{Language: "dwglob", Command: "fake", RootMarkers: []string{"dwglob.toml"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{
		syncKind: protocol.SyncFull,
		watched:  watched,
		registerWatchers: json.RawMessage(
			`[{"id":"w1","method":"workspace/didChangeWatchedFiles","registerOptions":{"watchers":[{"globPattern":"**/*.dwg"}]}}]`),
	}), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 10 * time.Millisecond

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "dwglob.toml"), []byte("x\n"), 0o644)
	if err := m.Open(filepath.Join(dir, "main.dwg"), "dwglob", "x"); err != nil {
		t.Fatal(err)
	}
	waitForWatchers(t, m)

	distInfo := filepath.Join(dir, ".venv", "lib", "python3.12", "site-packages", "rich-13.7.0.dist-info")
	m.DepEvent("dwglob", dir, distInfo, protocol.FileChangeCreated)
	select {
	case p := <-watched:
		if len(p.Changes) != 1 || p.Changes[0].URI != protocol.PathToURI(distInfo) {
			t.Fatalf("changes = %+v, want exactly %s", p.Changes, distInfo)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the marker never passed the registered globs")
	}
}

// TestDepEventNotifyOnlyLanguageNeverRestarts: Go, TypeScript and PHP declare
// markers without the restart flag — the notification is the whole reaction.
func TestDepEventNotifyOnlyLanguageNeverRestarts(t *testing.T) {
	langreg.Register(langreg.Language{
		ID: "dwnotify", Extensions: []string{"dwn"},
		Deps: &langreg.DepWatch{Files: []string{"dwnotify.mod"}},
	})
	m, dir, statuses := depTestManager(t, "dwnotify", "dwn", "dwnotify.mod")
	defer m.Shutdown()

	before := serverStartTimes(m)
	for i := 0; i < 5; i++ {
		m.DepEvent("dwnotify", dir, filepath.Join(dir, "dwnotify.mod"), protocol.FileChangeChanged)
	}
	time.Sleep(4 * m.depDelay)

	if got := serverStartTimes(m); !sameStarts(before, got) {
		t.Fatalf("a notify-only language must not restart: %v -> %v", before, got)
	}
	if got := statuses.dependencyToasts(); len(got) != 0 {
		t.Fatalf("a notify-only language must raise no restart toast, got %v", got)
	}
}

// TestDepEventRestartLanguageCoalesces: a restarting language restarts — once
// — however many marker events the install produced, and explains itself with
// a toast. This is the `pip install` touching dozens of dist-info entries.
func TestDepEventRestartLanguageCoalesces(t *testing.T) {
	langreg.Register(langreg.Language{
		ID: "dwpy", Extensions: []string{"dwpy"},
		Deps: &langreg.DepWatch{Files: []string{"dwpy.toml"}, Restart: true},
	})
	m, dir, statuses := depTestManager(t, "dwpy", "dwpy", "dwpy.toml")
	defer m.Shutdown()

	before := serverStartTimes(m)
	site := filepath.Join(dir, ".venv", "lib", "python3.12", "site-packages")
	for i := 0; i < 20; i++ {
		m.DepEvent("dwpy", dir, filepath.Join(site, "pkg", "file"), protocol.FileChangeCreated)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !sameStarts(before, serverStartTimes(m)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sameStarts(before, serverStartTimes(m)) {
		t.Fatal("a restarting language's server never came back up")
	}
	// Settle well past the debounce: the burst must have cost exactly one.
	time.Sleep(4 * m.depDelay)
	toasts := statuses.dependencyToasts()
	if len(toasts) != 1 {
		t.Fatalf("20 marker events must cost one restart, got %d: %v", len(toasts), toasts)
	}
	if toasts[0] != "fake restarted: dependencies changed" {
		t.Fatalf("toast = %q, want the server command and the reason", toasts[0])
	}

	// The open document survived the restart, re-opened against the new server.
	m.mu.Lock()
	_, stillOpen := m.docs[filepath.Join(dir, "main.dwpy")]
	m.mu.Unlock()
	if !stillOpen {
		t.Fatal("the open buffer must survive a dependency restart")
	}
}

// TestDepEventRestartIsRootScoped: a dependency update in one project must not
// take a sibling project's server of the same language down.
func TestDepEventRestartIsRootScoped(t *testing.T) {
	langreg.Register(langreg.Language{
		ID: "dwroot", Extensions: []string{"dwr"},
		Deps: &langreg.DepWatch{Files: []string{"dwroot.toml"}, Restart: true},
	})
	spec := lsp.ServerSpec{Language: "dwroot", Command: "fake", RootMarkers: []string{"dwroot.toml"}}
	m := New(resolver(spec), fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = 10 * time.Millisecond
	m.depDelay = 20 * time.Millisecond

	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		_ = os.WriteFile(filepath.Join(dir, "dwroot.toml"), []byte("x\n"), 0o644)
		if err := m.Open(filepath.Join(dir, "main.dwr"), "dwroot", "x"); err != nil {
			t.Fatal(err)
		}
	}
	before := serverStartTimes(m)
	if len(before) != 2 {
		t.Fatalf("setup: want two rooted servers, got %v", before)
	}

	m.DepEvent("dwroot", a, filepath.Join(a, "dwroot.toml"), protocol.FileChangeChanged)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := serverStartTimes(m); got[key("dwroot", a)] != before[key("dwroot", a)] {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := serverStartTimes(m)
	if got[key("dwroot", a)] == before[key("dwroot", a)] {
		t.Fatal("the updated root's server never restarted")
	}
	if got[key("dwroot", b)] != before[key("dwroot", b)] {
		t.Fatal("the sibling root's server must not restart")
	}
}

// TestDepEventUntaggedFallsBackToFileEvent: DepEvent with no language is
// exactly FileEvent, so the bridge can route every event through one door.
func TestDepEventUntaggedFallsBackToFileEvent(t *testing.T) {
	m := New(nil, fakeConnector(), Callbacks{})
	defer m.Shutdown()
	m.watchedDelay = time.Hour // never flush; inspect the pending state instead

	m.DepEvent("", "", "/proj/x.go", protocol.FileChangeChanged)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.watchedPending[normalizeAbs("/proj/x.go")]; !ok {
		t.Fatalf("untagged event missing from the batch: %v", m.watchedPending)
	}
	if len(m.watchedMarkers) != 0 {
		t.Fatalf("untagged event must tag no marker, got %v", m.watchedMarkers)
	}
	if len(m.depPending) != 0 {
		t.Fatalf("untagged event must arm no restart, got %v", m.depPending)
	}
}

// TestMarkerWantsRootRule pins the marker-interest rule in isolation: same
// language, and the server either sits inside the markers' project root or
// owns the marker path outright.
func TestMarkerWantsRootRule(t *testing.T) {
	cases := []struct {
		name string
		dm   depMarker
		srv  *server
		path string
		want bool
	}{
		{"same lang, server at the project root",
			depMarker{lang: "py", root: "/proj"}, &server{lang: "py", root: "/proj"}, "/proj/pyproject.toml", true},
		{"same lang, server in a sub-root",
			depMarker{lang: "py", root: "/proj"}, &server{lang: "py", root: "/proj/svc"}, "/proj/pyproject.toml", true},
		{"marker outside the root but inside the server's",
			depMarker{lang: "py", root: "/other"}, &server{lang: "py", root: "/proj"}, "/proj/.venv/x", true},
		{"other language",
			depMarker{lang: "py", root: "/proj"}, &server{lang: "go", root: "/proj"}, "/proj/pyproject.toml", false},
		{"unrelated roots",
			depMarker{lang: "py", root: "/other"}, &server{lang: "py", root: "/proj"}, "/other/pyproject.toml", false},
	}
	for _, c := range cases {
		if got := markerWants(c.dm, c.srv, c.path); got != c.want {
			t.Errorf("%s: markerWants = %v, want %v", c.name, got, c.want)
		}
	}
}

// --- helpers ---------------------------------------------------------------

// depTestManager spins up a manager with one open document of langID, a short
// debounce, and a status recorder.
func depTestManager(t *testing.T, langID, ext, marker string) (*Manager, string, *statusRecorder) {
	t.Helper()
	rec := &statusRecorder{}
	spec := lsp.ServerSpec{Language: langID, Command: "fake", RootMarkers: []string{marker}}
	m := New(resolver(spec), fakeConnector(), Callbacks{Status: rec.record})
	m.watchedDelay = 10 * time.Millisecond
	m.depDelay = 30 * time.Millisecond

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(filepath.Join(dir, "main."+ext), langID, "x"); err != nil {
		t.Fatal(err)
	}
	return m, dir, rec
}

// serverStartTimes snapshots each live server's spawn time by key, which is
// how a test tells "restarted" from "kept running".
func serverStartTimes(m *Manager) map[string]time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]time.Time{}
	for k, srv := range m.servers {
		out[k] = srv.startedAt
	}
	return out
}

func sameStarts(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !b[k].Equal(v) {
			return false
		}
	}
	return true
}
