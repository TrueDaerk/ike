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
	m.archExtractInput, m.archExtractPos = "", 0
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
	if base := filepath.Base(m.archExtractInput); base != "src" {
		t.Errorf("prefilled target %q, want a ./src proposal", m.archExtractInput)
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
