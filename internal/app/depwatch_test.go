package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	langreg "ike/internal/lang"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/watch"
)

// depwatch_test.go guards the app half of #2613: what gets registered with the
// watcher, and how a marker event is tagged for the LSP bridge.

// depProject lays out a project with the markers a Python-shaped language
// declares — a manifest in the root and a site-packages directory buried
// under two pruned segments — and registers that language.
func depProject(t *testing.T) (root, site string) {
	t.Helper()
	root = t.TempDir()
	site = filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(filepath.Join(site, "rich"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"depmark.toml", "requirements.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(site, "rich", "console.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	langreg.Register(langreg.Language{
		ID: "depmark", Extensions: []string{"depmark"},
		Deps: &langreg.DepWatch{
			Files:   []string{"depmark.toml", "requirements*.txt"},
			Dirs:    func(string) []string { return []string{site} },
			Restart: true,
		},
	})
	return root, site
}

// TestDepWatchesAreNonRecursive guards the acceptance rule the whole design
// rests on: after opening the project the watcher's registered path set holds
// the site-packages *directory* and not a single file below it.
func TestDepWatchesAreNonRecursive(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	root, site := depProject(t)

	m := newSized()
	if err := m.watcher.Start(root); err != nil {
		t.Fatal(err)
	}
	defer m.watcher.Stop()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	got := m.watcher.WatchedPaths()
	var hasSite bool
	for _, p := range got {
		if p == site {
			hasSite = true
			continue
		}
		if rel, err := filepath.Rel(site, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			t.Errorf("a path below the marker directory is watched: %q", p)
		}
	}
	if !hasSite {
		t.Fatalf("WatchedPaths = %v, want the site-packages directory %s", got, site)
	}
	// The static root manifest registers too; the glob entry names no path.
	if !containsPath(got, filepath.Join(root, "depmark.toml")) {
		t.Errorf("WatchedPaths = %v, want the root manifest", got)
	}
	for _, p := range got {
		if filepath.Base(p) == "requirements*.txt" {
			t.Errorf("a glob entry was registered as a path: %q", p)
		}
	}
}

// TestDepMarkerEventsAreTagged guards the routing seam: an event on a marker
// — or on an entry of a marker directory — reaches the hook subscribers
// carrying the declaring language and the project root, while an ordinary
// project file carries neither.
func TestDepMarkerEventsAreTagged(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	root, site := depProject(t)

	var got []plugin.FileChange
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{
		Hooks: []plugin.Hook{{
			ID: "p.dep", Event: plugin.EventExternalFileChange,
			Notify: func(h host.API, payload any) tea.Cmd {
				if fc, ok := payload.(plugin.FileChange); ok {
					got = append(got, fc)
				}
				return nil
			},
		}},
	}})
	m := NewWith(reg, host.MapConfig{})
	if err := m.watcher.Start(root); err != nil {
		t.Fatal(err)
	}
	defer m.watcher.Stop()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	distInfo := filepath.Join(site, "rich-13.7.0.dist-info")
	plain := filepath.Join(root, "main.depmark")
	reqs := filepath.Join(root, "requirements-dev.txt")
	for _, ev := range []watch.EventMsg{
		{Kind: watch.FileChanged, Path: filepath.Join(root, "depmark.toml")},
		{Kind: watch.FileCreated, Path: distInfo},
		{Kind: watch.FileChanged, Path: reqs},
		{Kind: watch.FileChanged, Path: plain},
	} {
		tm, _ = m.Update(ev)
		m = tm.(Model)
	}

	if len(got) != 4 {
		t.Fatalf("hook payloads = %+v, want four", got)
	}
	for i, fc := range got[:3] {
		if fc.DepLang != "depmark" {
			t.Errorf("payload[%d] (%s) DepLang = %q, want depmark", i, fc.Path, fc.DepLang)
		}
		if fc.DepRoot != root {
			t.Errorf("payload[%d] DepRoot = %q, want %q", i, fc.DepRoot, root)
		}
	}
	if got[3].DepLang != "" {
		t.Errorf("an ordinary project file must carry no DepLang, got %q", got[3].DepLang)
	}
}

// TestDepWatchesFollowProjectSwitch guards the reconcile: re-rooting the
// watcher drops the old project's marker registrations and takes the new
// project's, so a switch never leaks a watch.
func TestDepWatchesFollowProjectSwitch(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	root, site := depProject(t)

	m := newSized()
	if err := m.watcher.Start(root); err != nil {
		t.Fatal(err)
	}
	defer m.watcher.Stop()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	if !containsPath(m.watcher.WatchedPaths(), site) {
		t.Fatalf("setup: %v lacks %s", m.watcher.WatchedPaths(), site)
	}

	other := t.TempDir()
	if err := m.watcher.Start(other); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 31})
	m = tm.(Model)

	got := m.watcher.WatchedPaths()
	if containsPath(got, filepath.Join(root, "depmark.toml")) {
		t.Errorf("the old project's manifest watch leaked: %v", got)
	}
	if !containsPath(got, filepath.Join(other, "depmark.toml")) {
		t.Errorf("WatchedPaths = %v, want the new project's manifest", got)
	}
}
