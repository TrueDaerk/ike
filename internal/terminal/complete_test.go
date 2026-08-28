package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
		{`$ ls My\ Doc`, "ls", `My\ Doc`},
		{`$ cat My\ Documents/fi`, "cat", `My\ Documents/fi`},
		{`$ ls My\ Doc `, "ls", ""},
		{`$ ls My\`, "ls", `My\`},
		{`$ My\ tool ar`, `My\ tool`, "ar"},
		{`$ echo a\;b`, "echo", `a\;b`},
	}
	for _, tc := range cases {
		cmd, word := parseCmdline(tc.before)
		if cmd != tc.cmd || word != tc.word {
			t.Errorf("parseCmdline(%q) = (%q, %q), want (%q, %q)", tc.before, cmd, word, tc.cmd, tc.word)
		}
	}
}

func TestUnescapeShellWord(t *testing.T) {
	cases := map[string]string{
		`My\ Doc`:   "My Doc",
		"plain":     "plain",
		`a\;b`:      "a;b",
		`tail\`:     "tail",
		`double\\x`: `double\x`,
	}
	for in, want := range cases {
		if got := unescapeShellWord(in); got != want {
			t.Errorf("unescapeShellWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasDanglingEscape(t *testing.T) {
	for in, want := range map[string]bool{`My\`: true, `My\ Doc`: false, `x\\`: false, "plain": false} {
		if got := hasDanglingEscape(in); got != want {
			t.Errorf("hasDanglingEscape(%q) = %v, want %v", in, got, want)
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
	if len(got) != 1 || got[0] != (candidate{text: "makeit", kind: candFinal}) {
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
	if strings.Join(candTexts(got), ",") != strings.Join(want, ",") {
		t.Fatalf("makeCandidates = %v, want %v", got, want)
	}
	// Make targets are atomic tokens: accepting one finishes the word (#2261).
	for _, c := range got {
		if c.kind != candFinal {
			t.Fatalf("make target %q must be a final candidate, got kind %v", c.text, c.kind)
		}
	}
	if got := makeCandidates(dir, "do"); len(got) != 1 || got[0].text != "docs" {
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
	if strings.Join(candTexts(got), ",") != "main.go,map.txt" {
		t.Fatalf("pathCandidates(ma) = %v", got)
	}
	// Files are final candidates (#2261): accepting one ends the argument.
	for _, c := range got {
		if c.kind != candFinal {
			t.Fatalf("file %q must be a final candidate, got kind %v", c.text, c.kind)
		}
	}
	// A directory keeps its trailing slash and its dir kind; the word's dir
	// part is preserved.
	got = pathCandidates(dir, "s")
	if len(got) != 1 || got[0] != (candidate{text: "src/", kind: candDir}) {
		t.Fatalf("pathCandidates(s) = %v, want [{src/ dir}]", got)
	}
	got = pathCandidates(dir, "src/a")
	if len(got) != 1 || got[0] != (candidate{text: "src/app/", kind: candDir}) {
		t.Fatalf("pathCandidates(src/a) = %v, want [{src/app/ dir}]", got)
	}
	// Dotfiles only on explicit request.
	if got = pathCandidates(dir, ""); strings.Contains(strings.Join(candTexts(got), ","), ".hidden") {
		t.Fatalf("dotfile leaked into %v", got)
	}
	if got = pathCandidates(dir, "."); len(got) != 1 || got[0].text != ".hidden" {
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
	if got := candidates("make", "bui", dir, nil); len(got) != 1 || got[0].text != "build" {
		t.Fatalf("make routing = %v, want [build]", got)
	}
	if got := candidates("ls", "bui", dir, nil); len(got) != 1 || got[0].text != "bui.txt" {
		t.Fatalf("path routing = %v, want [bui.txt]", got)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "buildit"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	exes := scanPathExecutables(binDir)
	if got := candidates("bui", "bui", dir, exes); len(got) != 1 ||
		got[0] != (candidate{text: "buildit", kind: candFinal}) {
		t.Fatalf("command routing = %v, want [{buildit final}]", got)
	}
	// A word with a slash always completes as a path, even as the first word.
	if got := candidates("./bu", "./bu", dir, exes); len(got) != 1 || got[0].text != "./bui.txt" {
		t.Fatalf("slash word must route to paths, got %v", got)
	}
}

// TestExeCache (#2193): a cold cache misses and starts one background scan; a
// filled same-env cache hits without rescanning; a changed env misses again
// (invalidation) while still triggering a rescan for the new env.
func TestExeCache(t *testing.T) {
	binA, binB := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(binA, "tool-a"), []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binB, "tool-b"), []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &exeCache{}
	scanned := make(chan struct{}, 4)
	done := func() { scanned <- struct{}{} }
	if _, ok := c.cached(binA, done); ok {
		t.Fatal("a cold cache must miss")
	}
	<-scanned
	names, ok := c.cached(binA, done)
	if !ok || len(names) != 1 || names[0] != "tool-a" {
		t.Fatalf("filled cache = %v ok=%v, want [tool-a] true", names, ok)
	}
	// A different PATH invalidates: miss now, hit for the new env after the
	// rescan lands.
	if _, ok := c.cached(binB, done); ok {
		t.Fatal("a changed env must miss")
	}
	<-scanned
	if names, ok := c.cached(binB, done); !ok || len(names) != 1 || names[0] != "tool-b" {
		t.Fatalf("rescanned cache = %v ok=%v, want [tool-b] true", names, ok)
	}
}

// TestColdCachePostponesRefresh (#2193): a command-word refresh against a cold
// executable cache must not scan PATH synchronously in Update — it closes the
// popup, arms the pending refresh, and completes once the scan lands.
func TestColdCachePostponesRefresh(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "ec" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of ec", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ec"
	})
	// Drop the warm cache: the next refresh sees a cold one.
	m.exe = &exeCache{}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if m.comp.open {
		t.Fatal("a cold cache must postpone the popup, not scan in Update")
	}
	if !m.pendingSuggest || !m.pendingManual {
		t.Fatalf("postponed refresh must arm pending (manual), got suggest=%v manual=%v",
			m.pendingSuggest, m.pendingManual)
	}
	waitExes(t, m)
	m.OnOutput() // the scan's OutputMsg re-runs the refresh through here
	if !m.comp.open || !m.comp.focused {
		t.Fatalf("the landed scan must reopen the popup as the manual request it was, got open=%v focused=%v",
			m.comp.open, m.comp.focused)
	}
	found := false
	for _, it := range m.comp.items {
		if it.text == "echo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reopened popup must offer echo, got %v", m.comp.items)
	}
}

// waitExes waits for the prewarmed PATH executable scan (#2193): tests drive
// Update/OnOutput synchronously, so they wait for the cache directly instead
// of the scan's OutputMsg round trip through the app.
func waitExes(t *testing.T, m *Model) {
	t.Helper()
	waitFor(t, "PATH executable scan", func() bool {
		_, ok := m.exeNames()
		return ok
	})
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
	waitExes(t, &m)
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
		if it.text == "echo" {
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
	// An executable is a finished token, so the accept ends it with a space
	// (#2261): the word under the cursor is empty again, `echo ` is on the line.
	waitFor(t, "pasted remainder and trailing space", func() bool {
		cmd, word := parseCmdline(m.lineBeforeCursor())
		return cmd == "echo" && word == "" && strings.HasSuffix(m.lineBeforeCursor(), "echo ")
	})
	// The popup renders into the view while open.
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	m.SetFocused(true)
	if m.comp.open && !strings.Contains(m.View(), m.comp.items[0].text) {
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
	got := candidates("./ta", "./ta", m.sess.Cwd(), nil)
	if len(got) != 1 || got[0].text != "./target-file.txt" {
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
	if len(got) != 1 || got[0].text != "./Documents/" {
		t.Fatalf("fold path candidates = %v, want [./Documents/]", got)
	}
	if got := pathCandidates(dir, "./READ"); len(got) != 1 || got[0].text != "./readme.md" {
		t.Fatalf("upper-typed fold = %v, want [./readme.md]", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "Makefile", "..", "Makefile2"), []byte("Build-All:\n\techo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mdir, "Makefile"), []byte("Build-All:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := makeCandidates(mdir, "build"); len(got) != 1 || got[0].text != "Build-All" {
		t.Fatalf("fold make candidates = %v, want [Build-All]", got)
	}

	bin := t.TempDir()
	exe := filepath.Join(bin, "MyTool")
	if err := os.WriteFile(exe, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := commandCandidates(bin, "myt"); len(got) != 1 || got[0].text != "MyTool" {
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
	m.comp = completion{open: true, items: []candidate{{text: "Makefile"}}, sel: 0, word: "mak"}
	m.acceptCompletion()
	waitFor(t, "case-corrected line", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "Makefile ")
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
	if !m.comp.open || m.comp.items[0] != (candidate{text: "ansible/", kind: candDir}) {
		t.Fatalf("auto popup must suggest ansible/, got open=%v items=%v", m.comp.open, m.comp.items)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.comp.open {
		t.Fatal("tab-accepting a directory must close the popup")
	}
	if m.pendingSuggest {
		t.Fatal("tab-accept must not arm an auto-suggest refresh")
	}
	// A directory ends in "/" and gets no trailing space (#2261) — the word
	// stays open so completion can descend into it.
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
	if !m.comp.open || m.comp.items[0].text != "ansible/ansible.cfg" {
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
		cmd, word := parseCmdline(m.lineBeforeCursor())
		return cmd != "ec" && word == "" && strings.HasPrefix(strings.ToLower(cmd), "ec")
	})
	clearLine()

	// Tab accepts even unfocused.
	openAuto()
	if !m.completionKey("tab") {
		t.Fatal("tab must accept regardless of focus")
	}
	waitFor(t, "candidate inserted by tab", func() bool {
		cmd, word := parseCmdline(m.lineBeforeCursor())
		return cmd != "ec" && word == "" && strings.HasPrefix(strings.ToLower(cmd), "ec")
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
	waitExes(t, &m)
	return &m
}

// TestWrappedLineAutoSuggestSilent (#1431, #2262): a command longer than the
// pane width soft-wraps; the continuation row alone used to parse as a fresh
// short command ("ls" here) and command candidates opened the popup on
// garbage. The logical line is joined across the wrap, so the true word (a
// nonsense blob with no candidates) keeps auto-suggest silent — no bogus
// suggestions for the wrapped tail.
func TestWrappedLineAutoSuggestSilent(t *testing.T) {
	c := &collector{}
	m := startNarrowShModel(t, c, t.TempDir())
	promptX, promptY := m.sess.CursorPosition()

	// Fill the first row exactly so the tail "ls" lands on the next row.
	word := strings.Repeat("q", m.gridW()-promptX-len("echo ")) + "ls"
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
	filler := strings.Repeat("q", m.gridW()-promptX-len("echo ")-len(" ta"))
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
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0].text != "target-file.txt" {
		t.Fatalf("ctrl+space on a wrapped line = open=%v items=%v, want [target-file.txt]",
			m.comp.open, m.comp.items)
	}
}

// TestWrappedLineAutoSuggestCompletes (#2262): auto-suggest keeps working on
// a continuation row of a joined soft-wrap chain — the word spanning the wrap
// boundary ("ta" ends the first row, "rg" opens the continuation row)
// completes as its full joined self, and tab-accepting pastes the correct
// remainder across the boundary.
func TestWrappedLineAutoSuggestCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startNarrowShModel(t, c, dir)
	promptX, promptY := m.sess.CursorPosition()

	filler := strings.Repeat("q", m.gridW()-promptX-len("echo ")-len(" ta"))
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
	m.OnOutput()
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0].text != "target-file.txt" {
		t.Fatalf("auto-suggest on a wrapped word = open=%v items=%v, want [target-file.txt]",
			m.comp.open, m.comp.items)
	}
	if m.comp.word != "targ" {
		t.Fatalf("popup word = %q, want the full joined word %q", m.comp.word, "targ")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	waitFor(t, "remainder pasted across the wrap", func() bool {
		return strings.Contains(m.lineBeforeCursor(), "target-file.txt ")
	})
}

// TestWrappedLineAutoSuggestAfterWrap (#2262): a fresh word typed entirely
// after the wrap boundary (the wrapped part is an earlier word) auto-suggests
// normally on the continuation row.
func TestWrappedLineAutoSuggestAfterWrap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startNarrowShModel(t, c, dir)
	promptX, promptY := m.sess.CursorPosition()

	// The filler word fills the first row exactly, so " targ" sits wholly on
	// the continuation row.
	filler := strings.Repeat("q", m.gridW()-promptX-len("echo "))
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
	m.OnOutput()
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0].text != "target-file.txt" {
		t.Fatalf("auto-suggest after the wrap = open=%v items=%v, want [target-file.txt]",
			m.comp.open, m.comp.items)
	}
}

// TestEarlyWrapBlankLastColumnSilent (#2262): the one pattern that still
// suppresses auto-suggest — the row above holds content but its last column
// is blank (the shape an early-wrapping line editor leaves), so the
// SoftWrapped heuristic cannot join the chain and the cursor row's text
// carries no prompt marker. The "word" may be a wrapped tail; completing it
// standalone was the historical garbage-suggestion bug. ctrl+space still
// completes on demand.
func TestEarlyWrapBlankLastColumnSilent(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 30, 24, []string{"PS1="}, c.send)
	if m.sess == nil {
		t.Fatalf("spawn failed: %s", m.err)
	}
	t.Cleanup(func() { m.Close() })
	waitExes(t, &m)
	// Output a row of width-1 characters: content above, last column blank —
	// exactly the shape that defeats the SoftWrapped heuristic.
	tail := strings.Repeat("q", m.gridW()-1)
	for _, r := range "echo " + tail + "\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "output row", func() bool { return strings.Contains(plainView(m.sess), tail) })
	waitFor(t, "at prompt", func() bool { return m.completionActive() })

	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	waitFor(t, "echoed ls", func() bool {
		_, w := parseCmdline(m.lineBeforeCursor())
		return w == "ls"
	})
	m.OnOutput()
	if m.comp.open {
		t.Fatalf("auto-suggest must stay silent below a blank-last-column row, got %v", m.comp.items)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open {
		t.Fatal("ctrl+space must still complete on demand")
	}
}

// TestWrappedLinePopupPosition (#2262): the popup renders sanely on a
// continuation row — a word spanning the wrap starts on the row above, so the
// anchor clamps to the continuation row's left edge and every view row stays
// within the pane width, one intact bordered row per entry.
func TestWrappedLinePopupPosition(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startNarrowShModel(t, c, dir)
	promptX, promptY := m.sess.CursorPosition()

	filler := strings.Repeat("q", m.gridW()-promptX-len("echo ")-len(" ta"))
	for _, r := range "echo " + filler + " targ" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "cursor wrapped to the next row", func() bool {
		_, y := m.sess.CursorPosition()
		return y > promptY
	})
	waitFor(t, "echoed wrapped line", func() bool {
		_, w := parseCmdline(m.lineBeforeCursor())
		return w == "targ"
	})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open {
		t.Fatal("ctrl+space must open the popup on the continuation row")
	}
	m.SetFocused(true)
	found := false
	rows := strings.Split(m.View(), "\n")
	for i, line := range rows {
		if w := ansi.StringWidth(line); w > 30 {
			t.Fatalf("view row wider than pane (%d > 30): %q", w, ansi.Strip(line))
		}
		if strings.Contains(ansi.Strip(line), "│ target-file.txt │") {
			found = true
			// The popup must sit adjacent to the cursor's continuation row.
			_, cy := m.sess.CursorPosition()
			if i < cy-len(m.comp.items)-2 || i > cy+len(m.comp.items)+2 {
				t.Fatalf("popup row %d too far from cursor row %d", i, cy)
			}
		}
	}
	if !found {
		t.Fatal("popup entry row must render intact on the continuation row")
	}
}

// TestWrappedCompletionAfterResize (#1464): the soft-wrap join must survive a
// width resize (the session reflows) — ctrl+space on a command whose argument
// wraps mid-word after the resize completes against the joined logical line,
// path candidates for the real word instead of command candidates for the
// tail.
func TestWrappedCompletionAfterResize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := New("terminal", "/bin/sh", dir, 80, 24, nil, c.send)
	if m.sess == nil {
		t.Fatalf("spawn failed: %s", m.err)
	}
	t.Cleanup(func() { m.Close() })
	waitFor(t, "prompt", func() bool { return strings.Contains(plainView(m.sess), "$") })
	waitFor(t, "at prompt", func() bool { return m.completionActive() })
	waitExes(t, &m)

	m.SetSize(30, 24)
	waitFor(t, "resized", func() bool { return m.sess.Width() == m.gridW() })
	waitFor(t, "at prompt after resize", func() bool { return m.completionActive() })
	promptX, promptY := m.sess.CursorPosition()

	// "ta" fills the last columns of the prompt row, "rg" wraps mid-word.
	filler := strings.Repeat("q", m.gridW()-promptX-len("echo ")-len(" ta"))
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
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0].text != "target-file.txt" {
		t.Fatalf("ctrl+space after resize = open=%v items=%v, want [target-file.txt]",
			m.comp.open, m.comp.items)
	}
}

// TestAutoSuggestSuppressedWithoutPrompt (#1464): when the text left of the
// cursor carries no recognizable prompt marker although the row above holds
// content, the cursor row may be an unrecognized continuation row — the
// safety net keeps auto-suggest quiet instead of completing against a wrapped
// tail. ctrl+space still completes on demand.
func TestAutoSuggestSuppressedWithoutPrompt(t *testing.T) {
	c := &collector{}
	m := New("terminal", "/bin/sh", t.TempDir(), 80, 24, []string{"PS1="}, c.send)
	if m.sess == nil {
		t.Fatalf("spawn failed: %s", m.err)
	}
	t.Cleanup(func() { m.Close() })
	waitExes(t, &m)
	for _, r := range "echo hello\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "echo output", func() bool { return strings.Contains(plainView(m.sess), "hello") })
	waitFor(t, "at prompt", func() bool { return m.completionActive() })

	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	waitFor(t, "echoed ls", func() bool {
		_, w := parseCmdline(m.lineBeforeCursor())
		return w == "ls"
	})
	m.OnOutput()
	if m.comp.open {
		t.Fatalf("auto-suggest must stay quiet without a prompt marker, got %v", m.comp.items)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open {
		t.Fatal("ctrl+space must still complete without a prompt marker")
	}
}

// TestPopupNearRightEdgeStaysIntact (#1463): a popup anchored at a word near
// the right pane edge used to overflow the pane width, so the surrounding
// render wrapped its rows and the box fell apart. The x origin is clamped so
// the whole box stays inside the pane, one intact row per entry.
func TestPopupNearRightEdgeStaysIntact(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startNarrowShModel(t, c, dir)
	promptX, promptY := m.sess.CursorPosition()

	// "echo <filler> ta" puts the cursor on the last column without wrapping.
	filler := strings.Repeat("q", m.gridW()-promptX-len("echo ")-len(" ta")-1)
	for _, r := range "echo " + filler + " ta" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echoed line", func() bool {
		_, w := parseCmdline(m.lineBeforeCursor())
		return w == "ta"
	})
	if _, y := m.sess.CursorPosition(); y != promptY {
		t.Fatalf("line must not wrap for this test, cursor moved to row %d", y)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if !m.comp.open || len(m.comp.items) != 1 || m.comp.items[0].text != "target-file.txt" {
		t.Fatalf("popup = open=%v items=%v, want [target-file.txt]", m.comp.open, m.comp.items)
	}
	m.SetFocused(true)
	found := false
	for _, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > 30 {
			t.Fatalf("view row wider than pane (%d > 30): %q", w, ansi.Strip(line))
		}
		if strings.Contains(ansi.Strip(line), "│ target-file.txt │") {
			found = true
		}
	}
	if !found {
		t.Fatal("popup entry row must stay one intact bordered row")
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
	if !m.comp.open || m.comp.items[0].text != "target-file.txt" {
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
	m.comp = completion{open: true, items: []candidate{{text: "alpha"}, {text: "beta"}}, sel: 0, word: "a", auto: true}
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

// TestAcceptStaleSnapshotUsesLiveWord (#1538): a keystroke that echoed after
// the popup's last refresh leaves the snapshot word stale; accepting must
// compute the remainder from the line as it is now, not double-type the
// in-between keys (`mkdi` + tab used to insert `ir` → `mkdiir`).
func TestAcceptStaleSnapshotUsesLiveWord(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "mkdi" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of mkdi", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "mkdi"
	})
	// Popup snapshot predates the last keystroke: computed at "mkd".
	m.comp = completion{open: true, items: []candidate{{text: "mkdir"}}, sel: 0, word: "mkd"}
	m.acceptCompletion()
	waitFor(t, "line completes to mkdir", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "mkdir ")
	})
}

// TestAcceptStaleSnapshotCaseCorrect (#1538): the case-correcting accept
// (#968) erases the live word's length, not the snapshot's — a stale snapshot
// backspaced too few characters and left typed keys behind the candidate.
func TestAcceptStaleSnapshotCaseCorrect(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "makef" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of makef", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "makef"
	})
	// Snapshot at "mak"; the live word "makef" matches "Makefile" only
	// case-insensitively, so the accept takes the erase-and-retype path and
	// must backspace 5 characters, not the snapshot's 3.
	m.comp = completion{open: true, items: []candidate{{text: "Makefile"}}, sel: 0, word: "mak"}
	m.acceptCompletion()
	waitFor(t, "case-corrected full word", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "Makefile ")
	})
}

// TestAcceptDivergedWordDropsAccept (#1538): the line moved away from the
// candidate between refresh and tab — accepting must insert nothing rather
// than stale text.
func TestAcceptDivergedWordDropsAccept(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "mx" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of mx", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "mx"
	})
	m.comp = completion{open: true, items: []candidate{{text: "mkdir"}}, sel: 0, word: "m"}
	m.acceptCompletion()
	// Any text the accept had sent would land before this probe keystroke, so
	// the word settling at exactly "mxz" proves nothing stale was inserted.
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	waitFor(t, "line unchanged plus probe", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "mxz"
	})
}

// TestEscapeShellWord (#1539): shell-special characters in inserted
// completions are backslash-escaped; plain words and a leading dash pass
// through untouched.
func TestEscapeShellWord(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain.txt", "plain.txt"},
		{"My Documents/", `My\ Documents/`},
		{"a'b", `a\'b`},
		{`a"b`, `a\"b`},
		{`back\slash`, `back\\slash`},
		{"pri$ce", `pri\$ce`},
		{"a&b|c;d", `a\&b\|c\;d`},
		{"glob*?.txt", `glob\*\?.txt`},
		{"br[ack]ets", `br\[ack\]ets`},
		{"-flagfile", "-flagfile"},
		{"hash#bang!", `hash\#bang\!`},
		{"cur{ly}~", `cur\{ly\}\~`},
	}
	for _, tc := range cases {
		if got := escapeShellWord(tc.in); got != tc.want {
			t.Errorf("escapeShellWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestAcceptEscapesSpecialChars (#1539): accepting a candidate containing a
// space types it backslash-escaped so the shell sees a single argument.
func TestAcceptEscapesSpecialChars(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "ls My" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of My", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "My"
	})
	m.comp = completion{open: true, items: []candidate{{text: "My Documents/", kind: candDir}}, sel: 0, word: "My"}
	m.acceptCompletion()
	waitFor(t, "escaped remainder on the line", func() bool {
		return strings.Contains(m.lineBeforeCursor(), `My\ Documents/`)
	})
}

// TestAcceptContinuesEscapedWord (#1552): a word already escaped on the line
// (`My\ Doc`) parses as one word, matches the unescaped candidate, and the
// accepted remainder extends it.
func TestAcceptContinuesEscapedWord(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range `ls My\ Doc` {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of escaped word", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == `My\ Doc`
	})
	m.comp = completion{open: true, items: []candidate{{text: "My Documents/", kind: candDir}}, sel: 0, word: `My\ Doc`}
	m.acceptCompletion()
	waitFor(t, "remainder extends escaped word", func() bool {
		return strings.Contains(m.lineBeforeCursor(), `My\ Documents/`)
	})
}

