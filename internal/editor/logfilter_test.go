package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/histories"
	"ike/internal/watch"
)

// logfilter_test.go covers the live filter/highlight over a followed buffer
// (#2255): narrowing existing content and new appends, the highlight-only
// mode, the regex toggle with its inline error, the follow/pause semantics
// over the filtered tail, and the merged rotation set sharing the pipeline.

// filtering opens the follow-filter line on m (hiding mode) and types pattern
// into it, without committing — the live state is what the view shows.
func filtering(t *testing.T, m Model, action, pattern string) Model {
	t.Helper()
	m, _ = m.Update(ActionMsg{Action: action})
	if !m.filtering {
		t.Fatalf("%s must open the filter line (cmdMsg %q)", action, m.cmdMsg)
	}
	return send(m, keys(pattern)...)
}

// visibleLines lists the buffer lines the view still shows.
func visibleLines(m Model) []string {
	var out []string
	for i := 0; i < m.buf.LineCount(); i++ {
		if !m.lineHidden(i) {
			out = append(out, m.buf.Line(i))
		}
	}
	return out
}

// followedLog loads a small log, enters follow mode and returns view and path.
func followedLog(t *testing.T, content string) (Model, string) {
	t.Helper()
	m, path := loaded(t, content)
	return following(t, m), path
}

// TestFollowFilterHidesNonMatchingLines: a typed pattern narrows the view
// live, leaving the buffer itself untouched.
func TestFollowFilterHidesNonMatchingLines(t *testing.T) {
	m, _ := followedLog(t, "info start\nerror boom\ninfo idle\nerror again\n")
	m = filtering(t, m, "follow_filter", "error")
	if got := visibleLines(m); len(got) != 2 || got[0] != "error boom" || got[1] != "error again" {
		t.Fatalf("only matching lines must stay visible, got %q", got)
	}
	if m.buf.LineCount() != 4 {
		t.Fatalf("the filter is view-level: the buffer keeps every line, got %d", m.buf.LineCount())
	}
	if m.cursor.Line != 3 {
		t.Fatalf("the auto-scroll must stick to the filtered tail (line 3), got %d", m.cursor.Line)
	}
}

// TestFollowFilterAppliesToAppends: lines streamed in while the filter is
// active are filtered too, and the view follows the filtered tail.
func TestFollowFilterAppliesToAppends(t *testing.T) {
	m, path := followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	appendToFile(t, path, "info three\nerror four\ninfo five\n")
	m, _ = changedEvent(m, path)
	if got := visibleLines(m); len(got) != 2 || got[1] != "error four" {
		t.Fatalf("appended lines must be filtered too, got %q", got)
	}
	if m.cursor.Line != 3 {
		t.Fatalf("the cursor must follow the filtered tail (line 3), got %d", m.cursor.Line)
	}
	if m.FollowPaused() {
		t.Fatal("streaming into a filtered view must not pause the follow")
	}
}

// TestFollowFilterClearRestoresEverything: an emptied pattern brings the whole
// stream back, and so does the explicit clear command.
func TestFollowFilterClearRestoresEverything(t *testing.T) {
	m, _ := followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_filter", "error")
	if len(visibleLines(m)) != 1 {
		t.Fatalf("the filter must narrow first, got %q", visibleLines(m))
	}
	m = send(m, special(tea.KeyBackspace), special(tea.KeyBackspace),
		special(tea.KeyBackspace), special(tea.KeyBackspace), special(tea.KeyBackspace))
	if got := visibleLines(m); len(got) != 2 {
		t.Fatalf("an emptied pattern must restore every line, got %q", got)
	}

	m, _ = followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.Update(ActionMsg{Action: "follow_filter_clear"})
	if got := visibleLines(m); len(got) != 2 {
		t.Fatalf("view.clearFollowFilter must restore every line, got %q", got)
	}
	if m.FollowFilterLabel() != "" {
		t.Fatalf("a cleared filter shows no badge, got %q", m.FollowFilterLabel())
	}
}

// TestFollowFilterEscapeRestoresPreviousFilter: Esc on the filter line puts
// the filter that was active when it opened back, like Esc on a search line.
func TestFollowFilterEscapeRestoresPreviousFilter(t *testing.T) {
	m, _ := followedLog(t, "error one\ninfo two\nwarn three\n")
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = filtering(t, m, "follow_filter", "warn")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := visibleLines(m); len(got) != 1 || got[0] != "error one" {
		t.Fatalf("Esc must restore the committed filter, got %q", got)
	}
}

