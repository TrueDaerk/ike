package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
)

func TestApplyEditsToLinesBottomUp(t *testing.T) {
	lines := []string{"aaa", "bbb", "ccc"}
	// Given top-down, must apply bottom-up to stay position-stable.
	got := applyEditsToLines(lines, []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, Text: "AAA"},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 3, Text: "CCC"},
	})
	if strings.Join(got, "\n") != "AAA\nbbb\nCCC" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyEditsToLinesMultiLineAndClamp(t *testing.T) {
	lines := []string{"one", "two", "three"}
	got := applyEditsToLines(lines, []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 2, EndLine: 2, EndCol: 99, Text: "X\nY"}, // end col clamps
	})
	if strings.Join(got, "\n") != "onX\nY" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyEditsToDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("old name\nkeep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := applyEditsToDisk(path, []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 8, Text: "title"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old title\nkeep\n" {
		t.Fatalf("file = %q", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode should survive, got %v", info.Mode().Perm())
	}
}

func TestDispatchWorkspaceEditsSplitsOpenAndDisk(t *testing.T) {
	dir := t.TempDir()
	closed := filepath.Join(dir, "closed.go")
	if err := os.WriteFile(closed, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Send delivers through the host's dispatcher goroutine (#2027), so the
	// captured messages arrive on a channel, not in a slice the test reads.
	sent := make(chan tea.Msg, 4)
	rec := host.New(host.MapConfig{})
	rec.SetSender(func(msg tea.Msg) { sent <- msg })
	n, err := dispatchWorkspaceEdits(rec, []manager.FileEdits{
		{Path: "/open.go", Open: true, Edits: []ilsp.FormatEdit{{EndCol: 1, Text: "X"}}},
		{Path: closed, Open: false, Edits: []ilsp.FormatEdit{{EndCol: 3, Text: "xyz"}}},
		{Path: "/empty.go", Open: true}, // no edits: skipped
	})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	select {
	case msg := <-sent:
		if fm, ok := msg.(ilsp.FormatEditsMsg); !ok || fm.Path != "/open.go" {
			t.Fatalf("sent = %+v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("open file should go through Send, nothing arrived")
	}
	select {
	case msg := <-sent:
		t.Fatalf("only the open file may be Sent, also got %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
	data, _ := os.ReadFile(closed)
	if string(data) != "xyz\n" {
		t.Fatalf("closed file should be rewritten, got %q", data)
	}
}
