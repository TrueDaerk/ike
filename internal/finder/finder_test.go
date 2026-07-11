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

// TestReopenPreselectsQuery guards #277: the remembered query is selected on
// re-open — typing replaces it wholesale, backspace keeps it and just edits.
func TestReopenPreselectsQuery(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	m.Update(key("esc"))

	m.Open(t.TempDir())
	if m.query != "needle" || !m.preselect {
		t.Fatalf("re-open should preselect the remembered query, query=%q preselect=%v", m.query, m.preselect)
	}
	typeText(m, "x")
	if m.query != "x" {
		t.Fatalf("typing over the preselected query should replace it, got %q", m.query)
	}

	// Backspace instead of typing edits the remembered text and drops the
	// selection, so subsequent typing appends.
	typeText(m, "yz") // query "xyz", no longer preselected
	m.Update(key("esc"))
	m.Open(t.TempDir())
	m.Update(key("backspace"))
	if m.query != "xy" || m.preselect {
		t.Fatalf("backspace should edit the prefill, got query=%q preselect=%v", m.query, m.preselect)
	}
	typeText(m, "z")
	if m.query != "xyz" {
		t.Fatalf("after backspace typing appends, got %q", m.query)
	}
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

func TestFooterPluralizesCounts(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 3))
	if v := m.View(); !strings.Contains(v, "1 match in 1 file") {
		t.Fatalf("singular counts must not be pluralized:\n%s", v)
	}
	feed(m, match("a.go", 5))
	if v := m.View(); !strings.Contains(v, "2 matches in 1 file") {
		t.Fatalf("mixed counts must pluralize independently:\n%s", v)
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

// The ctrl chords mirror every alt binding (#422): on macOS Option is a
// composition key, so alt never reaches the terminal.
func TestCtrlChordsMirrorAltBindings(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 3))
	for _, tc := range []struct {
		code rune
		flag *bool
	}{
		{'c', &m.caseSensitive},
		{'w', &m.wholeWord},
		{'x', &m.regex},
	} {
		before := m.gen
		m.Update(tea.KeyPressMsg{Code: tc.code, Mod: tea.ModCtrl})
		if !*tc.flag || m.gen == before {
			t.Fatalf("ctrl+%c must toggle and rescan", tc.code)
		}
	}

	r := openedReplace(t)
	cmd := r.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if req := cmd().(ReplaceRequestMsg); len(req.Items) != 2 {
		t.Fatalf("ctrl+f must batch the selected file's matches: %+v", req.Items)
	}
	cmd = r.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if req := cmd().(ReplaceRequestMsg); len(req.Items) != 1 {
		t.Fatalf("ctrl+a must batch the remaining matches: %+v", req.Items)
	}

	r = openedReplace(t)
	cmd = r.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if _, ok := cmd().(OpenLocationMsg); !ok {
		t.Fatal("ctrl+enter must navigate, not replace")
	}
}

func TestCtrlUpRecallsHistory(t *testing.T) {
	m := opened(t)
	typeText(m, "first")
	feed(m, match("a.go", 1))
	m.Update(key("enter"))
	m.Open(t.TempDir())
	for range "first" {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	if m.query != "first" {
		t.Fatalf("ctrl+up must recall the last committed query, got %q", m.query)
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

// Content rows of the find-mode overlay (0 = first row inside the border):
// 0 title, 1 blank, 2 query, 3 toggles, 4 include, 5 exclude, 6 blank,
// 7+ results. Click takes panel-local coordinates, so x = column+2 (border +
// padding) and y = row+1 (border).
func clickAt(m *Model, col, row int) tea.Cmd { return m.Click(col+2, row+1) }

func TestClickTogglesFlipModes(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	m.View()
	if cmd := clickAt(m, 10, 3); cmd != nil {
		t.Fatal("toggle click must not emit a command")
	}
	if !m.caseSensitive {
		t.Fatal("click on the Case toggle must enable case sensitivity")
	}
	clickAt(m, 29, 3)
	if !m.wholeWord {
		t.Fatal("click on the Word toggle must enable whole-word")
	}
	clickAt(m, 48, 3)
	if !m.regex {
		t.Fatal("click on the Regex toggle must enable regex")
	}
	clickAt(m, 10, 3)
	if m.caseSensitive {
		t.Fatal("second click on the Case toggle must disable it again")
	}
	// The indent left of the first toggle is dead space.
	clickAt(m, 2, 3)
	if m.caseSensitive {
		t.Fatal("click left of the toggles must not flip anything")
	}
}

func TestClickFocusesInputFields(t *testing.T) {
	m := opened(t)
	m.View()
	clickAt(m, 5, 4)
	typeText(m, "x")
	if m.include != "x" {
		t.Fatalf("click on the Include row must focus it, include=%q", m.include)
	}
	clickAt(m, 5, 5)
	typeText(m, "y")
	if m.exclude != "y" {
		t.Fatalf("click on the Exclude row must focus it, exclude=%q", m.exclude)
	}
	clickAt(m, 5, 2)
	typeText(m, "q")
	if m.query != "q" {
		t.Fatalf("click on the Search row must focus it, query=%q", m.query)
	}
}

func TestClickReplaceRowFocusesReplaceField(t *testing.T) {
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.OpenReplace(t.TempDir())
	m.View()
	// Replace mode shifts the rows: 2 query, 3 replace, 4 toggles, ...
	clickAt(m, 5, 3)
	typeText(m, "z")
	if m.replace != "z" {
		t.Fatalf("click on the Replace row must focus it, replace=%q", m.replace)
	}
}

func TestClickSelectsThenOpensResult(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 1), match("a.go", 9), match("b.go", 2))
	m.View()
	// Result rows start at content row 7: 7 header a.go, 8 item0, 9 item1,
	// 10 header b.go, 11 item2.
	if cmd := clickAt(m, 5, 11); cmd != nil {
		t.Fatal("first click must only select, not open")
	}
	if m.list.Cursor() != 2 {
		t.Fatalf("click must move the cursor to the row's item, got %d", m.list.Cursor())
	}
	cmd := clickAt(m, 5, 11)
	if cmd == nil {
		t.Fatal("second click on the selected match must open it")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "b.go" || msg.Line != 2 {
		t.Fatalf("open message = %+v", msg)
	}
	if m.IsOpen() {
		t.Fatal("opening a match must close the overlay")
	}
	// Header rows are labels, not stops.
	m.Open(t.TempDir())
	feed(m, match("a.go", 1), match("b.go", 2))
	m.View()
	m.list.SetCursor(1)
	clickAt(m, 5, 7)
	if m.list.Cursor() != 1 {
		t.Fatalf("header click must not move the cursor, got %d", m.list.Cursor())
	}
}

func TestWheelScrollsResults(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 1), match("a.go", 9), match("b.go", 2))
	m.Wheel(2)
	if m.list.Cursor() != 2 {
		t.Fatalf("wheel must move the cursor, got %d", m.list.Cursor())
	}
	m.Wheel(-3)
	if m.list.Cursor() != 0 {
		t.Fatalf("wheel must clamp at the top, got %d", m.list.Cursor())
	}
}
