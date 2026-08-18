package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/watch"
)

// follow_test.go covers follow ("tail -f") mode (#1928): streaming appends,
// partial-line continuation, pause/resume of the auto-scroll, truncation and
// rotation handling, and the read-only guarantee.

// following toggles follow mode on m via the palette action and returns the
// updated model.
func following(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if !m.Following() {
		t.Fatalf("toggle_follow must enable follow mode (cmdMsg %q)", m.cmdMsg)
	}
	return m
}

// appendToFile appends text to path on disk.
func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// changedEvent delivers a FileChanged watcher event for path.
func changedEvent(m Model, path string) (Model, tea.Cmd) {
	return m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
}

// TestFollowStreamsAppends: appended lines stream into the buffer, the buffer
// is read-only, and the cursor/viewport stick to the end.
func TestFollowStreamsAppends(t *testing.T) {
	m, path := loaded(t, "one\ntwo\n")
	m = following(t, m)
	if !m.ReadOnly() {
		t.Fatal("follow mode must make the buffer read-only")
	}
	appendToFile(t, path, "three\nfour\n")
	m, _ = changedEvent(m, path)
	if m.buf.LineCount() != 4 || line(m, 2) != "three" || line(m, 3) != "four" {
		t.Fatalf("appended lines must stream in, got %q", m.buf.Lines())
	}
	if m.cursor.Line != 3 {
		t.Fatalf("cursor must follow to the last line, got %d", m.cursor.Line)
	}
	if m.FollowPaused() {
		t.Fatal("streaming an append must not pause follow")
	}
	if m.dirty {
		t.Fatal("streamed appends must not mark the buffer dirty")
	}
}

// TestFollowContinuesPartialLines: an append without a trailing newline shows
// as a partial line that the next append continues in place.
func TestFollowContinuesPartialLines(t *testing.T) {
	m, path := loaded(t, "a\n")
	m = following(t, m)
	appendToFile(t, path, "par")
	m, _ = changedEvent(m, path)
	if got := line(m, 1); got != "par" {
		t.Fatalf("partial line must show, got %q", got)
	}
	appendToFile(t, path, "tial\nnext\n")
	m, _ = changedEvent(m, path)
	if line(m, 1) != "partial" || line(m, 2) != "next" {
		t.Fatalf("continuation must merge in place, got %q", m.buf.Lines())
	}
}

// TestFollowEmptyFileFirstAppend: following an empty file streams the first
// line into the single empty buffer line, not below it.
func TestFollowEmptyFileFirstAppend(t *testing.T) {
	m, path := loaded(t, "")
	m = following(t, m)
	appendToFile(t, path, "first\n")
	m, _ = changedEvent(m, path)
	if m.buf.LineCount() != 1 || line(m, 0) != "first" {
		t.Fatalf("first append into an empty file must fill line 0, got %q", m.buf.Lines())
	}
}

// TestFollowPauseAndResume: moving up pauses the auto-scroll (visible via
// FollowPaused/FollowLabel), jumping back to the end resumes it.
func TestFollowPauseAndResume(t *testing.T) {
	m, path := loaded(t, strings.Repeat("line\n", 30))
	m = following(t, m)
	m = send(m, key('k'))
	if !m.FollowPaused() {
		t.Fatal("moving the cursor up must pause follow")
	}
	if got := m.FollowLabel(); got != "FOLLOW (paused)" {
		t.Fatalf("paused label = %q", got)
	}
	pausedCursor, pausedTop := m.cursor.Line, m.view.Top
	appendToFile(t, path, "tail\n")
	m, _ = changedEvent(m, path)
	if m.cursor.Line != pausedCursor || m.view.Top != pausedTop {
		t.Fatal("a paused view must not auto-scroll on append")
	}
	if line(m, m.buf.LineCount()-1) != "tail" {
		t.Fatal("appends must keep streaming while paused")
	}
	m = send(m, key('G'))
	if m.FollowPaused() {
		t.Fatal("jumping to the end must resume follow")
	}
	appendToFile(t, path, "more\n")
	m, _ = changedEvent(m, path)
	if m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatal("a resumed view must auto-scroll again")
	}
}

// TestFollowWheelScrollPauses: a wheel scroll away from the end pauses,
// scrolling back down resumes.
func TestFollowWheelScrollPauses(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("line\n", 40))
	m = following(t, m)
	m.ScrollBy(-5)
	if !m.FollowPaused() {
		t.Fatal("a wheel scroll up must pause follow")
	}
	m.ScrollBy(50)
	m.followToEnd() // the cursor stayed at the end; the wheel only moved the view
	m.refreshFollowPause()
	if m.FollowPaused() {
		t.Fatal("scrolling back to the end must resume follow")
	}
}