// TestAcceptCaseCorrectEscapes (#1539): the erase-and-retype path (#968)
// escapes the full retyped candidate too.
func TestAcceptCaseCorrectEscapes(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "ls my" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of my", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "my"
	})
	m.comp = completion{open: true, items: []candidate{{text: "My Documents/", kind: candDir}}, sel: 0, word: "my"}
	m.acceptCompletion()
	waitFor(t, "escaped case-corrected word", func() bool {
		return strings.Contains(m.lineBeforeCursor(), `My\ Documents/`)
	})
}

// candTexts is the plain-string view of a candidate list, for assertions that
// only care about the offered words.
func candTexts(cs []candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.text
	}
	return out
}

// TestCandidateAccepted (#2261) is the accept rule itself: a final candidate
// (file, executable, make target) gains a trailing space, a directory does
// not, and an existing space right of the cursor suppresses the appended one.
func TestCandidateAccepted(t *testing.T) {
	cases := []struct {
		c          candidate
		spaceAhead bool
		want       string
	}{
		{candidate{text: "main.go", kind: candFinal}, false, "main.go "},
		{candidate{text: "main.go", kind: candFinal}, true, "main.go"},
		{candidate{text: "build", kind: candFinal}, false, "build "},
		{candidate{text: "src/", kind: candDir}, false, "src/"},
		{candidate{text: "src/", kind: candDir}, true, "src/"},
	}
	for _, tc := range cases {
		if got := tc.c.accepted(tc.spaceAhead); got != tc.want {
			t.Errorf("%+v.accepted(%v) = %q, want %q", tc.c, tc.spaceAhead, got, tc.want)
		}
	}
}

