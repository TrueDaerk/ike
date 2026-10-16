package settings

// complexity_page_test.go covers the Settings-UI half of the project
// complexity index (#2916): the "Complexity" page carries the [complexity]
// keys, the exclude list rejects an invalid glob in the form, every edit
// persists and shows in the list.

import (
	"fmt"
	"strings"
	"testing"

	"ike/internal/config"
)

func complexityPage(t *testing.T) Page {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "Complexity" {
			return p
		}
	}
	t.Fatal("no Complexity settings page")
	return Page{}
}

func complexityEntry(t *testing.T, key string) Entry {
	t.Helper()
	for _, e := range complexityPage(t).Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("%s must be configurable on the Complexity page", key)
	return Entry{}
}

func TestComplexityPageEntries(t *testing.T) {
	p := complexityPage(t)
	if p.Description == "" {
		t.Fatal("the Complexity page needs a description")
	}
	want := []string{"complexity.enabled", "complexity.exclude", "complexity.ccn_warn", "complexity.cognitive_warn", "complexity.comment_density_warn", "complexity.overrides", "complexity.problems", "complexity.status_segment", "complexity.churn_days", "complexity.trend_commits", "complexity.baseline", "complexity.agent_verdict", "complexity.agent_apply"}
	if len(p.Entries) != len(want) {
		t.Fatalf("Complexity page has %d entries, want %d: %+v", len(p.Entries), len(want), p.Entries)
	}
	for i, key := range want {
		e := p.Entries[i]
		if e.Key != key || e.Title == "" || e.Description == "" || e.Scope != config.UserScope {
			t.Errorf("entry %d = %+v, want user-scoped %s with title and description", i, e, key)
		}
	}
	for _, key := range []string{"complexity.enabled", "complexity.problems", "complexity.status_segment", "complexity.baseline", "complexity.agent_verdict", "complexity.agent_apply"} {
		if e := complexityEntry(t, key); e.Type != Bool {
			t.Errorf("%s type = %v, want Bool", key, e.Type)
		}
	}
	if e := complexityEntry(t, "complexity.exclude"); e.Type != List || e.ValidateEntry == nil {
		t.Errorf("exclude = %+v, want a validated List", e)
	}
	if e := complexityEntry(t, "complexity.overrides"); e.Type != List || e.ValidateEntry == nil {
		t.Errorf("overrides = %+v, want a validated List", e)
	}
	for _, key := range []string{"complexity.ccn_warn", "complexity.cognitive_warn"} {
		if e := complexityEntry(t, key); e.Type != Int || e.Min != 1 || e.Max != 200 {
			t.Errorf("%s = %+v, want an Int bounded 1..200", key, e)
		}
	}
}

// TestComplexityExcludeRejectsInvalidGlob: a malformed pattern is refused in
// place with a message and nothing is staged; valid globs commit and persist
// as the typed list the index reads.
func TestComplexityExcludeRejectsInvalidGlob(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.exclude")}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	ed.idx = len(ed.items)
	m.Update(key("enter"))
	ed.tf.Set("src/[gen")
	m.Update(key("enter"))
	if !ed.editing || ed.err != "invalid glob pattern" {
		t.Fatalf("an invalid glob must stay in edit mode with a message (editing=%v err=%q)", ed.editing, ed.err)
	}
	if len(ed.items) != 0 || m.Dirty() {
		t.Fatalf("nothing may be staged: items=%v dirty=%v", ed.items, m.Dirty())
	}
	for _, g := range []string{"vendor/**", "**/*_test.go"} {
		ed.tf.Set(g)
		m.Update(key("enter"))
		if ed.err != "" {
			t.Fatalf("%q: err = %q, want accepted", g, ed.err)
		}
		ed.idx = len(ed.items)
		m.Update(key("enter"))
	}
	m.Update(key("esc"))
	commit(t, m)
	got := config.Get().Complexity.Exclude
	if len(got) != 2 || got[0] != "vendor/**" || got[1] != "**/*_test.go" {
		t.Fatalf("exclude = %v, want [vendor/** **/*_test.go]", got)
	}
	if view := m.View(); !strings.Contains(view, "vendor/**") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
}

// TestComplexityEnabledToggles: the master switch flips on enter and
// persists.
func TestComplexityEnabledToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: complexityPage(t).Entries}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	before := config.Get().Complexity.Enabled
	if !before {
		t.Fatal("complexity.enabled must default to on")
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.Enabled; got == before {
		t.Fatalf("enabled did not toggle from %v", before)
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.Enabled; got != before {
		t.Fatalf("enabled did not toggle back to %v", before)
	}
}

// TestComplexityProblemsToggles: the Problems feed switch (#2920) defaults
// on, shows in the list, flips on enter and persists.
func TestComplexityProblemsToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.problems")}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if !config.Get().Complexity.Problems {
		t.Fatal("complexity.problems must default to on")
	}
	if view := m.View(); !strings.Contains(view, "Problems feed") {
		t.Fatalf("the entry list must show the setting:\n%s", view)
	}
	m.Update(key("enter"))
	commit(t, m)
	if config.Get().Complexity.Problems {
		t.Fatal("problems did not toggle off")
	}
	m.Update(key("enter"))
	commit(t, m)
	if !config.Get().Complexity.Problems {
		t.Fatal("problems did not toggle back on")
	}
}

// TestComplexityStatusSegmentToggles: the status segment switch (#2921)
// defaults off, shows in the list, flips on enter and persists.
func TestComplexityStatusSegmentToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.status_segment")}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if config.Get().Complexity.StatusSegment {
		t.Fatal("complexity.status_segment must default to off")
	}
	if view := m.View(); !strings.Contains(view, "Status segment") {
		t.Fatalf("the entry list must show the setting:\n%s", view)
	}
	m.Update(key("enter"))
	commit(t, m)
	if !config.Get().Complexity.StatusSegment {
		t.Fatal("status_segment did not toggle on")
	}
	m.Update(key("enter"))
	commit(t, m)
	if config.Get().Complexity.StatusSegment {
		t.Fatal("status_segment did not toggle back off")
	}
}

