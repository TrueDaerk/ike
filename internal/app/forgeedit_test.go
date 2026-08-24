package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ghissues"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// forgeedit_test.go covers the buffer save-chain wiring of #2087: opening a
// forge text as a markdown buffer, the push a save dispatches, and the three
// outcomes — pushed (buffer closed, scratch removed, issues refreshed), stale
// base (warned, nothing written, reload/overwrite offered) and failed (buffer
// and text kept, retry works).

// commentTarget is the comment the tests edit.
var commentTarget = forge.TextTarget{Kind: forge.TextComment, Issue: 12, ID: "77"}

// openEdit opens one forge text as an edit buffer and returns the model plus
// the scratch path it was bound to.
func openEdit(t *testing.T, m Model, target forge.TextTarget, base string) (Model, string) {
	t.Helper()
	out, _ := m.Update(ghissues.EditTextRequestMsg{Target: target, Base: base, Title: "a title"})
	m = out.(Model)
	paths := make([]string, 0, len(m.forgeEdits))
	for p := range m.forgeEdits {
		paths = append(paths, p)
	}
	if len(paths) != 1 {
		t.Fatalf("expected exactly one bound buffer, got %v", paths)
	}
	return m, paths[0]
}

func TestForgeEditOpensPrefilledMarkdownBuffer(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "the current comment")

	if !strings.HasSuffix(path, ".md") {
		t.Fatalf("a forge text must open as markdown, got %q", path)
	}
	if !strings.Contains(path, "issue-12-comment-77") {
		t.Fatalf("the buffer should name what it edits, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "the current comment\n" {
		t.Fatalf("prefill = %q", data)
	}
	if m.editorForPath(path) == nil {
		t.Fatal("the edit buffer must be open in an editor")
	}
	if got, ok := m.ForgeEditTarget(path); !ok || got != commentTarget {
		t.Fatalf("binding = %+v/%v", got, ok)
	}
}

func TestForgeEditReusesAnAlreadyOpenBuffer(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "text")
	out, _ := m.Update(ghissues.EditTextRequestMsg{Target: commentTarget, Base: "text"})
	m = out.(Model)
	if len(m.forgeEdits) != 1 {
		t.Fatalf("a second request for the same text must not open a second buffer: %v", m.forgeEdits)
	}
	if _, ok := m.forgeEdits[path]; !ok {
		t.Fatal("the original binding must survive")
	}
}

func TestSavingABoundBufferPushesItsFileContent(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "before")
	if err := os.WriteFile(path, []byte("after the edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The save signal the editor emitter sends for every written buffer.
	_, cmd := m.Update(forgeEditSavedMsg{path: path})
	if cmd == nil {
		t.Fatal("saving a bound buffer must dispatch a push")
	}
	// An unbound path is the ordinary case and must stay inert.
	if _, cmd := m.Update(forgeEditSavedMsg{path: path + ".other"}); cmd != nil {
		t.Fatal("an unbound save must not push anything")
	}
}

func TestEmptyNewCommentIsNotPosted(t *testing.T) {
	target := forge.TextTarget{Kind: forge.TextNewComment, Issue: 12}
	m, path := openEdit(t, newSized(), target, "")
	if _, cmd := m.Update(forgeEditSavedMsg{path: path}); cmd != nil {
		t.Fatal("an empty new comment must not be posted")
	}
	if _, ok := m.ForgeEditTarget(path); !ok {
		t.Fatal("the buffer must stay bound for the text that may still follow")
	}
}

func TestSuccessfulPushClosesTheBufferAndRefreshes(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "before")

	out, _ := m.Update(forge.SaveTextMsg{Target: commentTarget, Path: path, Body: "after"})
	m = out.(Model)

	if _, ok := m.ForgeEditTarget(path); ok {
		t.Fatal("a pushed text must not stay bound")
	}
	if m.editorForPath(path) != nil {
		t.Fatal("a pushed text's buffer must close")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the scratch file must be removed after a push, stat err = %v", err)
	}
	if list, _ := scratch.List(); len(list) != 0 {
		t.Fatalf("the scratch store must be empty again, got %v", list)
	}
}