// TestAcceptTrailingSpaceByType (#2261): accepting a file, a make target or a
// PATH executable finishes the argument with a space; accepting a directory
// inserts the trailing "/" alone so completion can continue inside it.
func TestAcceptTrailingSpaceByType(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ansible"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"target-file.txt", "Makefile"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("build:\n\techo x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		typed string
		word  string
		want  string
	}{
		{"file", "ls ta", "ta", "target-file.txt "},
		{"make target", "make bui", "bui", "build "},
		{"executable", "ec", "ec", "echo "},
		{"directory", "cd an", "an", "ansible/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &collector{}
			m := startShModelIn(t, c, dir)
			for _, r := range tc.typed {
				m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			waitFor(t, "echo of "+tc.typed, func() bool {
				_, word := parseCmdline(m.lineBeforeCursor())
				return word == tc.word
			})
			m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
			sel := -1
			for i, it := range m.comp.items {
				if it.text == strings.TrimSuffix(tc.want, " ") {
					sel = i
				}
			}
			if sel < 0 {
				t.Fatalf("candidates for %q must offer %q, got %v", tc.typed, tc.want, m.comp.items)
			}
			m.comp.sel = sel
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			waitFor(t, "accepted "+tc.name, func() bool {
				return strings.HasSuffix(m.lineBeforeCursor(), tc.want)
			})
			// Nothing beyond the expected tail: a directory must not gain a
			// space, a final candidate must not gain a second one.
			if line := m.lineBeforeCursor(); strings.HasSuffix(line, tc.want+" ") {
				t.Fatalf("accepted %s got an extra trailing space: %q", tc.name, line)
			}
		})
	}
}

