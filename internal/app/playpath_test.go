package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/docpath"
	"ike/internal/jqplay"
	"ike/internal/lang"
)

// playpath_test.go covers the playground's append-path command (#2783): the
// result cursor's path computed from the result buffer, appended to the
// program as a stage (as written and generalised), the yq spelling, and the
// xmq no-op.

// putResultCursor moves the result buffer's cursor onto the first occurrence
// of needle, one rune in, so it stands inside that token.
func putResultCursor(t *testing.T, m Model, needle string) {
	t.Helper()
	for i, line := range strings.Split(m.play.result.Text(), "\n") {
		if col := strings.Index(line, needle); col >= 0 {
			m.play.resultEd.SetCursor(i, len([]rune(line[:col]))+1)
			return
		}
	}
	t.Fatalf("%q is not in the result:\n%s", needle, m.play.result.Text())
}

// TestPlayAppendPathFromResultCursor is the acceptance case: standing on a
// nested key in the jq result, the command appends its path as a stage, the
// query line gets the focus with the caret at the end, and the run shows the
// value.
func TestPlayAppendPathFromResultCursor(t *testing.T) {
	m := openJQ(t, playApp(t, `{"items":[{"name":"a"},{"name":"b"},{"name":"c"},{"name":"d","tags":["x"]}]}`))
	m = setProgram(m, ".items")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	putResultCursor(t, m, `"name": "d"`)

	tm, cmd := m.Update(AppendPlayPathMsg{})
	m = drainCmd(tm.(Model), cmd)

	s := m.play
	if got, want := s.program.Text, ".items | .[3].name"; got != want {
		t.Fatalf("program = %q, want %q (status %q, result %q)", got, want, m.play.status, m.play.result.Text())
	}
	if s.bufFocus {
		t.Error("the query line must take the focus back")
	}
	if s.program.Cur != s.program.Len() {
		t.Errorf("caret at %d, want the end (%d)", s.program.Cur, s.program.Len())
	}
	if got := s.result.Text(); got != `"d"` {
		t.Errorf("result = %q, want the appended path's value", got)
	}
}

// TestPlayAppendPathGeneralised: the Any flavour writes every index as `[]`,
// so the stage reaches the same key in all items.
func TestPlayAppendPathGeneralised(t *testing.T) {
	m := openJQ(t, playApp(t, `{"items":[{"name":"a"},{"name":"b"}]}`))
	m = setProgram(m, ".items")
	putResultCursor(t, m, `"name": "b"`)

	tm, cmd := m.Update(AppendPlayPathMsg{Any: true})
	m = drainCmd(tm.(Model), cmd)

	if got, want := m.play.program.Text, ".items | .[].name"; got != want {
		t.Fatalf("program = %q, want %q (status %q, result %q)", got, want, m.play.status, m.play.result.Text())
	}
	if got := m.play.result.Text(); got != "\"a\"\n\"b\"" {
		t.Errorf("result = %q, want every item's name", got)
	}
}

// TestPlayAppendPathChord: the default ctrl+. reaches the command from the
// result buffer through the playground's Global fallback.
func TestPlayAppendPathChord(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":{"b":1}}`))
	// The first-start LSP onboarding dialog swallows scripted keys; a no-op
	// once an earlier test dismissed it.
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m = setProgram(m, ".")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	putResultCursor(t, m, `"b"`)
	m = drainKey(m, tea.KeyPressMsg{Code: '.', Mod: tea.ModCtrl})
	if got := m.play.program.Text; got != ".a.b" {
		t.Fatalf("program = %q, want the identity replaced by the path (status %q, onboarding %v)", got, m.play.status, m.onboardingOpen())
	}
	if m.play.bufFocus {
		t.Error("the query line must take the focus back")
	}
}

// TestPlayAppendPathAtRoot: nothing to append at the document root — the
// program is left alone and the info row says why.
func TestPlayAppendPathAtRoot(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	m.play.resultEd.SetCursor(0, 0)
	tm, _ := m.Update(AppendPlayPathMsg{})
	m = tm.(Model)
	if m.play.program.Text != "." {
		t.Fatalf("program = %q, want it untouched", m.play.program.Text)
	}
	if !m.play.statusWarn || !strings.Contains(m.play.status, "root") {
		t.Errorf("status = %q (warn %v), want the root notice", m.play.status, m.play.statusWarn)
	}
}

// TestPlayAppendPathYQ: a yq result is YAML and the path takes yq's spelling
// for a key needing quotes.
func TestPlayAppendPathYQ(t *testing.T) {
	// The path scan reads the result buffer's language id; internal/app does
	// not pull in the shipped language plugins, so a bare entry stands in.
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yaml", "yml"}})
	m := openYQ(t, yqApp(t, "spec:\n  containers:\n    - name: web\n      my-port: 80\n"))
	m = setProgram(m, ".spec")
	putResultCursor(t, m, "my-port")

	tm, cmd := m.Update(AppendPlayPathMsg{})
	m = drainCmd(tm.(Model), cmd)

	if got, want := m.play.program.Text, `.spec | .containers[0]."my-port"`; got != want {
		t.Fatalf("program = %q, want %q (status %q, result %q)", got, want, m.play.status, m.play.result.Text())
	}
	if got := strings.TrimSpace(m.play.result.Text()); got != "80" {
		t.Errorf("result = %q, want the appended path's value", got)
	}
}

// TestPlayAppendPathXMQ: xmq's own notation has no JSON path — a no-op with a
// notice — and even a to-json result is only named, never appended to a
// command line that is not a jq pipeline.
func TestPlayAppendPathXMQ(t *testing.T) {
	fakeXMQOnPath(t)
	m := openXMQ(t, xmqApp(t, "xml", "<r/>\n"))
	m = setProgram(m, "select //a")
	tm, _ := m.Update(AppendPlayPathMsg{})
	m = tm.(Model)
	if m.play.program.Text != "select //a" {
		t.Fatalf("program = %q, want it untouched", m.play.program.Text)
	}
	if !m.play.statusWarn || !strings.Contains(m.play.status, "to-json") {
		t.Errorf("status = %q, want the to-json notice", m.play.status)
	}

	m = setProgram(m, "to-json")
	steps := []docpath.Step{{Key: "r"}, {Key: "a"}}
	path, reason := playResultPath(jqplay.DialectXMQ, m.play.result, true, steps, false)
	if path != "" || !strings.Contains(reason, ".r.a") {
		t.Errorf("to-json: path %q reason %q, want the path named and nothing appended", path, reason)
	}
}

// TestPlayAppendStage: the identity (and the blank program) is replaced, a
// trailing comment keeps the stage off its line.
func TestPlayAppendStage(t *testing.T) {
	for _, c := range []struct{ program, want string }{
		{"", ".a"},
		{" . ", ".a"},
		{".items", ".items | .a"},
		{".items # all", ".items # all\n| .a"},
	} {
		if got := playAppendStage(c.program, ".a"); got != c.want {
			t.Errorf("playAppendStage(%q) = %q, want %q", c.program, got, c.want)
		}
	}
}
