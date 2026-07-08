package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/finder"
	"ike/internal/locations"
	"ike/internal/search"
)

func replaceReq(items []locations.Item, rep string, q search.Query) finder.ReplaceRequestMsg {
	return finder.ReplaceRequestMsg{Items: items, Replacement: rep, Query: q}
}

func itemAt(path string, line int, text string, start, end int) locations.Item {
	return locations.Item{Path: path, Line: line, Text: text, StartCol: start, EndCol: end}
}

func TestReplaceOnDiskUnopenedFile(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "u.txt")
	if err := os.WriteFile(path, []byte("a needle\nplain\nb needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(replaceReq([]locations.Item{
		itemAt(path, 1, "a needle", 2, 8),
		itemAt(path, 3, "b needle", 2, 8),
	}, "thread", search.Query{Pattern: "needle"}))
	m = tm.(Model)
	data, _ := os.ReadFile(path)
	if string(data) != "a thread\nplain\nb thread\n" {
		t.Fatalf("disk content wrong: %q", data)
	}
	if len(m.history) == 0 || !strings.Contains(m.history[len(m.history)-1].text, "2 replacements in 1 files") {
		t.Fatalf("summary notification missing, history=%+v", m.history)
	}
}

func TestReplaceThroughDirtyBufferNotDisk(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "d.txt")
	if err := os.WriteFile(path, []byte("keep needle safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	// Dirty the buffer with an edit at the END of the line (the match's line
	// prefix stays valid).
	for _, k := range []tea.KeyPressMsg{
		{Code: 'A', Text: "A"}, {Code: '!', Text: "!"}, {Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	ed := m.activeEditor()
	if !ed.Dirty() {
		t.Fatal("test setup: buffer must be dirty")
	}
	tm, _ = m.Update(replaceReq([]locations.Item{
		itemAt(path, 1, "keep needle safe", 5, 11),
	}, "thread", search.Query{Pattern: "needle"}))
	m = tm.(Model)

	if got := m.activeEditor().Text(); !strings.Contains(got, "keep thread safe!") {
		t.Fatalf("buffer must carry the replacement (and the unsaved edit): %q", got)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "thread") {
		t.Fatalf("a dirty buffer's file must NOT be rewritten on disk: %q", data)
	}
	if !m.activeEditor().Dirty() {
		t.Fatal("the buffer stays dirty after a through-buffer replacement")
	}
	// One undo reverts the replacement (not the user's own edit).
	m = drainKey(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := m.activeEditor().Text(); !strings.Contains(got, "keep needle safe!") {
		t.Fatalf("undo must revert the replacement as one unit: %q", got)
	}
}

func TestReplaceCaptureGroupsOnDisk(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "c.txt")
	if err := os.WriteFile(path, []byte("id: item-42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := search.Query{Pattern: `(\w+)-(\d+)`, Regex: true}
	tm, _ := m.Update(replaceReq([]locations.Item{
		itemAt(path, 1, "id: item-42", 4, 11),
	}, "$2/$1", q))
	_ = tm
	data, _ := os.ReadFile(path)
	if string(data) != "id: 42/item\n" {
		t.Fatalf("capture groups on disk wrong: %q", data)
	}
}

func TestReplaceSkipsStaleDiskMatches(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "s.txt")
	if err := os.WriteFile(path, []byte("rewritten meanwhile\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(replaceReq([]locations.Item{
		itemAt(path, 1, "the old needle line", 8, 14), // scanned text no longer on disk
	}, "x", search.Query{Pattern: "needle"}))
	m = tm.(Model)
	data, _ := os.ReadFile(path)
	if string(data) != "rewritten meanwhile\n" {
		t.Fatalf("stale match must not touch the file: %q", data)
	}
	if len(m.history) == 0 || !strings.Contains(m.history[len(m.history)-1].text, "skipped") {
		t.Fatalf("summary must report skipped stale matches, history=%+v", m.history)
	}
}

func TestReplaceInPathCommandAndOverlay(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("project.replaceInPath"); !ok {
		t.Fatal("project.replaceInPath must be a registry command")
	}
	tm, _ := m.Update(OpenReplaceInPathMsg{})
	m = tm.(Model)
	if !m.finder.IsOpen() {
		t.Fatal("project.replaceInPath must open the overlay")
	}
	frame := m.render()
	if !strings.Contains(frame, "Replace") {
		t.Fatal("replace-mode overlay missing from the frame")
	}
}
