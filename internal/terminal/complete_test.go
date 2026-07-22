package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseCmdline(t *testing.T) {
	cases := []struct {
		before    string
		cmd, word string
	}{
		{"sh-3.2$ ma", "ma", "ma"},
		{"sh-3.2$ make do", "make", "do"},
		{"sh-3.2$ make ", "make", ""},
		{"❯ ls -lh src/ma", "ls", "src/ma"},
		{"% git st", "git", "st"},
		{"$ ", "", ""},
		{"$ echo hi && make cl", "make", "cl"},
		{"$ cat a | gr", "gr", "gr"},
		{"$ sleep 1; ls do", "ls", "do"},
		{"no prompt at all ls x", "no", "x"},
	}
	for _, tc := range cases {
		cmd, word := parseCmdline(tc.before)
		if cmd != tc.cmd || word != tc.word {
			t.Errorf("parseCmdline(%q) = (%q, %q), want (%q, %q)", tc.before, cmd, word, tc.cmd, tc.word)
		}
	}
}

func TestCommandCandidates(t *testing.T) {
	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"makeit": 0o755, "makenot": 0o644, "other": 0o755} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	got := commandCandidates(dir, "make")
	if len(got) != 1 || got[0] != "makeit" {
		t.Fatalf("commandCandidates = %v, want [makeit] (executables only, prefix match)", got)
	}
}

func TestMakeCandidates(t *testing.T) {
	dir := t.TempDir()
	mk := "VAR=1\n\nbuild: dep\n\techo x\n\ndocs lint:\n\techo y\n\n.PHONY: build\n# comment:\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(mk), 0o644); err != nil {
		t.Fatal(err)
	}
	got := makeCandidates(dir, "")
	want := []string{"build", "docs", "lint"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("makeCandidates = %v, want %v", got, want)
	}
	if got := makeCandidates(dir, "do"); len(got) != 1 || got[0] != "docs" {
		t.Fatalf("prefix filter = %v, want [docs]", got)
	}
	if got := makeCandidates(t.TempDir(), ""); len(got) != 0 {
		t.Fatalf("no Makefile must yield nothing, got %v", got)
	}
}

func TestPathCandidates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", "map.txt", ".hidden", "src/app/x.go"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := pathCandidates(dir, "ma")
	if strings.Join(got, ",") != "main.go,map.txt" {
		t.Fatalf("pathCandidates(ma) = %v", got)
	}
	// A directory keeps its trailing slash; the word's dir part is preserved.
	got = pathCandidates(dir, "s")
	if len(got) != 1 || got[0] != "src/" {
		t.Fatalf("pathCandidates(s) = %v, want [src/]", got)
	}
	got = pathCandidates(dir, "src/a")
	if len(got) != 1 || got[0] != "src/app/" {
		t.Fatalf("pathCandidates(src/a) = %v, want [src/app/]", got)
	}
	// Dotfiles only on explicit request.
	if got = pathCandidates(dir, ""); strings.Contains(strings.Join(got, ","), ".hidden") {
		t.Fatalf("dotfile leaked into %v", got)
	}
	if got = pathCandidates(dir, "."); len(got) != 1 || got[0] != ".hidden" {
		t.Fatalf("pathCandidates(.) = %v, want [.hidden]", got)
	}
}

func TestCandidatesSourceRouting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\techo x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bui.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// First word → commands; after make → targets; otherwise → paths.
	if got := candidates("make", "bui", dir, t.TempDir()); len(got) != 1 || got[0] != "build" {
		t.Fatalf("make routing = %v, want [build]", got)
	}
	if got := candidates("ls", "bui", dir, t.TempDir()); len(got) != 1 || got[0] != "bui.txt" {
		t.Fatalf("path routing = %v, want [bui.txt]", got)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "buildit"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := candidates("bui", "bui", dir, binDir); len(got) != 1 || got[0] != "buildit" {
		t.Fatalf("command routing = %v, want [buildit]", got)
	}
	// A word with a slash always completes as a path, even as the first word.
	if got := candidates("./bu", "./bu", dir, binDir); len(got) != 1 || got[0] != "./bui.txt" {
		t.Fatalf("slash word must route to paths, got %v", got)
	}
}

// startShModel spawns a live /bin/sh model for popup integration tests.
func startShModel(t *testing.T, c *collector) *Model {
	t.Helper()
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	if m.sess == nil {
		t.Fatalf("spawn failed: %s", m.err)
	}
	t.Cleanup(func() { m.Close() })
	waitFor(t, "prompt", func() bool { return strings.Contains(plainView(m.sess), "$") })
	return &m
}

