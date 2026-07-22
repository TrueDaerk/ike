package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/project"
	"ike/internal/registry"
)

// bufferClosedRecorder returns a registry with a hook plugin recording every
// EventBufferClosed payload into the returned slice pointer.
func bufferClosedRecorder() (*registry.Registry, *[]string) {
	got := &[]string{}
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Hooks: []plugin.Hook{{
		ID: "p.didclose", Event: plugin.EventBufferClosed,
		Notify: func(h host.API, payload any) tea.Cmd {
			if path, ok := payload.(string); ok {
				*got = append(*got, path)
			}
			return nil
		},
	}}}})
	return reg, got
}

// writeFiles creates the named files under dir and returns their paths.
func writeFiles(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
		if err := os.WriteFile(paths[i], []byte(n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// TestCloseTabFiresBufferClosed guards #827: closing an editor tab fires
// EventBufferClosed with the file path once the view is gone.
func TestCloseTabFiresBufferClosed(t *testing.T) {
	reg, got := bufferClosedRecorder()
	m := sizedWith(t, reg, 100, 40)
	paths := writeFiles(t, t.TempDir(), "a.txt", "b.txt")

	out, _ := m.Update(explorer.OpenFileMsg{Path: paths[0]})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: paths[1]})
	m = out.(Model)
	if len(*got) != 0 {
		t.Fatalf("no close yet, got %v", *got)
	}

	// The active tab is b; cmd+w closes it.
	out, _ = m.Update(CloseTabMsg{})
	m = out.(Model)
	if len(*got) != 1 || (*got)[0] != paths[1] {
		t.Fatalf("EventBufferClosed payloads = %v, want [%s]", *got, paths[1])
	}
	if m.editorWithFile(paths[0]) == "" {
		t.Fatal("a must stay open")
	}
}

// TestSharedViewsFireBufferClosedOnce guards #827: a file shown by two panes
// (#142 shared document) fires only when its last view closes.
func TestSharedViewsFireBufferClosedOnce(t *testing.T) {
	reg, got := bufferClosedRecorder()
	m := sizedWith(t, reg, 120, 40)
	paths := writeFiles(t, t.TempDir(), "a.txt", "b.txt")
	a, b := paths[0], paths[1]

	out, _ := m.Update(explorer.OpenFileMsg{Path: a})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: b, NewPane: true})
	m = out.(Model)
	// Open a as a second tab of the new pane: two views of a exist now.
	// A plain open would focus a's existing pane since #930, so build the
	// second view directly — the split/tab-drag flows that still create one.
	if !m.openInTab(m.activeEditorKey(), a) {
		t.Fatal("fixture: second view of a failed to open")
	}
	if len(m.editorKeysForPath(a)) != 2 {
		t.Fatalf("fixture: a must be open in two panes, keys = %v", m.editorKeysForPath(a))
	}

	// Close a's tab in the second pane: one view remains — no event.
	out, _ = m.Update(CloseTabMsg{})
	m = out.(Model)
	if len(*got) != 0 {
		t.Fatalf("a still has a view, got %v", *got)
	}

	// Close the second pane (single tab b now): b's only view goes.
	out, _ = m.Update(CloseTabMsg{})
	m = out.(Model)
	if len(*got) != 1 || (*got)[0] != b {
		t.Fatalf("EventBufferClosed payloads = %v, want [%s]", *got, b)
	}
}

// TestBufferStaysOpenInParkedWorkspace guards #827: a file also shown by a
// parked background workspace's editor keeps its LSP document — closing the
// active workspace's view fires nothing.
func TestBufferStaysOpenInParkedWorkspace(t *testing.T) {
	base := t.TempDir()
	pa, pb := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{pa, pb} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shared := writeFiles(t, base, "shared.txt")[0]

	reg, got := bufferClosedRecorder()
	t.Chdir(pa)
	t.Setenv("IKE_CONFIG_DIR", "")
	m := NewWith(reg, host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(Model)

	out, _ := m.Update(explorer.OpenFileMsg{Path: shared})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: pb})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: shared})
	m = out.(Model)

	// Close the view in the active workspace; the parked one still shows it.
	out, _ = m.Update(CloseTabMsg{})
	m = out.(Model)
	if len(*got) != 0 {
		t.Fatalf("the parked workspace still shows the file, got %v", *got)
	}
	if m.pathOpenAnywhere(shared) != true {
		t.Fatal("fixture: the parked view must count as open")
	}
}
