package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
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
	return startShModelIn(t, c, t.TempDir())
}

// startShModelIn is startShModel with an explicit working directory, for tests
// that need known files to complete against.
func startShModelIn(t *testing.T, c *collector, dir string) *Model {
	t.Helper()
	m := New("terminal", "/bin/sh", dir, 80, 24, nil, c.send)
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

// TestAcceptDirectoryClosesPopup (#1335): tab-accepting a directory inserts it
// and ends the completion interaction. The popup used to re-open on the
// accepted directory's contents, so the next Enter — the natural key to run
// the command — accepted a preselected child instead of submitting, turning
// `cd an` → tab → enter into `cd ansible/ansible.cfg`.
func TestAcceptDirectoryClosesPopup(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ansible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ansible", "ansible.cfg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startShModelIn(t, c, dir)
	for _, r := range "cd an" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of 'cd an'", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "an"
	})
	m.OnOutput()
	if !m.comp.open || m.comp.items[0] != "ansible/" {
		t.Fatalf("auto popup must suggest ansible/, got open=%v items=%v", m.comp.open, m.comp.items)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.comp.open {
		t.Fatal("tab-accepting a directory must close the popup")
	}
	if m.pendingSuggest {
		t.Fatal("tab-accept must not arm an auto-suggest refresh")
	}
	waitFor(t, "pasted directory", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ansible/"
	})
	// The echo of the pasted remainder must not reopen the popup either.
	m.OnOutput()
	if m.comp.open {
		t.Fatal("the accepted directory must not auto-reopen the popup")
	}
	// Enter is therefore not consumed: it reaches the shell and submits.
	if m.completionKey("enter") {
		t.Fatal("enter after a tab-accept must reach the shell")
	}

	// Typing on still completes inside the accepted directory.
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !m.pendingSuggest {
		t.Fatal("typing after an accept must arm the refresh again")
	}
	waitFor(t, "echo of 'ansible/a'", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ansible/a"
	})
	m.OnOutput()
	if !m.comp.open || m.comp.items[0] != "ansible/ansible.cfg" {
		t.Fatalf("continued typing must complete inside the directory, got open=%v items=%v", m.comp.open, m.comp.items)
	}
}

// TestEnterFocusRule (#1432): an auto-suggest popup opens unfocused — enter
// passes through to the shell and runs the typed line; up/down focus it, and
// only then does enter accept. Tab accepts regardless of focus, esc closes
// regardless of focus, and a ctrl+space popup opens focused.
func TestEnterFocusRule(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	openAuto := func() {
		for _, r := range "ec" {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		waitFor(t, "echo of ec", func() bool {
			_, word := parseCmdline(m.lineBeforeCursor())
			return word == "ec"
		})
		m.OnOutput()
		if !m.comp.open {
			t.Fatal("auto-suggest must open the popup")
		}
	}
	promptX, _ := m.sess.CursorPosition()
	clearLine := func() {
		m.comp = completion{}
		m.pendingSuggest = false
		m.sess.SendKey(keyFor('\x15')) // ctrl+u clears the shell line
		waitFor(t, "cleared line", func() bool {
			x, _ := m.sess.CursorPosition()
			return x == promptX
		})
	}

	// Auto popup starts unfocused: enter is not consumed and closes the popup.
	openAuto()
	if m.comp.focused {
		t.Fatal("an auto-suggest popup must open unfocused")
	}
	if m.completionKey("enter") {
		t.Fatal("enter on an unfocused popup must reach the shell")
	}
	if m.comp.open {
		t.Fatal("enter must close the unfocused popup")
	}
	waitFor(t, "line unchanged", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ec" // no candidate inserted
	})
	clearLine()

	// down focuses; enter then accepts the selection.
	openAuto()
	if !m.completionKey("down") || !m.comp.focused {
		t.Fatal("down must focus the popup")
	}
	if !m.completionKey("enter") {
		t.Fatal("enter on a focused popup must accept")
	}
	if m.comp.open {
		t.Fatal("accepting must close the popup")
	}
	waitFor(t, "candidate inserted", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word != "ec" && strings.HasPrefix(strings.ToLower(word), "ec")
	})
	clearLine()

	// Tab accepts even unfocused.
	openAuto()
	if !m.completionKey("tab") {
		t.Fatal("tab must accept regardless of focus")
	}
	waitFor(t, "candidate inserted by tab", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word != "ec" && strings.HasPrefix(strings.ToLower(word), "ec")
	})
	clearLine()

	// Esc closes a focused popup without accepting.
	openAuto()
	m.completionKey("down")
	if !m.completionKey("esc") || m.comp.open {
		t.Fatal("esc must close the focused popup")
	}
	clearLine()

	// ctrl+space opens focused: enter accepts right away.
	openAuto()
	m.completionKey("esc")
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open || !m.comp.focused {
		t.Fatal("ctrl+space must open the popup focused")
	}
	if !m.completionKey("enter") {
		t.Fatal("enter after ctrl+space must accept right away")
	}
}

