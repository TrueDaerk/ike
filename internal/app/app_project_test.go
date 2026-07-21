package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/registry"
)

// TestProjectPickerOpensLocked verifies the project.switch wiring (#12): the
// dispatched OpenPickerMsg opens the palette locked to the picker mode.
func TestProjectPickerOpensLocked(t *testing.T) {
	m := sized(t, 100, 40)
	if m.palette.IsOpen() {
		t.Fatal("palette should start closed")
	}
	out, _ := m.Update(project.OpenPickerMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("OpenPickerMsg should open the palette")
	}
}

// switchModel builds a sized model with per-project state files (no
// IKE_CONFIG_DIR redirect), so a switch resolves session/layout under each
// project's own .ike directory exactly like production.
func switchModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", "")
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// sameDir compares two paths through symlink resolution (macOS temp dirs live
// behind /var -> /private/var).
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func cwd(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestSwitchProjectReRoots covers the clean-buffer transaction: chdir, explorer
// re-rooted, old project's session persisted, and the follow-up cmd batch
// emitting SwitchedMsg.
func TestSwitchProjectReRoots(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("cwd should be the new root, got %s", cwd(t))
	}
	if !sameDir(t, m.explorer().Root(), dst) {
		t.Fatalf("explorer should re-root, got %s", m.explorer().Root())
	}
	if cmd == nil {
		t.Fatal("switch should return the follow-up batch")
	}
	if _, err := os.Stat(filepath.Join(src, ".ike", "session.json")); err != nil {
		t.Errorf("old project's session should persist on switch: %v", err)
	}
}

// TestSwitchSameRootIsNoop: switching to the current root does not rebuild.
func TestSwitchSameRootIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: cwd(t)})
	m = out.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "already in") {
		t.Fatalf("same-root switch should surface a friendly no-op, got %+v", m.toasts)
	}
}

// TestSwitchFailedSurfacesError: an invalid path never mutates the project and
// lands as an error toast.
func TestSwitchFailedSurfacesError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)
	before := cwd(t)

	msg := project.SwitchTo(filepath.Join(dir, "missing"))()
	fail, ok := msg.(project.SwitchFailedMsg)
	if !ok {
		t.Fatalf("expected SwitchFailedMsg, got %#v", msg)
	}
	out, _ := m.Update(fail)
	m = out.(Model)
	if cwd(t) != before {
		t.Fatal("failed switch must not change the project")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "cannot switch") {
		t.Fatalf("failure should surface as an error toast, got %+v", m.toasts)
	}
}

// TestSwitchWithDirtyBufferIsSeamless (#777): dirty buffers no longer gate
// the switch — the workspace (buffers included) parks in the background, the
// file stays unwritten, and switching back resumes the edit in place.
func TestSwitchWithDirtyBufferIsSeamless(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	m := switchModel(t)
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("dirty switch must proceed seamlessly, cwd = %s", cwd(t))
	}
	if m.switchPromptOpen() {
		t.Fatal("seamless switch must not open the unsaved-changes prompt")
	}
	if cmd == nil {
		t.Fatal("switch should return the follow-up batch")
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("parking must not write the buffer, file = %q", data)
	}

	// Switching back resumes the parked workspace: the edit is still there,
	// still unsaved.
	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	found := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || ed.Path() != file {
				continue
			}
			found = true
			if !ed.Dirty() {
				t.Fatal("resumed buffer must still be dirty")
			}
			if !strings.HasPrefix(ed.Text(), "Xone") {
				t.Fatalf("resumed buffer lost the edit, text = %q", ed.Text())
			}
		}
	}
	if !found {
		t.Fatal("resumed workspace must still hold the dirty buffer")
	}
}

