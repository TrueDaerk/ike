package vcspanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func logPanel() Model {
	m := New(nil)
	m.SetSize(100, 14)
	m.SetFocused(true)
	m.SetVCS(&vcs.Snapshot{Root: "/r", Branch: "main"})
	return m
}

func entries(subjects ...string) []vcs.LogEntry {
	var out []vcs.LogEntry
	for i, s := range subjects {
		out = append(out, vcs.LogEntry{
			Hash:      strings.Repeat("a", 39) + string(rune('0'+i)),
			ShortHash: "aaaa00" + string(rune('0'+i)),
			Author:    "t",
			Time:      time.Now().Add(-time.Hour),
			Subject:   s,
		})
	}
	return out
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func TestLogLazyLoadAndPaging(t *testing.T) {
	m := logPanel()
	cmd := m.Update(key("2"))
	if cmd == nil {
		t.Fatal("opening the log must request the first window")
	}
	req, ok := cmd().(LogRequestMsg)
	if !ok || req.Offset != 0 {
		t.Fatalf("request = %#v", req)
	}
	// Re-selecting the loaded tab stays quiet.
	m.ApplyLog(vcs.LogMsg{Entries: entries("one", "two"), HasMore: true})
	if cmd := m.Update(key("2")); cmd != nil {
		t.Fatal("loaded log must not re-request")
	}
	// j at the tail with more history requests the next window.
	m.Update(key("j"))
	cmd = m.Update(key("j"))
	if cmd == nil {
		t.Fatal("tail j must page")
	}
	if req := cmd().(LogRequestMsg); req.Offset != 2 {
		t.Fatalf("paging offset = %d", req.Offset)
	}
	m.ApplyLog(vcs.LogMsg{Entries: entries("three"), Offset: 2})
	if len(m.logEntries) != 3 || m.logHasMore {
		t.Fatalf("after append: %d entries, more=%v", len(m.logEntries), m.logHasMore)
	}
}

func TestLogExpandAndFileDiff(t *testing.T) {
	m := logPanel()
	m.Update(key("2"))
	m.ApplyLog(vcs.LogMsg{Entries: entries("one", "two")})

	// enter on a commit requests its details.
	cmd := m.Update(enter())
	show, ok := cmd().(ShowRequestMsg)
	if !ok || show.Hash != m.logEntries[0].Hash {
		t.Fatalf("show request = %#v", show)
	}
	m.ApplyShow(vcs.ShowMsg{
		Entry: m.logEntries[0],
		Files: []vcs.CommitFile{{Path: "c.txt", Status: vcs.StatusRenamed, OldPath: "b.txt"}},
	})
	if len(m.logRows) != 3 {
		t.Fatalf("rows after expand = %d", len(m.logRows))
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "R c.txt") || !strings.Contains(v, "▾") {
		t.Fatalf("expanded view:\n%s", v)
	}

	// enter on the file opens the parent-vs-commit diff.
	m.Update(key("j"))
	cmd = m.Update(enter())
	od, ok := cmd().(OpenCommitDiffMsg)
	if !ok || od.Path != "c.txt" || od.OldPath != "b.txt" || od.Hash != m.logEntries[0].Hash {
		t.Fatalf("diff msg = %#v", od)
	}

	// enter on the commit again collapses it.
	m.Update(key("k"))
	m.Update(enter())
	if len(m.logRows) != 2 {
		t.Fatalf("rows after collapse = %d", len(m.logRows))
	}
}

func TestLogReloadResetsAndErrors(t *testing.T) {
	m := logPanel()
	// A never-opened log stays lazy on reload.
	if m.ReloadLog() != nil {
		t.Fatal("reload before first open must stay lazy")
	}
	m.Update(key("2"))
	m.ApplyLog(vcs.LogMsg{Entries: entries("one")})
	if m.ReloadLog() == nil {
		t.Fatal("reload of a loaded log must request")
	}
	m.ApplyLog(vcs.LogMsg{Entries: entries("amended"), Offset: 0})
	if len(m.logEntries) != 1 || m.logEntries[0].Subject != "amended" {
		t.Fatalf("reload did not replace: %+v", m.logEntries)
	}
	m.ApplyLog(vcs.LogMsg{Err: errFake})
	if !strings.Contains(ansi.Strip(m.View()), "log: boom") {
		t.Fatal("error must render")
	}
}

func TestLogTableColumns(t *testing.T) {
	m := logPanel()
	m.Update(key("2"))
	m.ApplyLog(vcs.LogMsg{Entries: []vcs.LogEntry{{
		Hash:      strings.Repeat("a", 40),
		ShortHash: "aaaa000",
		Author:    "Alice Wonderland Extra Long",
		Time:      time.Now().Add(-2 * time.Hour),
		Subject:   "feat: table",
	}}})
	v := ansi.Strip(m.View())
	// Header row + aligned cells; the over-long author clips with ellipsis.
	for _, want := range []string{"Commit", "Subject", "Author", "Date", "▸ aaaa000", "feat: table", "2 hours ago"} {
		if !strings.Contains(v, want) {
			t.Fatalf("table missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Alice Wonderland Extra Long") {
		t.Fatal("author must clip to its column")
	}
	if !strings.Contains(v, "Alice Wonderl…") {
		t.Fatalf("clipped author cell missing:\n%s", v)
	}

	// Narrow panel: date, then author drop; the subject survives.
	m.SetSize(46, 14)
	v = ansi.Strip(m.View())
	if strings.Contains(v, "Date") {
		t.Fatalf("narrow panel should drop the date column:\n%s", v)
	}
	if !strings.Contains(v, "feat: table") {
		t.Fatal("subject must survive narrow widths")
	}
}

type fakeErr struct{}

func (fakeErr) Error() string { return "boom" }

var errFake = fakeErr{}
