package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/agenttrace"
	"ike/internal/tracepanel"
)

// agenttrace_stored_diff_test.go covers D on a stored session (#2882): the
// diff comes from the stored session — its transcript while it exists, its
// record once Claude Code pruned it — never from the live session the trace
// reads meanwhile; a change whose hunks the record cap dropped gets its own
// notice.

// storeTraceRecord files transcript as a stored session record; edit, when
// set, adjusts the record before it is written.
func storeTraceRecord(t *testing.T, transcript string, edit func(*agenttrace.Record)) {
	t.Helper()
	sess, err := agenttrace.Load(transcript)
	if sess == nil {
		t.Fatalf("load %s: %v", transcript, err)
	}
	rec := agenttrace.NewRecord(sess, transcript, true)
	if edit != nil {
		edit(rec)
	}
	if err := traceStore().Save(rec); err != nil {
		t.Fatal(err)
	}
}

// writeStoredTranscript writes the diffTranscript session id into dir.
func writeStoredTranscript(t *testing.T, dir, id string) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	body := diffTranscript(t, id, projectRoot(), filepath.Join(dir, "main.go"), filepath.Join(dir, "new.go"), filepath.Join(dir, "other.go"))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The changed file is on disk: its base is disabled for the stored
	// session's sake only.
	if err := os.WriteFile(filepath.Join(dir, "other.go"), []byte("0\n0\n0\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// showStoredDiff shows the stored session id in the pane and presses D on
// key.
func showStoredDiff(t *testing.T, m Model, id, key string) Model {
	t.Helper()
	out, cmd := m.Update(tracepanel.ShowHistoryMsg{ID: id})
	m = traceDrain(t, out.(Model), cmd)
	p := m.agentTracePanel()
	if !p.Stored() || p.Info().ID != id {
		t.Fatalf("stored session not shown: %+v", p.Info())
	}
	p.SetViewMode(tracepanel.ViewTree)
	if !p.Select(key) {
		t.Fatalf("no %s in the stored session:\n%s", key, p.View())
	}
	msg, ok := p.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})().(tracepanel.DiffMsg)
	if !ok || msg.Key != key || msg.Linked != "" {
		t.Fatalf("D = %#v", msg)
	}
	return runTraceDiff(t, m, msg)
}

func TestTraceDiffOnStoredSession(t *testing.T) {
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	liveTarget := filepath.Join(t.TempDir(), "live.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-live.jsonl"), []byte(transcriptLines("sess-live", projectRoot(), liveTarget, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	if m.traceReader == nil || m.agentTracePanel().Info().ID != "sess-live" {
		t.Fatalf("the trace must read the live session: %+v", m.agentTracePanel().Info())
	}

	// The stored session's transcript lives elsewhere; e4/f0 does not
	// exist in the live one.
	tmp := t.TempDir()
	old := writeStoredTranscript(t, tmp, "sess-old")
	storeTraceRecord(t, old, nil)

	// The transcript still exists: it is the source, the live one is not.
	m = showStoredDiff(t, m, "sess-old", "e4/f0")
	if !m.traceDiffOpen() {
		t.Fatalf("D on a stored session must open the view: %q", traceNotices(m))
	}
	view := ansi.Strip(m.shell.Content().Render(200))
	for _, want := range []string{"other.go", "from structuredPatch", "@@ -4,1 +4,1 @@", "-1", "+2"} {
		if !strings.Contains(view, want) {
			t.Errorf("stored view lacks %q:\n%s", want, view)
		}
	}
	m.closeTraceDiff()

	// Claude Code pruned the transcript: the record's hunks remain, and the
	// base strip says why only they do.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	m = showStoredDiff(t, m, "sess-old", "e4/f0")
	if !m.traceDiffOpen() {
		t.Fatalf("D must open from the record alone: %q", traceNotices(m))
	}
	view = ansi.Strip(m.shell.Content().Render(200))
	for _, want := range []string{"other.go", "+1 −1", "@@ -4,1 +4,1 @@", "+2", "working file: stored session — the record kept only the changed hunks"} {
		if !strings.Contains(view, want) {
			t.Errorf("record-only view lacks %q:\n%s", want, view)
		}
	}
	m.closeTraceDiff()

	// The record cap dropped the change's hunks: the dedicated notice, the
	// counts still in the header.
	old = writeStoredTranscript(t, tmp, "sess-old")
	storeTraceRecord(t, old, func(r *agenttrace.Record) {
		for i := range r.Diffs {
			if r.Diffs[i].Key == "e4/f0" {
				r.Diffs[i].Hunks, r.Diffs[i].Dropped = nil, true
			}
		}
	})
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	m.history = nil
	m = showStoredDiff(t, m, "sess-old", "e4/f0")
	got := traceNotices(m)
	if !strings.Contains(got, traceDroppedNotice) || strings.Contains(got, "no diff for this change") {
		t.Fatalf("notice = %q", got)
	}
	view = ansi.Strip(m.shell.Content().Render(200))
	if !strings.Contains(view, "+1 −1") || !strings.Contains(view, traceDroppedNotice) {
		t.Errorf("dropped view:\n%s", view)
	}
}

// TestTraceDiffStoredWithoutLiveReader: after a restart no live session is
// read; the stored session picked from the history still diffs from its
// record.
func TestTraceDiffStoredWithoutLiveReader(t *testing.T) {
	m, _ := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m = openTrace(t, m)
	if m.traceReader != nil {
		t.Fatal("no live session expected")
	}
	old := writeStoredTranscript(t, t.TempDir(), "sess-old")
	storeTraceRecord(t, old, nil)
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	m = showStoredDiff(t, m, "sess-old", "e4/f0")
	if !m.traceDiffOpen() {
		t.Fatalf("D must open: %q", traceNotices(m))
	}
	if view := ansi.Strip(m.shell.Content().Render(200)); !strings.Contains(view, "@@ -4,1 +4,1 @@") || !strings.Contains(view, "stored session") {
		t.Errorf("view:\n%s", view)
	}
}

// TestTraceRecordMarksDroppedHunks: the record cap marks the diffs it
// strips, and an older record without the mark reads as dropped from its
// truncated flag.
func TestTraceRecordMarksDroppedHunks(t *testing.T) {
	rec := &agenttrace.Record{ID: "x", Diffs: []agenttrace.RecordDiff{{
		Key: "e1/f0", Keys: []string{"e1/f0"}, Path: "a.go", Op: "edit", Counted: true, Added: 1,
		Hunks: []agenttrace.DiffHunk{{Lines: []string{"+" + strings.Repeat("x", agenttrace.MaxRecordBytes)}}},
	}}}
	if _, err := rec.Encode(); err != nil {
		t.Fatal(err)
	}
	if !rec.Truncated || !rec.Diffs[0].Dropped || rec.Diffs[0].Hunks != nil {
		t.Fatalf("encode must drop and mark: %+v", rec.Diffs[0])
	}
	rec.Diffs[0].Dropped = false
	if d := rec.Session().KnownDiffs[0]; !d.Dropped || d.Added != 1 {
		t.Fatalf("a truncated record's hunkless counted diff reads as dropped: %+v", d)
	}
}
