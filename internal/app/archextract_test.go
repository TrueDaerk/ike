package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/archview"
)

// extractNotice is the newest notification the model recorded — the summary
// toast the extraction raised, read from the history ring the Update pass
// drains into.
func extractNotice(m Model) string {
	if len(m.history) == 0 {
		return ""
	}
	return m.history[0].text
}

// startExtract opens the extraction prompt for a request from the pane.
func startExtract(t *testing.T, m Model, req archview.ExtractMsg) Model {
	t.Helper()
	m.onboarding = nil // the first-start dialog would own the keyboard
	out, _ := m.Update(req)
	m = out.(Model)
	if !m.archiveExtractPromptOpen() {
		t.Fatal("an extract request must open the target-directory prompt")
	}
	return m
}

// typeExtractPath clears the proposal, types dest and confirms it.
func typeExtractPath(t *testing.T, m Model, dest string) Model {
	t.Helper()
	m.archExtractDir.Input.Clear()
	m.archExtractDir.Refresh()
	for _, r := range dest {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return out.(Model)
}

// TestArchiveExtractAllWritesMembers: E extracts the whole archive into the
// typed directory and reports what landed there.
func TestArchiveExtractAllWritesMembers(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	// The proposal is a directory next to the archive, named after it.
	if base := filepath.Base(m.archExtractDir.Input.Text); base != "src" {
		t.Errorf("prefilled target %q, want a ./src proposal", m.archExtractDir.Input.Text)
	}

	dest := filepath.Join(t.TempDir(), "out")
	m = typeExtractPath(t, m, dest)
	if m.archiveExtractPromptOpen() {
		t.Error("enter must close the prompt")
	}
	for name, want := range map[string]string{"cmd/main.go": "package main\n", "README.md": "# hi\n"} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if notice := extractNotice(m); !strings.Contains(notice, "extracted 2 file(s)") {
		t.Errorf("notice = %q, want the extraction summary", notice)
	}
}

// TestArchiveExtractSingleMember: e on a row extracts exactly that member.
func TestArchiveExtractSingleMember(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p, Members: []string{"cmd/main.go"}})
	dest := filepath.Join(t.TempDir(), "out")
	m = typeExtractPath(t, m, dest)

	if _, err := os.Stat(filepath.Join(dest, "cmd", "main.go")); err != nil {
		t.Fatalf("the selected member must be extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err == nil {
		t.Fatal("only the selected member may be extracted")
	}
}

// TestArchiveExtractEscapeCancels: esc closes the prompt and writes nothing.
func TestArchiveExtractEscapeCancels(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.tar", map[string]string{"a.txt": "A\n"})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.archiveExtractPromptOpen() {
		t.Fatal("esc must close the prompt")
	}
	if _, err := os.Stat(defaultExtractDir(p)); err == nil {
		t.Fatal("a cancelled extraction must not create the target directory")
	}
}

