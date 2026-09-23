package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// copyTestModel opens file in a fresh model rooted at its project directory,
// with the cwd cache refreshed (#608 caches it, and the prompt resolves
// relative paths against it) and the first-start LSP dialog dismissed — it
// would otherwise swallow the scripted keys these tests press.
func copyTestModel(t *testing.T, root, file string) Model {
	t.Helper()
	t.Chdir(root)
	invalidateCwd()
	t.Cleanup(invalidateCwd)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	return m
}

// TestF5OpensCopyPromptPrefilled guards the file.copy entry point (#2696): f5
// with an editor focused opens the destination prompt prefilled with a "-copy"
// duplicate next to the source, and enter makes that copy.
func TestF5OpensCopyPromptPrefilled(t *testing.T) {
	root, file := projectDir(t)
	m := copyTestModel(t, root, file)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF5})
	if !m.fileCopyPromptOpen() {
		t.Fatal("f5 must open the copy destination prompt")
	}
	if got := m.fileCopy.dest.Input.Text; got != "a-copy.txt" {
		t.Fatalf("prefill = %q, want %q", got, "a-copy.txt")
	}

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if m.fileCopyPromptOpen() {
		t.Fatal("enter must close the prompt")
	}
	copied := filepath.Join(root, "a-copy.txt")
	if data, err := os.ReadFile(copied); err != nil || string(data) != "one\n" {
		t.Fatalf("copy = %q, %v; want the source's content", data, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the source must survive the copy: %v", err)
	}
}

// TestCopyPromptTypedDirectoryCopiesThere: typing another path copies the
// entry there instead of duplicating it in place.
func TestCopyPromptTypedDirectoryCopiesThere(t *testing.T) {
	root, file := projectDir(t)
	m := copyTestModel(t, root, file)

	m = dispatch(t, m, CopyFileMsg{})
	for range len(m.fileCopy.dest.Input.Text) {
		tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = tm.(Model)
	}
	m = typeInto(m, filepath.Join("sub", "a.txt"))
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	if _, err := os.Stat(filepath.Join(root, "sub", "a.txt")); err != nil {
		t.Fatalf("the copy must land in the typed directory: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the source must stay put: %v", err)
	}
}

// TestCopyPromptGuardsExistingDestination: an existing destination raises the
// overwrite guard, "s" (the primary answer) keeps the file, "o" replaces it.
// Nothing is ever written before an answer.
func TestCopyPromptGuardsExistingDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"skip", tea.KeyPressMsg{Code: 's', Text: "s"}, "old\n"},
		{"enter skips", tea.KeyPressMsg{Code: tea.KeyEnter}, "old\n"},
		{"esc cancels", tea.KeyPressMsg{Code: tea.KeyEscape}, "old\n"},
		{"overwrite", tea.KeyPressMsg{Code: 'o', Text: "o"}, "one\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file := projectDir(t)
			dest := filepath.Join(root, "a-copy.txt")
			if err := os.WriteFile(dest, []byte("old\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			m := copyTestModel(t, root, file)

			m = dispatch(t, m, CopyFileMsg{})
			tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = tm.(Model)
			if !m.fileCopyGuardOpen() {
				t.Fatal("an existing destination must raise the overwrite guard")
			}
			if data, _ := os.ReadFile(dest); string(data) != "old\n" {
				t.Fatalf("nothing may be written before the answer, got %q", data)
			}

			tm, cmd := m.Update(tc.key)
			m = drainCmd(tm.(Model), cmd)
			if m.fileCopyGuardOpen() {
				t.Fatal("the guard must close on an answer")
			}
			if data, _ := os.ReadFile(dest); string(data) != tc.want {
				t.Fatalf("destination = %q, want %q", data, tc.want)
			}
		})
	}
}

// TestCopyPromptEscCancels: esc in the destination prompt copies nothing.
func TestCopyPromptEscCancels(t *testing.T) {
	root, file := projectDir(t)
	m := copyTestModel(t, root, file)

	m = dispatch(t, m, CopyFileMsg{})
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.fileCopyPromptOpen() {
		t.Fatal("esc must close the prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "a-copy.txt")); err == nil {
		t.Fatal("a cancelled copy must not touch the disk")
	}
}

// TestCopyPromptHintNamesANewPath: the shared directory autocomplete says
// "new directory" when nothing matches; for a copy destination the last
// component is a file name, so the prompt re-words it (#2696).
func TestCopyPromptHintNamesANewPath(t *testing.T) {
	root, file := projectDir(t)
	m := copyTestModel(t, root, file)

	m = dispatch(t, m, CopyFileMsg{})
	body := m.fileCopy.dest.Body("hint")
	if !strings.Contains(body, fileCopyNewHint) {
		t.Fatalf("body must name the new path:\n%s", body)
	}
	if strings.Contains(body, dirPromptNewHint) {
		t.Fatalf("body must not call a file name a new directory:\n%s", body)
	}
}

// TestCopyDestNameKeepsExtension covers the prefill's naming rules: the suffix
// goes before the extension for a file, at the very end for a directory and
// for an extension-only name like ".env".
func TestCopyDestNameKeepsExtension(t *testing.T) {
	for _, tc := range []struct {
		path  string
		isDir bool
		want  string
	}{
		{filepath.Join("p", "a.txt"), false, filepath.Join("p", "a-copy.txt")},
		{filepath.Join("p", "notes"), false, filepath.Join("p", "notes-copy")},
		{filepath.Join("p", "pkg"), true, filepath.Join("p", "pkg-copy")},
		{filepath.Join("p", "app.test.go"), true, filepath.Join("p", "app.test.go-copy")},
		{filepath.Join("p", ".env"), false, filepath.Join("p", ".env-copy")},
	} {
		if got := copyDestName(tc.path, tc.isDir); got != tc.want {
			t.Errorf("copyDestName(%q, %v) = %q, want %q", tc.path, tc.isDir, got, tc.want)
		}
	}
}
