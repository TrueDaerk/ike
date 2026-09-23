package app

import (
	"strings"
	"testing"
	"time"

	"ike/internal/project"
	"ike/internal/terminal"

	tea "charm.land/bubbletea/v2"
)

// workspace_guard_busy_test.go covers the #2702 rule: the close/quit guard
// counts a terminal only while foreground work runs in it. The unit half
// drives wsActivity.addTerm through a fake session; the integration half
// takes a real shell from idle prompt to `sleep` and back through
// project.close.

// fakeTerm is a guardTerm stand-in: a session whose Running/Busy/kind and
// foreground process name are set by the test.
type fakeTerm struct {
	running bool
	busy    bool
	cmd     bool
	tool    string
	label   string
	fg      string
}

func (f fakeTerm) Running() bool          { return f.running }
func (f fakeTerm) Busy() bool             { return f.busy }
func (f fakeTerm) IsCommand() bool        { return f.cmd }
func (f fakeTerm) Tool() string           { return f.tool }
func (f fakeTerm) Label() string          { return f.label }
func (f fakeTerm) ForegroundName() string { return f.fg }

// TestGuardIgnoresIdleShell: a live shell sitting at its prompt is no
// activity — nothing is lost but scrollback, so the guard never opens.
func TestGuardIgnoresIdleShell(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: true})
	if a.busy() || len(a.running) != 0 {
		t.Fatalf("an idle shell must not gate the guard, got %+v", a)
	}
}

// TestGuardCountsBusyShellAndNamesIt: a shell with a foreground job counts
// and the prompt body names the process.
func TestGuardCountsBusyShellAndNamesIt(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: true, busy: true, fg: "vim"})
	if !a.busy() {
		t.Fatal("a busy shell must gate the guard")
	}
	if got := strings.Join(a.summary(), "\n"); got != "running shell process: vim" {
		t.Errorf("the guard names the running process, got %q", got)
	}
}

// TestGuardBusyShellWithoutNameStaysGeneric: the name lookup is best effort —
// without one the line still says something is running.
func TestGuardBusyShellWithoutNameStaysGeneric(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: true, busy: true})
	if got := strings.Join(a.summary(), "\n"); got != "running shell process" {
		t.Errorf("the fallback wording must still report the process, got %q", got)
	}
}

// TestGuardIgnoresDeadAndFinishedSessions: a closed session, and a command
// session whose process already exited (running but not busy), are gone —
// neither asks.
func TestGuardIgnoresDeadAndFinishedSessions(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: false, busy: true, fg: "vim"})
	a.addTerm(fakeTerm{running: true, cmd: true, label: "build"})       // exited
	a.addTerm(fakeTerm{running: true, tool: runToolName, label: "dev"}) // exited run
	if a.busy() {
		t.Fatalf("finished sessions must not gate the guard, got %+v", a)
	}
}

// TestGuardCountsRunningCommandSession: a command still running counts and is
// named by its configuration label, like before #2702.
func TestGuardCountsRunningCommandSession(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: true, busy: true, cmd: true, label: "npm run dev"})
	a.addTerm(fakeTerm{running: true, busy: true, tool: runToolName, label: "tests"})
	a.addTerm(fakeTerm{running: true, busy: true, cmd: true})
	want := "run npm run dev\nrun tests\nrun command"
	if got := strings.Join(a.summary(), "\n"); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

// TestGuardCountsToolPaneRegardless: a tool pane keeps its own rule — its
// exit closes the pane, so a live one is always work the close would kill.
func TestGuardCountsToolPaneRegardless(t *testing.T) {
	var a wsActivity
	a.addTerm(fakeTerm{running: true, tool: "lazygit"})
	if !a.busy() || strings.Join(a.summary(), "") != "tool lazygit" {
		t.Errorf("a live tool pane must gate the guard, got %+v", a)
	}
}

// waitBusy blocks until term has a foreground job.
func waitBusy(t *testing.T, term *terminal.Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if term.Busy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the foreground job never took the shell")
}

// TestCloseProjectIdleShellNoPrompt: the #2702 headline — a project whose
// only live state is an idle shell closes straight away, and the same shell
// with `sleep` running raises the guard naming the process.
func TestCloseProjectIdleShellNoPrompt(t *testing.T) {
	roots := peekFixture(t, "origin", "api")
	switchAutoSaveOff(t)
	m := dismissOnboarding(switchModel(t))
	m, _ = driveGroupMsg(t, m, project.SwitchProjectMsg{Root: roots[1]})
	m = dismissOnboarding(m)

	out, _ := m.Update(TerminalNewMsg{})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || !inst.Terminal().Running() {
		t.Fatal("setup: terminal.new must open a running shell")
	}
	term := inst.Terminal()
	waitIdle(t, term)
	if act := collectActivity(m.activeWS()); act.busy() {
		t.Fatalf("an idle shell must not make the workspace busy, got %+v", act)
	}
	if workspaceBusy(m.activeWS()) {
		t.Error("the eviction guard must agree: an idle shell is not busy")
	}

	term.SendLine("sleep 30")
	waitBusy(t, term)
	act := collectActivity(m.activeWS())
	if !act.busy() {
		t.Fatal("a shell running sleep must make the workspace busy")
	}
	line := strings.Join(act.summary(), "\n")
	if !strings.HasPrefix(line, "running shell process") {
		t.Errorf("the guard must name the running shell process, got %q", line)
	}
	if !workspaceBusy(m.activeWS()) {
		t.Error("the eviction guard must agree: a busy shell is busy")
	}

	out, _ = m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("closing a project with a running process must prompt")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)

	term.Close()
	waitDead(t, term)
	out, _ = m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if m.projectClosePromptOpen() {
		t.Fatal("with nothing running the close must not prompt")
	}
}

// waitDead blocks until term's session is gone.
func waitDead(t *testing.T, term *terminal.Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !term.Running() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the shell never ended")
}
