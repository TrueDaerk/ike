package editor

import (
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/idcolor"
	"ike/internal/lang"
)

// registerIDLangs registers the languages identifier coloring scopes on;
// language plugins live under plugins/ and are not imported here.
func registerIDLangs(t *testing.T) {
	t.Helper()
	lang.Register(lang.Language{ID: "log", Extensions: []string{"log"}})
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	lang.Register(lang.Language{ID: "http", Extensions: []string{"http"}})
	lang.Register(lang.Language{ID: "go", Extensions: []string{"go"}})
	lang.Register(lang.Language{ID: "toml", Extensions: []string{"toml"}})
}

// TestIDColorScope (#1626): identifier coloring applies to logs, JSON and
// .http buffers, and to nothing else.
func TestIDColorScope(t *testing.T) {
	registerIDLangs(t)
	const id = "3f2a9c1b8e"
	for name, want := range map[string]bool{
		"a.log":  true,
		"a.json": true,
		"a.http": true,
		"a.go":   false,
		"a.toml": false,
		"a.txt":  false,
	} {
		m, _ := loadedAs(t, name, "x\nid "+id+"\n")
		got := len(m.lineIDColors(1)) > 0
		if got != want {
			t.Errorf("%s: identifier spans = %v, want %v", name, got, want)
		}
	}
}

// TestIDColorSameIdentifierSameColor (#1626): two occurrences of one
// identifier land on one slot, and a different identifier is allowed to
// differ — the whole point of hashing the identifier itself.
func TestIDColorSameIdentifierSameColor(t *testing.T) {
	registerIDLangs(t)
	m, _ := loadedAs(t, "a.log", "req 3f2a9c1b8e start\nreq 3f2a9c1b8e done\nreq 7c6b5a4d3e other\n")
	first, second := m.lineIDColors(0), m.lineIDColors(1)
	third := m.lineIDColors(2)
	if len(first) != 1 || len(second) != 1 || len(third) != 1 {
		t.Fatalf("want one span per line, got %v / %v / %v", first, second, third)
	}
	if first[0].Slot != second[0].Slot {
		t.Fatalf("same identifier on two lines got slots %d and %d", first[0].Slot, second[0].Slot)
	}
	if first[0].Slot == third[0].Slot {
		t.Skip("hash collision between the two sample identifiers")
	}
}

// TestIDColorRendering (#1626): the identifier's cells carry the rainbow
// color of its slot, and the toggle removes it again.
func TestIDColorRendering(t *testing.T) {
	registerIDLangs(t)
	m, _ := loadedAs(t, "a.log", "x\nreq 3f2a9c1b8e done\n")
	spans := m.lineIDColors(1)
	if len(spans) != 1 {
		t.Fatalf("want one span, got %v", spans)
	}
	st, ok := m.idColorStyle(spans[0])
	if !ok {
		t.Fatal("rainbow slot must resolve to a style")
	}
	marker := ansiParams(st.Render("x"))
	if marker == "" {
		t.Fatal("rainbow style renders without ANSI color")
	}
	view := m.View()
	if !strings.Contains(view, marker) {
		t.Errorf("view misses the identifier color %q:\n%q", marker, view)
	}
	if !strings.Contains(stripAnsiAll(view), "3f2a9c1b8e") {
		t.Error("identifier text must stay visible")
	}

	m.idColors = false
	m.bumpRender()
	if strings.Contains(m.View(), marker) {
		t.Error("toggle off must remove the identifier color")
	}
}

// ansiParams returns the leading SGR sequence of a rendered string.
func ansiParams(s string) string {
	end := strings.Index(s, "m")
	if !strings.HasPrefix(s, "\x1b[") || end < 0 {
		return ""
	}
	return s[:end+1]
}

// TestToggleIDColorsSticks (#1626): the per-view toggle overrides the config
// default and applyConfig no longer clobbers it.
func TestToggleIDColorsSticks(t *testing.T) {
	registerIDLangs(t)
	m, _ := loadedAs(t, "a.log", "req 3f2a9c1b8e\n")
	m.toggleIDColors()
	if m.idColors {
		t.Fatal("toggle must switch identifier colors off")
	}
	m.Configure(host.MapConfig{"editor.id_colors": "true"})
	if m.idColors {
		t.Error("applyConfig must not clobber the per-view override")
	}
}

// TestIDColorMinLengthConfig (#1626): editor.id_color_min_length moves the
// detection threshold, clamped at the package floor.
func TestIDColorMinLengthConfig(t *testing.T) {
	registerIDLangs(t)
	m, _ := loadedAs(t, "a.log", "req abc123 done\n")
	if got := m.lineIDColors(0); len(got) != 0 {
		t.Fatalf("abc123 must not color at the default minimum: %v", got)
	}
	m.Configure(host.MapConfig{"editor.id_color_min_length": "6"})
	if got := m.lineIDColors(0); len(got) != 1 {
		t.Fatalf("abc123 must color at minimum 6: %v", got)
	}
	m.Configure(host.MapConfig{"editor.id_color_min_length": "1"})
	if m.idColorMin != idcolor.MinLengthFloor {
		t.Errorf("minimum = %d, want the floor %d", m.idColorMin, idcolor.MinLengthFloor)
	}
}

// TestIDColorOffByConfig (#1626): editor.id_colors = false disables detection
// for a buffer that never toggled per view.
func TestIDColorOffByConfig(t *testing.T) {
	registerIDLangs(t)
	m, _ := loadedAs(t, "a.log", "req 3f2a9c1b8e done\n")
	m.Configure(host.MapConfig{"editor.id_colors": "false"})
	if got := m.lineIDColors(0); got != nil {
		t.Errorf("config off must yield no spans, got %v", got)
	}
}
