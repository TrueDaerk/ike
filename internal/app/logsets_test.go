package app

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/lang"
	"ike/internal/logline"
	"ike/internal/watch"
)

// logsets_test.go covers the app half of the merged rotated log set (#1996):
// the command merges the set behind the focused buffer into one read-only
// timeline (gz members decompressed, origin separators naming each region),
// opening a rotated log offers it, and follow mode keeps working across a
// rotation of the newest member.

// logSetDir writes a rotation set into a fresh temp dir and returns the live
// log's path: app.log plus a numbered member and a compressed older one.
func logSetDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeLog(t, dir, "app.log", "2026-08-10 10:00:00 INFO live\n")
	writeLog(t, dir, "app.log.1", "2026-08-09 09:00:00 INFO yesterday\n")
	writeGzLog(t, dir, "app.log.2.gz", "2026-08-08 08:00:00 INFO compressed\n")
	return filepath.Join(dir, "app.log")
}

func writeLog(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeGzLog(t *testing.T, dir, name, body string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeLog(t, dir, name, buf.String())
}

// registerLogLanguage makes ".log" resolve to the log language, which the
// plugin does in a real build (blank-imported in cmd/ike).
func registerLogLanguage(t *testing.T) {
	t.Helper()
	lang.Register(lang.Language{ID: "log", Extensions: []string{"log"}})
}

// mergedEditor opens the rotation set behind path through the command and
// returns the model plus the merged view.
func mergedEditor(t *testing.T, m Model, path string) (Model, *editor.Model) {
	t.Helper()
	out, cmd := m.openPath(path, false)
	m = drainCmd(out.(Model), cmd)
	out, cmd = m.Update(OpenMergedLogMsg{})
	m = drainCmd(out.(Model), cmd)
	vpath := mergedLogPath(canonicalPath(path))
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatalf("no merged timeline at %q", vpath)
	}
	return m, ed
}

// TestOpenRotatedSetMergesEveryMember: the timeline holds all members in
// chronological order, gz decompressed, each region named by its separator,
// in a read-only buffer that resolves as a log.
func TestOpenRotatedSetMergesEveryMember(t *testing.T) {
	registerLogLanguage(t)
	m := newSized()
	path := logSetDir(t)
	m, ed := mergedEditor(t, m, path)

	text := ed.Text()
	want := []string{"compressed", "yesterday", "live"}
	at := -1
	for _, w := range want {
		i := strings.Index(text, w)
		if i < 0 {
			t.Fatalf("%q missing from the timeline:\n%s", w, text)
		}
		if i < at {
			t.Fatalf("%q is out of chronological order:\n%s", w, text)
		}
		at = i
	}
	for _, name := range []string{"app.log.2.gz", "app.log.1", "app.log"} {
		if !strings.Contains(text, logline.OriginLine(name)) {
			t.Fatalf("no origin separator for %q:\n%s", name, text)
		}
	}
	if !ed.ReadOnly() || !ed.MergedLog() {
		t.Fatal("a merged timeline is a read-only merged buffer")
	}
	if l, ok := lang.ByPath(ed.Path()); !ok || l.ID != "log" {
		t.Fatalf("language for %q = %v/%v, want log", ed.Path(), l.ID, ok)
	}
	// It names itself as a timeline rather than as the file it started from.
	if title, ok := mergedLogTitle(ed.Path()); !ok || title != "app.log (merged)" {
		t.Fatalf("title = %q/%v", title, ok)
	}
	if got := m.editorTitle(ed); !strings.Contains(got, "[RO]") {
		t.Fatalf("editorTitle = %q, want a read-only marker", got)
	}
	// Editing is refused and the virtual path never becomes a file.
	before := ed.Text()
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if ed.Text() != before {
		t.Fatalf("the timeline was edited: %q", ed.Text())
	}
	if _, err := os.Stat(ed.Path()); err == nil {
		t.Fatalf("the virtual path became a file: %q", ed.Path())
	}
	// Re-running the command refreshes that one tab instead of adding another.
	out, cmd := m.Update(OpenMergedLogMsg{})
	m = drainCmd(out.(Model), cmd)
	if n := countTabsForPath(m, ed.Path()); n != 1 {
		t.Fatalf("the timeline opened %d times, want 1", n)
	}
}

