package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/project"
	"ike/internal/vcs"
)

// cloneProjectsDir points [project] directory at a fresh temp dir and returns it.
func cloneProjectsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "projects")
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.Project.Directory = dir
	config.Set(&c)
	return dir
}

// openClone opens the clone dialog through the msg the project.clone command
// dispatches.
func openClone(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(project.OpenCloneMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.clonePromptOpen() {
		t.Fatal("project.clone must open the clone dialog")
	}
	return m
}

// TestClonePromptDerivesNameFromURL guards the type-URL-and-enter path: the
// directory name follows the URL until it is edited by hand.
func TestClonePromptDerivesNameFromURL(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)

	m = typeInto(m, "https://github.com/TrueDaerk/ike.git")
	if m.cloneName.Text != "ike" {
		t.Fatalf("derived name = %q, want %q", m.cloneName.Text, "ike")
	}

	// tab moves to the name field; typing there stops the derivation.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "-fork")
	if m.cloneName.Text != "ike-fork" {
		t.Fatalf("edited name = %q, want %q", m.cloneName.Text, "ike-fork")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "x")
	if m.cloneName.Text != "ike-fork" {
		t.Fatalf("hand-edited name must not follow the URL again, got %q", m.cloneName.Text)
	}
}

// TestClonePromptPasteIntoURLField guards #1873: a bracketed paste into the
// focused URL field inserts at the cursor and re-derives the name exactly
// like typing does.
func TestClonePromptPasteIntoURLField(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)

	tm, _ := m.Update(tea.PasteMsg{Content: "https://github.com/TrueDaerk/ike.git"})
	m = tm.(Model)

	if m.cloneURL.Text != "https://github.com/TrueDaerk/ike.git" {
		t.Fatalf("cloneURL = %q, want the pasted text", m.cloneURL.Text)
	}
	if m.cloneName.Text != "ike" {
		t.Fatalf("a paste into the URL field must re-derive the name, got %q", m.cloneName.Text)
	}
}

// TestClonePromptPasteIntoNameField: a paste into the name field lands at the
// cursor and stops the URL-follows derivation, same as typing there.
func TestClonePromptPasteIntoNameField(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)
	m = typeInto(m, "https://github.com/TrueDaerk/ike.git")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})

	tm, _ := m.Update(tea.PasteMsg{Content: "-fork"})
	m = tm.(Model)

	if m.cloneName.Text != "ike-fork" {
		t.Fatalf("cloneName = %q, want %q", m.cloneName.Text, "ike-fork")
	}
	if !m.cloneNameEdited {
		t.Fatal("a paste into the name field must mark it hand-edited")
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "x")
	if m.cloneName.Text != "ike-fork" {
		t.Fatalf("a hand-edited name (via paste) must not follow the URL again, got %q", m.cloneName.Text)
	}
}

// TestClonePromptCmdVPastesFromClipboard: Cmd+V (no bracketed-paste terminal
// support) routes the system clipboard into the focused field the same way
// (#1273 pattern, #1873).
func TestClonePromptCmdVPastesFromClipboard(t *testing.T) {
	restore := clipboardRead
	clipboardRead = func() string { return "https://example.com/org/repo.git" }
	t.Cleanup(func() { clipboardRead = restore })

	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)

	tm, _ := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	m = tm.(Model)

	if m.cloneURL.Text != "https://example.com/org/repo.git" {
		t.Fatalf("cmd+v did not paste into the URL field, got %q", m.cloneURL.Text)
	}
}

// TestCloneNameGhostUntilEdited guards #1873: the derived name renders
// dimmed (ghost text) while it merely follows the URL, so typing the URL no
// longer looks like writing into both fields at once. Once hand-edited, the
// field renders like any other real input.
func TestCloneNameGhostUntilEdited(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)

	m = typeInto(m, "https://github.com/TrueDaerk/ike.git")
	if m.cloneName.Text != "ike" {
		t.Fatalf("derived name = %q, want %q", m.cloneName.Text, "ike")
	}
	ghost := cloneGhostStyle.Render("ike")
	if !strings.Contains(m.View().Content, ghost) {
		t.Fatalf("derived name must render as dimmed ghost text while unedited:\n%s", m.View().Content)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "-fork")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the URL field; the name unfocuses

	view := m.View().Content
	if strings.Contains(view, cloneGhostStyle.Render("ike-fork")) {
		t.Fatal("a hand-edited name must no longer render as ghost text")
	}
	if !strings.Contains(plainView(m), "ike-fork") {
		t.Fatalf("hand-edited name must still render, got:\n%s", plainView(m))
	}
}

