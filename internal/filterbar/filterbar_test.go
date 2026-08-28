package filterbar

// The shared filter row's own cases (#2156): key handling, the quick-key seam
// (SetTerm/HasTerm), the inline completion and what the row renders. Each
// pane's schema and matching live in that pane's tests.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/filterexpr"
)

var demo = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "severity", Aliases: []string{"sev"}, Values: []string{"error", "warning"}},
	{Name: "file", ValueDoc: "a path or glob"},
	{Name: "scope", Values: []string{"file", "project"}},
}}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// typeIn feeds a whole string through Key, one rune at a time.
func typeIn(m *Model, s string) {
	for _, r := range s {
		m.Key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestTypingParsesIntoAQuery(t *testing.T) {
	m := New(demo)
	if handled, _ := m.Key(key("x")); handled {
		t.Fatal("an unfocused bar must not consume keys")
	}
	m.Focus()
	if !m.Active() {
		t.Fatal("Focus must activate the bar")
	}
	typeIn(&m, "sev:error boom")
	if got := m.Query().Value("severity"); got != "error" {
		t.Fatalf("severity = %q", got)
	}
	if got := m.Query().Match; got != "boom" {
		t.Fatalf("match = %q", got)
	}
	if m.Empty() {
		t.Fatal("a typed filter must not report Empty")
	}
}

// A half-written expression keeps the last good query, so the list under the
// row does not empty out mid-token.
func TestBrokenExpressionKeepsTheLastQuery(t *testing.T) {
	m := New(demo)
	m.Focus()
	typeIn(&m, "sev:error ")
	typeIn(&m, "sev:e")
	if m.Err() == "" {
		t.Fatal("an unfinished value must be reported")
	}
	if got := m.Query().Value("severity"); got != "error" {
		t.Fatalf("severity = %q, want the last good parse", got)
	}
}

func TestEnterAppliesEscClears(t *testing.T) {
	m := New(demo)
	m.Focus()
	typeIn(&m, "scope:file")
	if handled, changed := m.Key(key("enter")); !handled || changed {
		t.Fatalf("enter: handled=%v changed=%v", handled, changed)
	}
	if m.Active() || m.Text() != "scope:file" {
		t.Fatalf("enter must apply and blur, text=%q active=%v", m.Text(), m.Active())
	}
	m.Focus()
	handled, changed := m.Key(key("esc"))
	if !handled || !changed || m.Active() || m.Text() != "" {
		t.Fatalf("esc must clear and blur: handled=%v changed=%v text=%q", handled, changed, m.Text())
	}
}

// List navigation is not the bar's: the pane must still see it while typing.
func TestNavigationKeysFallThrough(t *testing.T) {
	m := New(demo)
	m.Focus()
	if handled, _ := m.Key(key("down")); handled {
		t.Fatal("down must fall through to the list")
	}
}

func TestBackspaceEdits(t *testing.T) {
	m := New(demo)
	m.Focus()
	typeIn(&m, "abc")
	if _, changed := m.Key(key("backspace")); !changed || m.Text() != "ab" {
		t.Fatalf("text = %q", m.Text())
	}
}

func TestSetTermIsTheQuickKeySeam(t *testing.T) {
	m := New(demo)
	m.SetText("sev:error crash")
	if !m.SetTerm("scope", "file") {
		t.Fatal("SetTerm must report the change")
	}
	if !m.HasTerm("scope", "file") {
		t.Fatalf("scope term missing from %q", m.Text())
	}
	// The rest of the expression survives the quick key untouched.
	if q := m.Query(); q.Value("severity") != "error" || q.Match != "crash" {
		t.Fatalf("quick key clobbered the expression: %q", m.Text())
	}
	// Toggling off removes exactly that field.
	if !m.SetTerm("scope", "") || m.HasTerm("scope", "file") {
		t.Fatalf("scope term survived removal: %q", m.Text())
	}
	if m.SetTerm("scope", "") {
		t.Fatal("a no-op SetTerm must report no change")
	}
}

func TestCompletionOffersNamesThenValues(t *testing.T) {
	m := New(demo)
	m.Focus()
	typeIn(&m, "sco")
	if got := m.Completion(); got != "pe:" {
		t.Fatalf("name ghost = %q, want pe:", got)
	}
	if handled, changed := m.Key(key("tab")); !handled || !changed || m.Text() != "scope:" {
		t.Fatalf("tab must accept the ghost, text=%q", m.Text())
	}
	typeIn(&m, "fi")
	if got := m.Completion(); got != "le " {
		t.Fatalf("value ghost = %q, want \"le \"", got)
	}
	m.Key(key("tab"))
	if m.Text() != "scope:file " {
		t.Fatalf("text = %q", m.Text())
	}
	// A completed value ends in its separator, so the filter applies at once.
	if !m.HasTerm("scope", "file") {
		t.Fatalf("accepted completion did not apply: %q", m.Text())
	}
}

// A quoted value keeps its span together, so the completion sees the whole
// prefix rather than the last word of it.
func TestCompletionInsideAQuotedValue(t *testing.T) {
	m := New(demo)
	m.Candidates = func(string) []string { return []string{"src dir/app.go"} }
	m.Focus()
	typeIn(&m, `file:"src d`)
	if got := m.Completion(); got != `ir/app.go" ` {
		t.Fatalf("ghost = %q", got)
	}
}

// A pane may supply its own candidates for a free-form field (the files it
// currently lists).
func TestInjectedCandidates(t *testing.T) {
	m := New(demo)
	m.Candidates = func(field string) []string {
		if field == "file" {
			return []string{"internal/app/app.go"}
		}
		return nil
	}
	m.Focus()
	typeIn(&m, "file:internal/a")
	if got := m.Completion(); got != "pp/app.go " {
		t.Fatalf("ghost = %q", got)
	}
}

func TestViewShowsHintThenFilterThenError(t *testing.T) {
	m := New(demo)
	if v := ansi.Strip(m.View(80, nil)); !strings.Contains(v, "/ filter") || !strings.Contains(v, "severity:") {
		t.Fatalf("idle row must hint the fields:\n%s", v)
	}
	m.SetText("scope:file")
	if v := ansi.Strip(m.View(80, nil)); !strings.Contains(v, "filter: scope:file") {
		t.Fatalf("idle row must show the expression:\n%s", v)
	}
	m.Focus()
	typeIn(&m, " sev:nope")
	if v := ansi.Strip(m.View(120, nil)); !strings.Contains(v, "sev: wants error, warning") {
		t.Fatalf("focused row must show the parse error:\n%s", v)
	}
}

// #1379's rule: an overlong row is clipped, never wrapped.
func TestViewClipsToWidth(t *testing.T) {
	m := New(demo)
	m.SetText(strings.Repeat("wide ", 40))
	for _, w := range []int{10, 24, 40} {
		v := m.View(w, nil)
		if strings.Contains(v, "\n") {
			t.Fatalf("row at width %d contains a line break", w)
		}
		if got := ansi.StringWidth(ansi.Strip(v)); got > w {
			t.Fatalf("row at width %d renders %d cells", w, got)
		}
	}
}

func TestPasteInserts(t *testing.T) {
	m := New(demo)
	if m.Paste("scope:file") {
		t.Fatal("an unfocused bar must ignore a paste")
	}
	m.Focus()
	if !m.Paste("scope:file") || !m.HasTerm("scope", "file") {
		t.Fatalf("paste did not apply: %q", m.Text())
	}
}
