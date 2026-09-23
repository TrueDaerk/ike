package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/project"
)

// workspace_guard_toolexempt_test.go covers the per-tool guard opt-out
// (#2704): a [[tools.custom]] entry with guard = false is killed with the
// workspace without a prompt and named in a notice afterwards, while the Run
// tool, command sessions and the default (no guard key) keep asking.

// noGuardTool is sleepTool with guard = false.
func noGuardTool(name string) config.ToolEntry {
	e := sleepTool(name)
	no := false
	e.Guard = &no
	return e
}

// guardedTool is sleepTool with an explicit guard = true.
func guardedTool(name string) config.ToolEntry {
	e := sleepTool(name)
	yes := true
	e.Guard = &yes
	return e
}

// TestGuardExemptToolIsNotActivity: guard = false keeps a live tool pane out
// of wsActivity.running — the workspace is not busy — but records the name so
// the close can report it.
func TestGuardExemptToolIsNotActivity(t *testing.T) {
	withTools(t, noGuardTool("sql"))
	var a wsActivity
	a.addTerm(fakeTerm{running: true, tool: "sql"})
	if a.busy() {
		t.Fatalf("guard = false must not gate the guard, got %+v", a)
	}
	if got := a.exemptTools(); len(got) != 1 || got[0] != "sql" {
		t.Errorf("exemptTools = %v, want [sql]", got)
	}
}

// TestGuardTrueAndAbsentKeyStillAsk: the default is guarded, and an explicit
// guard = true is the same thing spelled out.
func TestGuardTrueAndAbsentKeyStillAsk(t *testing.T) {
	withTools(t, sleepTool("sql"), guardedTool("yarn"))
	for _, name := range []string{"sql", "yarn"} {
		var a wsActivity
		a.addTerm(fakeTerm{running: true, tool: name})
		if !a.busy() || strings.Join(a.summary(), "") != "tool "+name {
			t.Errorf("%s must gate the guard, got %+v", name, a)
		}
		if len(a.exemptTools()) != 0 {
			t.Errorf("%s must not be reported as exempt, got %v", name, a.exemptTools())
		}
	}
}

// TestGuardExemptUnconfiguredToolStillAsks: a live tool with no config entry
// (a stale layout entry, a built-in) keeps the old rule.
func TestGuardExemptUnconfiguredToolStillAsks(t *testing.T) {
	withTools(t, noGuardTool("sql"))
	var a wsActivity
	a.addTerm(fakeTerm{running: true, tool: "k9s"})
	if !a.busy() {
		t.Fatalf("an unconfigured tool must keep gating the guard, got %+v", a)
	}
}

// TestGuardRunToolAndCommandsCannotBeExempted (#1905): what runs in the Run
// tool, and in a command session, is the user's program — guard = false on an
// entry of the same name changes nothing.
func TestGuardRunToolAndCommandsCannotBeExempted(t *testing.T) {
	withTools(t, noGuardTool(runToolName), noGuardTool("build"))
	var a wsActivity
	a.addTerm(fakeTerm{running: true, busy: true, tool: runToolName, label: "dev"})
	a.addTerm(fakeTerm{running: true, busy: true, cmd: true, label: "build"})
	want := "run dev\nrun build"
	if got := strings.Join(a.summary(), "\n"); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if len(a.exemptTools()) != 0 {
		t.Errorf("the Run tool and commands are never exempt, got %v", a.exemptTools())
	}
}

// TestGuardGlobalToolUnaffectedByFlag (#1890): a global tool detaches instead
// of dying, so it gates no guard with or without the flag — and it is not
// reported as killed either.
func TestGuardGlobalToolUnaffectedByFlag(t *testing.T) {
	entry := noGuardTool("dash")
	entry.Global = true
	withTools(t, entry)
	var a wsActivity
	a.addTerm(fakeTerm{running: true, tool: "dash"})
	if a.busy() || len(a.exemptTools()) != 0 {
		t.Fatalf("a global tool must be untouched by guard = false, got %+v", a)
	}
}

// TestExemptToolsNoticeWording pins the notice (#2704) and its dedup/sort.
func TestExemptToolsNoticeWording(t *testing.T) {
	if got := exemptToolsNotice(nil); got != "" {
		t.Errorf("no exempt tools must produce no notice, got %q", got)
	}
	if got := exemptToolsNotice([]string{"sql"}); got != "closed 1 tool without asking: sql" {
		t.Errorf("singular notice = %q", got)
	}
	a := wsActivity{exempt: []string{"yarn", "sql", "yarn"}}
	if got := exemptToolsNotice(a.exemptTools()); got != "closed 2 tools without asking: sql, yarn" {
		t.Errorf("notice = %q, want the deduplicated sorted list", got)
	}
}

