package keydoctor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

// binding is a test binding on a parsed chord.
func binding(chord, command string, ctx keymap.Context) keymap.Binding {
	return keymap.Binding{Chord: keymap.MustParseChord(chord), Command: command, Context: ctx, Title: command}
}

// darwinEnv is a macOS terminal that composes the Option layer — the setup
// the issue's motivating cases come from.
func darwinEnv() keymap.TerminalEnv {
	return keymap.TerminalEnv{GOOS: "darwin", Terminal: "Ghostty"}
}

// pinGOOS pins keymap.GOOS for the test: Classify reads it.
func pinGOOS(t *testing.T, goos string) {
	t.Helper()
	prev := keymap.GOOS
	t.Cleanup(func() { keymap.GOOS = prev })
	keymap.GOOS = goos
	keymap.SetProbeVerdicts(nil)
}

// TestAnalyzeReportsDeadAndAtRisk (#2161): the audit judges each bound chord,
// keeps deliverable ones silent, and orders dead findings ahead of risky ones.
func TestAnalyzeReportsDeadAndAtRisk(t *testing.T) {
	pinGOOS(t, "darwin")
	bindings := []keymap.Binding{
		binding("ctrl+s", "editor.save", keymap.Global),
		binding("alt+f7", "editor.findUsages", keymap.Global),
		binding("alt+b", "vcs.branches", keymap.Global),
		binding("ctrl+f8", "debug.toggleBreakpoint", keymap.Global),
	}
	findings := Analyze(darwinEnv(), bindings)
	if len(findings) != 3 {
		t.Fatalf("findings = %d, want 3 (ctrl+s is deliverable)", len(findings))
	}
	if findings[0].Class != keymap.Dead || findings[1].Class != keymap.Dead {
		t.Fatalf("dead findings must sort first: %+v", findings)
	}
	if findings[2].Class != keymap.AtRisk || findings[2].Binding.Chord.String() != "alt+f7" {
		t.Fatalf("alt+f7 should be the at-risk tail: %+v", findings[2])
	}
	for _, f := range findings {
		if f.Reason == "" {
			t.Fatalf("finding %s carries no reason", f.Binding.Chord)
		}
	}
}

// TestEveryDeadBindingGetsASuggestion (#2161): the acceptance criterion —
// every dead binding offers at least one alternative, and every offer is
// deliverable here and free in the keymap.
func TestEveryDeadBindingGetsASuggestion(t *testing.T) {
	pinGOOS(t, "darwin")
	env := darwinEnv()
	bindings := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, "darwin"), nil, "darwin").Bindings()
	findings := Analyze(env, bindings)
	dead := 0
	for _, f := range findings {
		if f.Class != keymap.Dead {
			continue
		}
		dead++
		if len(f.Suggestions) == 0 {
			t.Fatalf("dead binding %s (%s) offers no rebind", f.Binding.Chord, f.Binding.Command)
		}
		for _, s := range f.Suggestions {
			if class, why := env.Deliverability(s); class != keymap.Live {
				t.Fatalf("suggestion %s for %s is %v (%s)", s, f.Binding.Chord, class, why)
			}
			if occupied(s, bindings) {
				t.Fatalf("suggestion %s for %s collides with the keymap", s, f.Binding.Chord)
			}
		}
	}
	if dead == 0 {
		t.Fatal("the darwin default set must contain dead bindings to audit")
	}
}

// TestSuggestAvoidsConflictsAndPrefixes (#2161): a candidate that is taken —
// exactly, or as the prefix of a multi-step chord — is never offered.
func TestSuggestAvoidsConflictsAndPrefixes(t *testing.T) {
	pinGOOS(t, "darwin")
	bindings := []keymap.Binding{
		binding("alt+b", "vcs.branches", keymap.Global),
		binding("ctrl+b", "editor.gotoDeclaration", keymap.Global),
		binding("ctrl+k c", "pane.close", keymap.Global),
		binding("ctrl+v", "editor.paste", keymap.Editor),
	}
	got := Suggest(darwinEnv(), bindings[0], bindings, 5)
	if len(got) == 0 {
		t.Fatal("a dead alt chord must get alternatives")
	}
	for _, s := range got {
		switch s.String() {
		case "ctrl+b", "ctrl+k c", "ctrl+v":
			t.Fatalf("suggested a taken chord: %s", s)
		case "ctrl+k":
			t.Fatalf("suggested %s, the prefix of an existing multi-step chord", s)
		}
	}
	// The offer stays mnemonic: the command's initials come before the
	// two-step escape hatch.
	if !strings.HasPrefix(got[0].String(), "ctrl+") {
		t.Fatalf("first suggestion %q should be a plain ctrl chord", got[0])
	}
}