// TestOpenRotatedSetFromARotatedMember: the command works from any member —
// opening app.log.1 and merging yields the same timeline.
func TestOpenRotatedSetFromARotatedMember(t *testing.T) {
	registerLogLanguage(t)
	m := newSized()
	live := logSetDir(t)
	member := filepath.Join(filepath.Dir(live), "app.log.1")

	out, cmd := m.openPath(member, false)
	m = drainCmd(out.(Model), cmd)
	out, cmd = m.Update(OpenMergedLogMsg{})
	m = drainCmd(out.(Model), cmd)
	if ed := readOnlyEditor(m, mergedLogPath(canonicalPath(live))); ed == nil {
		t.Fatal("merging from a rotated member must open the set's timeline")
	}
}

// TestOpenRotatedSetWithoutSiblingsSaysSo: no set, no second view of the same
// content.
func TestOpenRotatedSetWithoutSiblingsSaysSo(t *testing.T) {
	registerLogLanguage(t)
	m := newSized()
	path := writeLog(t, t.TempDir(), "lonely.log", "one line\n")

	out, cmd := m.openPath(path, false)
	m = drainCmd(out.(Model), cmd)
	out, cmd = m.Update(OpenMergedLogMsg{})
	m = drainCmd(out.(Model), cmd)
	if ed := readOnlyEditor(m, mergedLogPath(canonicalPath(path))); ed != nil {
		t.Fatal("a log without rotated siblings must not open a timeline")
	}
	if got := toastText(m); !strings.Contains(got, "no rotated log set") {
		t.Fatalf("toasts = %q, want the explanation", got)
	}
}

// TestOpenRotatedLogOffersTheTimeline: opening a log that belongs to a set
// says so once — the discoverability half of the feature.
func TestOpenRotatedLogOffersTheTimeline(t *testing.T) {
	registerLogLanguage(t)
	m := newSized()
	path := logSetDir(t)

	out, cmd := m.openPath(path, false)
	m = drainCmd(out.(Model), cmd)
	if got := toastText(m); !strings.Contains(got, "rotated log set") {
		t.Fatalf("toasts = %q, want the merged-timeline offer", got)
	}
	// Once per path: re-opening the same file stays quiet.
	m.toasts, m.history = nil, nil
	out, cmd = m.openPath(path, false)
	m = drainCmd(out.(Model), cmd)
	if got := toastText(m); strings.Contains(got, "rotated log set") {
		t.Fatalf("the offer must be raised once, got %q", got)
	}
}

// TestOpenPlainLogOffersNothing: a log with no rotated siblings, and a rotated
// non-log file, stay quiet.
func TestOpenPlainLogOffersNothing(t *testing.T) {
	registerLogLanguage(t)
	dir := t.TempDir()
	writeLog(t, dir, "data.csv", "a,b\n")
	writeLog(t, dir, "data.csv.1", "a,b\n")

	m := newSized()
	out, cmd := m.openPath(filepath.Join(dir, "data.csv"), false)
	m = drainCmd(out.(Model), cmd)
	if got := toastText(m); strings.Contains(got, "rotated log set") {
		t.Fatalf("a rotated non-log file must not offer a timeline, got %q", got)
	}
}

