package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/palette"
)

// playvars_test.go covers the playground's variables line (#2786): binding
// `$name` into the run, the string/JSON reading and the parse error on the
// info row, the show / focus / hide toggle and its default chord, the header
// geometry and mouse mapping while the row is up, and the variables riding
// along with the saved filters and the per-source last program.

// toggleVars dispatches playground.variables and drains its rerun.
func toggleVars(m Model) Model {
	tm, cmd := m.Update(TogglePlayVarsMsg{})
	return drainCmd(tm.(Model), cmd)
}

// varsPlay opens the jq playground over two records and the issue's
// program, which cannot compile until `$id` is bound.
func varsPlay(t *testing.T) Model {
	t.Helper()
	m := openJQ(t, dismissOnboarding(playApp(t, `[{"id":41,"n":"a"},{"id":42,"n":"b"}]`)))
	return setProgram(m, ".[] | select(.id == $id) | .n")
}

// TestPlayVarsBindSelectByID is the issue's acceptance case: without the
// variable gojq's compile error takes the info row; `id=42` on the variables
// line makes the program run and select the record.
func TestPlayVarsBindSelectByID(t *testing.T) {
	m := varsPlay(t)
	if !strings.Contains(m.play.runErr, "variable not defined: $id") {
		t.Fatalf("runErr = %q, want gojq's compile error", m.play.runErr)
	}
	m = toggleVars(m)
	if !m.play.varsShown || !m.play.varsFocus || m.play.bufFocus {
		t.Fatalf("the toggle must show and focus the line: shown=%v focus=%v", m.play.varsShown, m.play.varsFocus)
	}
	m = typeInto(m, "id=42")
	if m.play.runErr != "" || m.play.result.Text() != `"b"` {
		t.Fatalf("result = %q err %q, want the selected record's name", m.play.result.Text(), m.play.runErr)
	}
	if m.play.program.Text != ".[] | select(.id == $id) | .n" {
		t.Errorf("typing on the variables line must not reach the program, got %q", m.play.program.Text)
	}
	// A string: `--arg`'s fallback, quoted or bare alike.
	m = setProgram(m, "$who")
	m.play.vars.Set("who=alice")
	m = drainCmd(m, m.runPlayNow())
	if m.play.result.Text() != `"alice"` {
		t.Errorf("bare word = %q, want a string", m.play.result.Text())
	}
	m.play.vars.Set(`who=["a", "b"]`)
	m = drainCmd(m, m.runPlayNow())
	if got := strings.Join(strings.Fields(m.play.result.Text()), ""); got != `["a","b"]` {
		t.Errorf("JSON value = %q, want the array", got)
	}
}

// TestPlayVarsParseErrorOnInfoRow: a line that does not parse is reported
// on the info row, and fixing it runs again.
func TestPlayVarsParseErrorOnInfoRow(t *testing.T) {
	m := varsPlay(t)
	m = toggleVars(m)
	m = typeInto(m, "id")
	if row := playInfoRowPlain(m); !strings.Contains(row, `E: variables: "id" is not name=value`) {
		t.Fatalf("info row = %q, want the variables error", row)
	}
	m = typeInto(m, "=41")
	if m.play.runErr != "" || m.play.result.Text() != `"a"` {
		t.Fatalf("after the fix: result %q err %q", m.play.result.Text(), m.play.runErr)
	}
}

// TestPlayVarsToggleCycle: show+focus, esc back to the query line with the
// line still up and binding, focus again, hide — and a hidden line binds
// nothing, so the program fails to compile again.
func TestPlayVarsToggleCycle(t *testing.T) {
	m := varsPlay(t)
	m = toggleVars(m)
	m = typeInto(m, "id=41")
	m = pressKey(m, tea.KeyEscape)
	if !m.playOpen() || m.play.varsFocus || !m.play.varsShown {
		t.Fatalf("esc must return to the query line, not close: open=%v focus=%v shown=%v", m.playOpen(), m.play.varsFocus, m.play.varsShown)
	}
	if m.play.result.Text() != `"a"` {
		t.Fatalf("the shown line still binds, result = %q", m.play.result.Text())
	}
	m = toggleVars(m)
	if !m.play.varsFocus {
		t.Fatal("the toggle over a shown, unfocused line must focus it")
	}
	m = toggleVars(m)
	if m.play.varsShown || m.play.varsFocus {
		t.Fatal("the toggle from the focused line must hide it")
	}
	if !strings.Contains(m.play.runErr, "variable not defined: $id") {
		t.Errorf("a hidden line must not bind, runErr = %q", m.play.runErr)
	}
	m = toggleVars(m)
	if m.play.vars.Text != "id=41" || m.play.runErr != "" {
		t.Errorf("showing again restores the text and its binding: %q err %q", m.play.vars.Text, m.play.runErr)
	}
}

