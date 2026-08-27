package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/host"
)

// follow_test.go covers the app half of follow mode (#1928): the demand-armed
// poll tick arms on FollowMsg, re-arms while a view follows, and self-stops
// once none does — the no-idle-cost guarantee of
// wiki/architecture/performance.md.

func TestFollowTickArmsAndSelfStops(t *testing.T) {
	m := sized(t, 100, 40)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)

	// FollowMsg (the editor's toggle-on) arms the tick.
	out, cmd := m.Update(editor.FollowMsg{Path: path, On: true})
	m = out.(Model)
	if cmd == nil || !m.followTickArmed {
		t.Fatal("FollowMsg must arm the follow tick")
	}

	// With a following view, an elapsed tick re-arms.
	_ = m.routeToEditor(path, editor.ActionMsg{Action: "toggle_follow"})
	if !m.anyFollowing() {
		t.Fatal("setup: the routed toggle must enable follow")
	}
	m.followTickArmed = false
	if cmd := m.followTick(); cmd == nil || !m.followTickArmed {
		t.Fatal("the tick must re-arm while a view follows")
	}

	// Once no view follows, the tick self-stops: no re-arm, no idle cost.
	_ = m.routeToEditor(path, editor.ActionMsg{Action: "toggle_follow"})
	if m.anyFollowing() {
		t.Fatal("setup: the second toggle must end follow")
	}
	m.followTickArmed = true
	if cmd := m.followTick(); cmd != nil || m.followTickArmed {
		t.Fatal("the tick must self-stop with no following view")
	}
}

func TestFollowInterval(t *testing.T) {
	if got := followInterval(nil); got != 500*time.Millisecond {
		t.Fatalf("default interval = %v", got)
	}
	cfg := host.MapConfig{"editor.follow_poll_ms": "250"}
	if got := followInterval(cfg); got != 250*time.Millisecond {
		t.Fatalf("configured interval = %v", got)
	}
}

// TestFollowFilterBadgeInStatusLine: the follow filter (#2255) shows in the
// status line right after the FOLLOW badge, so a narrowed pane says so.
func TestFollowFilterBadgeInStatusLine(t *testing.T) {
	m := sized(t, 100, 40)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("error one\ninfo two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	_ = m.routeToEditor(path, editor.ActionMsg{Action: "toggle_follow"})
	_ = m.routeToEditor(path, editor.ActionMsg{Action: "follow_filter"})
	for _, r := range "error" {
		_ = m.routeToEditor(path, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("setup: no active editor")
	}
	if got := followSegment(m, ed); got != "FOLLOW" {
		t.Fatalf("follow badge = %q", got)
	}
	if got := followFilterSegment(m, ed); got != "FILTER error (1)" {
		t.Fatalf("filter badge = %q", got)
	}
	line := ansi.Strip(m.statusLine())
	if !strings.Contains(line, "FOLLOW") || !strings.Contains(line, "FILTER error (1)") {
		t.Fatalf("status line must carry both badges, got %q", line)
	}
}
