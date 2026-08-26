package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/logline"
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

// TestStatusLineSVColumnSegment guards the csv column segment (#1659): the
// caret's column shows with the header name from the first row, and the slot
// stays hidden for ordinary files.
func TestStatusLineSVColumnSegment(t *testing.T) {
	lang.Register(lang.Language{ID: "csv", Extensions: []string{"csv"}})
	dir := t.TempDir()
	data := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(data, []byte("name,qty\napple,3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	// Widen the bar so the overflow guard cannot drop the segment under test.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = tm.(Model)
	tm, _ = m.openPath(data, false)
	m = tm.(Model)
	if line := m.statusLine(); !strings.Contains(line, "column 1: name") {
		t.Fatalf("csv column segment missing: %q", line)
	}

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(plain, false)
	m = tm.(Model)
	if line := m.statusLine(); strings.Contains(line, "column 1") {
		t.Fatalf("non-csv buffer must hide the column segment: %q", line)
	}
}

// TestStatusLineDocPathSegment guards the JSON/YAML path segment (#1660): the
// caret's path shows in a YAML buffer and the slot stays hidden elsewhere.
func TestStatusLineDocPathSegment(t *testing.T) {
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yaml", "yml"}})
	dir := t.TempDir()
	doc := filepath.Join(dir, "deploy.yaml")
	body := "spec:\n  containers:\n    - name: web\n"
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	// Widen the bar so the overflow guard cannot drop the segment under test.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = tm.(Model)
	tm, _ = m.openPath(doc, false)
	m = tm.(Model)
	if ed := m.focusedEditor(); ed != nil {
		ed.SetCursor(2, 8) // on the sequence item's key
	}
	if line := m.statusLine(); !strings.Contains(line, "spec.containers[0].name") {
		t.Fatalf("doc path segment missing: %q", line)
	}

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("spec: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(plain, false)
	m = tm.(Model)
	if line := m.statusLine(); strings.Contains(line, "spec.containers") {
		t.Fatalf("non-structured buffer must hide the path segment: %q", line)
	}
}

// TestStatusLineSearchSegment guards the in-buffer search counter (#2145): a
// committed search shows "current/total" in the status line, n advances the
// index in place, and clearing the highlights hides the slot again.
func TestStatusLineSearchSegment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hits.txt")
	if err := os.WriteFile(path, []byte("foo one\nbar\nfoo two\nfoo three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 400, Height: 30})
	m = tm.(Model)
	tm, _ = m.openPath(path, false)
	m = tm.(Model)
	m.setFocus(m.activeEditorKey())

	press := func(m Model, ks ...tea.KeyPressMsg) Model {
		for _, k := range ks {
			tm, _ := m.Update(k)
			m = tm.(Model)
		}
		return m
	}
	m = press(m, tea.KeyPressMsg{Text: "/", Code: '/'},
		tea.KeyPressMsg{Text: "f", Code: 'f'},
		tea.KeyPressMsg{Text: "o", Code: 'o'},
		tea.KeyPressMsg{Text: "o", Code: 'o'},
		tea.KeyPressMsg{Code: tea.KeyEnter})
	if line := m.statusLine(); !strings.Contains(line, "⌕ 2/3") {
		t.Fatalf("search segment missing after commit: %q", line)
	}
	m = press(m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	if line := m.statusLine(); !strings.Contains(line, "⌕ 3/3") {
		t.Fatalf("search segment did not follow n: %q", line)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if line := m.statusLine(); strings.Contains(line, "⌕") {
		t.Fatalf("cleared highlights must hide the search segment: %q", line)
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

// TestStatusLineModeBadge guards the mode badge (#1323): the mode segment
// renders in its mode colour, so the mode is recognizable without reading the
// label, and the colour changes with the mode.
func TestStatusLineModeBadge(t *testing.T) {
	m := newSized()
	m.setFocus(m.activeEditorKey())

	normal := m.statusLine()
	if !strings.Contains(normal, modeSGR(m, editor.Normal)) {
		t.Fatalf("normal mode badge missing its colour: %q", normal)
	}
	if strings.Contains(normal, modeSGR(m, editor.Insert)) {
		t.Fatalf("normal mode must not paint the insert colour: %q", normal)
	}

	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})
	insert := m.statusLine()
	if !strings.Contains(insert, "INSERT") {
		t.Fatalf("mode segment should read INSERT: %q", insert)
	}
	if !strings.Contains(insert, modeSGR(m, editor.Insert)) {
		t.Fatalf("insert mode badge missing its colour: %q", insert)
	}

	// The badge only recolours cells the layout already reserved.
	if w := lipgloss.Width(insert); w != lipgloss.Width(normal) {
		t.Fatalf("badge changed the bar width: %d vs %d", w, lipgloss.Width(normal))
	}
}

// modeSGR is the background-colour parameter a mode's badge must carry.
func modeSGR(m Model, md editor.Mode) string {
	r, g, b, _ := editor.ModeColor(md, m.pal()).RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// TestStatusLineLogSpanSegment guards the selection-span segment (#1729): a
// visual selection over a log buffer reports the elapsed time between its
// first and last timestamped line, and the segment is hidden without one.
func TestStatusLineLogSpanSegment(t *testing.T) {
	lang.Register(lang.Language{ID: "log", Extensions: []string{"log"}, Spans: logline.Spans})
	path := filepath.Join(t.TempDir(), "app.log")
	content := "10:11:10 INFO request start\n\tat Foo.bar(Foo.java:42)\n10:13:40 INFO response sent\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	m.setFocus(m.activeEditorKey())

	if line := m.statusLine(); strings.Contains(line, "Δ") {
		t.Fatalf("no selection must show no span: %q", line)
	}
	m = drainKey(m, tea.KeyPressMsg{Text: "V", Code: 'V', Mod: tea.ModShift})
	m = drainKey(m, tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = drainKey(m, tea.KeyPressMsg{Text: "j", Code: 'j'})
	if line := m.statusLine(); !strings.Contains(line, "Δ +2m 30s") {
		t.Fatalf("selection span missing from the bar: %q", line)
	}
}