// TestPlayVarsDefaultChord: ctrl+alt+b reaches playground.variables from the
// query line and, as the line's own unclaimed key, hides it again.
func TestPlayVarsDefaultChord(t *testing.T) {
	m := varsPlay(t)
	chord := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl | tea.ModAlt}
	m = drainKey(m, chord)
	if !m.play.varsShown || !m.play.varsFocus {
		t.Fatalf("ctrl+alt+b must show the variables line (status %q)", m.play.status)
	}
	m = drainKey(m, chord)
	if m.play.varsShown {
		t.Fatal("ctrl+alt+b from the line must hide it")
	}
	if !strings.Contains(m.play.runErr, "variable not defined: $id") {
		t.Errorf("the hidden line must stop binding, runErr = %q", m.play.runErr)
	}
}

// TestPlayVarsHeaderGeometry: the header grows by exactly the variables row
// while it is shown — the reserved rows, the rendered body and the result
// buffer's height agree, with and without the stale banner.
func TestPlayVarsHeaderGeometry(t *testing.T) {
	m := openJQ(t, dismissOnboarding(playApp(t, `{"items":[`+strings.Repeat(`{"n":1},`, 40)+`{"n":2}]}`)))
	m = setProgram(m, ".items[]")
	key := m.play.paneKey
	r := m.lay.Panes[key]
	width := paneInterior(r.W, paneChromeW)
	before, height := m.playHeaderRowsFor(key), playResultHeight(m)
	m = toggleVars(m)
	if got := m.playHeaderRowsFor(key); got != before+1 {
		t.Fatalf("header = %d rows with the line up, want %d", got, before+1)
	}
	if got := playResultHeight(m); got != height-1 {
		t.Errorf("result = %d rows, want %d", got, height-1)
	}
	check := func(what string) {
		t.Helper()
		if got, want := len(strings.Split(m.playInlineBody(width), "\n")), paneInterior(r.H, paneChromeH); got != want {
			t.Errorf("%s: the body renders %d rows, want %d", what, got, want)
		}
		rows := strings.Split(ansi.Strip(m.playInlineBody(width)), "\n")
		if !strings.HasPrefix(rows[m.playQueryRowCount()], "> $:") {
			t.Errorf("%s: the variables row is %q", what, rows[m.playQueryRowCount()])
		}
	}
	check("resting")
	m = typeInto(m, "x") // a parse error: the stale banner joins the header
	if !m.play.playStale() {
		t.Fatal("a bad line must mark the result stale")
	}
	check("stale")
	m = toggleVars(m)
	if got := m.playHeaderRowsFor(key); got != before {
		t.Errorf("hidden: header = %d rows, want %d", got, before)
	}
}

// TestPlayVarsMouse: a click on the variables row focuses it and places its
// caret; a click on the query row above still reaches the query line, and
// the chips on the info row below still toggle.
func TestPlayVarsMouse(t *testing.T) {
	m := varsPlay(t)
	m = toggleVars(m)
	m = typeInto(m, "id=42")
	m = pressKey(m, tea.KeyEscape)
	r := m.lay.Panes[m.play.paneKey]
	x0, y0 := r.X+paneContentX, r.Y+paneContentY
	click := func(x, y int) {
		tm, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m = drainCmd(tm.(Model), cmd)
	}
	click(x0+m.playPrefixW()+2, y0+1)
	if !m.play.varsFocus || m.play.vars.Cur != 2 {
		t.Fatalf("a click on the variables row: focus=%v cur=%d, want focus at 2", m.play.varsFocus, m.play.vars.Cur)
	}
	click(x0+m.playPrefixW()+3, y0)
	if m.play.varsFocus || m.play.bufFocus || m.play.program.Cur != 3 {
		t.Fatalf("a click on the query row: varsFocus=%v cur=%d, want the query caret at 3", m.play.varsFocus, m.play.program.Cur)
	}
	click(x0, y0+2) // the info row leads with the -r chip
	if !m.play.opts.Raw || m.play.result.Text() != "b" {
		t.Errorf("the chip under the variables row must still toggle -r: opts %+v result %q", m.play.opts, m.play.result.Text())
	}
}

