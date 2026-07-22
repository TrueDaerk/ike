package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/lang"
)

// segToolchain is a test toolchain whose interpreter is a fixed path.
type segToolchain struct{ path string }

func (t segToolchain) Detect(string) (map[string]any, bool) {
	return map[string]any{"p": t.path}, true
}
func (t segToolchain) Interpreter(string) (string, bool) { return t.path, t.path != "" }

// TestStatusLineNotificationCounter guards the counter segment (#101): drained
// notifications raise "● N", and opening the history view resets it.
func TestStatusLineNotificationCounter(t *testing.T) {
	m := newSized()
	m.setFocus(m.activeEditorKey())
	m.host.Notify(host.Info, "one")
	m.host.Notify(host.Warn, "two")
	m.drainNotifications()
	if line := m.statusLine(); !strings.Contains(line, "● 2") {
		t.Fatalf("two unseen notifications should show '● 2': %q", line)
	}
	tm, _ := m.Update(ShowNotificationHistoryMsg{})
	m = tm.(Model)
	if line := m.statusLine(); strings.Contains(line, "●") {
		t.Fatalf("opening the history must reset the counter: %q", line)
	}
}

// TestStatusLineToolchainSegment guards the toolchain segment (#101): a
// language with a resolvable interpreter shows "<lang>:<name>" for the focused
// buffer — the venv directory's name when the binary lives in a venv.
func TestStatusLineToolchainSegment(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, "proj-env")
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(venv, "bin", "python")
	for path, data := range map[string]string{interp: "#!/bin/sh\n", filepath.Join(venv, "pyvenv.cfg"): "home = /usr\n"} {
		if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lang.Register(lang.Language{ID: "segtc", Extensions: []string{"stc"}, Toolchain: segToolchain{path: interp}})

	m := newSized()
	code := filepath.Join(dir, "main.stc")
	if err := os.WriteFile(code, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The temp path in the file segment is long; widen the bar so the #659
	// truncation guard cannot clip the segment under test.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = tm.(Model)
	tm, _ = m.openPath(code, false)
	m = tm.(Model)
	if line := m.statusLine(); !strings.Contains(line, "segtc:proj-env") {
		t.Fatalf("toolchain segment should name the venv: %q", line)
	}

	// The label is cached per language; a config reload drops the cache so an
	// interpreter change re-resolves.
	if _, ok := m.toolchainSeg["segtc"]; !ok {
		t.Fatal("toolchain label should be cached after a render")
	}
	m.reloadConfig(config.Get())
	if _, ok := m.toolchainSeg["segtc"]; ok {
		t.Fatal("config reload must drop the cached toolchain labels")
	}
}

// TestStatusLineSegmentsExtensible guards the slot model (#101): an appended
// segment renders without touching statusLine(), and an empty render hides
// the slot (no dangling divider).
func TestStatusLineSegmentsExtensible(t *testing.T) {
	orig := statusLeft
	defer func() { statusLeft = orig }()
	statusLeft = append(statusLeft,
		statusSegment{id: "custom", render: func(Model, *editor.Model) string { return "CUSTOM-SEG" }},
		statusSegment{id: "hidden", render: func(Model, *editor.Model) string { return "" }},
	)
	m := newSized()
	m.setFocus(m.activeEditorKey())
	line := m.statusLine()
	if !strings.Contains(line, "│ CUSTOM-SEG") {
		t.Fatalf("appended segment missing from the status line: %q", line)
	}
	if strings.Contains(line, "CUSTOM-SEG │  ") || strings.Contains(line, "│ │") {
		t.Fatalf("empty segment must not leave a dangling divider: %q", line)
	}
}

// composeStatus (#471): priority-aware shrinking instead of blunt clipping.

func segList(pairs ...string) []renderedSeg {
	var out []renderedSeg
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, renderedSeg{id: pairs[i], text: pairs[i+1]})
	}
	return out
}

func TestComposeStatusFitsUntouched(t *testing.T) {
	left := segList("mode", "NORMAL", "file", "main.go")
	right := segList("cursor", "Ln 1, Col 1")
	line := composeStatus(left, right, 80)
	if lipgloss.Width(line) != 80 {
		t.Fatalf("width = %d, want 80", lipgloss.Width(line))
	}
	for _, want := range []string{"NORMAL", "main.go", "Ln 1, Col 1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

func TestComposeStatusShrinksFileFirst(t *testing.T) {
	longPath := "internal/some/deeply/nested/directory/with/a/really/long/file_name.go"
	left := segList("mode", "NORMAL", "file", longPath, "eol", "LF")
	right := segList("cursor", "Ln 120, Col 42")
	line := composeStatus(left, right, 60)
	if w := lipgloss.Width(line); w > 60 {
		t.Fatalf("width = %d, want <= 60", w)
	}
	// The cursor — the high-value right segment — survives; the path gets a
	// middle ellipsis instead.
	if !strings.Contains(line, "Ln 120, Col 42") {
		t.Fatalf("cursor segment must survive: %q", line)
	}
	if !strings.Contains(line, "…") || strings.Contains(line, longPath) {
		t.Fatalf("file path must shrink with a middle ellipsis: %q", line)
	}
	if !strings.Contains(line, "LF") {
		t.Fatalf("eol must survive while the path alone absorbs the overflow: %q", line)
	}
}

func TestComposeStatusDropsLowPriorityInOrder(t *testing.T) {
	left := segList(
		"mode", "NORMAL",
		"file", "a/deep/path/to/the/current/buffer/file_with_long_name.go",
		"eol", "LF", "encoding", "UTF-8", "indent", "Spaces: 4",
		"diagnostics", "3E 1W",
	)
	right := segList("branch", "⎇ feature/very-long-branch", "cursor", "Ln 9, Col 3")
	line := composeStatus(left, right, 50)
	if w := lipgloss.Width(line); w > 50 {
		t.Fatalf("width = %d, want <= 50", w)
	}
	// eol/encoding/indent drop before diagnostics and the cursor.
	for _, gone := range []string{"LF", "UTF-8", "Spaces"} {
		if strings.Contains(line, gone) {
			t.Fatalf("%q should have dropped: %q", gone, line)
		}
	}
	if !strings.Contains(line, "Ln 9, Col 3") {
		t.Fatalf("cursor must survive: %q", line)
	}
	if !strings.Contains(line, "3E 1W") {
		t.Fatalf("diagnostics outrank eol/encoding/indent: %q", line)
	}
}

func TestComposeStatusHardClipLastResort(t *testing.T) {
	left := segList("mode", "NORMAL", "file", "somefile_with_a_name.go")
	right := segList("cursor", "Ln 1, Col 1")
	line := composeStatus(left, right, 20) // narrower than mode+min file+cursor
	if w := lipgloss.Width(line); w > 20 {
		t.Fatalf("width = %d, want <= 20", w)
	}
}

func TestMiddleEllipsis(t *testing.T) {
	if got := middleEllipsis("abcdefghij", 7); got != "abc…hij" || len([]rune(got)) != 7 {
		t.Fatalf("middleEllipsis = %q", got)
	}
	if got := middleEllipsis("short", 10); got != "short" {
		t.Fatalf("no-op expected, got %q", got)
	}
	if got := middleEllipsis("abcdef", 1); got != "…" {
		t.Fatalf("max 1 = %q", got)
	}
}