// TestAcceptNoDoubleSpaceMidLine (#2261): accepting where the line already has
// a space right of the cursor inserts the candidate alone — the argument stays
// separated by exactly one space.
func TestAcceptNoDoubleSpaceMidLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "target-file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	m := startShModelIn(t, c, dir)
	for _, r := range "ls ta zzz" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of the line", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "ls ta zzz")
	})
	// Move the cursor back in front of the space before "zzz".
	for range "zzz " {
		m.sess.SendKey(vt.KeyPressEvent{Code: vt.KeyLeft})
	}
	waitFor(t, "cursor back at the word", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ta"
	})
	if m.spaceFollowsCursor() != true {
		t.Fatal("a space right of the cursor must be detected")
	}
	m.comp = completion{open: true, items: []candidate{{text: "target-file.txt"}}, sel: 0, word: "ta"}
	m.acceptCompletion()
	waitFor(t, "candidate inserted", func() bool {
		return strings.Contains(plainView(m.sess), "target-file.txt zzz")
	})
	if strings.Contains(plainView(m.sess), "target-file.txt  zzz") {
		t.Fatal("accept must not double the existing space")
	}
}

// TestAcceptExplicitShorterCandidate (#2261): a candidate that is a strict
// prefix of other still-matching ones is an explicit pick and counts as final
// — it gets its trailing space like any other finished token.
func TestAcceptExplicitShorterCandidate(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"target.txt", "target.txt.bak"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &collector{}
	m := startShModelIn(t, c, dir)
	for _, r := range "ls tar" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of tar", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "tar"
	})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if len(m.comp.items) != 2 || m.comp.items[0].text != "target.txt" {
		t.Fatalf("popup must offer both files, got %v", m.comp.items)
	}
	m.comp.sel = 0 // the shorter one, still a prefix of target.txt.bak
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	waitFor(t, "shorter pick finished with a space", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "target.txt ")
	})
}

// TestSpaceFollowsCursorAtLineEnd (#2261): at the end of the command line
// nothing follows the cursor, so an accepted final candidate appends its
// space.
func TestSpaceFollowsCursorAtLineEnd(t *testing.T) {
	c := &collector{}
	m := startShModel(t, c)
	for _, r := range "ls ta" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of ta", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "ta"
	})
	if m.spaceFollowsCursor() {
		t.Fatal("nothing follows the cursor at the end of the line")
	}
}
