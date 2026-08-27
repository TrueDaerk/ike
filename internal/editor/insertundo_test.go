package editor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor/register"
	ilsp "ike/internal/lsp"
)

// --- insert-mode undo granularity (#1818) ----------------------------------

// esc is the Escape key press that commits an insert session.
func esc() tea.KeyPressMsg { return special(tea.KeyEscape) }

// undoSteps applies count undos and returns the resulting model.
func undoSteps(m Model, count int) Model {
	for i := 0; i < count; i++ {
		m = send(m, key('u'))
	}
	return m
}

// TestInsertTypingUndoesWordWise: typing `foo bar baz` in one insert session
// leaves three changes, and three undos peel the words off from the right —
// each word carrying the whitespace typed after it.
func TestInsertTypingUndoesWordWise(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo bar baz")
	m = send(m, esc())
	if got := line(m, 0); got != "foo bar baz" {
		t.Fatalf("typed line = %q", got)
	}
	for _, want := range []string{"foo bar ", "foo ", ""} {
		m = send(m, key('u'))
		if got := line(m, 0); got != want {
			t.Fatalf("undo landed on %q, want %q", got, want)
		}
	}
	if m.hist.CanUndo() {
		t.Fatal("three undos must exhaust the insert — no extra step left")
	}
}

// TestInsertTypingRedoMirrorsWordSteps: redo retraces the very same boundaries
// in reverse.
func TestInsertTypingRedoMirrorsWordSteps(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo bar baz")
	m = send(m, esc())
	m = undoSteps(m, 3)
	for _, want := range []string{"foo ", "foo bar ", "foo bar baz"} {
		m, _ = m.Update(modKey('r', tea.ModCtrl))
		if got := line(m, 0); got != want {
			t.Fatalf("redo landed on %q, want %q", got, want)
		}
	}
}

// TestInsertUndoCursorFollowsTheStep: every step restores its own
// CursorBefore/CursorAfter, so the caret sits where the removed word started
// (normal-mode clamped) after an undo and where it ended after a redo.
func TestInsertUndoCursorFollowsTheStep(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo bar baz")
	m = send(m, esc())
	m = send(m, key('u')) // removes "baz"
	if got := line(m, 0); got != "foo bar " || m.cursor.Col != 7 {
		t.Fatalf("after undo: line %q col %d, want %q col 7", got, m.cursor.Col, "foo bar ")
	}
	// Redo restores the step's CursorAfter, normal-mode clamped onto the last
	// rune of the re-inserted word.
	m, _ = m.Update(modKey('r', tea.ModCtrl))
	if got := line(m, 0); got != "foo bar baz" || m.cursor.Col != 10 {
		t.Fatalf("after redo: line %q col %d, want %q col 10", got, m.cursor.Col, "foo bar baz")
	}
}