// TestFollowFilterRegexToggle: the \v marker switches the pattern to a regex,
// which the ctrl+r toggle writes into the line.
func TestFollowFilterRegexToggle(t *testing.T) {
	m, _ := followedLog(t, "code 404\ncode ok\ncode 500\n")
	m = filtering(t, m, "follow_filter", `\vcode \d+`)
	if got := visibleLines(m); len(got) != 2 || got[0] != "code 404" {
		t.Fatalf("the regex must narrow to the numeric codes, got %q", got)
	}
	if !strings.Contains(m.FollowFilterLabel(), "~") {
		t.Fatalf("the badge must mark a regex pattern, got %q", m.FollowFilterLabel())
	}
	// ctrl+r drops the marker again: the same text as a literal matches nothing.
	m, _ = m.Update(modKey('r', tea.ModCtrl))
	if m.cmdline != `code \d+` {
		t.Fatalf("ctrl+r must remove the \\v marker, got %q", m.cmdline)
	}
	if got := visibleLines(m); len(got) != 0 {
		t.Fatalf("as a literal the pattern matches nothing, got %q", got)
	}
}

// TestFollowFilterBadRegexReportsInline: an invalid regex is reported, not
// silently demoted to a literal, and nothing is hidden while it stands.
func TestFollowFilterBadRegexReportsInline(t *testing.T) {
	m, _ := followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_filter", `\v(unclosed`)
	if m.logFilt.err == "" {
		t.Fatal("an invalid regex must report an error")
	}
	if got := visibleLines(m); len(got) != 2 {
		t.Fatalf("a broken pattern must leave the stream unfiltered, got %q", got)
	}
	if !strings.Contains(m.FollowFilterLabel(), m.logFilt.err) {
		t.Fatalf("the badge must carry the error, got %q", m.FollowFilterLabel())
	}
	if !strings.Contains(m.commandLineRow(), "missing closing )") {
		t.Fatalf("the filter line must show the error inline, got %q", m.commandLineRow())
	}
	// Completing the group fixes it live.
	m = send(m, key(')'))
	if m.logFilt.err != "" {
		t.Fatalf("a completed pattern must clear the error, got %q", m.logFilt.err)
	}
}

// TestFollowHighlightHidesNothing: highlight mode colours the matches and
// keeps every line visible.
func TestFollowHighlightHidesNothing(t *testing.T) {
	m, _ := followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_highlight", "error")
	if got := visibleLines(m); len(got) != 2 {
		t.Fatalf("highlight mode must hide nothing, got %q", got)
	}
	if m.FollowFiltering() {
		t.Fatal("highlight mode is not the hiding mode")
	}
	if spans := m.logFilterSpans(0); len(spans) != 1 || spans[0].Start != 0 || spans[0].End != 5 {
		t.Fatalf("the match must be spanned for highlighting, got %+v", spans)
	}
	if spans := m.logFilterSpans(1); len(spans) != 0 {
		t.Fatalf("a non-matching line carries no spans, got %+v", spans)
	}
	if !strings.HasPrefix(m.FollowFilterLabel(), "HIGHLIGHT") {
		t.Fatalf("the badge must name the highlight mode, got %q", m.FollowFilterLabel())
	}
}

// TestFollowHighlightPaintsMatches: the highlight reaches the rendered rows —
// the matched run is styled, the line's text itself untouched.
func TestFollowHighlightPaintsMatches(t *testing.T) {
	m, _ := followedLog(t, "error one\n")
	plain := m.View()
	m = filtering(t, m, "follow_highlight", "error")
	painted := m.View()
	if painted == plain {
		t.Fatal("highlight mode must change how the matching line renders")
	}
	if !strings.Contains(ansi.Strip(painted), "error one") {
		t.Fatalf("the line's text must survive the highlight:\n%s", painted)
	}
}

// TestFollowFilterPauseSemanticsIntact: scrolling away from the filtered tail
// pauses the follow, appends keep streaming without moving the view, and
// jumping back to the filtered tail resumes it.
func TestFollowFilterPauseSemanticsIntact(t *testing.T) {
	m, path := followedLog(t, "error one\ninfo a\ninfo b\nerror two\n")
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.FollowPaused() {
		t.Fatal("the filtered tail is the bottom: the follow must run")
	}
	// k steps one *visible* line up — off the filtered tail.
	m = send(m, key('k'))
	if !m.FollowPaused() {
		t.Fatalf("moving off the filtered tail must pause, cursor %d", m.cursor.Line)
	}
	if m.cursor.Line != 0 {
		t.Fatalf("k must skip the filtered-out lines, got line %d", m.cursor.Line)
	}
	appendToFile(t, path, "error three\n")
	m, _ = changedEvent(m, path)
	if m.cursor.Line != 0 {
		t.Fatalf("a paused view must not move with an append, got line %d", m.cursor.Line)
	}
	m = send(m, key('G'))
	if m.FollowPaused() {
		t.Fatalf("returning to the filtered tail must resume, cursor %d", m.cursor.Line)
	}
}