// TestMergedTimelineFollowSurvivesARotation is the end-to-end follow contract:
// appends to the newest member stream into the timeline, and a logrotate-style
// rotation re-merges the set — the old content moving into its own region,
// the new file's lines continuing at the end.
func TestMergedTimelineFollowSurvivesARotation(t *testing.T) {
	registerLogLanguage(t)
	m := newSized()
	path := logSetDir(t)
	m, ed := mergedEditor(t, m, path)

	// Follow the timeline: it tails the live log, not its own virtual path.
	if cmd := ed.Reparse(); cmd != nil {
		_ = cmd()
	}
	*ed, _ = ed.Update(editor.ActionMsg{Action: "toggle_follow"})
	if !ed.Following() {
		t.Fatal("a merged timeline must be followable")
	}
	if got := ed.FollowSource(); got != canonicalPath(path) {
		t.Fatalf("follow source = %q, want the live log %q", got, canonicalPath(path))
	}

	// An append streams in.
	appendTo(t, path, "2026-08-10 10:00:01 INFO more\n")
	out, cmd := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: canonicalPath(path)})
	m = drainCmd(out.(Model), cmd)
	ed = readOnlyEditor(m, mergedLogPath(canonicalPath(path)))
	if !strings.Contains(ed.Text(), "INFO more") {
		t.Fatalf("appends must stream into the timeline:\n%s", ed.Text())
	}

	// logrotate: the live log moves to .1 (the old .1 to .2) and is recreated.
	dir := filepath.Dir(path)
	rename(t, filepath.Join(dir, "app.log.1"), filepath.Join(dir, "app.log.3"))
	rename(t, path, filepath.Join(dir, "app.log.1"))
	writeLog(t, dir, "app.log", "2026-08-11 11:00:00 INFO after rotation\n")

	out, cmd = m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: canonicalPath(path)})
	m = drainCmd(out.(Model), cmd)
	ed = readOnlyEditor(m, mergedLogPath(canonicalPath(path)))
	if ed == nil {
		t.Fatal("a rotation must not close the timeline")
	}
	text := ed.Text()
	if !strings.Contains(text, "after rotation") {
		t.Fatalf("the re-merge must pick the new file up:\n%s", text)
	}
	if !strings.Contains(text, "INFO more") {
		t.Fatalf("the rotated-away content must stay in the timeline:\n%s", text)
	}
	if i, j := strings.Index(text, "INFO more"), strings.Index(text, "after rotation"); i > j {
		t.Fatalf("the rotated content must sort before the new file:\n%s", text)
	}
	if !ed.Following() {
		t.Fatal("follow must survive the rotation")
	}
	// And it keeps streaming from the replacement file.
	appendTo(t, path, "2026-08-11 11:00:01 INFO still tailing\n")
	out, cmd = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: canonicalPath(path)})
	m = drainCmd(out.(Model), cmd)
	ed = readOnlyEditor(m, mergedLogPath(canonicalPath(path)))
	if !strings.HasSuffix(ed.Text(), "INFO still tailing") {
		t.Fatalf("follow must keep streaming after the rotation:\n%s", ed.Text())
	}
}

// TestMergedTimelineRespectsTheLargeFileCap: the merge is bounded by the
// large-file thresholds, and what a cap costs is the oldest end.
func TestMergedTimelineRespectsTheLargeFileCap(t *testing.T) {
	registerLogLanguage(t)
	dir := t.TempDir()
	writeLog(t, dir, "app.log", strings.Repeat("live line\n", 200))
	writeLog(t, dir, "app.log.1", strings.Repeat("old line\n", 200))

	m := newCapped(t, 1) // 1 KB
	m, ed := mergedEditor(t, m, filepath.Join(dir, "app.log"))
	if n := len(ed.Text()); n > 1024 {
		t.Fatalf("timeline holds %d bytes, want at most the 1 KB cap", n)
	}
	if !strings.Contains(ed.Text(), "live line") {
		t.Fatal("the newest member must survive the cap")
	}
	if got := toastText(m); !strings.Contains(got, "incomplete") {
		t.Fatalf("a cut timeline must warn, got %q", got)
	}
}

// appendTo appends text to path on disk.
func appendTo(t *testing.T, path, text string) {
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

func rename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
}

// toastText joins every notification the model raised, live and historical.
func toastText(m Model) string {
	var b strings.Builder
	for _, ts := range m.toasts {
		b.WriteString(ts.text)
		b.WriteString("\n")
	}
	for _, h := range m.history {
		b.WriteString(h.text)
		b.WriteString("\n")
	}
	return b.String()
}
