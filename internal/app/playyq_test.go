package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/jqplay"
	"ike/internal/lang"
	"ike/internal/scratch"
)

// playyq_test.go covers the yq playground (#2039) on the app side: that the
// shared mode really does open over a YAML buffer, render YAML, write a
// `.yaml` scratch and keep its saved filters apart from jq's. The evaluation
// itself is internal/jqplay's; what is tested here is the wiring — the one
// thing a shared implementation can still get wrong per dialect.

// yqApp opens body as a .yaml file in the focused editor: the "open YAML
// buffer" the yq playground is written for.
func yqApp(t *testing.T, body string) Model {
	t.Helper()
	noDebounce(t)
	m := newSized()
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// openYQ opens the yq playground through the real command message.
func openYQ(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(OpenPlaygroundMsg{Dialect: jqplay.DialectYQ})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("yaml.yqPlayground must open the playground")
	}
	if m.play.dialect != jqplay.DialectYQ {
		t.Fatalf("the open playground is %v, want the yq dialect", m.play.dialect)
	}
	return m
}

// TestYQPlaygroundEvaluatesLive is the issue's acceptance case: a query
// against the YAML buffer at hand, run live, rendered back as YAML.
func TestYQPlaygroundEvaluatesLive(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n  containers:\n    - name: api\n      image: alpine\n    - name: web\n      image: nginx\n"))
	m = setProgram(m, ".spec.containers[] | .name")

	s := m.play
	if s.result.Err != "" {
		t.Fatalf("valid program reported %q", s.result.Err)
	}
	if got := s.result.Text(); got != "api\n---\nweb" {
		t.Fatalf("result = %q, want the two names as a YAML stream", got)
	}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "2 value(s)") {
		t.Errorf("the header should summarise the value count, got:\n%s", v)
	}
	if !strings.Contains(v, "deploy.yaml") {
		t.Errorf("the input line should name the queried buffer, got:\n%s", v)
	}
}

// TestYQPlaygroundRendersYAML: the result buffer holds YAML, not the JSON the
// engine works in — the whole point of the second dialect.
func TestYQPlaygroundRendersYAML(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n  image: alpine\n"))
	m = setProgram(m, ".spec")
	if got := m.play.result.Text(); got != "image: alpine\nreplicas: 3" {
		t.Fatalf("result = %q, want block YAML", got)
	}
	if strings.Contains(m.play.result.Text(), "{") {
		t.Error("a YAML result must not be rendered as JSON")
	}
}

// TestYQPlaygroundLabelsItself: the query line and the pane title say yq, so
// the two playgrounds are never confused for one another on screen.
func TestYQPlaygroundLabelsItself(t *testing.T) {
	m := openYQ(t, yqApp(t, "a: 1\n"))
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "> yq: ") {
		t.Errorf("the query line should carry the yq label, got:\n%s", v)
	}
	if !strings.Contains(v, "YQ — ") {
		t.Errorf("the pane title should name the mode, got:\n%s", v)
	}
	if strings.Contains(v, "> jq: ") {
		t.Errorf("the jq label must not appear in a yq playground, got:\n%s", v)
	}
}

// TestYQPlaygroundResultFolds: the result window folds YAML blocks with the
// member-counting placeholder (#2029), which the indentation scan has to
// supply where JSON's delimiters do not exist.
func TestYQPlaygroundResultFolds(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  meta:\n    a: 1\n    b: 2\n"))
	m = setProgram(m, ".")
	s := m.play
	if len(s.folds) == 0 {
		t.Fatalf("no folds installed for %q", s.result.Text())
	}
	f, ok := s.folds[1] // the `meta:` row; row 0 is the `spec:` block around it
	if !ok {
		t.Fatalf("folds = %+v, want one on the `meta:` header row", s.folds)
	}
	if got := f.Label(); got != "⋯ 2 keys" {
		t.Errorf("placeholder = %q, want the member count without a JSON closer", got)
	}
}