// TestSwitchRoundTripResumesWorkspaceLive (#777): the parked workspace comes
// back as the same live unit — same registry, same running terminal session.
func TestSwitchRoundTripResumesWorkspaceLive(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	srcPanes := m.activeWS().Panes
	termKey := srcPanes.Focused()
	sess := srcPanes.Get(termKey).Terminal()
	t.Cleanup(func() { sess.Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if m.activeWS().Panes == srcPanes {
		t.Fatal("the new project must get its own pane registry")
	}
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			t.Fatal("the src terminal must stay parked, not follow into dst")
		}
	}
	if !sess.Running() {
		t.Fatal("the parked terminal must keep running in the background")
	}

	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	if m.activeWS().Panes != srcPanes {
		t.Fatal("switching back must resume the parked registry, not rebuild")
	}
	inst := m.activeWS().Panes.Get(termKey)
	if inst == nil || inst.Kind() != pane.KindTerminal || inst.Terminal() != sess {
		t.Fatal("the resumed workspace must hold the same live terminal session")
	}
	if !inst.Terminal().Running() {
		t.Fatal("the resumed session must still be running")
	}
}

// TestSwitchNewTerminalSpawnsInNewRoot (#779): everything root-derived spawns
// against the active workspace's root after a switch — a fresh terminal lands
// in the new project, not the old one.
func TestSwitchNewTerminalSpawnsInNewRoot(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(TerminalNewMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("terminal.new must open a terminal after the switch")
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	if !sameDir(t, inst.Terminal().Dir(), dst) {
		t.Fatalf("new terminal must anchor at the new root, dir = %q", inst.Terminal().Dir())
	}
}

// TestResumeNewTerminalSpawnsInResumedRoot (#779): after switching back, new
// root-derived work anchors at the resumed project again — while the parked
// project's existing terminals kept their own origin dir untouched.
func TestResumeNewTerminalSpawnsInResumedRoot(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(src)
	m := switchModel(t)
	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	first := m.activeWS().Panes.FocusedInstance()
	t.Cleanup(func() { first.Terminal().Close() })

	out, _ = m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: src})
	m = out.(Model)
	if !sameDir(t, first.Terminal().Dir(), src) {
		t.Fatalf("parked terminal's origin dir must survive, got %q", first.Terminal().Dir())
	}
	out, _ = m.Update(TerminalNewMsg{})
	m = out.(Model)
	second := m.activeWS().Panes.FocusedInstance()
	if second == nil || second.Kind() != pane.KindTerminal {
		t.Fatal("terminal.new must open a terminal after the resume")
	}
	t.Cleanup(func() { second.Terminal().Close() })
	if !sameDir(t, second.Terminal().Dir(), src) {
		t.Fatalf("post-resume terminal must anchor at the resumed root, dir = %q", second.Terminal().Dir())
	}
}

// TestSwitchReAnchorsConfigLayer (#779): the project config layer follows the
// switch — a [ui] setting in the target's .ike/settings.toml is effective
// right after switching, and the config options point at the new root.
func TestSwitchReAnchorsConfigLayer(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst, filepath.Join(dst, ".ike")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dst, ".ike", "settings.toml"),
		[]byte("[palette]\nmax_results = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	m := switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if v, ok := m.host.Config().Get("palette.max_results"); !ok || v != "3" {
		t.Fatalf("project config layer must re-resolve on switch, got %q ok=%v", v, ok)
	}
}

// evictFixture builds three projects; c's config caps background workspaces
// at one, so arriving there forces an eviction decision about a (#780).
func evictFixture(t *testing.T) (a, b, c string) {
	t.Helper()
	base := t.TempDir()
	a, b, c = filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "c")
	for _, d := range []string{a, b, filepath.Join(c, ".ike")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(c, ".ike", "settings.toml"),
		[]byte("[project]\nmax_workspaces = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, b, c
}

// TestWorkspaceCapEvictsIdleLRU (#780): past project.max_workspaces the
// least-recently-used idle background workspace drops silently.
func TestWorkspaceCapEvictsIdleLRU(t *testing.T) {
	a, b, c := evictFixture(t)
	t.Chdir(a)
	m := switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: c})
	m = out.(Model)
	if m.evictPromptOpen() {
		t.Fatal("an idle workspace must evict without a prompt")
	}
	bg := m.ws.Background()
	if len(bg) != 1 || !sameDir(t, bg[0], b) {
		t.Fatalf("background = %v, want just b", bg)
	}
}

