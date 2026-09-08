package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httppane"
	"ike/internal/jqplay"
	"ike/internal/pane"
	"ike/internal/project"
)

// playground_switch_test.go covers the playground across a project switch
// (#2535): the mode is bound to its document (#2355), the document survives
// the switch parked in its workspace, so the mode parks and resumes with it.

// playSwitchModel is a switch-capable model (two projects, cwd in a) with a
// JSON file of project a open and the jq playground running program over it.
func playSwitchModel(t *testing.T, body, program string) (m Model, b string) {
	t.Helper()
	noDebounce(t)
	a, b := twoProjects(t)
	m = dismissOnboarding(switchModel(t))
	path := filepath.Join(a, "data.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	m = drainCmd(tm.(Model), cmd)
	m = openJQ(t, m)
	m = setProgram(m, program)
	return m, b
}

// switchTo performs a full switch to root and feeds back the playground's
// own messages from the switch's command batch. The batch also holds timers
// that never fire in a test (the LSP quiet wait, the idle shutdown), so it
// cannot be drained whole: every leaf runs on its own goroutine and only a
// playground message that arrives promptly is delivered.
func switchTo(t *testing.T, m Model, root string) Model {
	t.Helper()
	out, cmd := m.Update(project.SwitchProjectMsg{Root: root})
	m = out.(Model)
	if !sameDir(t, cwd(t), root) {
		t.Fatalf("switch landed in %s, want %s", cwd(t), root)
	}
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		got := make(chan tea.Msg, 1)
		go func() { got <- c() }()
		var msg tea.Msg
		select {
		case msg = <-got:
		case <-time.After(500 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case playParseDoneMsg, playEvalDoneMsg, playDebounceMsg:
			var next tea.Cmd
			out, next = m.Update(msg)
			m = out.(Model)
			pending = append(pending, next)
		}
	}
	return m
}

// TestPlaygroundSurvivesProjectSwitch is the acceptance case: away and back
// keeps the query, the result, the history position and the multi-line
// state; the other project shows no playground meanwhile.
func TestPlaygroundSurvivesProjectSwitch(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[1]")
	a := cwd(t)
	m = runJQProgram(m, ".foo[1]")
	m = runJQProgram(m, ".foo[2]")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp}) // browsing: histIdx > -1
	if m.play.histIdx < 0 || m.play.program.Text != ".foo[1]" {
		t.Fatalf("setup: ↑ must be browsing the history, got idx %d program %q", m.play.histIdx, m.play.program.Text)
	}
	m.play.expanded = true
	key := m.play.paneKey
	wantResult := m.play.result.Text()
	wantIdx := m.play.histIdx

	m = switchTo(t, m, b)
	if m.playOpen() {
		t.Fatal("project b must not show project a's playground")
	}
	parked := m.ws.Peek(a)
	if parked == nil {
		t.Fatal("setup: a must be parked")
	}
	extras, _ := parked.Aux.(wsExtras)
	if extras.play == nil {
		t.Fatal("the playground must park with its workspace")
	}

	m = switchTo(t, m, a)
	s := m.play
	if s == nil {
		t.Fatal("switching back must bring the playground back")
	}
	if s.program.Text != ".foo[1]" {
		t.Errorf("program = %q, want the query it was left with", s.program.Text)
	}
	if got := s.result.Text(); got != wantResult {
		t.Errorf("result = %q, want %q", got, wantResult)
	}
	if s.histIdx != wantIdx {
		t.Errorf("history position = %d, want %d", s.histIdx, wantIdx)
	}
	if !s.expanded {
		t.Error("the multi-line state must survive")
	}
	if !m.playInlineActive(key) {
		t.Error("the resumed playground must render over its document")
	}
	if s.hist != m.playHistory {
		t.Error("the resumed state must point at the model's session history")
	}
	v := m.render()
	if !strings.Contains(v, "jq:") || !strings.Contains(v, "3") {
		t.Errorf("the resumed playground does not render, got:\n%s", v)
	}
}