// TestFollowTruncationReloads: a file that shrank reloads wholesale with a
// rotation notice, and follow keeps working from the new content.
func TestFollowTruncationReloads(t *testing.T) {
	m, path := loaded(t, "one\ntwo\nthree\n")
	m = following(t, m)
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := changedEvent(m, path)
	if m.buf.LineCount() != 1 || line(m, 0) != "fresh" {
		t.Fatalf("truncation must reload the buffer, got %q", m.buf.Lines())
	}
	if !m.Following() {
		t.Fatal("truncation must not end follow mode")
	}
	if got := noticeIn(t, cmd); !strings.Contains(got, "truncated") {
		t.Fatalf("truncation must toast, got %q", got)
	}
	appendToFile(t, path, "next\n")
	m, _ = changedEvent(m, path)
	if line(m, 1) != "next" {
		t.Fatalf("appends after a truncation reload must stream, got %q", m.buf.Lines())
	}
}

// TestFollowRotationRemoveCreate: logrotate-style remove + create reloads
// from the replacement file instead of appending into stale offsets.
func TestFollowRotationRemoveCreate(t *testing.T) {
	m, path := loaded(t, "old one\nold two\n")
	m = following(t, m)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	if !m.Following() {
		t.Fatal("a removed file must keep follow armed for the replacement")
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: path})
	if m.buf.LineCount() != 1 || line(m, 0) != "fresh" {
		t.Fatalf("rotation must reload from the new file, got %q", m.buf.Lines())
	}
	if got := noticeIn(t, cmd); !strings.Contains(got, "rotated") {
		t.Fatalf("rotation must toast, got %q", got)
	}
	appendToFile(t, path, "next\n")
	m, _ = changedEvent(m, path)
	if line(m, 1) != "next" {
		t.Fatalf("appends after a rotation must stream, got %q", m.buf.Lines())
	}
}

