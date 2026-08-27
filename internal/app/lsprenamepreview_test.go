package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// previewFiles is the payload of a synthetic two-file rename: "Greet" becomes
// "Greeting" in an open buffer and in a file on disk.
func previewFiles() []ilsp.RenamePreviewFile {
	return []ilsp.RenamePreviewFile{
		{
			Path:   "/proj/a.go",
			Open:   true,
			Edits:  2,
			Before: "func Greet() {}\nvar x = Greet\n",
			After:  "func Greeting() {}\nvar x = Greeting\n",
		},
		{
			Path:   "/proj/b.go",
			Edits:  1,
			Before: "call(Greet)\n",
			After:  "call(Greeting)\n",
		},
	}
}

// openRenamePreview raises the dialog for the synthetic rename and reports
// whether the confirm continuation ran.
func openRenamePreview(t *testing.T) (Model, *int) {
	t.Helper()
	m := sized(t, 100, 40)
	applied := 0
	out, _ := m.Update(ilsp.RenamePreviewMsg{
		OldName: "Greet",
		NewName: "Greeting",
		Files:   previewFiles(),
		Apply: func() tea.Cmd {
			applied++
			return nil
		},
	})
	m = out.(Model)
	if !m.lspRenamePreviewOpen() {
		t.Fatal("a multi-file rename must open the preview dialog")
	}
	return m, &applied
}

func TestLSPRenamePreviewListsFilesAndDiff(t *testing.T) {
	m, _ := openRenamePreview(t)
	body := m.lspRenamePreviewBody(80)
	for _, want := range []string{"Greet", "Greeting", "2 edits", "1 edit", "3 edits in 2 files"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "a.go") || !strings.Contains(body, "b.go") {
		t.Fatalf("preview must list every affected file:\n%s", body)
	}
	// The selected file's edits show as a diff: the old line removed, the
	// renamed line added.
	if !strings.Contains(body, "- func Greet() {}") || !strings.Contains(body, "+ func Greeting() {}") {
		t.Fatalf("preview must show the edits as a diff:\n%s", body)
	}
	// The open buffer is marked as such — it is edited in-buffer, not on disk.
	if !strings.Contains(body, "open buffer") {
		t.Fatalf("an open file must be marked:\n%s", body)
	}
	if !m.shell.IsOpen() || !strings.Contains(m.shell.View(), "a.go") {
		t.Fatal("the dialog must render in the floating shell")
	}
}

func TestLSPRenamePreviewSelectionFollowsCursor(t *testing.T) {
	m, _ := openRenamePreview(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = out.(Model)
	if m.lspRenamePreview.cursor != 1 {
		t.Fatalf("j must move the cursor, got %d", m.lspRenamePreview.cursor)
	}
	body := m.lspRenamePreviewBody(80)
	if !strings.Contains(body, "diff for /proj/b.go") {
		t.Fatalf("the diff must follow the selection:\n%s", body)
	}
	if !strings.Contains(body, "+ call(Greeting)") {
		t.Fatalf("second file's diff missing:\n%s", body)
	}
}

func TestLSPRenamePreviewConfirmApplies(t *testing.T) {
	m, applied := openRenamePreview(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if *applied != 1 {
		t.Fatalf("enter must run the apply continuation once, ran %d times", *applied)
	}
	if m.lspRenamePreviewOpen() {
		t.Fatal("enter must close the dialog")
	}
}

func TestLSPRenamePreviewCancelAppliesNothing(t *testing.T) {
	m, applied := openRenamePreview(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if *applied != 0 {
		t.Fatal("esc must not apply anything")
	}
	if m.lspRenamePreviewOpen() || m.lspRenamePreview != nil {
		t.Fatal("esc must close the dialog and drop its state")
	}
}

func TestLSPRenamePreviewSwallowsKeys(t *testing.T) {
	m, applied := openRenamePreview(t)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m = out.(Model)
	if cmd != nil || !m.lspRenamePreviewOpen() || *applied != 0 {
		t.Fatal("the dialog is modal: unrelated keys must have no effect")
	}
}

// TestLSPRenamePreviewIgnoresEmptyPayload guards the dialog against a message
// with nothing to show — it must not open on an empty file list.
func TestLSPRenamePreviewIgnoresEmptyPayload(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(ilsp.RenamePreviewMsg{NewName: "x", Apply: func() tea.Cmd { return nil }})
	m = out.(Model)
	if m.lspRenamePreviewOpen() {
		t.Fatal("an empty preview must not open the dialog")
	}
}
