package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
	"ike/internal/project"
)

// TestSwitchPromptRendersWithLongRoot guards the overlay drop: a raw absolute
// root in the prompt body once pushed the shell box past the terminal width,
// and overlay.Center silently drops an oversized box — the guard existed but
// never rendered. CompactPath bounds the line.
func TestSwitchPromptRendersWithLongRoot(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, strings.Repeat("deeply-nested-segment/", 6), "proj")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	m := switchModel(t)
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !m.switchPromptOpen() {
		t.Fatal("guard should be open")
	}
	if w := lipgloss.Width(m.shell.View()); w > m.width {
		t.Fatalf("prompt box (%d) must fit the terminal (%d)", w, m.width)
	}
	if !strings.Contains(m.render(), "closes every open file") {
		t.Fatal("guard prompt missing from the rendered frame")
	}
	// The full root still reaches the switch when confirmed.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("confirm should switch to the full root, cwd = %s", cwd(t))
	}
}