// TestWorkspaceCapGuardsBusyEviction (#780): a busy LRU workspace (dirty
// buffer) asks first; e evicts, esc keeps it over the limit.
func TestWorkspaceCapGuardsBusyEviction(t *testing.T) {
	a, b, c := evictFixture(t)
	file := filepath.Join(a, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	m := switchModel(t)
	m = openDirty(t, m, file)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: c})
	m = out.(Model)
	if !m.evictPromptOpen() {
		t.Fatal("a busy LRU workspace must open the eviction guard")
	}
	if len(m.ws.Background()) != 2 {
		t.Fatal("nothing may be evicted before the answer")
	}
	// esc keeps it (over the limit).
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.evictPromptOpen() || len(m.ws.Background()) != 2 {
		t.Fatal("esc must keep the busy workspace")
	}
	// Trigger again via another switch round and confirm with e.
	out, _ = m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, _ = m.Update(project.SwitchProjectMsg{Root: c})
	m = out.(Model)
	if !m.evictPromptOpen() {
		t.Fatal("the guard must re-ask on the next switch")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = out.(Model)
	bg := m.ws.Background()
	if len(bg) != 1 || !sameDir(t, bg[0], b) {
		t.Fatalf("e must evict the busy workspace, background = %v", bg)
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("eviction discards, never writes, file = %q", data)
	}
}

// TestCloseWorkspaceFromList guards #820: a parked background workspace can
// be unloaded from the recent-projects list without switching; the active
// one cannot.
func TestCloseWorkspaceFromList(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(a)
	m := switchModel(t)
	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)

	bg := m.ws.Background()
	if len(bg) != 1 {
		t.Fatalf("background = %v, want the parked a", bg)
	}
	root := bg[0]
	if m.ws.Peek(root) == nil {
		t.Fatal("parked workspace must be peekable (the list marker source)")
	}

	// Closing the active workspace from the list is refused.
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: m.activeWS().Root})
	m = out.(Model)
	if m.activeWS() == nil || len(m.ws.Background()) != 1 {
		t.Fatal("closing the active project from the list must be a no-op")
	}

	// Closing the background workspace unloads it without switching.
	active := m.activeWS().Root
	out, _ = m.Update(project.CloseWorkspaceMsg{Path: root})
	m = out.(Model)
	if m.ws.Peek(root) != nil || len(m.ws.Background()) != 0 {
		t.Fatal("close-from-list must drop the background workspace")
	}
	if m.activeWS().Root != active {
		t.Fatal("close-from-list must not switch the active project")
	}
}

// TestPickerMarksOpenWorkspaces (#820): picker items for in-memory roots
// carry the badge and the close aux action.
func TestPickerMarksOpenWorkspaces(t *testing.T) {
	entries := []project.Entry{{Name: "alpha", Path: "/p/alpha"}, {Name: "beta", Path: "/p/beta"}}
	pm := project.NewPickerMode(func() []project.Entry { return entries })
	pm.SetOpen(func(path string) bool { return path == "/p/alpha" })
	items := pm.Results("", palette.Context{})
	if len(items) < 2 {
		t.Fatalf("items = %d, want the two history entries", len(items))
	}
	if items[0].Badge != "●" {
		t.Fatal("open workspace must carry the ● badge")
	}
	if aux, ok := items[0].Aux.(project.CloseWorkspaceMsg); !ok || aux.Path != "/p/alpha" {
		t.Fatalf("open workspace aux = %#v, want CloseWorkspaceMsg", items[0].Aux)
	}
	if items[1].Badge != "" || items[1].Aux != nil {
		t.Fatal("historical-only entry must stay unmarked")
	}
}