// TestFollowRotationBeforeChangeEvent: a remove coalesced into a plain change
// (the app rewrites removed-then-recreated to FileChanged) still reloads.
func TestFollowRotationBeforeChangeEvent(t *testing.T) {
	m, path := loaded(t, "old one\nold two\n")
	m = following(t, m)
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	if err := os.WriteFile(path, []byte("longer than the old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changedEvent(m, path)
	if m.buf.LineCount() != 1 || line(m, 0) != "longer than the old content" {
		t.Fatalf("a change after a remove must reload wholesale, got %q", m.buf.Lines())
	}
}

// TestFollowImpliesReadOnly: edits are refused while following; toggling off
// restores writability and reports on the ex line.
func TestFollowImpliesReadOnly(t *testing.T) {
	m, _ := loaded(t, "one\n")
	m = following(t, m)
	m = send(m, key('i'))
	if m.mode != Normal {
		t.Fatal("insert mode must be refused while following")
	}
	if m.cmdMsg != roMessage {
		t.Fatalf("refusal must land on the ex line, got %q", m.cmdMsg)
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if m.Following() || m.ReadOnly() {
		t.Fatal("toggling off must end follow and restore writability")
	}
}

// TestFollowRefusesDirtyBuffer: unsaved edits and streaming appends must
// never interleave.
func TestFollowRefusesDirtyBuffer(t *testing.T) {
	m, _ := loaded(t, "one\n")
	m = typeKeys(send(m, key('i')), "x")
	m = send(m, special(tea.KeyEscape))
	if !m.dirty {
		t.Fatal("setup: the buffer must be dirty")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if m.Following() {
		t.Fatal("follow must refuse a dirty buffer")
	}
}

// TestFollowIgnoresOtherPaths: events for other files never touch the
// followed buffer.
func TestFollowIgnoresOtherPaths(t *testing.T) {
	m, _ := loaded(t, "one\n")
	m = following(t, m)
	before := m.docVersion
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: "/elsewhere/other.log"})
	if m.docVersion != before {
		t.Fatal("an event for another path must be ignored")
	}
}

// TestFollowStopsOnLoad: loading another file into the view ends follow mode.
func TestFollowStopsOnLoad(t *testing.T) {
	m, path := loaded(t, "one\n")
	m = following(t, m)
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if m.Following() || m.ReadOnly() {
		t.Fatal("loading a file must reset follow mode")
	}
}

// TestFollowExtendsLogRunsIncrementally: appended lines extend the cached
// repeat runs from the tail instead of a whole-buffer rescan, and the result
// matches a full recompute over the same content.
func TestFollowExtendsLogRunsIncrementally(t *testing.T) {
	m := logLoaded(t, repeatDoc)
	st := m.logRuns() // populate the cache the append will extend
	// A whole-buffer rescan rebuilds head/end from scratch; the incremental
	// extension only touches the tail. The sentinel below a real line index
	// survives only the incremental path.
	st.head[-99] = -99
	st.end[-99] = -99
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if !m.Following() {
		t.Fatal("setup: follow must engage on the log buffer")
	}
	tail := `2026-08-06T19:08:13.290797001Z time="2026-08-06T19:08:13Z" level=info msg="Session started"
2026-08-06T19:08:43.274874796Z time="2026-08-06T19:08:43Z" level=info msg="Session started"

2026-08-06T19:09:13.303770698Z time="2026-08-06T19:09:13Z" level=info msg="Cleanup"
`
	appendToFile(t, m.path, tail)
	m, _ = changedEvent(m, m.path)
	got := m.logRuns()
	if got.version != m.docVersion || got.appendFrom != 0 {
		t.Fatal("logRuns must consume the hint and stamp the version")
	}
	if _, ok := got.head[-99]; !ok {
		t.Fatal("the append must extend the cached runs, not rescan the buffer")
	}
	delete(got.head, -99)
	delete(got.end, -99)
	fresh := logLoaded(t, repeatDoc+tail)
	want := fresh.logRuns()
	if len(got.head) != len(want.head) || len(got.end) != len(want.end) {
		t.Fatalf("incremental runs differ from a full scan: head %v vs %v, end %v vs %v",
			got.head, want.head, got.end, want.end)
	}
	for l, h := range want.head {
		if got.head[l] != h {
			t.Errorf("head[%d] = %d, want %d", l, got.head[l], h)
		}
	}
	for h, e := range want.end {
		if got.end[h] != e {
			t.Errorf("end[%d] = %d, want %d", h, got.end[h], e)
		}
	}
	// The appended "Session started" pair must extend line 3 into a run.
	if want.end[3] != 5 {
		t.Fatalf("fixture: line 3 must head a run to 5, got %v", want.end)
	}
}

// TestFollowExtendLogRunsAfterMerge: continuing an unterminated tail line
// rescans that line too, so a changed key breaks (or joins) runs correctly.
func TestFollowExtendLogRunsAfterMerge(t *testing.T) {
	base := `2026-08-06T19:06:13Z level=info msg="poll ok"
2026-08-06T19:06:43Z level=info msg="poll ok"
2026-08-06T19:07:13Z level=info msg="poll`
	m := logLoaded(t, base)
	m.logRuns()
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	appendToFile(t, m.path, " ok\"\n2026-08-06T19:07:43Z level=info msg=\"poll ok\"\n")
	m, _ = changedEvent(m, m.path)
	got := m.logRuns()
	fresh := logLoaded(t, base+" ok\"\n2026-08-06T19:07:43Z level=info msg=\"poll ok\"\n")
	want := fresh.logRuns()
	if len(got.head) != len(want.head) {
		t.Fatalf("merged-line runs differ from a full scan: %v vs %v", got.head, want.head)
	}
	for l, h := range want.head {
		if got.head[l] != h {
			t.Errorf("head[%d] = %d, want %d", l, got.head[l], h)
		}
	}
	// The completed line joins the run: 0 heads it through line 3.
	if want.end[0] != 3 {
		t.Fatalf("fixture: line 0 must head a run to 3, got %v", want.end)
	}
}

// TestSplitIncompleteTail: a lone CR and a split multi-byte rune are held
// back, complete tails pass through whole.
func TestSplitIncompleteTail(t *testing.T) {
	cases := []struct {
		in   string
		use  string
		held string
	}{
		{"plain\n", "plain\n", ""},
		{"half\r", "half", "\r"},
		{"snowman ☃", "snowman ☃", ""},
		{"cut \xe2\x98", "cut ", "\xe2\x98"},
		{"", "", ""},
	}
	for _, c := range cases {
		use, held := splitIncompleteTail([]byte(c.in))
		if string(use) != c.use || string(held) != c.held {
			t.Errorf("splitIncompleteTail(%q) = %q, %q; want %q, %q",
				c.in, use, held, c.use, c.held)
		}
	}
}