// TestClonePromptRejectsBadInput keeps validation failures in the dialog.
func TestClonePromptRejectsBadInput(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if !m.clonePromptOpen() || m.cloneRunning {
		t.Fatal("an empty URL must keep the dialog open and start no clone")
	}
	if m.cloneErr == "" {
		t.Fatal("an empty URL must be explained")
	}

	m = typeInto(m, "https://example.com/org/repo.git")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "/absolute")
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if m.cloneRunning {
		t.Fatal("a name with a path separator must not start a clone")
	}
	if !strings.Contains(m.cloneErr, "separator") {
		t.Fatalf("error = %q, want the path-separator explanation", m.cloneErr)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.clonePromptOpen() {
		t.Fatal("esc must close the dialog")
	}
}

// TestCloneEndToEndOpensTheClone drives a real clone of a local repository
// through the dialog and checks that the finished clone re-roots the IDE.
func TestCloneEndToEndOpensTheClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := initTestRepo(t)
	base := newSized()
	dir := cloneProjectsDir(t)
	m := openClone(t, base)

	// The name field follows the URL: the clone lands under the repository's
	// own base name.
	m = typeInto(m, src)

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if !m.cloneRunning || cmd == nil {
		t.Fatal("enter must start the clone")
	}
	msg, ok := cmd().(vcs.CloneDoneMsg)
	if !ok {
		t.Fatal("the clone command must resolve to a CloneDoneMsg")
	}
	if msg.Err != nil {
		t.Fatalf("clone failed: %v", msg.Err)
	}
	dest := filepath.Join(dir, filepath.Base(src))
	if msg.Dest != dest {
		t.Fatalf("Dest = %q, want %q", msg.Dest, dest)
	}

	tm, cmd = m.Update(msg)
	m = tm.(Model)
	if m.clonePromptOpen() {
		t.Fatal("a successful clone must close the dialog")
	}
	if cmd == nil {
		t.Fatal("a successful clone must switch to the checkout")
	}
	if !hasSwitchTo(cmd, dest) {
		t.Fatalf("clone must switch to %q, got %#v", dest, cmd())
	}
}

// hasSwitchTo reports whether running cmd (expanding batches) yields a switch
// to root — the toast tick rides in the same batch.
func hasSwitchTo(cmd tea.Cmd, root string) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case project.SwitchProjectMsg:
		return msg.Root == root
	case tea.BatchMsg:
		for _, c := range msg {
			if hasSwitchTo(c, root) {
				return true
			}
		}
	}
	return false
}

// TestCloneFailureStaysInDialog keeps a failed clone editable.
func TestCloneFailureStaysInDialog(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)
	m = typeInto(m, "https://example.invalid/org/repo.git")

	tm, _ := m.Update(vcs.CloneDoneMsg{URL: "https://example.invalid/org/repo.git", Dest: "/tmp/x", Err: os.ErrNotExist})
	m = tm.(Model)
	if !m.clonePromptOpen() || m.cloneRunning {
		t.Fatal("a failed clone must leave the dialog open and editable")
	}
	if m.cloneErr == "" {
		t.Fatal("a failed clone must show the git error")
	}
}

// plainView renders the model and strips styling so assertions read the text.
func plainView(m Model) string {
	return ansiSeq.ReplaceAllString(m.View().Content, "")
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m")

// initTestRepo creates a repository with one commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-qm", "initial")
	return dir
}

// TestCloneCommandOpensDialog drives the real command path (palette / menu →
// registry → host dispatch), not just the msg the dialog listens for.
func TestCloneCommandOpensDialog(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := drainCmd(base, base.RunCommand("project.clone"))
	if !m.clonePromptOpen() {
		t.Fatal("running project.clone must open the clone dialog")
	}
}

// TestCloneDialogRenders guards that the open dialog is actually painted (the
// shell content, not just the model flag).
func TestCloneDialogRenders(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)
	view := plainView(m)
	if !strings.Contains(view, "Clone Repository") || !strings.Contains(view, "Repository URL") {
		t.Fatalf("clone dialog is not rendered:\n%s", view)
	}
}

// TestCloneDialogSurvivesLongURL guards the box width: the shell drops a box
// wider than the terminal, so a long URL must scroll inside its field instead
// of growing the line.
func TestCloneDialogSurvivesLongURL(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	m := openClone(t, base)
	m = typeInto(m, "https://example.com/"+strings.Repeat("very-long-path-segment/", 8)+"repo.git")

	if !strings.Contains(plainView(m), "Clone Repository") {
		t.Fatalf("a long URL must not hide the dialog:\n%s", plainView(m))
	}
	for _, line := range strings.Split(plainView(m), "\n") {
		if len([]rune(line)) > 100 {
			t.Fatalf("rendered line exceeds the terminal width: %q", line)
		}
	}
}
