package app

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/menu"
)

// packTestModel builds a sized model whose explorer is rooted at root, with
// the initial scan applied and the first-start dialog dismissed.
func packTestModel(t *testing.T, root string) Model {
	t.Helper()
	t.Chdir(root)
	invalidateCwd()
	t.Cleanup(invalidateCwd)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := New()
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	queue := []tea.Cmd{m.Init()}
	for len(queue) > 0 {
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			out, _ := m.Update(msg)
			m = out.(Model)
		}
	}
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	m.focusExplorer()
	return m
}

// runPackCmd runs cmd's tree like drainCmd, but applies directory scans once
// (without following the poll they re-arm) so a refresh-and-select lands.
func runPackCmd(m Model, cmd tea.Cmd) Model {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		// Timers (the notice's auto-dismiss, the VCS refresh debounce) block
		// for their whole delay; they carry nothing these tests look at.
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(500 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case nil:
			continue
		case tea.BatchMsg:
			pending = append(pending, msg...)
			continue
		case explorer.ScanDoneMsg:
			tm, _ := m.Update(msg)
			m = tm.(Model)
			continue
		}
		tm, next := m.Update(msg)
		m = tm.(Model)
		pending = append(pending, next)
	}
	return m
}

// selectEntry puts the explorer cursor on path.
func selectEntry(t *testing.T, m Model, path string) Model {
	t.Helper()
	tm, cmd := m.Update(explorer.SelectPathMsg{Path: path})
	m = runPackCmd(tm.(Model), cmd)
	if got, _, _ := m.explorer().Selected(); got != path {
		t.Fatalf("selected %q, want %q", got, path)
	}
	return m
}

// runMsg dispatches msg and runs everything it produces.
func runMsg(m Model, msg tea.Msg) Model {
	tm, cmd := m.Update(msg)
	return runPackCmd(tm.(Model), cmd)
}

// menuCommands lists the command ids of a menu.
func menuCommands(items []menu.Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[it.Command] = true
	}
	return out
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGzipFile(t *testing.T, p, s string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(s))
	zw.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyInto moves a fixture archive into the project root.
func copyInto(t *testing.T, src, dir string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, filepath.Base(src))
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dest
}

// TestExplorerMenuGatesArchiveActions: the node menu offers extraction on
// archives and plain .gz files, gzip on other files and zip on directories —
// and never the wrong family (#2805).
func TestExplorerMenuGatesArchiveActions(t *testing.T) {
	root := t.TempDir()
	zipPath := copyInto(t, writeTestArchive(t, "x.zip", map[string]string{"a.txt": "a"}), root)
	tgz := copyInto(t, writeTestArchive(t, "y.tar.gz", map[string]string{"b.txt": "b"}), root)
	gz := filepath.Join(root, "app.log.gz")
	writeGzipFile(t, gz, "log\n")
	plain := filepath.Join(root, "notes.txt")
	writeFile(t, plain, "hi")
	dir := filepath.Join(root, "sub")
	writeFile(t, filepath.Join(dir, "c.txt"), "c")
	m := packTestModel(t, root)

	cases := []struct {
		path string
		want []string
		not  []string
	}{
		{zipPath, []string{"explorer.extractHere", "explorer.extractTo"}, []string{"explorer.compressGzip", "explorer.compressZip"}},
		{tgz, []string{"explorer.extractHere", "explorer.extractTo"}, []string{"explorer.compressGzip", "explorer.compressZip"}},
		{gz, []string{"explorer.extractHere", "explorer.extractTo"}, []string{"explorer.compressGzip", "explorer.compressZip"}},
		{plain, []string{"explorer.compressGzip"}, []string{"explorer.extractHere", "explorer.extractTo", "explorer.compressZip"}},
		{dir, []string{"explorer.compressZip"}, []string{"explorer.extractHere", "explorer.extractTo", "explorer.compressGzip"}},
	}
	for _, c := range cases {
		m = selectEntry(t, m, c.path)
		got := menuCommands(m.explorerMenuItems())
		for _, id := range c.want {
			if !got[id] {
				t.Errorf("%s: menu lacks %s", filepath.Base(c.path), id)
			}
		}
		for _, id := range c.not {
			if got[id] {
				t.Errorf("%s: menu must not offer %s", filepath.Base(c.path), id)
			}
		}
	}
}

