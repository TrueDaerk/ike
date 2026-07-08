package finder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(key(string(r)))
	}
}

// opened builds an open finder over a scratch root. The search service sends
// into the void (tests feed Apply directly with the finder's generation).
func opened(t *testing.T) *Model {
	t.Helper()
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.Open(t.TempDir())
	return m
}

// feed injects a batch + done for the finder's current generation, as the
// root model would after the service streamed them.
func feed(m *Model, matches ...search.Match) {
	m.Apply(search.BatchMsg{Gen: m.gen, Matches: matches})
	m.Apply(search.DoneMsg{Gen: m.gen, Total: len(matches)})
}

func match(path string, line int) search.Match {
	return search.Match{Path: path, Line: line, Text: "needle text", StartCol: 0, EndCol: 6}
}

func TestTypingScansAndRendersResults(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	if !m.scanning {
		t.Fatal("typing a query must start a scan")
	}
	feed(m, match("a.go", 3), match("b.go", 7))
	v := m.View()
	// The match range renders styled, so the line text is split around it.
	for _, want := range []string{"a.go", "b.go", "needle", " text", "2 matches in 2 files"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}

func TestStaleGenerationIsDropped(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	stale := m.gen
	typeText(m, "s") // new scan, new generation
	m.Apply(search.BatchMsg{Gen: stale, Matches: []search.Match{match("old.go", 1)}})
	if m.list.Total() != 0 {
		t.Fatal("results from a superseded scan must be dropped")
	}
}

func TestEnterOpensSelectedMatchAndCloses(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 3), match("b.go", 7))
	m.Update(key("down"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a match must dispatch a command")
	}
	loc, ok := cmd().(OpenLocationMsg)
	if !ok || loc.Path != "b.go" || loc.Line != 7 || loc.Col != 0 {
		t.Fatalf("unexpected location: %+v", loc)
	}
	if m.IsOpen() {
		t.Fatal("enter must close the overlay")
	}
	if !m.HasResults() {
		t.Fatal("results must survive closing for next/prev-match")
	}
}

func TestAdvanceWorksAfterClose(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 3), match("b.go", 7))
	m.Close()
	if it, ok := m.Advance(1); !ok || it.Path != "b.go" {
		t.Fatalf("advance after close must step the retained results, got %+v", it)
	}
	if it, _ := m.Advance(1); it.Path != "a.go" {
		t.Fatal("advance must wrap")
	}
}

func TestTogglesRescan(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 3))
	before := m.gen
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !m.caseSensitive || m.gen == before {
		t.Fatal("alt+c must toggle case sensitivity and rescan")
	}
	if m.list.Total() != 0 {
		t.Fatal("a rescan must clear the previous results")
	}
	v := m.View()
	if !strings.Contains(v, "[x] Case") {
		t.Fatalf("toggle state missing from view:\n%s", v)
	}
}

func TestGlobFieldsFeedTheQuery(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	m.Update(key("tab")) // include field
	typeText(m, "*.go, *.md")
	if got := splitGlobs(m.include); len(got) != 2 || got[0] != "*.go" || got[1] != "*.md" {
		t.Fatalf("include globs parsed wrong: %v", got)
	}
	m.Update(key("tab")) // exclude field
	typeText(m, "vendor/*")
	if got := splitGlobs(m.exclude); len(got) != 1 || got[0] != "vendor/*" {
		t.Fatalf("exclude globs parsed wrong: %v", got)
	}
}

func TestQueryHistoryRecall(t *testing.T) {
	m := opened(t)
	typeText(m, "first")
	feed(m, match("a.go", 1))
	m.Update(key("down")) // ensure selection exists
	m.Update(key("enter"))
	m.Open(t.TempDir())
	// Clear the retained query, then recall.
	for range "first" {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.query != "" {
		t.Fatalf("test setup: query not cleared, got %q", m.query)
	}
	m.Update(key("up")) // list is empty → history recall
	if m.query != "first" {
		t.Fatalf("up must recall the last committed query, got %q", m.query)
	}
}

// openedReplace builds an open replace-mode finder with three matches in two
// files.
func openedReplace(t *testing.T) *Model {
	t.Helper()
	m := New(search.New(nil))
	m.SetSize(100, 40)
	m.OpenReplace(t.TempDir())
	typeText(m, "needle")
	feed(m, match("a.go", 1), match("a.go", 5), match("b.go", 2))
	return m
}

func TestReplaceModeEnterReplacesCurrentMatch(t *testing.T) {
	m := openedReplace(t)
	m.Update(key("tab")) // focus the replace field
	typeText(m, "thread")
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must dispatch a replace request")
	}
	req, ok := cmd().(ReplaceRequestMsg)
	if !ok || len(req.Items) != 1 || req.Replacement != "thread" {
		t.Fatalf("unexpected request: %+v", req)
	}
	if req.Items[0].Path != "a.go" || req.Items[0].Line != 1 {
		t.Fatalf("wrong match replaced: %+v", req.Items[0])
	}
	if m.list.Total() != 2 || !m.IsOpen() {
		t.Fatal("the applied match leaves the list; the overlay stays open")
	}
}

func TestReplaceModeAltFReplacesFile(t *testing.T) {
	m := openedReplace(t)
	cmd := m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModAlt})
	req := cmd().(ReplaceRequestMsg)
	if len(req.Items) != 2 || req.Items[0].Path != "a.go" {
		t.Fatalf("alt+f must batch the selected file's matches: %+v", req.Items)
	}
	if m.list.Total() != 1 || m.list.Files() != 1 {
		t.Fatalf("applied file must leave the list: %d in %d files", m.list.Total(), m.list.Files())
	}
}

func TestReplaceModeAltAReplacesAll(t *testing.T) {
	m := openedReplace(t)
	cmd := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	req := cmd().(ReplaceRequestMsg)
	if len(req.Items) != 3 {
		t.Fatalf("alt+a must batch every match: %+v", req.Items)
	}
	if m.list.Total() != 0 {
		t.Fatal("replace-all must clear the list")
	}
}

func TestReplaceModePreviewShowsBeforeAfter(t *testing.T) {
	m := openedReplace(t)
	m.Update(key("tab"))
	typeText(m, "thread")
	v := m.View()
	if !strings.Contains(v, "- needle text") || !strings.Contains(v, "+ thread text") {
		t.Fatalf("preview rows missing:\n%s", v)
	}
}

func TestReplaceModeAltEnterOpensInstead(t *testing.T) {
	m := openedReplace(t)
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if _, ok := cmd().(OpenLocationMsg); !ok {
		t.Fatal("alt+enter must navigate, not replace")
	}
}

func TestFindModeHasNoReplaceField(t *testing.T) {
	m := opened(t)
	m.Update(key("tab"))
	if m.focus == fieldReplace {
		t.Fatal("find mode must skip the replace field in the tab cycle")
	}
}

func TestEndToEndScanAgainstDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hit.txt"), []byte("the needle is here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := make(chan tea.Msg, 64)
	m := New(search.New(func(msg tea.Msg) { got <- msg }))
	m.SetSize(100, 30)
	m.Open(root)
	typeText(m, "needle")
	// Drain service messages back into the finder, as the root model would.
	deadline := time.After(10 * time.Second)
	for m.scanning {
		select {
		case msg := <-got:
			m.Apply(msg)
		case <-deadline:
			t.Fatal("scan did not finish")
		}
	}
	if m.list.Total() != 1 {
		t.Fatalf("end-to-end scan found %d matches, want 1", m.list.Total())
	}
}