// TestSuggestKeepsFunctionKeyRow (#2161): a dead ctrl+fN keeps its number —
// the smallest change to muscle memory is the shifted variant.
func TestSuggestKeepsFunctionKeyRow(t *testing.T) {
	pinGOOS(t, "darwin")
	b := binding("ctrl+f8", "debug.toggleBreakpoint", keymap.Global)
	got := Suggest(darwinEnv(), b, []keymap.Binding{b}, 3)
	if len(got) == 0 || got[0].String() != "shift+f8" {
		t.Fatalf("suggestions = %v, want shift+f8 first", got)
	}
}

// TestReportRebindFlow (#2161): the report lists the findings, cycles the
// offer and emits the rebind the app persists.
func TestReportRebindFlow(t *testing.T) {
	pinGOOS(t, "darwin")
	m := New()
	m.SetSize(120, 40)
	bindings := []keymap.Binding{
		binding("alt+b", "vcs.branches", keymap.Global),
		binding("ctrl+f8", "debug.toggleBreakpoint", keymap.Global),
	}
	m.OpenReport(darwinEnv(), bindings)
	if !m.IsOpen() {
		t.Fatal("OpenReport must open the overlay")
	}
	if len(m.Findings()) != 2 {
		t.Fatalf("findings = %+v", m.Findings())
	}
	view := m.View()
	if !strings.Contains(view, "alt+b") || !strings.Contains(view, "dead") {
		t.Fatalf("report must list the dead chords:\n%s", view)
	}
	// Move to the second finding and take its second offer.
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	first := m.findings[1].Suggestions[0]
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	second := m.findings[1].Suggestions[m.pick[1]]
	if second.Equal(first) {
		t.Fatal("tab must cycle to another suggestion")
	}
	msg := runCmd(t, m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	rb, ok := msg.(RebindMsg)
	if !ok {
		t.Fatalf("enter = %#v, want RebindMsg", msg)
	}
	if rb.Command != "debug.toggleBreakpoint" || rb.Old.String() != "ctrl+f8" || !rb.New.Equal(second) {
		t.Fatalf("rebind = %+v", rb)
	}
	if !m.IsOpen() {
		t.Fatal("applying a rebind must keep the report open for the next finding")
	}
	// A second enter on the same row must not write twice.
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("an applied finding must not rebind again")
	}
	// esc closes.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.IsOpen() {
		t.Fatal("esc must close the report")
	}
}

// TestReportCleanKeymap (#2161): nothing dead, nothing alarming — the report
// says so instead of showing an empty list.
func TestReportCleanKeymap(t *testing.T) {
	pinGOOS(t, "linux")
	m := New()
	m.SetSize(100, 24)
	m.OpenReport(keymap.TerminalEnv{GOOS: "linux", Terminal: "xterm"}, []keymap.Binding{
		binding("ctrl+s", "editor.save", keymap.Global),
	})
	if len(m.Findings()) != 0 {
		t.Fatalf("findings = %+v, want none", m.Findings())
	}
	if !strings.Contains(m.View(), "deliverable") {
		t.Fatalf("clean report must say so:\n%s", m.View())
	}
}

// TestSummaryOffersReview (#2161): finishing a probe run can save and go
// straight to the audit, which then judges against the fresh verdicts.
func TestSummaryOffersReview(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	m.Open("testterm")
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}) // → summary
	if !strings.Contains(m.View(), "review dead bindings") {
		t.Fatalf("summary must offer the review:\n%s", m.View())
	}
	msg := runCmd(t, m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}))
	res, ok := msg.(ResultMsg)
	if !ok || !res.Save || !res.Review {
		t.Fatalf("r = %#v, want a saving ResultMsg with Review", msg)
	}
}