// TestAutoRefreshKeepsFocus (#1432): typing on with a focused popup keeps the
// focus across the auto refresh, so enter still accepts.
func TestAutoRefreshKeepsFocus(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	waitFor(t, "echo of e", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "e"
	})
	m.OnOutput()
	if !m.comp.open {
		t.Fatal("auto popup must open")
	}
	m.completionKey("down")
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	waitFor(t, "echo of ec", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ec"
	})
	m.OnOutput()
	if !m.comp.open || !m.comp.focused {
		t.Fatalf("auto refresh must keep focus, got open=%v focused=%v", m.comp.open, m.comp.focused)
	}
}

// startNarrowShModel spawns a /bin/sh model narrow enough that a short typed
// command soft-wraps, for the wrapped-line tests (#1431).
func startNarrowShModel(t *testing.T, c *collector, dir string) *Model {
	t.Helper()
	m := New("terminal", "/bin/sh", dir, 30, 24, nil, c.send)
	if m.sess == nil {
		t.Fatalf("spawn failed: %s", m.err)
	}
	t.Cleanup(func() { m.Close() })
	waitFor(t, "prompt", func() bool { return strings.Contains(plainView(m.sess), "$") })
	waitFor(t, "at prompt", func() bool { return m.completionActive() })
	return &m
}

// TestWrappedLineAutoSuggestSilent (#1431): a command longer than the pane
// width soft-wraps; the continuation row alone used to parse as a fresh short
// command ("ls" here) and command candidates opened the popup on garbage. The
// logical line is now joined across the wrap, so the true word (a nonsense
// blob with no candidates) keeps auto-suggest silent, and the parse still
// sees the real command head past the wrap.
func TestWrappedLineAutoSuggestSilent(t *testing.T) {
	c := &collector{}
	m := startNarrowShModel(t, c, t.TempDir())
	promptX, promptY := m.sess.CursorPosition()

	// Fill the first row exactly so the tail "ls" lands on the next row.
	word := strings.Repeat("q", 30-promptX-len("echo ")) + "ls"
	for _, r := range "echo " + word {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "cursor wrapped to the next row", func() bool {
		_, y := m.sess.CursorPosition()
		return y > promptY
	})
	waitFor(t, "echoed wrapped line", func() bool {
		cmd, w := parseCmdline(m.lineBeforeCursor())
		return cmd == "echo" && w == word
	})
	m.OnOutput()
	if m.comp.open {
		t.Fatalf("auto-suggest must stay silent on a wrapped line, got items %v", m.comp.items)
	}
}

