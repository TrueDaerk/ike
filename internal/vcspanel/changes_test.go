package vcspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func snapWith(entries ...vcs.FileEntry) *vcs.Snapshot {
	return &vcs.Snapshot{Root: "/r", Branch: "main", Entries: entries}
}

func changesPanel() Model {
	m := New(nil)
	m.SetSize(90, 14)
	m.SetFocused(true)
	m.SetVCS(snapWith(
		vcs.FileEntry{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'},
		vcs.FileEntry{Path: "b.go", Status: vcs.StatusAdded, X: 'A', Y: '.'},
	))
	return m
}

func TestChangesRowsFromSnapshot(t *testing.T) {
	m := changesPanel()
	if len(m.chRows) != 2 || m.chRows[0].Path != "a.go" || m.chRows[1].Path != "b.go" {
		t.Fatalf("rows = %+v", m.chRows)
	}
	// The badge column is two cells wide so partially staged codes align
	// with the single-letter ones (#1868).
	v := ansi.Strip(m.View())
	for _, want := range []string{"M  a.go", "A  b.go", "enter diff"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// A refresh keeps the cursor on the same path.
	m.Update(key("j"))
	m.SetVCS(snapWith(
		vcs.FileEntry{Path: "0.go", Status: vcs.StatusUntracked, X: '?', Y: '?'},
		vcs.FileEntry{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'},
		vcs.FileEntry{Path: "b.go", Status: vcs.StatusAdded, X: 'A', Y: '.'},
	))
	if m.chRows[m.chCursor].Path != "b.go" {
		t.Fatalf("cursor drifted to %q", m.chRows[m.chCursor].Path)
	}
}

// TestChangesShowsPartiallyStagedCode guards #1868: a file staged and then
// edited again keeps both porcelain letters in the list.
func TestChangesShowsPartiallyStagedCode(t *testing.T) {
	m := New(nil)
	m.SetSize(90, 14)
	m.SetVCS(snapWith(
		vcs.FileEntry{Path: "a.go", Status: vcs.StatusPartiallyStaged, X: 'A', Y: 'M'},
	))
	if got := m.chRows[0].Code; got != "AM" {
		t.Fatalf("code = %q want %q", got, "AM")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "AM a.go") {
		t.Fatalf("view missing the AM badge:\n%s", v)
	}
}

func TestChangesNavigationAndDiff(t *testing.T) {
	m := changesPanel()
	if cmd := m.Update(key("j")); cmd != nil || m.chCursor != 1 {
		t.Fatalf("j: cursor=%d cmd=%v", m.chCursor, cmd)
	}
	if cmd := m.Update(key("k")); cmd != nil || m.chCursor != 0 {
		t.Fatalf("k: cursor=%d cmd=%v", m.chCursor, cmd)
	}
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if od, ok := cmd().(OpenDiffMsg); !ok || od.Path != "a.go" {
		t.Fatalf("enter = %#v", cmd())
	}
}
