package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func TestBlameToggleAndMsg(t *testing.T) {
	m, _ := loaded(t, "a\nb\n")
	if m.BlameOn() {
		t.Fatal("blame must start off")
	}
	if !m.ToggleBlame() || !m.BlameOn() {
		t.Fatal("toggle on failed")
	}
	m, _ = m.Update(vcs.BlameMsg{Path: m.path, Lines: map[int]vcs.BlameLine{
		0: {Author: "Alice", Time: time.Now().Add(-2 * time.Hour), Summary: "feat: x"},
	}})
	if len(m.blame) != 1 {
		t.Fatal("blame map not stored")
	}
	// Foreign path ignored; toggle off drops the cache.
	m, _ = m.Update(vcs.BlameMsg{Path: "/other.go", Lines: nil})
	if len(m.blame) != 1 {
		t.Fatal("foreign blame applied")
	}
	if m.ToggleBlame() {
		t.Fatal("second toggle must turn off")
	}
	if m.blame != nil {
		t.Fatal("toggle off must drop the cache")
	}
}

func TestBlameAnnotationRendersOnCursorLine(t *testing.T) {
	m, _ := loaded(t, "short\nother\n")
	m.SetSize(100, 10)
	m.ToggleBlame()
	m, _ = m.Update(vcs.BlameMsg{Path: m.path, Lines: map[int]vcs.BlameLine{
		0: {Author: "Alice", Time: time.Now().Add(-2 * time.Hour), Summary: "feat: x"},
	}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Alice, 2 hours ago · feat: x") {
		t.Fatalf("annotation missing:\n%s", v)
	}
	// Off again: annotation gone.
	m.ToggleBlame()
	if strings.Contains(ansi.Strip(m.View()), "Alice") {
		t.Fatal("annotation must disappear when toggled off")
	}
}

func TestBlameAnnotationSkippedWithoutRoom(t *testing.T) {
	m, _ := loaded(t, strings.Repeat("x", 40)+"\n")
	m.SetSize(30, 5) // narrower than content + annotation
	m.ToggleBlame()
	m, _ = m.Update(vcs.BlameMsg{Path: m.path, Lines: map[int]vcs.BlameLine{
		0: {Author: "Alice", Time: time.Now(), Summary: "s"},
	}})
	if strings.Contains(ansi.Strip(m.View()), "Alice") {
		t.Fatal("annotation must hide when it does not fit")
	}
}