// TestFollowFilterBadgeCountsMatches: the badge counts matching lines and
// keeps counting over appends (the incremental extension of the cache).
func TestFollowFilterBadgeCountsMatches(t *testing.T) {
	m, path := followedLog(t, "error one\ninfo two\n")
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.FollowFilterLabel(); got != "FILTER error (1)" {
		t.Fatalf("badge must count the matches, got %q", got)
	}
	// A partial line completed by the next append: the count must not double
	// or lose the continued line.
	appendToFile(t, path, "error thr")
	m, _ = changedEvent(m, path)
	if got := m.FollowFilterLabel(); got != "FILTER error (2)" {
		t.Fatalf("a partial matching line counts once, got %q", got)
	}
	appendToFile(t, path, "ee\ninfo four\nerror five\n")
	m, _ = changedEvent(m, path)
	if got := m.FollowFilterLabel(); got != "FILTER error (3)" {
		t.Fatalf("the continued line must not be counted twice, got %q", got)
	}
	if want := m.countFilterMatches(0, m.buf.LineCount()); want != 3 {
		t.Fatalf("the incremental count must match a full scan, full scan says %d", want)
	}
	m = filtering(t, m, "follow_filter", "nothing-matches-this")
	if got := m.FollowFilterLabel(); !strings.Contains(got, "no matches") {
		t.Fatalf("a pattern without matches must say so, got %q", got)
	}
}

// TestFollowFilterOnMergedLogSet: a merged rotation set (#1996) shares the
// follow pipeline, so it filters the same way — over its older members too.
func TestFollowFilterOnMergedLogSet(t *testing.T) {
	m, src := mergedView(t, "live error\nlive noise\n")
	m = following(t, m)
	m = filtering(t, m, "follow_filter", "error")
	if got := visibleLines(m); len(got) != 1 || got[0] != "live error" {
		t.Fatalf("the merged timeline must filter like any followed buffer, got %q", got)
	}
	appendToFile(t, src, "later error\n")
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: src})
	if got := visibleLines(m); len(got) != 2 || got[1] != "later error" {
		t.Fatalf("appends into the set must be filtered too, got %q", got)
	}
}

// TestFollowFilterNeedsFollowMode: the filter is follow-mode state — it
// refuses to open elsewhere, and leaving follow mode drops it.
func TestFollowFilterNeedsFollowMode(t *testing.T) {
	m, _ := loaded(t, "error one\ninfo two\n")
	m, _ = m.Update(ActionMsg{Action: "follow_filter"})
	if m.filtering {
		t.Fatal("the filter line must not open without follow mode")
	}
	if !strings.Contains(m.cmdMsg, "follow mode") {
		t.Fatalf("the refusal must say what is missing, got %q", m.cmdMsg)
	}
	m = following(t, m)
	m = filtering(t, m, "follow_filter", "error")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if m.Following() {
		t.Fatal("toggle_follow must leave follow mode")
	}
	if got := visibleLines(m); len(got) != 2 {
		t.Fatalf("leaving follow mode must restore the whole buffer, got %q", got)
	}
	if m.FollowFilterLabel() != "" {
		t.Fatalf("no badge outside follow mode, got %q", m.FollowFilterLabel())
	}
}

// TestFollowFilterRecallsItsOwnHistory: committed patterns land in the
// filter's recall bucket, not the search or ex one, and up re-applies them.
func TestFollowFilterRecallsItsOwnHistory(t *testing.T) {
	store := &histories.Store{}
	m, _ := followedLog(t, "error one\ninfo two\nwarn three\n")
	m.SetHistories(store)
	m = filtering(t, m, "follow_filter", "warn")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := store.All(histories.FollowFilter); len(got) != 1 || got[0] != "warn" {
		t.Fatalf("the committed pattern must land in the filter bucket, got %q", got)
	}
	if got := store.All(histories.Search); len(got) != 0 {
		t.Fatalf("the search bucket must stay untouched, got %q", got)
	}
	// Reopening prefills the active pattern, selected; one backspace clears
	// it, and up recalls the committed one and re-applies it live.
	m = filtering(t, m, "follow_filter", "")
	m = send(m, special(tea.KeyBackspace))
	if m.cmdline != "" {
		t.Fatalf("backspace must clear the prefilled selection, got %q", m.cmdline)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cmdline != "warn" {
		t.Fatalf("up must recall the filter pattern, got %q", m.cmdline)
	}
	if got := visibleLines(m); len(got) != 1 || got[0] != "warn three" {
		t.Fatalf("a recalled pattern must apply live, got %q", got)
	}
}

// TestFollowFilterLinePrefix: the filter line announces its kind, so it is
// never mistaken for a search or an ex command.
func TestFollowFilterLinePrefix(t *testing.T) {
	m, _ := followedLog(t, "error one\n")
	m = filtering(t, m, "follow_filter", "err")
	if got := m.CommandLine(); got != "|err" {
		t.Fatalf("the filter line reads |pattern, got %q", got)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = filtering(t, m, "follow_highlight", "err")
	if got := m.CommandLine(); got != "*err" {
		t.Fatalf("the highlight line reads *pattern, got %q", got)
	}
}