// TestInsertSeparatorsJoinTheFollowingWord: separators typed before any word —
// the space starting the insert here — belong to the word that follows, so the
// undo boundary never lands between them.
func TestInsertSeparatorsJoinTheFollowingWord(t *testing.T) {
	m, _ := loaded(t, "one\n")
	m = typeKeys(m, "A two three")
	m = send(m, esc())
	if got := line(m, 0); got != "one two three" {
		t.Fatalf("typed line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "one two " {
		t.Fatalf("first undo = %q, want %q", got, "one two ")
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "one" {
		t.Fatalf("second undo = %q, want %q — the leading space rides with its word", got, "one")
	}
}

// TestPasteIntoInsertIsOneUndoStep: a bracketed paste mid-insert is exactly one
// change — one undo removes the block, no more and no less.
func TestPasteIntoInsertIsOneUndoStep(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "i")
	m.PasteText("hello world block")
	m = send(m, esc())
	if got := line(m, 0); got != "hello world blockabc" {
		t.Fatalf("pasted line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "abc" {
		t.Fatalf("one undo must remove exactly the block, got %q", got)
	}
	if len(m.HistoryTree()) != 2 {
		t.Fatalf("paste + esc must leave one change, tree has %d nodes", len(m.HistoryTree()))
	}
}

// TestPasteThenTypingUndoSeparately is the mixed case: characters typed after a
// paste undo on their own — even without forming a whole word — and the block
// only goes with the next undo.
func TestPasteThenTypingUndoSeparately(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "i")
	m.PasteText("BLOCK")
	m = typeKeys(m, "xy")
	m = send(m, esc())
	if got := line(m, 0); got != "BLOCKxy" {
		t.Fatalf("line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "BLOCK" {
		t.Fatalf("first undo = %q, want the block untouched (%q)", got, "BLOCK")
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "" {
		t.Fatalf("second undo = %q, want the block gone", got)
	}
}

// TestTypingAroundPasteKeepsThreeSteps: type, paste, type — three changes that
// undo (and redo) in order.
func TestTypingAroundPasteKeepsThreeSteps(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo ")
	m.PasteText("BAR")
	m = typeKeys(m, "baz")
	m = send(m, esc())
	if got := line(m, 0); got != "foo BARbaz" {
		t.Fatalf("line = %q", got)
	}
	for _, want := range []string{"foo BAR", "foo ", ""} {
		m = send(m, key('u'))
		if got := line(m, 0); got != want {
			t.Fatalf("undo landed on %q, want %q", got, want)
		}
	}
	for _, want := range []string{"foo ", "foo BAR", "foo BARbaz"} {
		m, _ = m.Update(modKey('r', tea.ModCtrl))
		if got := line(m, 0); got != want {
			t.Fatalf("redo landed on %q, want %q", got, want)
		}
	}
}

// TestClipboardPasteIntoInsertIsOwnStep: Cmd+V mid-insert splits the session
// the same way the terminal's bracketed paste does.
func TestClipboardPasteIntoInsertIsOwnStep(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "iab")
	m.regs.Yank('+', register.Entry{Text: "PASTED"})
	m.clipboardPaste()
	m = typeKeys(m, "cd")
	m = send(m, esc())
	if got := line(m, 0); got != "abPASTEDcd" {
		t.Fatalf("line = %q", got)
	}
	for _, want := range []string{"abPASTED", "ab", ""} {
		m = send(m, key('u'))
		if got := line(m, 0); got != want {
			t.Fatalf("undo landed on %q, want %q", got, want)
		}
	}
}

// TestNormalModeChangeKeepsOneUndoUnit: `ciw` plus the word typed into it stays
// a single change — normal-mode operations keep their old semantics.
func TestNormalModeChangeKeepsOneUndoUnit(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = typeKeys(m, "ciwbye")
	m = send(m, esc())
	if got := line(m, 0); got != "bye world" {
		t.Fatalf("line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "hello world" {
		t.Fatalf("one undo must revert the whole change, got %q", got)
	}
	if len(m.HistoryTree()) != 2 {
		t.Fatalf("ciw must leave one change, tree has %d nodes", len(m.HistoryTree()))
	}
}

// TestInsertBreaksPushNoEmptyChange: an insert session that ends right after a
// paste (or without typing at all) pushes no empty or duplicated change.
func TestInsertBreaksPushNoEmptyChange(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = typeKeys(m, "i")
	m = send(m, esc()) // nothing typed at all
	if n := len(m.HistoryTree()); n != 1 {
		t.Fatalf("an empty insert must push nothing, tree has %d nodes", n)
	}
	if m.Dirty() {
		t.Fatal("an empty insert must not dirty the buffer")
	}
	m = typeKeys(m, "i")
	m.PasteText("XY")
	m = send(m, esc()) // the trailing break already committed the paste
	if n := len(m.HistoryTree()); n != 2 {
		t.Fatalf("paste + esc must leave exactly one change, tree has %d nodes", n)
	}
}

// TestInsertWordUndoBranchesTree: undo + typing branches the undo tree (#59) on
// the finer boundaries too — the abandoned word stays reachable via g-.
func TestInsertWordUndoBranchesTree(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo bar")
	m = send(m, esc()) // seq 1 "foo ", seq 2 "foo bar"
	m = send(m, key('u'))
	if got := line(m, 0); got != "foo " {
		t.Fatalf("undo = %q, want %q", got, "foo ")
	}
	m = typeKeys(m, "Abaz")
	m = send(m, esc()) // seq 3 "foo baz", sibling of seq 2
	if got := line(m, 0); got != "foo baz" {
		t.Fatalf("line = %q", got)
	}
	if n := len(m.HistoryTree()); n != 4 {
		t.Fatalf("tree has %d nodes, want 4 (root + two words + the branch)", n)
	}
	m = typeKeys(m, "g-")
	if got := line(m, 0); got != "foo bar" {
		t.Fatalf("g- landed on %q, want the abandoned branch %q", got, "foo bar")
	}
	m = typeKeys(m, "g+")
	if got := line(m, 0); got != "foo baz" {
		t.Fatalf("g+ landed on %q, want %q", got, "foo baz")
	}
}

// TestPersistentUndoKeepsInsertSegments: the finer changes survive a restart
// through the persistent undo store (#148) — the reloaded history undoes word
// by word, not in one lump.
func TestPersistentUndoKeepsInsertSegments(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('A'))
	m = typeKeys(m, " two three")
	m = send(m, esc())
	m, _ = m.Update(ActionMsg{Action: "write"})

	restored := New()
	if err := restored.Load(path); err != nil {
		t.Fatal(err)
	}
	restored.SetSize(80, 20)
	restored = send(restored, key('u'))
	if got := line(restored, 0); got != "one two " {
		t.Fatalf("restored first undo = %q, want %q", got, "one two ")
	}
	restored = send(restored, key('u'))
	if got := line(restored, 0); got != "one" {
		t.Fatalf("restored second undo = %q, want %q", got, "one")
	}
}

// --- finer boundaries: pause, newline, accepts (#2189) ----------------------

// withTypeClock installs a fake insert clock on m and returns the advance func.
func withTypeClock(m *Model) func(d time.Duration) {
	now := time.Unix(1000, 0)
	m.typeNow = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// TestInsertPauseSplitsUndo: a gap of insertPauseBreak between two typed runes
// closes the segment, so undo peels off only what was typed since the pause —
// even inside one uninterrupted word run — and redo mirrors the same steps.
func TestInsertPauseSplitsUndo(t *testing.T) {
	m, _ := loaded(t, "\n")
	advance := withTypeClock(&m)
	m = typeKeys(m, "iabc")
	advance(insertPauseBreak)
	m = typeKeys(m, "def")
	m = send(m, esc())
	if got := line(m, 0); got != "abcdef" {
		t.Fatalf("typed line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "abc" {
		t.Fatalf("undo landed on %q, want the pre-pause text %q", got, "abc")
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "" {
		t.Fatalf("second undo landed on %q, want empty", got)
	}
	for _, want := range []string{"abc", "abcdef"} {
		m, _ = m.Update(modKey('r', tea.ModCtrl))
		if got := line(m, 0); got != want {
			t.Fatalf("redo landed on %q, want %q", got, want)
		}
	}
}

// TestInsertShortGapDoesNotSplit: a gap below the pause threshold keeps the
// word run one unit.
func TestInsertShortGapDoesNotSplit(t *testing.T) {
	m, _ := loaded(t, "\n")
	advance := withTypeClock(&m)
	m = typeKeys(m, "iabc")
	advance(insertPauseBreak / 2)
	m = typeKeys(m, "def")
	m = send(m, esc())
	m = send(m, key('u'))
	if got := line(m, 0); got != "" {
		t.Fatalf("one undo must remove the whole run, got %q", got)
	}
}

// TestInsertPauseKeepsStructuralChangeJoined: thinking before the first typed
// rune never tears the structural edit (the `ciw` deletion) off the typing —
// only elapsed time between typed runes of one segment counts.
func TestInsertPauseKeepsStructuralChangeJoined(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	advance := withTypeClock(&m)
	m = typeKeys(m, "ciw")
	advance(10 * insertPauseBreak)
	m = typeKeys(m, "bye")
	m = send(m, esc())
	if got := line(m, 0); got != "bye world" {
		t.Fatalf("line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "hello world" {
		t.Fatalf("one undo must revert the whole change, got %q", got)
	}
	if n := len(m.HistoryTree()); n != 2 {
		t.Fatalf("cw + typing must stay one change, tree has %d nodes", n)
	}
}

// TestInsertNewlineClosesSegment: Enter is a boundary of its own (#2189) — the
// break rides with the text typed before it, and the next line's text undoes
// separately.
func TestInsertNewlineClosesSegment(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ifoo")
	m = send(m, special(tea.KeyEnter))
	m = typeKeys(m, "bar")
	m = send(m, esc())
	if line(m, 0) != "foo" || line(m, 1) != "bar" {
		t.Fatalf("buffer = %q/%q", line(m, 0), line(m, 1))
	}
	m = send(m, key('u'))
	if line(m, 0) != "foo" || m.buf.LineCount() != 2 || line(m, 1) != "" {
		t.Fatalf("first undo must remove only %q: %q/%q (%d lines)",
			"bar", line(m, 0), line(m, 1), m.buf.LineCount())
	}
	m = send(m, key('u'))
	if line(m, 0) != "" || m.buf.LineCount() != 1 {
		t.Fatalf("second undo must remove the word and its line break: %q (%d lines)",
			line(m, 0), m.buf.LineCount())
	}
}

// TestCompletionAcceptIsOwnUndoStep: an accepted completion is one unit — undo
// removes exactly the accepted text and restores the typed prefix (#2189).
func TestCompletionAcceptIsOwnUndoStep(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "iPr")
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	m = send(m, tab()) // accept
	if got := line(m, 0); got != "Println" {
		t.Fatalf("accepted line = %q", got)
	}
	m = send(m, esc())
	m = send(m, key('u'))
	if got := line(m, 0); got != "Pr" {
		t.Fatalf("undo must restore the typed prefix, got %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "" {
		t.Fatalf("second undo must remove the prefix, got %q", got)
	}
}

// TestSnippetExpansionIsOwnUndoStep: a Tab-triggered live template is one unit —
// undo removes the expanded body and restores the trigger word (#2189).
func TestSnippetExpansionIsOwnUndoStep(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{{Trigger: "pair", Body: "left($1) right($2)"}})
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "ipair")
	m = send(m, tab())
	if got := line(m, 0); got != "left() right()" {
		t.Fatalf("expansion = %q", got)
	}
	m = send(m, esc())
	m = send(m, key('u'))
	if got := line(m, 0); got != "pair" {
		t.Fatalf("undo must restore the trigger word, got %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 0); got != "" {
		t.Fatalf("second undo must remove the trigger word, got %q", got)
	}
}

// TestMacroReplayKeepsWordWiseUnits: a replayed macro feeds keys through the
// same typing path, so its insert splits at the same word boundaries.
func TestMacroReplayKeepsWordWiseUnits(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m = typeKeys(m, "qaA1 2")
	m = send(m, esc())
	m = typeKeys(m, "q") // stop recording
	m = typeKeys(m, "j@a")
	if line(m, 0) != "one1 2" || line(m, 1) != "two1 2" {
		t.Fatalf("buffer = %q/%q", line(m, 0), line(m, 1))
	}
	for _, want := range []string{"two1 ", "two", "one1 "} {
		m = send(m, key('u'))
		got := line(m, 1)
		if want == "one1 " {
			got = line(m, 0)
		}
		if got != want {
			t.Fatalf("undo landed on %q, want %q", got, want)
		}
	}
}

// TestDotRepeatedInsertIsOneUnit: "." replays the whole insert as a single
// change regardless of how the original session was split.
func TestDotRepeatedInsertIsOneUnit(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\n")
	m = typeKeys(m, "Afoo bar")
	m = send(m, esc())
	m = typeKeys(m, "j.")
	if got := line(m, 1); got != "twofoo bar" {
		t.Fatalf("dot line = %q", got)
	}
	m = send(m, key('u'))
	if got := line(m, 1); got != "two" {
		t.Fatalf("one undo must revert the whole repeat, got %q", got)
	}
	// The original insert keeps its word-wise steps.
	m = send(m, key('u'))
	if got := line(m, 0); got != "onefoo " {
		t.Fatalf("undo landed on %q, want %q", got, "onefoo ")
	}
}