// TestQuitAndEvictionGuardsHonourFlag: both the quit aggregation and the
// eviction probe read collectActivity, so an exempt tool pane leaves the
// workspace idle for them too.
func TestQuitAndEvictionGuardsHonourFlag(t *testing.T) {
	withTools(t, noGuardTool("sql"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "sql"})
	tool := m.toolPane("sql")
	if tool == nil || !tool.Terminal().Running() {
		t.Fatal("setup: the tool pane must be live")
	}
	t.Cleanup(tool.Terminal().Close)

	if act := collectActivity(m.activeWS()); act.busy() {
		t.Fatalf("an exempt tool must leave the workspace idle, got %+v", act)
	}
	if workspaceBusy(m.activeWS()) {
		t.Error("the eviction guard must treat an exempt tool as idle")
	}
	if _, running := m.quitActivity(); len(running) != 0 {
		t.Errorf("the quit guard must not list an exempt tool, got %v", running)
	}
	if got := collectActivity(m.activeWS()).exemptTools(); len(got) != 1 || got[0] != "sql" {
		t.Errorf("exemptTools = %v, want [sql]", got)
	}
}

// TestGuardedToolStillBlocksQuitAndEviction is the counterpart: the same pane
// without the flag keeps both guards firing.
func TestGuardedToolStillBlocksQuitAndEviction(t *testing.T) {
	withTools(t, sleepTool("sql"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "sql"})
	tool := m.toolPane("sql")
	if tool == nil || !tool.Terminal().Running() {
		t.Fatal("setup: the tool pane must be live")
	}
	t.Cleanup(tool.Terminal().Close)

	if !workspaceBusy(m.activeWS()) {
		t.Error("a guarded tool must keep the eviction guard firing")
	}
	if _, running := m.quitActivity(); len(running) != 1 || running[0] != "tool sql" {
		t.Errorf("the quit guard must list the tool, got %v", running)
	}
}

// TestCloseProjectExemptToolsNoPrompt is the acceptance case: a project whose
// only live state is two exempt tools closes without asking and the notice
// names what was killed.
func TestCloseProjectExemptToolsNoPrompt(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[1]})
	m = dismissOnboarding(m)
	// After the switch-in: the rebuilt model reloads the config from disk, so
	// the entries are installed once the workspace the test closes is active.
	withTools(t, noGuardTool("sql"), noGuardTool("yarn"))

	for _, name := range []string{"sql", "yarn"} {
		m = step(m, ToolOpenMsg{Name: name})
		tool := m.toolPane(name)
		if tool == nil || !tool.Terminal().Running() {
			t.Fatalf("setup: the %s tool pane must be live", name)
		}
		t.Cleanup(tool.Terminal().Close)
	}

	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if m.projectClosePromptOpen() {
		t.Fatal("a project whose only activity is exempt tools must close without asking")
	}
	if !containsSubstr(notices(m), "closed 2 tools without asking: sql, yarn") {
		t.Errorf("notices = %v, want the killed tools named", notices(m))
	}
}

// TestCloseProjectGuardedToolStillAsks: the same shape with the flag absent
// keeps the prompt.
func TestCloseProjectGuardedToolStillAsks(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[1]})
	m = dismissOnboarding(m)
	withTools(t, sleepTool("sql"))

	m = step(m, ToolOpenMsg{Name: "sql"})
	tool := m.toolPane("sql")
	if tool == nil || !tool.Terminal().Running() {
		t.Fatal("setup: the tool pane must be live")
	}
	t.Cleanup(tool.Terminal().Close)

	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("a guarded tool must keep the close prompt")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = out.(Model)
}

// TestPeekReturnExemptToolNoPrompt: the peek-return guard reads the same
// inventory (#2136), so an exempt tool returns straight away and the notice
// names it.
func TestPeekReturnExemptToolNoPrompt(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	m := switchModel(t)
	m, _ = enterPeek(t, m, roots[1])
	withTools(t, noGuardTool("sql"))

	m = step(m, ToolOpenMsg{Name: "sql"})
	tool := m.toolPane("sql")
	if tool == nil || !tool.Terminal().Running() {
		t.Fatal("setup: the tool pane must be live")
	}
	t.Cleanup(tool.Terminal().Close)

	out, _ := m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	if m.peekReturnPromptOpen() {
		t.Fatal("an exempt tool must not gate the peek return")
	}
	if m.peek != nil {
		t.Error("the return must drop the peek")
	}
	if !containsSubstr(notices(m), "closed 1 tool without asking: sql") {
		t.Errorf("notices = %v, want the killed tool named", notices(m))
	}
}