// TestCtrlSpaceOpensPopupAndAccepts guards #740 end to end: typing a command
// prefix, ctrl+space opens the popup, accepting pastes the remainder.
func TestCtrlSpaceOpensPopupAndAccepts(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	waitFor(t, "echo of ec", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ec"
	})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open {
		t.Fatal("ctrl+space must open the completion popup")
	}
	sel := -1
	for i, it := range m.comp.items {
		if it == "echo" {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("PATH candidates for 'ec' must include echo, got %v", m.comp.items)
	}
	m.comp.sel = sel
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.comp.open {
		t.Fatal("accepting must close the popup")
	}
	waitFor(t, "pasted remainder", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "echo"
	})
	// The popup renders into the view while open.
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	m.SetFocused(true)
	if m.comp.open && !strings.Contains(m.View(), m.comp.items[0]) {
		t.Fatal("open popup must render into the view")
	}
}

// TestAutoSuggestTriggersOnTyping: printable keys arm the pending refresh,
// OnOutput opens the popup; esc dismisses; the config toggle disables it.
func TestAutoSuggestTriggersOnTyping(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if !m.pendingSuggest {
		t.Fatal("typing must arm the auto-suggest refresh")
	}
	waitFor(t, "echo of ec", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ec"
	})
	m.OnOutput()
	if !m.comp.open || !m.comp.auto {
		t.Fatal("OnOutput must open the auto popup for a non-empty word")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.comp.open {
		t.Fatal("esc must dismiss the popup")
	}
	// Toggle off: typing no longer arms.
	m.SetAutoSuggest(false)
	m.pendingSuggest = false
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.pendingSuggest {
		t.Fatal("autosuggest=off must not arm the refresh")
	}
}

// TestPopupInactiveOnAltScreen: a full-screen child (alt screen) disables the
// popup entirely.
func TestPopupInactiveOnAltScreen(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "printf '\\033[?1049h'\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "alt screen", func() bool { return m.sess.AltScreen() })
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if m.comp.open {
		t.Fatal("popup must stay closed on the alt screen (#740)")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.pendingSuggest {
		t.Fatal("auto-suggest must not arm on the alt screen")
	}
}

// TestCompletionFollowsCd (#770): after the shell reports its cwd via OSC 7,
// path candidates resolve against the live directory, not the start dir.
func TestCompletionFollowsCd(t *testing.T) {
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "target-file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startShModel(t, c)
	// The shell "cd"s: report the new cwd via OSC 7.
	cmd := `printf '\033]7;file://host` + other + `\033\\'` + "\r"
	for _, r := range cmd {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "cwd update", func() bool { return m.sess.Cwd() == other })
	// Path candidates for "./ta" resolve in the live cwd.
	got := candidates("./ta", "./ta", m.sess.Cwd(), "")
	if len(got) != 1 || got[0] != "./target-file.txt" {
		t.Fatalf("candidates after cd = %v, want [./target-file.txt]", got)
	}
}

// TestCandidatesFoldCase (#968): typed prefixes match case-insensitively for
// paths, make targets, and commands.
func TestCandidatesFoldCase(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"Makefile", "Documents"} {
		if err := os.MkdirAll(filepath.Join(dir, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := pathCandidates(dir, "./doc")
	if len(got) != 1 || got[0] != "./Documents/" {
		t.Fatalf("fold path candidates = %v, want [./Documents/]", got)
	}
	if got := pathCandidates(dir, "./READ"); len(got) != 1 || got[0] != "./readme.md" {
		t.Fatalf("upper-typed fold = %v, want [./readme.md]", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "Makefile", "..", "Makefile2"), []byte("Build-All:\n\techo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mdir, "Makefile"), []byte("Build-All:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := makeCandidates(mdir, "build"); len(got) != 1 || got[0] != "Build-All" {
		t.Fatalf("fold make candidates = %v, want [Build-All]", got)
	}

	bin := t.TempDir()
	exe := filepath.Join(bin, "MyTool")
	if err := os.WriteFile(exe, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := commandCandidates(bin, "myt"); len(got) != 1 || got[0] != "MyTool" {
		t.Fatalf("fold command candidates = %v, want [MyTool]", got)
	}
}

// TestAcceptCaseCorrects (#968): accepting a candidate whose case differs
// from the typed prefix erases the word and pastes the canonical case;
// exact prefixes keep the remainder paste.
func TestAcceptCaseCorrects(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	// Type "mak" at the prompt.
	for _, r := range "mak" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of mak", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "mak"
	})
	// Fake an open popup offering the case-different candidate.
	m.comp = completion{open: true, items: []string{"Makefile"}, sel: 0, word: "mak"}
	m.acceptCompletion()
	waitFor(t, "case-corrected line", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "Makefile"
	})
}