// TestYQPlaygroundCopiesResult: ctrl+y puts the whole YAML result on the
// clipboard, the way it does for jq.
func TestYQPlaygroundCopiesResult(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := openYQ(t, yqApp(t, "list:\n  - a\n  - b\n"))
	m = setProgram(m, ".list")
	m = drainKey(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if copied != "- a\n- b" {
		t.Fatalf("clipboard = %q, want the YAML result", copied)
	}
}

// TestYQPlaygroundOpensResultAsYAMLScratch: ctrl+o writes the result into a
// fresh `.yaml` scratch — the extension is what makes the scratch highlight
// and re-query as YAML.
func TestYQPlaygroundOpensResultAsYAMLScratch(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n"))
	m = setProgram(m, ".spec")
	m = drainKey(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})

	if m.playOpen() {
		t.Error("opening the result as a scratch should close the playground")
	}
	ed := m.activeEditor()
	if ed == nil || filepath.Ext(ed.Path()) != ".yaml" {
		t.Fatalf("the scratch must land in an editor as .yaml, got %v", ed)
	}
	if dir, _ := scratch.Dir(); !strings.HasPrefix(ed.Path(), dir) {
		t.Errorf("the result must open as a scratch, got %q", ed.Path())
	}
	body, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(body)); got != "replicas: 3" {
		t.Errorf("scratch content = %q, want the YAML result", got)
	}
}

// TestYQPlaygroundBrokenInputNamesYAML: a document the decoder rejects is a
// sentence on the info row naming YAML — never a crash, and never the jq
// wording.
func TestYQPlaygroundBrokenInputNamesYAML(t *testing.T) {
	m := openYQ(t, yqApp(t, "a: 1\n\tb: 2\n"))
	if m.play.inputErr == "" {
		t.Fatal("a tab-indented document must report an input error")
	}
	if !strings.Contains(m.play.inputErr, "YAML") || strings.Contains(m.play.inputErr, "JSON") {
		t.Errorf("input error = %q, want it to name YAML", m.play.inputErr)
	}
	if !strings.Contains(ansi.Strip(m.render()), "E: ") {
		t.Error("the info row should show the input error")
	}
}