// TestWrappedLineCtrlSpaceCompletes (#1431): explicit ctrl+space on a wrapped
// line completes against the word under the cursor of the *logical* line —
// here a path prefix split by the wrap ("ta" ends the first row, "rg" opens
// the continuation row), whose tail used to be mistaken for a fresh command
// word.
func TestWrappedLineCtrlSpaceCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startNarrowShModel(t, c, dir)
	promptX, promptY := m.sess.CursorPosition()

	// "echo <filler> targ": "ta" fills the last two columns of the first row,
	// "rg" wraps onto the continuation row mid-word.
	filler := strings.Repeat("q", 30-promptX-len("echo ")-len(" ta"))
	for _, r := range "echo " + filler + " targ" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "cursor wrapped to the next row", func() bool {
		_, y := m.sess.CursorPosition()
		return y > promptY
	})
	waitFor(t, "echoed wrapped line", func() bool {
		cmd, w := parseCmdline(m.lineBeforeCursor())
		return cmd == "echo" && w == "targ"
	})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0] != "target-file.txt" {
		t.Fatalf("ctrl+space on a wrapped line = open=%v items=%v, want [target-file.txt]",
			m.comp.open, m.comp.items)
	}
}

// TestPopupInactiveWhileProgramRuns (#1340): the completion popup is a shell
// feature. While a foreground program reads stdin its suggestions are wrong,
// and its keys must reach the program — so typing never opens it, ctrl+space
// does nothing, an already-open popup is dropped, and popup-bound keys are not
// consumed. Completion returns with the prompt.
func TestPopupInactiveWhileProgramRuns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startShModelIn(t, c, dir)
	waitFor(t, "idle prompt", func() bool { return m.completionActive() })

	// Open the popup, then hand the terminal to a foreground program.
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	waitFor(t, "echo of t", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "t"
	})
	m.OnOutput()
	if !m.comp.open {
		t.Fatal("auto-suggest must open the popup at the prompt")
	}
	for _, r := range "\x15cat\r" { // ctrl+u clears the line, then run cat
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "cat owns the foreground", func() bool { return !m.completionActive() })

	// Typing into the program neither arms a refresh nor opens the popup.
	m.pendingSuggest = false
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.pendingSuggest {
		t.Fatal("typing into a running program must not arm auto-suggest")
	}
	if m.comp.open {
		t.Fatal("the open popup must be dropped once a program owns the terminal")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if m.comp.open {
		t.Fatal("ctrl+space must not open the popup while a program runs")
	}
	// Popup-bound keys stay unconsumed, so the raw route hands them to the child.
	for _, key := range []string{"tab", "enter", "up", "down", "esc"} {
		if m.completionKey(key) {
			t.Fatalf("%s must reach the running program, not the popup", key)
		}
	}

	// Back at the prompt, completion works again.
	m.sess.SendKey(vt.KeyPressEvent{Code: 'c', Mod: vt.ModCtrl}) // interrupt ends cat
	waitFor(t, "back at the prompt", func() bool { return m.completionActive() })
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	waitFor(t, "echo of 'ls t'", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "t"
	})
	m.OnOutput()
	if !m.comp.open || m.comp.items[0] != "target-file.txt" {
		t.Fatalf("completion must return with the prompt, got open=%v items=%v", m.comp.open, m.comp.items)
	}
}

// TestUnfocusedPopupNoHighlight (#1442): an unfocused popup highlights no row
// — enter would run the typed line, so a reversed first entry would falsely
// promise acceptance. Focusing brings the highlight back.
func TestUnfocusedPopupNoHighlight(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	base := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", 80)+"\n", 24), "\n")
	m.comp = completion{open: true, items: []string{"alpha", "beta"}, sel: 0, word: "a", auto: true}
	if v := m.completionView(base); strings.Contains(v, "\x1b[7m") {
		t.Fatal("unfocused popup must not reverse-video any row")
	}
	m.comp.focused = true
	if v := m.completionView(base); !strings.Contains(v, "\x1b[7m") {
		t.Fatal("focused popup must reverse-video the selected row")
	}
}

// TestAcceptTypesInsteadOfPaste (#1442): accepting sends the text as key
// presses, not a bracketed paste — zsh standout-highlights a pasted region,
// so the accepted text sat on the command line with a background. SendText
// must land on the shell line like typed input.
func TestAcceptTypesInsteadOfPaste(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	m.sess.SendText("echo hi")
	waitFor(t, "typed text echoed", func() bool {
		cmd, word := parseCmdline(m.lineBeforeCursor())
		return cmd == "echo" && word == "hi"
	})
}