// TestExplorerExtractHereZip: Extract Here unpacks x.zip into ./x/ and the
// tree selects the new directory.
func TestExplorerExtractHereZip(t *testing.T) {
	root := t.TempDir()
	p := copyInto(t, writeTestArchive(t, "x.zip", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	}), root)
	m := packTestModel(t, root)
	m = selectEntry(t, m, p)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if got, _ := os.ReadFile(filepath.Join(root, "x", "cmd", "main.go")); string(got) != "package main\n" {
		t.Fatalf("x/cmd/main.go = %q", got)
	}
	if sel, _, _ := m.explorer().Selected(); sel != filepath.Join(root, "x") {
		t.Errorf("selected %q, want the extracted directory", sel)
	}
}

// TestExplorerExtractHereTarGz: a tar.gz lands in the directory named after it
// without the compound suffix.
func TestExplorerExtractHereTarGz(t *testing.T) {
	root := t.TempDir()
	p := copyInto(t, writeTestArchive(t, "x.tar.gz", map[string]string{"a.txt": "a\n"}), root)
	m := packTestModel(t, root)
	m = selectEntry(t, m, p)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if got, _ := os.ReadFile(filepath.Join(root, "x", "a.txt")); string(got) != "a\n" {
		t.Fatalf("x/a.txt = %q", got)
	}
}

// TestExplorerExtractRefusesTraversal: a crafted zip naming ../evil.txt
// extracts its safe members and never writes outside the target.
func TestExplorerExtractRefusesTraversal(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"../evil.txt": "pwned", "ok.txt": "fine"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	p := filepath.Join(root, "bad.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m := packTestModel(t, root)
	m = selectEntry(t, m, p)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); err == nil {
		t.Fatal("a traversal member escaped the target directory")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "bad", "ok.txt")); string(got) != "fine" {
		t.Fatalf("the safe member must still extract, got %q", got)
	}
	if n := extractNotice(m); !strings.Contains(n, "unsafe path") {
		t.Errorf("notice = %q, want the unsafe-path skip reported", n)
	}
}

// TestExplorerExtractHereHonoursCap: the archive extraction cap applies to
// the explorer's entry point as well.
func TestExplorerExtractHereHonoursCap(t *testing.T) {
	root := t.TempDir()
	p := copyInto(t, writeTestArchive(t, "big.zip", map[string]string{"a.txt": strings.Repeat("x", 4096)}), root)
	gz := filepath.Join(root, "big.log.gz")
	writeGzipFile(t, gz, strings.Repeat("y", 4096))
	m := packTestModel(t, root)
	m.archExtractLimit = 1024

	m = selectEntry(t, m, p)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if _, err := os.Stat(filepath.Join(root, "big", "a.txt")); err == nil {
		t.Fatal("an archive past the cap must not extract")
	}
	m = selectEntry(t, m, gz)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if _, err := os.Stat(filepath.Join(root, "big.log")); err == nil {
		t.Fatal("a .gz past the cap must not extract")
	}
	if n := extractNotice(m); !strings.Contains(n, "cap") {
		t.Errorf("notice = %q, want the cap named", n)
	}
}