// TestPlayVarsSavedFilter: a filter saved with the line up stores the
// variables, the picker marks it with `$`, and picking it restores the line.
func TestPlayVarsSavedFilter(t *testing.T) {
	m := varsPlay(t)
	m = toggleVars(m)
	m = typeInto(m, "id=42")
	m = pressKey(m, tea.KeyEscape)
	m = saveFilter(t, m, "by id", false)
	m = toggleVars(m) // focus the line
	m = toggleVars(m) // hide it: the next save carries no variables
	m = setProgram(m, ".[0]")
	m = saveFilter(t, m, "first", false)
	f, _ := loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject).Get("by id")
	if f.Vars != "id=42" {
		t.Fatalf("saved vars = %q", f.Vars)
	}

	mode := m.playFilters
	mode.dialect = jqplay.DialectJQ
	mode.Refresh()
	marks := map[string]string{}
	for _, it := range mode.Results("", palette.Context{}) {
		marks[it.Title] = it.Hint
	}
	if !strings.HasSuffix(marks["by id"], " $") || strings.Contains(marks["first"], "$") {
		t.Errorf("picker hints = %q, want `$` on the filter with variables only", marks)
	}

	m.play.vars.Set("")
	tm, cmd := m.Update(InsertFilterMsg{Dialect: jqplay.DialectJQ, Scope: jqplay.ScopeProject, Name: "by id"})
	m = drainCmd(asModel(tm), cmd)
	if !m.play.varsShown || m.play.vars.Text != "id=42" || m.play.result.Text() != `"b"` {
		t.Fatalf("picked filter: shown=%v vars=%q result=%q err=%q", m.play.varsShown, m.play.vars.Text, m.play.result.Text(), m.play.runErr)
	}
}

// TestPlayVarsRememberedWithLastProgram: reopening over the same file brings
// back the program with its variables line shown — from the session and from
// the persisted store.
func TestPlayVarsRememberedWithLastProgram(t *testing.T) {
	m := varsPlay(t)
	m = toggleVars(m)
	m = typeInto(m, "id=41")
	m.closePlayground()
	m = openJQ(t, m)
	if !m.play.varsShown || m.play.vars.Text != "id=41" || m.play.result.Text() != `"a"` {
		t.Fatalf("reopen: shown=%v vars=%q result=%q", m.play.varsShown, m.play.vars.Text, m.play.result.Text())
	}
	if m.play.opts.Vars != "" {
		t.Errorf("opts must carry the toggles only, got vars %q", m.play.opts.Vars)
	}
	key := m.play.srcKey
	if got := m.playLastStoreOf().Options(key).Vars; got != "id=41" {
		t.Errorf("persisted vars = %q", got)
	}
	m.closePlayground()
	delete(m.playLastProgram, key)
	delete(m.playLastOpts, key)
	m = openJQ(t, m)
	if m.play.vars.Text != "id=41" || m.play.result.Text() != `"a"` {
		t.Errorf("restart: vars=%q result=%q", m.play.vars.Text, m.play.result.Text())
	}
}

// TestPlayVarsYQ: the yq playground binds the line the same way.
func TestPlayVarsYQ(t *testing.T) {
	m := openYQ(t, dismissOnboarding(yqApp(t, "items:\n  - {name: a, tag: x}\n  - {name: b, tag: y}\n")))
	m = setProgram(m, ".items[] | select(.tag == $t) | .name")
	m = toggleVars(m)
	m = typeInto(m, "t=y")
	if m.play.runErr != "" || strings.TrimSpace(m.play.result.Text()) != "b" {
		t.Fatalf("yq result = %q err %q", m.play.result.Text(), m.play.runErr)
	}
}