// TestArchiveExtractOverwritePrompt: an existing target raises the guard, "s"
// keeps the file on disk and "o" replaces it.
func TestArchiveExtractOverwritePrompt(t *testing.T) {
	p := writeTestArchive(t, "src.tar", map[string]string{"a.txt": "from archive\n", "b.txt": "b\n"})
	dest := t.TempDir()
	existing := filepath.Join(dest, "a.txt")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Skip: the existing file survives, the rest is extracted.
	m := newSized()
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)
	if !m.archiveExtractGuardOpen() {
		t.Fatal("an existing target must raise the overwrite guard")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = out.(Model)
	if m.archiveExtractGuardOpen() {
		t.Fatal("answering must close the guard")
	}
	if got, _ := os.ReadFile(existing); string(got) != "mine\n" {
		t.Fatalf("a.txt = %q, want the untouched file", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "b.txt")); err != nil {
		t.Fatalf("the non-conflicting member must still be extracted: %v", err)
	}
	if notice := extractNotice(m); !strings.Contains(notice, archive.SkipExists) {
		t.Errorf("notice = %q, want the skipped file reported", notice)
	}

	// Overwrite: the member replaces it.
	m = newSized()
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = out.(Model)
	if got, _ := os.ReadFile(existing); string(got) != "from archive\n" {
		t.Fatalf("a.txt = %q, want the archive's version", got)
	}

	// Cancel: nothing changes.
	if err := os.WriteFile(existing, []byte("mine again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = newSized()
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.archiveExtractGuardOpen() {
		t.Fatal("esc must close the guard")
	}
	if got, _ := os.ReadFile(existing); string(got) != "mine again\n" {
		t.Fatalf("a.txt = %q, want the file untouched by a cancelled run", got)
	}
}

// TestArchiveExtractGuardEnterSkips: enter answers the guard's primary option,
// which is skipping — confirming a prompt must never be what overwrites files.
func TestArchiveExtractGuardEnterSkips(t *testing.T) {
	p := writeTestArchive(t, "src.tar", map[string]string{"a.txt": "from archive\n"})
	dest := t.TempDir()
	existing := filepath.Join(dest, "a.txt")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)
	if !m.archiveExtractGuardOpen() {
		t.Fatal("an existing target must raise the overwrite guard")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if got, _ := os.ReadFile(existing); string(got) != "mine\n" {
		t.Fatalf("a.txt = %q, want enter to have skipped it", got)
	}
}

// TestArchiveExtractCapMessage: an archive whose members exceed the cap is
// refused with a message naming both sizes, and nothing is written.
func TestArchiveExtractCapMessage(t *testing.T) {
	m := newSized()
	m.archExtractLimit = 100
	p := writeTestArchive(t, "big.tar", map[string]string{"big.txt": strings.Repeat("x", 4096)})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	dest := filepath.Join(t.TempDir(), "out")
	m = typeExtractPath(t, m, dest)

	notice := extractNotice(m)
	if !strings.Contains(notice, "exceeds") || !strings.Contains(notice, "cap") {
		t.Fatalf("notice = %q, want the cap refusal", notice)
	}
	if !strings.Contains(notice, "4.0 KB") || !strings.Contains(notice, "100 B") {
		t.Errorf("notice = %q, want the offending size and the ceiling", notice)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a refused extraction must not create the target directory")
	}

	// The same archive extracts once the ceiling allows it.
	m.archExtractLimit = 1 << 20
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)
	if _, err := os.Stat(filepath.Join(dest, "big.txt")); err != nil {
		t.Fatalf("within the cap the member must land on disk: %v", err)
	}
}

// TestArchiveExtractSanitizesPaths: a crafted member never escapes the target
// directory, and the summary says how many entries were skipped.
func TestArchiveExtractSanitizesPaths(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "evil.tar", map[string]string{
		"../escape.txt": "evil\n",
		"ok.txt":        "fine\n",
	})
	root := t.TempDir()
	dest := filepath.Join(root, "out")
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	m = typeExtractPath(t, m, dest)

	if _, err := os.Stat(filepath.Join(root, "escape.txt")); err == nil {
		t.Fatal("a traversing member escaped the target directory")
	}
	if _, err := os.Stat(filepath.Join(dest, "ok.txt")); err != nil {
		t.Fatalf("the safe member must be extracted: %v", err)
	}
	notice := extractNotice(m)
	if !strings.Contains(notice, "1 skipped") || !strings.Contains(notice, archive.SkipUnsafePath) {
		t.Errorf("notice = %q, want the skipped unsafe path reported", notice)
	}
}

