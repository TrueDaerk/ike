package explorer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"ike/internal/host"
	"ike/internal/registry"
)

// fg returns a style's foreground colour value as a plain string.
func fg(s lipgloss.Style) string {
	c, _ := s.GetForeground().(lipgloss.Color)
	return string(c)
}

func TestColorResolutionGlobThenExtThenFallback(t *testing.T) {
	ct := colorTable{
		"dir":       "blue",
		"default":   "white",
		"go":        "cyan",
		"*.test.go": "magenta",
		"Makefile":  "yellow", // a glob-free exact name only matches via the glob path if it has wildcards; here it does not, so it never matches a non-".ext" file
	}
	cases := []struct {
		name  string
		isDir bool
		want  string // resolved foreground (lipgloss color value)
	}{
		{"main.go", false, "6"},      // ext "go" -> cyan
		{"main.test.go", false, "5"}, // glob "*.test.go" wins over ext "go" -> magenta
		{"sub", true, "4"},           // dir -> blue
		{"README", false, "7"},       // no ext, no glob -> default white
		{"notes.txt", false, "7"},    // unknown ext -> default white
	}
	for _, c := range cases {
		n := &node{name: c.name, isDir: c.isDir}
		if got := fg(ct.style(n)); got != c.want {
			t.Errorf("%s: foreground = %q want %q", c.name, got, c.want)
		}
	}
}

func TestHiddenToggleAndItalic(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "visible.txt"), "v")
	mustWrite(t, filepath.Join(root, ".hidden"), "h")
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := mounted(t, root, 40, 20)
	// hidden entries are filtered by default: only the root + visible.txt show.
	if got := names(m); len(got) != 2 || got[1] != "visible.txt" {
		t.Fatalf("default rows = %v want [root visible.txt]", got)
	}

	// toggle hidden on: .git and .hidden appear (dirs first).
	m, _ = m.Update(ToggleHiddenMsg{})
	got := names(m)
	want := []string{filepath.Base(root), ".git", ".hidden", "visible.txt"}
	if len(got) != len(want) {
		t.Fatalf("after toggle rows = %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q want %q", i, got[i], want[i])
		}
	}

	// hidden rows render italic; visible ones do not.
	if !m.nodeStyle(&node{name: ".hidden"}).GetItalic() {
		t.Fatal("hidden file should be italic")
	}
	if m.nodeStyle(&node{name: "visible.txt"}).GetItalic() {
		t.Fatal("visible file should not be italic")
	}

	// toggle back off.
	m, _ = m.Update(ToggleHiddenMsg{})
	if len(names(m)) != 2 {
		t.Fatalf("after toggle off rows = %v", names(m))
	}
}

func TestCollapseAllReturnsToRoot(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("l")) // expand sub
	if len(m.rows) != 5 {
		t.Fatalf("precondition rows = %v", names(m))
	}
	m, _ = m.Update(CollapseAllMsg{})
	if len(m.rows) != 4 {
		t.Fatalf("after collapseAll rows = %v want root+3", names(m))
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d want 0", m.cursor)
	}
}

func TestRefreshPicksUpNewFile(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 30, 20)
	mustWrite(t, filepath.Join(root, "z-new.txt"), "z")
	// not visible until a refresh re-scans the root.
	for _, n := range names(m) {
		if n == "z-new.txt" {
			t.Fatal("new file visible before refresh")
		}
	}
	m, cmd := m.Update(RefreshMsg{})
	m, _ = pumpScans(m, cmd)
	found := false
	for _, n := range names(m) {
		if n == "z-new.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("refresh did not pick up new file: %v", names(m))
	}
}

func TestCommandsRegistered(t *testing.T) {
	r := registry.New()
	r.Add(corePlugin{})
	for _, id := range []string{"explorer.toggleHidden", "explorer.refresh", "explorer.collapseAll", "explorer.reveal"} {
		if _, ok := r.Command(id); !ok {
			t.Errorf("command %q not registered", id)
		}
	}
	// the default keymaps resolve within the explorer context.
	if _, ok := r.ResolveKey(".", ctxID); !ok {
		t.Error("toggleHidden key not bound in explorer context")
	}
	// and the binding is discoverable by command id, so help can show the key.
	if got, ok := r.Binding("explorer.toggleHidden"); !ok || got != "." {
		t.Errorf("Binding(toggleHidden) = %q,%v want \".\",true", got, ok)
	}
}

func TestConfigureReadsExplorerSection(t *testing.T) {
	cfg := host.MapConfig{
		"explorer.show_hidden":    "true",
		"explorer.tree_indent":    "4",
		"explorer.colors.go":      "red",
		"explorer.colors.dir":     "green",
		"explorer.colors.default": "white",
	}
	m := New(".")
	m.Configure(cfg)
	if !m.showHidden {
		t.Error("show_hidden not applied")
	}
	if m.indent != 4 {
		t.Errorf("indent = %d want 4", m.indent)
	}
	if got := fg(m.colors.style(&node{name: "x.go"})); got != "1" {
		t.Errorf("go colour = %q want 1 (red)", got)
	}
}