// TestComplexityCommentDensityWarn: the comment-density threshold (#2934)
// is a bounded percentage on the Complexity page; non-numeric input is
// rejected in the form, an out-of-range value clamps, a sane value persists
// and shows in the list, and 0 (off) is accepted.
func TestComplexityCommentDensityWarn(t *testing.T) {
	restoreConfig(t)
	e := complexityEntry(t, "complexity.comment_density_warn")
	if e.Type != Int || e.Min != 0 || e.Max != 100 {
		t.Fatalf("comment_density_warn = %+v, want an Int bounded 0..100", e)
	}
	if got := config.Get().Complexity.CommentDensityWarn; got != 40 {
		t.Fatalf("comment_density_warn must default to 40, got %d", got)
	}
	m := New([]Page{{Title: "Complexity", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("lots")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid threshold must not write (err=%q)", ed.err)
	}
	ed.tf.Set("250")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.CommentDensityWarn; got != 100 {
		t.Fatalf("comment_density_warn = %d, want clamped 100", got)
	}
	for _, v := range []string{"55", "0"} {
		m.Update(key("enter"))
		m.editor.(*intEditor).tf.Set(v)
		m.Update(key("enter"))
		commit(t, m)
		if got := config.Get().Complexity.CommentDensityWarn; fmt.Sprint(got) != v {
			t.Fatalf("comment_density_warn = %d, want %s", got, v)
		}
	}
	if view := m.View(); !strings.Contains(view, "Comment density warning") {
		t.Fatalf("the entry list must show the setting:\n%s", view)
	}
}

// TestComplexityChurnDays: the hotspot window (#2957) is a bounded day
// count on the Complexity page; non-numeric input is rejected in the form,
// an out-of-range value clamps, a sane value persists and shows in the
// list.
func TestComplexityChurnDays(t *testing.T) {
	restoreConfig(t)
	e := complexityEntry(t, "complexity.churn_days")
	if e.Type != Int || e.Min != 1 || e.Max != 3650 {
		t.Fatalf("churn_days = %+v, want an Int bounded 1..3650", e)
	}
	if got := config.Get().Complexity.ChurnDays; got != 90 {
		t.Fatalf("churn_days must default to 90, got %d", got)
	}
	m := New([]Page{{Title: "Complexity", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if view := m.View(); !strings.Contains(view, "Hotspot churn window") || !strings.Contains(view, "90") {
		t.Fatalf("the entry list must show the setting and its value:\n%s", view)
	}
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("a quarter")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("an invalid window must not write (err=%q)", ed.err)
	}
	ed.tf.Set("9999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.ChurnDays; got != 3650 {
		t.Fatalf("churn_days = %d, want clamped 3650", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("30")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.ChurnDays; got != 30 {
		t.Fatalf("churn_days = %d, want 30", got)
	}
	if view := m.View(); !strings.Contains(view, "30") {
		t.Fatalf("the entry list must show the new value:\n%s", view)
	}
}

// TestComplexityTrendCommits: the trend window (#2962) is a bounded commit
// count on the Complexity page; non-numeric input is rejected in the form,
// an out-of-range value clamps, a sane value persists and shows in the
// list.
func TestComplexityTrendCommits(t *testing.T) {
	restoreConfig(t)
	e := complexityEntry(t, "complexity.trend_commits")
	if e.Type != Int || e.Min != 1 || e.Max != 500 {
		t.Fatalf("trend_commits = %+v, want an Int bounded 1..500", e)
	}
	if got := config.Get().Complexity.TrendCommits; got != 30 {
		t.Fatalf("trend_commits must default to 30, got %d", got)
	}
	m := New([]Page{{Title: "Complexity", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if view := m.View(); !strings.Contains(view, "Trend window") || !strings.Contains(view, "30") {
		t.Fatalf("the entry list must show the setting and its value:\n%s", view)
	}
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	ed.tf.Set("many")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("an invalid window must not write (err=%q)", ed.err)
	}
	ed.tf.Set("9999")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.TrendCommits; got != 500 {
		t.Fatalf("trend_commits = %d, want clamped 500", got)
	}
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("12")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Complexity.TrendCommits; got != 12 {
		t.Fatalf("trend_commits = %d, want 12", got)
	}
	if view := m.View(); !strings.Contains(view, "12") {
		t.Fatalf("the entry list must show the new value:\n%s", view)
	}
}

// TestComplexityAgentApplyToggles: the auto-apply switch of the simplify
// agent (#3009) defaults on, shows in the list, flips on enter (a Bool has
// no invalid input to reject) and persists.
func TestComplexityAgentApplyToggles(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Complexity", Entries: []Entry{complexityEntry(t, "complexity.agent_apply")}}}, testOpts(t))
	m.SetSize(100, 24)
	m.Open()
	m.Update(key("tab"))
	if !config.Get().Complexity.AgentApply {
		t.Fatal("complexity.agent_apply must default to on")
	}
	if view := m.View(); !strings.Contains(view, "Apply the agent's diff") {
		t.Fatalf("the entry list must show the setting:\n%s", view)
	}
	m.Update(key("enter"))
	commit(t, m)
	if config.Get().Complexity.AgentApply {
		t.Fatal("agent_apply did not toggle off")
	}
	m.Update(key("enter"))
	commit(t, m)
	if !config.Get().Complexity.AgentApply {
		t.Fatal("agent_apply did not toggle back on")
	}
}