// TestYQPlaygroundHistoryIsShared: the session program history is one list for
// both dialects (#2039) — a yq program *is* a jq program here, and the list is
// already deliberately shared across buffers.
func TestYQPlaygroundHistoryIsShared(t *testing.T) {
	m := openYQ(t, yqApp(t, "a: 1\n"))
	m = setProgram(m, ".a")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.closePlayground()

	m = openYQ(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.play.program.Text != ".a" {
		t.Fatalf("↑ restored %q, want the recorded program", m.play.program.Text)
	}
	if m.playHist() != m.play.hist {
		t.Error("the playground must share the model's one history list")
	}
}

// TestYQFiltersAreASeparateLibrary is the acceptance criterion that yq filters
// land apart from jq's: different files, and neither picker lists the other's
// entries.
func TestYQFiltersAreASeparateLibrary(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n"))
	m = setProgram(m, ".spec.replicas")
	m = saveFilter(t, m, "replica count", false)

	if f, ok := loadPlayFilters(jqplay.DialectYQ, jqplay.ScopeProject).Get("replica count"); !ok || f.Program != ".spec.replicas" {
		t.Fatalf("the yq project store holds %+v (ok=%v)", f, ok)
	}
	if loadPlayFilters(jqplay.DialectJQ, jqplay.ScopeProject).Has("replica count") {
		t.Error("a yq filter must not land in the jq library")
	}
	for _, scope := range jqplay.Scopes() {
		if playFilterFile(jqplay.DialectJQ, scope) == playFilterFile(jqplay.DialectYQ, scope) {
			t.Fatalf("the two dialects share the %s store file", scope)
		}
	}
	if got := filepath.Base(playFilterFile(jqplay.DialectYQ, jqplay.ScopeProject)); got != "yqfilters.json" {
		t.Errorf("the yq project store is %q", got)
	}
}

// TestYQFilterPickerFollowsTheOpenPlayground: ctrl+l in a yq query line lists
// the yq library, because the program it inserts has to run against the
// document the playground is over.
func TestYQFilterPickerFollowsTheOpenPlayground(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n"))
	m = setProgram(m, ".spec.replicas")
	m = saveFilter(t, m, "replica count", false)

	m = drainKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if m.playFilters.dialect != jqplay.DialectYQ {
		t.Fatalf("the picker opened over %v, want the open playground's dialect", m.playFilters.dialect)
	}
	if len(m.playFilters.entries) != 1 || m.playFilters.entries[0].Name != "replica count" {
		t.Fatalf("the picker lists %+v, want the yq filter", m.playFilters.entries)
	}
}

// TestYQFilterInsertOpensItsOwnPlayground: a yq filter picked with nothing
// open starts the yq playground, not the jq one — its program is written for
// YAML documents.
func TestYQFilterInsertOpensItsOwnPlayground(t *testing.T) {
	m := openYQ(t, yqApp(t, "spec:\n  replicas: 3\n"))
	m = setProgram(m, ".spec.replicas")
	m = saveFilter(t, m, "replica count", false)
	m.closePlayground()

	tm, cmd := m.Update(InsertFilterMsg{Dialect: jqplay.DialectYQ, Scope: jqplay.ScopeProject, Name: "replica count"})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() || m.play.dialect != jqplay.DialectYQ {
		t.Fatal("inserting a yq filter must open the yq playground")
	}
	if m.play.program.Text != ".spec.replicas" {
		t.Fatalf("query line = %q, want the filter's program", m.play.program.Text)
	}
	if got := m.play.result.Text(); got != "3" {
		t.Errorf("the filter must have run: result = %q", got)
	}
}

// TestYQPlaygroundIgnoresHTTPResponses: an open HTTP response is JSON in every
// workflow the .http client serves, so it must not outrank the YAML buffer the
// user asked about.
func TestYQPlaygroundIgnoresHTTPResponses(t *testing.T) {
	m := openYQ(t, yqApp(t, "a: 1\n"))
	m.closePlayground()
	src, ok := m.playSource(jqplay.DialectYQ)
	if !ok {
		t.Fatal("the YAML buffer must resolve as a yq source")
	}
	if strings.Contains(src.label, "HTTP") {
		t.Errorf("the yq source is %q, want the editor buffer", src.label)
	}
}

// TestYQPlaygroundAtPathSeedsTheYQPath: the yq spelling of the caret's
// document path is what seeds the query line (#1660) — `."my-key"` where jq
// writes `.["my-key"]`.
func TestYQPlaygroundAtPathSeedsTheYQPath(t *testing.T) {
	noDebounce(t)
	// The path seed reads the buffer's language id; internal/app does not pull
	// in the shipped language plugins, so a bare test-only entry stands in.
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yqseed"}})
	m := newSized()
	path := filepath.Join(t.TempDir(), "seed.yqseed")
	if err := os.WriteFile(path, []byte("spec:\n  my-key: ike\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPathAt(path, 1, 10)
	m = drainCmd(tm.(Model), cmd)

	tm, cmd = m.Update(OpenPlaygroundAtPathMsg{Dialect: jqplay.DialectYQ})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("yaml.yqPlaygroundAtPath must open the playground")
	}
	if got := m.play.program.Text; got != `.spec."my-key"` {
		t.Fatalf("seed program = %q, want the yq spelling of the caret's path", got)
	}
	if got := m.play.result.Text(); got != "ike" {
		t.Errorf("the seeded program must run: result = %q", got)
	}
}

// TestJQPlaygroundStillOpensOverJSON guards the acceptance criterion that the
// jq playground did not move: the same command, the same label, the same JSON
// rendering.
func TestJQPlaygroundStillOpensOverJSON(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":{"b":1}}`))
	if m.play.dialect != jqplay.DialectJQ {
		t.Fatalf("json.jqPlayground opened %v", m.play.dialect)
	}
	m = setProgram(m, ".a")
	if got := m.play.result.Text(); got != "{\n  \"b\": 1\n}" {
		t.Errorf("jq result = %q, want pretty JSON", got)
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "> jq: ") || !strings.Contains(v, "JQ — ") {
		t.Errorf("the jq chrome changed:\n%s", v)
	}
}
