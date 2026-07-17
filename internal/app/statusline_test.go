package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