// TestDefaultExtractDirStripsArchiveSuffix: the proposal drops the compound
// suffixes too, so backup.tar.gz proposes ./backup.
func TestDefaultExtractDirStripsArchiveSuffix(t *testing.T) {
	cases := map[string]string{
		"/tmp/backup.tar.gz":  "/tmp/backup",
		"/tmp/backup.tar.bz2": "/tmp/backup",
		"/tmp/backup.tgz":     "/tmp/backup",
		"/tmp/backup.tar":     "/tmp/backup",
		"/tmp/backup":         "/tmp/backup",
	}
	for in, want := range cases {
		if got := defaultExtractDir(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("defaultExtractDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestArchiveExtractZipWritesMembers: E on a zip runs the same extraction
// path as on a tar and reports the same summary (#2594).
func TestArchiveExtractZipWritesMembers(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.zip", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p})
	dest := filepath.Join(t.TempDir(), "out")
	m = typeExtractPath(t, m, dest)
	for name, want := range map[string]string{"cmd/main.go": "package main\n", "README.md": "# hi\n"} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if notice := extractNotice(m); !strings.Contains(notice, "extracted 2 file(s)") {
		t.Errorf("notice = %q, want the extraction summary", notice)
	}
}

// TestArchiveExtractZipSingleMember: e on a zip row extracts that member only.
func TestArchiveExtractZipSingleMember(t *testing.T) {
	m := newSized()
	p := writeTestArchive(t, "src.zip", map[string]string{
		"cmd/main.go": "package main\n",
		"README.md":   "# hi\n",
	})
	m = startExtract(t, m, archview.ExtractMsg{Archive: p, Members: []string{"cmd/main.go"}})
	dest := filepath.Join(t.TempDir(), "out")
	m = typeExtractPath(t, m, dest)
	if _, err := os.Stat(filepath.Join(dest, "cmd", "main.go")); err != nil {
		t.Fatalf("the selected member must be extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err == nil {
		t.Fatal("only the selected member may be extracted")
	}
}

// --- the live directory autocomplete (#2689) ---

// sep is the platform path separator, the suffix every directory candidate
// carries.
var sep = string(filepath.Separator)

// extractProjectRoot makes root the project root while the process working
// directory stays somewhere else, so a test proves a relative input resolves
// against the root and not against the cwd. It returns the root as the model
// sees it (macOS resolves the temp directory's symlink).
func extractProjectRoot(t *testing.T, root string) string {
	t.Helper()
	t.Chdir(root)
	invalidateCwd()
	seen, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	// Move the process away again: the cache keeps pointing at the root, so
	// completion that reached for os.Getwd() would look in the wrong place.
	t.Chdir(t.TempDir())
	t.Cleanup(invalidateCwd)
	return seen
}

// extractFixture writes an archive named src.tar into root.
func extractFixture(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	data, err := os.ReadFile(writeTestArchive(t, "src.tar", files))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "src.tar")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// extractDirTree is the project root every autocomplete test browses: three
// directories with overlapping prefixes plus a file that must never show up.
func extractDirTree(t *testing.T) (root, archivePath string) {
	t.Helper()
	root = extractProjectRoot(t, t.TempDir())
	for _, d := range []string{"outer", "src-backup", filepath.Join("out", "nested")} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "output.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, extractFixture(t, root, map[string]string{"a.txt": "A\n"})
}

// typeExtract types text into the open prompt, one key at a time.
func typeExtract(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	return m
}

// retypeExtract clears the proposal and types text.
func retypeExtract(t *testing.T, m Model, text string) Model {
	t.Helper()
	m.archExtractDir.Input.Clear()
	m.archExtractDir.Refresh()
	return typeExtract(t, m, text)
}

// pressExtract sends one key to the open prompt.
func pressExtract(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	out, _ := m.Update(key)
	return out.(Model)
}

// TestArchiveExtractPromptListsDirectoriesLive: the prompt offers the
// directories matching the prefilled proposal straight away, narrows on every
// keystroke, and never offers a file.
func TestArchiveExtractPromptListsDirectoriesLive(t *testing.T) {
	_, p := extractDirTree(t)
	m := startExtract(t, newSized(), archview.ExtractMsg{Archive: p})

	// Opening: the proposal is "src", matching src-backup/ in its parent.
	if got := m.archExtractDir.cands; !equalStrings(got, []string{"src-backup" + sep}) {
		t.Fatalf("candidates on open = %v, want [src-backup/]", got)
	}

	m = retypeExtract(t, m, "o")
	want := []string{"out" + sep, "outer" + sep}
	if got := m.archExtractDir.cands; !equalStrings(got, want) {
		t.Fatalf("candidates for %q = %v, want %v — dirs only, output.txt must not be offered", "o", got, want)
	}
	m = typeExtract(t, m, "ute")
	if got := m.archExtractDir.cands; !equalStrings(got, []string{"outer" + sep}) {
		t.Fatalf("candidates for %q = %v, want [outer/]", "oute", got)
	}
}

// TestArchiveExtractPromptSelectAndTab: down/up walk the list and tab
// completes the input to the highlight, with a trailing separator so typing
// continues inside the directory.
func TestArchiveExtractPromptSelectAndTab(t *testing.T) {
	_, p := extractDirTree(t)
	m := startExtract(t, newSized(), archview.ExtractMsg{Archive: p})
	m = retypeExtract(t, m, "o")

	if _, ok := m.archExtractDir.Highlighted(); ok {
		t.Fatal("typing must leave the list unhighlighted")
	}
	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got, _ := m.archExtractDir.Highlighted(); got != "out"+sep {
		t.Fatalf("down highlighted %q, want out/", got)
	}
	m = pressExtract(t, m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if got, _ := m.archExtractDir.Highlighted(); got != "outer"+sep {
		t.Fatalf("ctrl+n highlighted %q, want outer/", got)
	}
	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got, _ := m.archExtractDir.Highlighted(); got != "out"+sep {
		t.Fatalf("up highlighted %q, want out/", got)
	}

	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.archExtractDir.Input.Text; got != "out"+sep {
		t.Fatalf("tab completed to %q, want out/", got)
	}
	// Inside out/ the list is that directory's own children.
	if got := m.archExtractDir.cands; !equalStrings(got, []string{"out" + sep + "nested" + sep}) {
		t.Fatalf("candidates after tab = %v, want [out/nested/]", got)
	}
}

// TestArchiveExtractPromptEnterOnHighlight: enter on a highlighted candidate
// extracts into that directory, resolved against the project root even though
// the process working directory is elsewhere.
func TestArchiveExtractPromptEnterOnHighlight(t *testing.T) {
	root, p := extractDirTree(t)
	m := startExtract(t, newSized(), archview.ExtractMsg{Archive: p})
	m = retypeExtract(t, m, "o")
	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.archiveExtractPromptOpen() {
		t.Fatal("enter on a highlight must close the prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "out", "a.txt")); err != nil {
		t.Fatalf("the highlighted directory must receive the member: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "out")); err == nil {
		t.Fatal("a relative target must resolve against the project root, not the cwd")
	}
}

// TestArchiveExtractPromptNewDirectory: a name nothing matches is still a
// valid answer — the list says so and enter creates the directory.
func TestArchiveExtractPromptNewDirectory(t *testing.T) {
	root, p := extractDirTree(t)
	m := startExtract(t, newSized(), archview.ExtractMsg{Archive: p})
	m = retypeExtract(t, m, "brand-new")

	if len(m.archExtractDir.cands) != 0 {
		t.Fatalf("candidates = %v, want none for an unmatched name", m.archExtractDir.cands)
	}
	if body := m.archExtractDir.Body("hint"); !strings.Contains(body, dirPromptNewHint) {
		t.Fatalf("body = %q, want the %q hint", body, dirPromptNewHint)
	}
	m = pressExtract(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(filepath.Join(root, "brand-new", "a.txt")); err != nil {
		t.Fatalf("an unmatched name must still be extracted into: %v", err)
	}
}

// TestArchiveExtractPromptClickAcceptsCandidate: a click on a directory row is
// enter on that highlight.
func TestArchiveExtractPromptClickAcceptsCandidate(t *testing.T) {
	root, p := extractDirTree(t)
	m := startExtract(t, newSized(), archview.ExtractMsg{Archive: p})
	m = retypeExtract(t, m, "oute")

	m = shellRowClick(t, m, dirPromptHeaderRows) // the first candidate row
	if m.archiveExtractPromptOpen() {
		t.Fatal("a click on a candidate must accept it and close the prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "outer", "a.txt")); err != nil {
		t.Fatalf("the clicked directory must receive the member: %v", err)
	}
}

// equalStrings compares two candidate lists.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