func TestFailedPushKeepsTheBufferAndRetries(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "before")
	if err := os.WriteFile(path, []byte("my precious text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(forge.SaveTextMsg{
		Target: commentTarget, Path: path, Body: "my precious text\n",
		Err: errors.New("gh: HTTP 403"),
	})
	m = out.(Model)

	if _, ok := m.ForgeEditTarget(path); !ok {
		t.Fatal("a failed push must keep the binding")
	}
	if data, _ := os.ReadFile(path); string(data) != "my precious text\n" {
		t.Fatalf("no text may be lost to a failed push, got %q", data)
	}
	if m.editorForPath(path) == nil {
		t.Fatal("the buffer must stay open")
	}
	if !m.forgeEditDialogOpen() {
		t.Fatal("the error must be shown prominently, not swallowed")
	}
	body := m.shell.Content().Render(80)
	if !strings.Contains(body, "HTTP 403") {
		t.Fatalf("the dialog must name the error, got %q", body)
	}

	// 'r' retries the push and closes the dialog.
	out, cmd := m.updateForgeEditDialog(key("r"))
	m = out.(Model)
	if cmd == nil {
		t.Fatal("'r' must retry the push")
	}
	if m.forgeEditDialogOpen() {
		t.Fatal("the dialog must close on a retry")
	}
}

func TestStaleBaseWarnsAndOffersOverwriteOrReload(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "what I opened")
	if err := os.WriteFile(path, []byte("my version\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(forge.SaveTextMsg{
		Target: commentTarget, Path: path, Body: "my version\n",
		Stale: true, Current: "someone else's version",
	})
	m = out.(Model)

	if !m.forgeEditDialogOpen() {
		t.Fatal("a stale base must warn before overwriting")
	}
	body := m.shell.Content().Render(80)
	for _, want := range []string{"changed on the forge", "overwrite", "load"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the warning must offer %q, got %q", want, body)
		}
	}
	if data, _ := os.ReadFile(path); string(data) != "my version\n" {
		t.Fatalf("nothing may be written before the user decides, buffer file = %q", data)
	}

	// 'o' overwrites: the push runs again, this time forced.
	over, cmd := m.updateForgeEditDialog(key("o"))
	if cmd == nil {
		t.Fatal("'o' must push the user's version anyway")
	}
	if over.(Model).forgeEditDialogOpen() {
		t.Fatal("the dialog must close on overwrite")
	}
}

func TestStaleBaseReloadReplacesTheBufferAndRebases(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "what I opened")
	if err := os.WriteFile(path, []byte("my version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(forge.SaveTextMsg{
		Target: commentTarget, Path: path, Body: "my version\n",
		Stale: true, Current: "their version",
	})
	m = out.(Model)

	out, _ = m.updateForgeEditDialog(key("l"))
	m = out.(Model)

	if m.forgeEditDialogOpen() {
		t.Fatal("the dialog must close after the reload")
	}
	if data, _ := os.ReadFile(path); string(data) != "their version\n" {
		t.Fatalf("the reload must write the forge's version, got %q", data)
	}
	if e := m.forgeEdits[path]; e == nil || e.base != "their version" {
		t.Fatalf("the reload must re-base the binding, got %+v", e)
	}
	if ed := m.editorForPath(path); ed == nil || !strings.Contains(ed.Text(), "their version") {
		t.Fatal("the open buffer must show the reloaded text")
	}
	// The re-based buffer saves cleanly: no stale verdict is carried over.
	if _, cmd := m.Update(forgeEditSavedMsg{path: path}); cmd == nil {
		t.Fatal("a re-based buffer must still push on save")
	}
}

func TestStaleDialogCancelKeepsEverything(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "what I opened")
	out, _ := m.Update(forge.SaveTextMsg{
		Target: commentTarget, Path: path, Body: "mine", Stale: true, Current: "theirs",
	})
	m = out.(Model)
	out, _ = m.updateForgeEditDialog(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.forgeEditDialogOpen() {
		t.Fatal("esc must dismiss the warning")
	}
	if _, ok := m.ForgeEditTarget(path); !ok {
		t.Fatal("esc must keep the buffer bound so a later save retries")
	}
	if m.editorForPath(path) == nil {
		t.Fatal("esc must keep the buffer open")
	}
}

func TestPushOutcomeNeverStealsAnOpenOverlay(t *testing.T) {
	m, path := openEdit(t, newSized(), commentTarget, "before")
	// Something else owns the floating shell — a prompt, the help view.
	m.shell.SetContent(ui.ModelContent{Heading: "busy", Body: func() string { return "" }})
	m.shell.Open()

	out, _ := m.Update(forge.SaveTextMsg{
		Target: commentTarget, Path: path, Body: "mine", Err: errors.New("gh: HTTP 500"),
	})
	m = out.(Model)

	if m.forgeEditDialogOpen() {
		t.Fatal("a push outcome must not steal an open overlay")
	}
	if m.shell.Content().Title() != "busy" {
		t.Fatalf("the open overlay must survive, title = %q", m.shell.Content().Title())
	}
	if _, ok := m.ForgeEditTarget(path); !ok {
		t.Fatal("the buffer must stay bound so the next save retries")
	}
}