// TestExplorerExtractHereGzipAndOverwriteGuard: app.log.gz extracts to
// app.log; a second run asks first, s keeps the file and o replaces it.
func TestExplorerExtractHereGzipAndOverwriteGuard(t *testing.T) {
	root := t.TempDir()
	gz := filepath.Join(root, "app.log.gz")
	writeGzipFile(t, gz, "fresh\n")
	m := packTestModel(t, root)
	m = selectEntry(t, m, gz)
	m = runMsg(m, ExplorerExtractHereMsg{})
	out := filepath.Join(root, "app.log")
	if got, _ := os.ReadFile(out); string(got) != "fresh\n" {
		t.Fatalf("app.log = %q", got)
	}
	if sel, _, _ := m.explorer().Selected(); sel != out {
		t.Errorf("selected %q, want app.log", sel)
	}

	writeFile(t, out, "mine\n")
	m = selectEntry(t, m, gz)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if !m.packGuardOpen() {
		t.Fatal("an existing target must raise the overwrite guard")
	}
	m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // primary: keep
	if got, _ := os.ReadFile(out); string(got) != "mine\n" {
		t.Fatalf("enter must keep the existing file, got %q", got)
	}
	m = runMsg(m, ExplorerExtractHereMsg{})
	m = runMsg(m, tea.KeyPressMsg{Code: 'o', Text: "o"})
	if m.packGuardOpen() {
		t.Fatal("o must close the guard")
	}
	if got, _ := os.ReadFile(out); string(got) != "fresh\n" {
		t.Fatalf("o must overwrite, got %q", got)
	}
}

// TestExplorerExtractToOpensPrompt: Extract To… opens the archive viewer's
// target-directory prompt, and accepting it extracts and selects the target.
func TestExplorerExtractToOpensPrompt(t *testing.T) {
	root := t.TempDir()
	p := copyInto(t, writeTestArchive(t, "x.zip", map[string]string{"a.txt": "a"}), root)
	gz := filepath.Join(root, "app.log.gz")
	writeGzipFile(t, gz, "log\n")
	m := packTestModel(t, root)

	m = selectEntry(t, m, p)
	m = runMsg(m, ExplorerExtractToMsg{})
	if !m.archiveExtractPromptOpen() {
		t.Fatal("Extract To… must open the target-directory prompt")
	}
	if base := filepath.Base(m.archExtractDir.Input.Text); base != "x" {
		t.Errorf("prefill = %q, want the ./x proposal", m.archExtractDir.Input.Text)
	}
	m.archExtractDir.Input.Clear()
	m.archExtractDir.Refresh()
	m = typeInto(m, "out")
	m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got, _ := os.ReadFile(filepath.Join(root, "out", "a.txt")); string(got) != "a" {
		t.Fatalf("out/a.txt = %q", got)
	}
	if sel, _, _ := m.explorer().Selected(); sel != filepath.Join(root, "out") {
		t.Errorf("selected %q, want the target directory", sel)
	}

	m.focusExplorer()
	m = selectEntry(t, m, gz)
	m = runMsg(m, ExplorerExtractToMsg{})
	if !m.archiveExtractPromptOpen() {
		t.Fatal("Extract To… on a .gz must open the prompt too")
	}
	m.archExtractDir.Input.Clear()
	m.archExtractDir.Refresh()
	m = typeInto(m, "sub")
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got, _ := os.ReadFile(filepath.Join(root, "sub", "app.log")); string(got) != "log\n" {
		t.Fatalf("sub/app.log = %q", got)
	}
}

