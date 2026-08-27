package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/largefile"
	"ike/internal/registry"
	"ike/internal/vcs"
)

// Per-feature large-file degradation surfacing (#2159): the status-line badge,
// the detail popup, and the VCS gutter guard.

func TestLargeFileBadgeInStatusLine(t *testing.T) {
	m, _ := largeModel(t)
	if !strings.Contains(m.render(), "[large file]") {
		t.Fatal("the status line must badge the degraded document")
	}
}

func TestLargeFileDetailsPopup(t *testing.T) {
	m, _ := largeModel(t)
	m = step(m, LargeFileDetailsMsg{})
	if !m.largeDetail {
		t.Fatal("editor.largeFileDetails must open the popup for a degraded document")
	}
	frame := m.render()
	for _, want := range []string{"syntax highlighting", "LSP sync", "VCS gutter diff", "search match counter", "format on save"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the popup must list %q; frame misses it", want)
		}
	}
	// Any key closes it.
	m = step(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.largeDetail {
		t.Fatal("a key press must dismiss the popup")
	}
}

func TestLargeFileDetailsRefusedForNormalFiles(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := sizedWith(t, registry.New(), 120, 40)
	path := filepath.Join(t.TempDir(), "small.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	m = step(m, LargeFileDetailsMsg{})
	if m.largeDetail {
		t.Fatal("the popup must not open when nothing is degraded")
	}
}

func TestVCSMarksSkippedForLargeFile(t *testing.T) {
	m, path := largeModel(t)
	ed := m.activeEditor()
	if ed == nil || !ed.LargeFile() {
		t.Fatal("setup: focused editor must hold the flagged document")
	}
	cmd := m.vcsMarksCmd(ed)
	if cmd == nil {
		t.Fatal("a degraded document still needs its stale marks cleared")
	}
	msg, ok := cmd().(vcs.MarksMsg)
	if !ok || msg.Path != path || len(msg.Marks) != 0 {
		t.Fatalf("degraded document must yield a clearing MarksMsg, got %#v", msg)
	}
}
