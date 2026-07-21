package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"weak"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/workspace"
)

// twoProjects builds two project dirs and chdirs into the first.
func twoProjects(t *testing.T) (a, b string) {
	t.Helper()
	base := t.TempDir()
	a, b = filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	return a, b
}

// collected reports whether every weak pointer went nil within a few GC
// cycles. Two back-to-back cycles settle cross-generation references; more
// never hurt.
func collected(wps ...weak.Pointer[workspace.Workspace]) bool {
	for i := 0; i < 10; i++ {
		runtime.GC()
		live := false
		for _, wp := range wps {
			if wp.Value() != nil {
				live = true
			}
		}
		if !live {
			return true
		}
	}
	return false
}

// TestCloseWorkspaceReleasesWorkspace guards #825: after close-from-list the
// dropped Workspace must be garbage — nothing in the app (recent list, hooks,
// caches, goroutines) may keep it reachable.
func TestCloseWorkspaceReleasesWorkspace(t *testing.T) {
	_, b := twoProjects(t)
	m := switchModel(t)
	wp := weak.Make(m.activeWS())
	rp := weak.Make(m.activeWS().Panes)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	roots := m.ws.Background()
	if len(roots) != 1 {
		t.Fatalf("background = %v, want the parked a", roots)
	}
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: roots[0]})
	m = out.(Model)
	if m.ws.Peek(roots[0]) != nil {
		t.Fatal("close-from-list must drop the workspace")
	}
	if !collected(wp) {
		t.Fatal("the closed workspace is still reachable — memory leak (#825)")
	}
	for i := 0; i < 10 && rp.Value() != nil; i++ {
		runtime.GC()
	}
	if rp.Value() != nil {
		t.Fatal("the closed workspace's pane registry is still reachable — memory leak (#825)")
	}
}

// TestCloseWorkspaceStopsTerminal guards #825: closing a busy background
// workspace (discard) ends its terminal session — the PTY goroutines join in
// Session.Close — and the workspace becomes collectable.
func TestCloseWorkspaceStopsTerminal(t *testing.T) {
	_, b := twoProjects(t)
	m := switchModel(t)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	var sess *terminalSessionHandle
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			sess = &terminalSessionHandle{s: inst.Terminal()}
		}
	}
	if sess == nil || !sess.s.Running() {
		t.Fatal("fixture: no running terminal session")
	}
	t.Cleanup(func() { sess.s.Close() })
	wp := weak.Make(m.activeWS())

	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	root := m.ws.Background()[0]
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: root})
	m = out.(Model)
	if !m.wsClosePromptOpen() {
		t.Fatal("a running shell must open the close guard (#821)")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if m.ws.Peek(root) != nil {
		t.Fatal("d must close the workspace")
	}
	if sess.s.Running() {
		t.Fatal("the closed workspace's terminal must stop")
	}
	if !collected(wp) {
		t.Fatal("the closed busy workspace is still reachable — memory leak (#825)")
	}
}

// terminalSessionHandle keeps the test's session reference out of the weak
// check's way: the test may pin the session, never the workspace.
type terminalSessionHandle struct{ s sessionLike }

type sessionLike interface {
	Running() bool
	Close()
}

// TestCloseWorkspaceFiresWorkspaceClosedHook guards #825: tearing a background
// workspace down fires EventWorkspaceClosed with the workspace root, so
// subscribers (the LSP bridge) can release per-root state.
func TestCloseWorkspaceFiresWorkspaceClosedHook(t *testing.T) {
	_, b := twoProjects(t)
	var got []string
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Hooks: []plugin.Hook{{
		ID: "p.wsclose", Event: plugin.EventWorkspaceClosed,
		Notify: func(h host.API, payload any) tea.Cmd {
			root, _ := payload.(string)
			got = append(got, root)
			return nil
		},
	}}}})
	t.Setenv("IKE_CONFIG_DIR", "")
	m := NewWith(reg, host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	root := m.ws.Background()[0]
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: root})
	if _, ok := out.(Model); !ok {
		t.Fatal("update must return the model")
	}
	if len(got) != 1 || got[0] != root {
		t.Fatalf("EventWorkspaceClosed payloads = %v, want [%s]", got, root)
	}
}