// TestExplorerCompressGzip: a plain file gzips to <file>.gz beside it, the
// original kept and the new file selected.
func TestExplorerCompressGzip(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	writeFile(t, src, "hello\n")
	m := packTestModel(t, root)
	m = selectEntry(t, m, src)
	m = runMsg(m, ExplorerCompressGzipMsg{})
	dest := src + ".gz"
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("notes.txt.gz: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	got.ReadFrom(zr)
	if got.String() != "hello\n" {
		t.Errorf("round trip = %q", got.String())
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("the original must be kept")
	}
	if sel, _, _ := m.explorer().Selected(); sel != dest {
		t.Errorf("selected %q, want %q", sel, dest)
	}
	// Existing target: the guard asks first.
	m = selectEntry(t, m, src)
	m = runMsg(m, ExplorerCompressGzipMsg{})
	if !m.packGuardOpen() {
		t.Fatal("an existing .gz must raise the overwrite guard")
	}
	m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.packGuardOpen() {
		t.Fatal("esc must close the guard")
	}
}

// TestExplorerCompressZip: a directory zips to <dir>.zip with relative member
// paths, and a multi-selection zips too.
func TestExplorerCompressZip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "sub", "a.txt"), "a")
	m := packTestModel(t, root)
	m = selectEntry(t, m, dir)
	m = runMsg(m, ExplorerCompressZipMsg{})
	dest := filepath.Join(root, "proj.zip")
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("proj.zip: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	sort.Strings(names)
	if strings.Join(names, ",") != "proj/,proj/main.go,proj/sub/,proj/sub/a.txt" {
		t.Errorf("members = %v, want relative paths under proj/", names)
	}
	if sel, _, _ := m.explorer().Selected(); sel != dest {
		t.Errorf("selected %q, want %q", sel, dest)
	}
}

// TestExplorerArchiveActionsRefuseWrongKind: the commands say so instead of
// acting when the selection does not fit.
func TestExplorerArchiveActionsRefuseWrongKind(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	writeFile(t, src, "hi")
	m := packTestModel(t, root)
	m = selectEntry(t, m, src)
	m = runMsg(m, ExplorerExtractHereMsg{})
	if n := extractNotice(m); !strings.Contains(n, "select an archive") {
		t.Errorf("notice = %q", n)
	}
	m = runMsg(m, ExplorerCompressZipMsg{})
	if _, err := os.Stat(filepath.Join(root, "notes.txt.zip")); err == nil {
		t.Error("zip must not run on a single file")
	}
}

// TestExplorerEKeyExtractTo: e on the tree runs explorer.extractTo (#2903) —
// the target-directory prompt on an archive, the "select an archive" notice
// on a plain file — while an open speed search still takes it as a typed
// character, and the node menu's Extract To… shows the key.
func TestExplorerEKeyExtractTo(t *testing.T) {
	root := t.TempDir()
	p := copyInto(t, writeTestArchive(t, "x.zip", map[string]string{"a.txt": "a"}), root)
	src := filepath.Join(root, "notes.txt")
	writeFile(t, src, "hi")
	m := packTestModel(t, root)
	eKey := tea.KeyPressMsg{Code: 'e', Text: "e"}

	if got := m.commandInfo(m.reg)("explorer.extractTo").Shortcut; got != "e" {
		t.Errorf("Extract To… shortcut hint = %q, want e", got)
	}

	m = selectEntry(t, m, p)
	m = runMsg(m, eKey)
	if !m.archiveExtractPromptOpen() {
		t.Fatal("e on an archive must open the target-directory prompt")
	}
	if base := filepath.Base(m.archExtractDir.Input.Text); base != "x" {
		t.Errorf("prefill = %q, want the ./x proposal", m.archExtractDir.Input.Text)
	}
	if !m.archExtractReveal {
		t.Error("the key path must reveal the target in the tree like the menu entry")
	}
	m = runMsg(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.archiveExtractPromptOpen() {
		t.Fatal("esc must close the prompt")
	}

	m.focusExplorer()
	m = selectEntry(t, m, src)
	m = runMsg(m, eKey)
	if m.archiveExtractPromptOpen() {
		t.Fatal("e on a plain file must not open the prompt")
	}
	if n := extractNotice(m); !strings.Contains(n, "select an archive") {
		t.Errorf("notice = %q, want the select-an-archive notice", n)
	}

	m = runMsg(m, explorer.SearchMsg{})
	if !m.explorer().Searching() {
		t.Fatal("speed search did not open")
	}
	m = runMsg(m, eKey)
	if m.archiveExtractPromptOpen() {
		t.Fatal("e typed into the speed search must not extract")
	}
	if !m.explorer().Searching() {
		t.Fatal("e must stay a typed character in the speed search")
	}
}