// TestPlaygroundSwitchKeepsSessionHistory guards the pointer identity: the
// program history is session state and rides across the switch, so a
// playground opened in project b offers project a's programs.
func TestPlaygroundSwitchKeepsSessionHistory(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":1}`, ".foo")
	m = runJQProgram(m, ".foo")
	hist := m.playHistory
	m = switchTo(t, m, b)
	if m.playHistory != hist {
		t.Error("the program history must ride across the switch")
	}
	if got := m.playHistory.Len(); got == 0 {
		t.Error("the history lost its programs")
	}
}

// TestPlaygroundResumesPendingRun: a query typed right before the switch
// still has its result when the project comes back.
func TestPlaygroundResumesPendingRun(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[0]")
	a := cwd(t)
	m.play.program.Set(".foo[2]")
	_ = m.schedulePlayEval() // the tick is never delivered: the switch happens first
	if !m.play.pending {
		t.Fatal("setup: a run must be pending")
	}
	m = switchTo(t, m, b)
	m = switchTo(t, m, a)
	if m.play == nil {
		t.Fatal("the playground must come back")
	}
	if m.play.pending {
		t.Error("the pending run must have been re-driven")
	}
	if got := m.play.result.Text(); !strings.Contains(got, "3") {
		t.Errorf("result = %q, want the pending program's output", got)
	}
}

// TestPlaygroundResumeRereadsChangedFile: a followed file edited while the
// workspace was parked is re-read on resume (#2356 semantics), since no
// watcher event ever reached the playground.
func TestPlaygroundResumeRereadsChangedFile(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[0]")
	a := cwd(t)
	path := m.play.srcPath
	if path == "" {
		t.Fatal("setup: a whole-file source is followed")
	}
	m = switchTo(t, m, b)
	if err := os.WriteFile(path, []byte(`{"foo":[42]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m = switchTo(t, m, a)
	if m.play == nil {
		t.Fatal("the playground must come back")
	}
	if got := m.play.result.Text(); !strings.Contains(got, "42") {
		t.Errorf("result = %q, want the reloaded file's value", got)
	}
}

// TestPlaygroundClosesWithDocumentAfterResume: the "closes when its document
// leaves" rule (#2355) still applies to a resumed playground.
func TestPlaygroundClosesWithDocumentAfterResume(t *testing.T) {
	m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[0]")
	a := cwd(t)
	m = switchTo(t, m, b)
	m = switchTo(t, m, a)
	if m.play == nil {
		t.Fatal("the playground must come back")
	}
	key := m.play.paneKey
	srcTab := m.activeWS().Panes.Get(key).ActiveTab()
	m = openOther(t, m, `{"marker":"other-file"}`)
	m.closeTab(m.activeWS().Panes.Get(key), srcTab)
	out, _ := m.Update(nil)
	m = out.(Model)
	if m.playOpen() {
		t.Error("closing the queried document must still close the playground")
	}
}

// TestHTTPPlaygroundSurvivesProjectSwitch: a playground over a response pane
// behaves the same as long as the response pane survives the switch.
func TestHTTPPlaygroundSurvivesProjectSwitch(t *testing.T) {
	noDebounce(t)
	a, b := twoProjects(t)
	m := switchModel(t)
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.setFocus(pane.HTTPKey)
	m.layout()
	out, cmd := m.Update(httppane.JQPlaygroundMsg{})
	m = drainCmd(out.(Model), cmd)
	if !m.playOpen() || m.play.dialect != jqplay.DialectJQ || m.play.srcInst == nil {
		t.Fatal("setup: the jq playground must be open over the response")
	}
	m = setProgram(m, ".ok")
	want := m.play.result.Text()

	m = switchTo(t, m, b)
	if m.playOpen() {
		t.Fatal("project b must not show project a's playground")
	}
	m = switchTo(t, m, a)
	if m.play == nil {
		t.Fatal("the response playground must come back")
	}
	if m.play.program.Text != ".ok" || m.play.result.Text() != want {
		t.Errorf("program/result = %q/%q, want .ok/%q", m.play.program.Text, m.play.result.Text(), want)
	}
	if !m.playInlineActive(pane.HTTPKey) {
		t.Error("the resumed playground must render over the response pane")
	}
}
