package explorer

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/theme"
)

// fg returns a style's foreground colour as a "#rrggbb" hex string, regardless
// of the concrete lipgloss colour type it resolved to.
func fg(s lipgloss.Style) string {
	r, g, b, _ := s.GetForeground().RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// resolveAll builds the resolved-colour index a Model would hold (#1098).
func resolveAll(t colorTable) map[string]color.Color {
	out := make(map[string]color.Color, len(t))
	for k, v := range t {
		out[k] = theme.Resolve(v)
	}
	return out
}

func TestColorResolutionGlobThenExtThenFallback(t *testing.T) {
	ct := colorTable{
		"dir":       "blue",
		"default":   "white",
		"go":        "cyan",
		"*.test.go": "magenta",
		"Makefile":  "yellow", // a glob-free exact name only matches via the glob path if it has wildcards; here it does not, so it never matches a non-".ext" file
	}
	// #1051 suffix-tint model: only ext/glob keys resolve — dirs and files
	// without a match return nil (rows render in the plain foreground; the
	// legacy "dir"/"default" keys are accepted but no longer paint rows).
	cases := []struct {
		name  string
		isDir bool
		want  string // resolved suffix tint; "" = no tint
	}{
		{"main.go", false, "#5fd7d7"},      // ext "go" -> cyan
		{"main.test.go", false, "#d787ff"}, // glob "*.test.go" wins over ext "go" -> magenta
		{"sub", true, ""},                  // dir -> no tint (#1051/#1054)
		{"README", false, ""},              // no ext, no glob -> no tint
		{"notes.txt", false, ""},           // unknown ext -> no tint
	}
	for _, c := range cases {
		n := &node{name: c.name, isDir: c.isDir}
		got := ""
		if col := ct.suffixColor(n, ct.globs(), resolveAll(ct)); col != nil {
			r, g, b, _ := col.RGBA()
			got = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
		}
		if got != c.want {
			t.Errorf("%s: suffix tint = %q want %q", c.name, got, c.want)
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
	for _, id := range []string{"explorer.toggleHidden", "explorer.refresh", "explorer.collapseAll", "explorer.reveal", "explorer.newFile", "explorer.newFolder", "explorer.delete", "explorer.rename", "explorer.undo"} {
		if _, ok := r.Command(id); !ok {
			t.Errorf("command %q not registered", id)
		}
	}
	// the default keymaps resolve within the explorer context.
	if _, ok := r.ResolveKey(".", ctxID); !ok {
		t.Error("toggleHidden key not bound in explorer context")
	}
	if _, ok := r.ResolveKey("R", ctxID); !ok {
		t.Error("rename key not bound in explorer context")
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
	if col := m.colors.suffixColor(&node{name: "x.go"}, m.colorGlobs, m.colorVals); col == nil {
		t.Error("go suffix tint missing")
	} else {
		r, g, b, _ := col.RGBA()
		if got := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8); got != "#ff5555" {
			t.Errorf("go colour = %q want #ff5555 (red)", got)
		}
	}
}

// TestReconfigureDoesNotClobberToggle guards #629: a live reload whose
// show_hidden value is unchanged must not reset the runtime `.` toggle.
func TestReconfigureDoesNotClobberToggle(t *testing.T) {
	cfg := host.MapConfig{"explorer.show_hidden": "false"}
	m := New(".")
	m.Configure(cfg) // initial: default off
	if m.showHidden {
		t.Fatal("show_hidden should start off")
	}

	// User toggles it on at runtime.
	m, _ = m.Update(ToggleHiddenMsg{})
	if !m.showHidden {
		t.Fatal("toggle did not enable show_hidden")
	}

	// An unrelated reload re-applies the same (unchanged) config.
	m.Configure(cfg)
	if !m.showHidden {
		t.Fatal("reconfigure clobbered the runtime toggle (#629)")
	}

	// A genuine settings change to show_hidden still applies (off -> on -> off).
	m.Configure(host.MapConfig{"explorer.show_hidden": "true"})
	if !m.showHidden {
		t.Fatal("config change to true should apply")
	}
	m.Configure(host.MapConfig{"explorer.show_hidden": "false"})
	if m.showHidden {
		t.Fatal("config change to false should apply")
	}
}

// TestToggleEmitsPersist guards #629: toggling emits a HiddenToggledMsg so the
// app can persist immediately (survive a kill/crash, not only a clean quit).
func TestToggleEmitsPersist(t *testing.T) {
	m := New(".")
	_, cmd := m.Update(ToggleHiddenMsg{})
	if cmd == nil {
		t.Fatal("toggle produced no command")
	}
	msg := cmd()
	tg, ok := msg.(HiddenToggledMsg)
	if !ok {
		t.Fatalf("toggle emitted %T, want HiddenToggledMsg", msg)
	}
	if !tg.ShowHidden {
		t.Fatal("HiddenToggledMsg.ShowHidden = false after enabling")
	}
}
