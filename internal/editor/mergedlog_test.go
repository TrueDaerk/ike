package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/watch"
)

// mergedlog_test.go covers the editor half of the merged rotated log set
// (#1996): the buffer is read-only, follow mode tails the set's newest member
// instead of the view's own (virtual) path, and the reload cases of #1928 turn
// into a re-merge request rather than replacing the timeline with one file.

// mergedView builds a view holding a merged timeline of two files: the live
// log src (returned) and one rotated member above it.
func mergedView(t *testing.T, live string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "app.log")
	if err := os.WriteFile(src, []byte(live), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.SetSize(80, 20)
	m.SetFocused(true)
	text := "──── app.log.1 ────\nold line\n──── app.log ────\n" + live
	m.ShowMergedLog(src+"!merged/app.log", text, src, int64(len(live)),
		strings.HasSuffix(live, "\n"))
	return m, src
}

// TestMergedLogBufferIsReadOnly: the timeline has no writable home on disk.
func TestMergedLogBufferIsReadOnly(t *testing.T) {
	m, _ := mergedView(t, "live one\n")
	if !m.MergedLog() {
		t.Fatal("the view must report itself as a merged set")
	}
	if !m.ReadOnly() {
		t.Fatal("a merged timeline is read-only")
	}
	before := m.Text()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Text() != before {
		t.Fatalf("the buffer was edited: %q", m.Text())
	}
}

// TestMergedLogFollowStreamsFromTheNewestMember: follow mode tails the set's
// newest member and appends to the end of the timeline, where that member's
// lines already sit — the older regions stay untouched.
func TestMergedLogFollowStreamsFromTheNewestMember(t *testing.T) {
	m, src := mergedView(t, "live one\n")
	m = following(t, m)
	if got := m.FollowSource(); got != src {
		t.Fatalf("follow source = %q, want the newest member %q", got, src)
	}
	appendToFile(t, src, "live two\n")
	m, _ = changedEvent(m, src)
	if got := m.Text(); !strings.HasSuffix(got, "live one\nlive two") {
		t.Fatalf("the append must land at the timeline's end: %q", got)
	}
	if !strings.Contains(m.Text(), "old line") {
		t.Fatal("streaming must not drop the older regions")
	}
	if m.cursor.Line != m.buf.LineCount()-1 {
		t.Fatalf("the cursor must follow to the end, line %d of %d", m.cursor.Line, m.buf.LineCount())
	}
}

// TestMergedLogFollowNeedsAnAnchor: a set whose newest member is compressed
// offers no byte offset to resume from, so follow refuses instead of tailing
// bytes that are not the buffer's.
func TestMergedLogFollowNeedsAnAnchor(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	m.ShowMergedLog("/tmp/app.log!merged/app.log", "──── app.log.gz ────\nold\n", "", 0, true)
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if m.Following() {
		t.Fatal("a set with no follow anchor must not enter follow mode")
	}
	if !strings.Contains(m.cmdMsg, "compressed") {
		t.Fatalf("the refusal must say why, got %q", m.cmdMsg)
	}
}

// TestMergedLogRotationAsksForAReMerge: the rotated file's lines belong after
// the ones the buffer holds, which no append can express — so the view asks
// the root model for a fresh merge and parks until it lands.
func TestMergedLogRotationAsksForAReMerge(t *testing.T) {
	m, src := mergedView(t, "live one\n")
	m = following(t, m)
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: src})
	if !m.Following() {
		t.Fatal("a removed source must keep follow armed for the replacement")
	}
	if err := os.WriteFile(src, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: src})
	if !mergeRequested(cmd, m.Path()) {
		t.Fatal("a rotation must ask for a re-merge of the whole set")
	}
	if got := m.Text(); !strings.Contains(got, "old line") || strings.Contains(got, "fresh") {
		t.Fatalf("the buffer must wait for the merge, got %q", got)
	}
	// Parked: further events are ignored until the merge lands.
	appendToFile(t, src, "more\n")
	m, cmd = changedEvent(m, src)
	if cmd != nil || strings.Contains(m.Text(), "more") {
		t.Fatalf("events must be parked while the re-merge runs: %q", m.Text())
	}
	// The merge lands: content replaced, follow still armed and anchored.
	text := "──── app.log.1 ────\nlive one\n──── app.log ────\nfresh\nmore\n"
	m.ShowMergedLog(m.Path(), text, src, int64(len("fresh\nmore\n")), true)
	if !m.Following() || !m.ReadOnly() {
		t.Fatal("a re-merge must keep the view following, read-only")
	}
	appendToFile(t, src, "after\n")
	m, _ = changedEvent(m, src)
	if !strings.HasSuffix(m.Text(), "more\nafter") {
		t.Fatalf("appends after a rotation must stream, got %q", m.Text())
	}
}

// TestMergedLogTruncationAsksForAReMerge: copytruncate rotation — the source
// shrank below the anchor, which for a timeline also means re-reading the set.
func TestMergedLogTruncationAsksForAReMerge(t *testing.T) {
	m, src := mergedView(t, "live one\nlive two\n")
	m = following(t, m)
	if err := os.WriteFile(src, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := changedEvent(m, src)
	if !mergeRequested(cmd, m.Path()) {
		t.Fatal("a truncated source must ask for a re-merge")
	}
}

// TestMergedLogStopFollowKeepsTheTimeline: leaving follow mode must not
// reconcile against the virtual path — there is no file there.
func TestMergedLogStopFollowKeepsTheTimeline(t *testing.T) {
	m, _ := mergedView(t, "live one\n")
	m = following(t, m)
	before := m.Text()
	m, _ = m.Update(ActionMsg{Action: "toggle_follow"})
	if m.Following() {
		t.Fatal("the second toggle must leave follow mode")
	}
	if m.Text() != before {
		t.Fatalf("leaving follow must keep the timeline: %q", m.Text())
	}
	if !m.ReadOnly() {
		t.Fatal("a merged timeline stays read-only after follow")
	}
}

// TestMergedLogLoadClearsTheSet: opening a real file in the same view leaves
// the merged state behind.
func TestMergedLogLoadClearsTheSet(t *testing.T) {
	m, src := mergedView(t, "live one\n")
	m = following(t, m)
	if err := m.Load(src); err != nil {
		t.Fatal(err)
	}
	if m.MergedLog() || m.FollowSource() != "" || m.Following() {
		t.Fatal("loading a file must clear the merged-set state")
	}
}

// mergeRequested reports whether cmd carries a re-merge request for vpath.
func mergeRequested(cmd tea.Cmd, vpath string) bool {
	for _, msg := range flatten(cmd) {
		if req, ok := msg.(MergeLogSetMsg); ok && req.Path == vpath {
			return true
		}
	}
	return false
}

// flatten walks a command tree collecting the messages it produces.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, flatten(sub)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
