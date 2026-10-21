package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/format"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// reformat_unsaved_test.go covers reformatting a file-less buffer whose
// language is known through "Treat Buffer as …" (#3029): external tools get a
// synthetic project-root path with the language's extension, nothing lands on
// disk, a tier that needs a real file is skipped, and a buffer without any
// language explains that one must be set first.

// unsavedReformatApp is a typed file-less buffer in an app whose cwd — the
// project root reformat hands external tools — is a fresh, empty temp dir,
// with a channel-backed sender observing the async results.
func unsavedReformatApp(t *testing.T, langID, content string) (Model, string, chan tea.Msg) {
	t.Helper()
	ensureLang(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := typedBuffer(t, langID, content)
	root := t.TempDir()
	t.Chdir(root)
	root, _ = os.Getwd() // symlink-resolved, as reformat sees it
	msgs := make(chan tea.Msg, 16)
	m.host.SetSender(func(msg tea.Msg) { msgs <- msg })
	format.ResetForTest()
	t.Cleanup(format.ResetForTest)
	return m, root, msgs
}

// fakeFormatter writes an executable shell script that appends its arguments
// to a log (one invocation per line) and runs body. It lives outside root so
// a listing of root shows only what the reformat itself created.
func fakeFormatter(t *testing.T, body string) (bin, argLog string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "fakefmt")
	argLog = filepath.Join(dir, "args.log")
	script := "#!/bin/sh\necho \"$@\" >> '" + argLog + "'\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argLog
}

// reformatUnsaved runs Reformat File and applies the buffer-keyed result.
func reformatUnsaved(t *testing.T, m Model, msgs chan tea.Msg) Model {
	t.Helper()
	out, cmd := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("an unsaved typed buffer with a formatter must yield a command; toasts: %v", toastTexts(m))
	}
	_ = cmd()
	msg := awaitMsg(t, msgs, func(msg tea.Msg) bool {
		switch v := msg.(type) {
		case reformatBufferEditsMsg:
			return true
		case ilsp.FormatEditsMsg:
			t.Fatal("an unsaved buffer's result must not travel path-keyed")
		case ilsp.ServerStatusMsg:
			if v.Kind == ilsp.ServerEventError {
				t.Fatalf("reformat failed: %s", v.Text)
			}
		}
		return false
	})
	out, _ = m.Update(msg)
	return out.(Model)
}

// assertEmptyDir fails when dir holds anything — the "nothing written" rule.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("reformatting an unsaved buffer must write nothing, found %s", e.Name())
	}
}

// TestReformatUnsavedJSONViaExternalStdin is the acceptance case for a stdin
// tool: the buffer is formatted, ${FILE} is the synthetic root/buffer.json
// (prettier --stdin-filepath picks its parser from it), nothing is created
// on disk, the buffer stays file-less, and one undo reverts the reformat.
func TestReformatUnsavedJSONViaExternalStdin(t *testing.T) {
	m, root, msgs := unsavedReformatApp(t, "json", `{"a":1}`)
	bin, argLog := fakeFormatter(t, "tr a-z A-Z")
	format.RegisterExternalDefault("json", format.External{Command: bin, Args: []string{"--stdin-filepath", "${FILE}"}})

	m = reformatUnsaved(t, m, msgs)

	ed := m.activeEditor()
	if got := strings.TrimSpace(ed.Text()); got != `{"A":1}` {
		t.Fatalf("the tool's output must land in the buffer, got %q", got)
	}
	args, _ := os.ReadFile(argLog)
	want := "--stdin-filepath " + filepath.Join(root, "buffer.json")
	if strings.TrimSpace(string(args)) != want {
		t.Fatalf("the tool must get the synthetic project-root path\nwant %q\ngot  %q", want, strings.TrimSpace(string(args)))
	}
	if ed.HasFile() {
		t.Fatalf("the buffer must stay file-less, got %q", ed.Path())
	}
	assertEmptyDir(t, root)

	// u straight into the view: the startup focus need not be the editor.
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if got := strings.TrimSpace(ed.Text()); got != `{"a":1}` {
		t.Fatalf("one undo must revert the whole reformat, got %q", got)
	}
}

// TestReformatUnsavedMarkdownViaTempFile: a temp-file-mode tool formats its
// own temp copy, which carries the language's extension; the synthetic path
// is never created.
func TestReformatUnsavedMarkdownViaTempFile(t *testing.T) {
	m, root, msgs := unsavedReformatApp(t, "markdown", "# title\n")
	bin, argLog := fakeFormatter(t, "tr a-z A-Z < \"$1\" > \"$1.out\" && mv \"$1.out\" \"$1\"")
	format.RegisterExternalDefault("markdown", format.External{Command: bin, Args: []string{"${FILE}"}, TempFile: true})

	m = reformatUnsaved(t, m, msgs)

	if got := strings.TrimSpace(m.activeEditor().Text()); got != "# TITLE" {
		t.Fatalf("the tool's output must land in the buffer, got %q", got)
	}
	args, _ := os.ReadFile(argLog)
	file := strings.TrimSpace(string(args))
	if filepath.Ext(file) != ".md" || strings.HasPrefix(file, root) {
		t.Fatalf("a temp-file tool must format a .md temp copy outside the project, got %q", file)
	}
	assertEmptyDir(t, root)
}

// TestReformatUnsavedSkipsTierNeedingFile: the LSP-like tier needs a real
// file, so an unsaved buffer falls through to the next provider — and with
// no other provider, the usual "no formatter" notice appears.
func TestReformatUnsavedSkipsTierNeedingFile(t *testing.T) {
	m, _, msgs := unsavedReformatApp(t, "json", `{"a":1}`)
	needsFile := format.Provider{
		Name: "server", Tier: format.TierLSP, NeedsFile: true,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			t.Error("a tier needing a real file must not run for an unsaved buffer")
			return format.Result{}, nil
		},
	}
	format.Register(needsFile)

	out, _ := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if !hasToast(m, "no formatter for json") {
		t.Fatalf(`only a file-needing tier: want "no formatter for json", got %v`, toastTexts(m))
	}

	format.Register(format.Provider{
		Name: "built-in", Tier: format.TierBuiltin,
		Format: func(ctx context.Context, req format.Request) (format.Result, error) {
			return format.TextResult("{\n  \"a\": 1\n}"), nil
		},
	})
	m = reformatUnsaved(t, m, msgs)
	if got := m.activeEditor().Text(); !strings.Contains(got, "\"a\": 1") {
		t.Fatalf("the chain must fall through to the built-in provider, got %q", got)
	}
}

// TestReformatUntypedBufferAsksForLanguage: a file-less buffer without a
// language says a language must be set, instead of the old file-backed wording.
func TestReformatUntypedBufferAsksForLanguage(t *testing.T) {
	m := filelessModel(t, "plain", 0, 0)
	out, _ := m.Update(format.FileRequestMsg{})
	m = out.(Model)
	if !hasToast(m, "reformat: no language set — choose a language for this buffer first") {
		t.Fatalf("expected the no-language notice, got %v", toastTexts(m))
	}
}

// TestPrettierOfferForUnsavedBuffer: the #3023 install offer appears for an
// unsaved buffer under the same conditions, and accepting it reformats that
// buffer with the freshly installed prettier.
func TestPrettierOfferForUnsavedBuffer(t *testing.T) {
	f := newPrettierFixture(t)
	m := filelessModel(t, "a: 1", 0, 0)
	out, _ := m.Update(SetBufferLangMsg{ID: "prettylang"})
	m = out.(Model)
	if got := m.activeEditor().LangID(); got != "prettylang" {
		t.Fatalf("the buffer must be treated as prettylang, got %q", got)
	}
	m.host.SetSender(func(msg tea.Msg) { f.msgs <- msg })
	t.Chdir(t.TempDir())
	f.m = m

	_ = f.update(format.FileRequestMsg{})
	if !f.promptOpen() {
		t.Fatal("the prettier offer must open for an unsaved buffer too")
	}
	f.run(f.update(tea.KeyPressMsg{Code: '1', Text: "1"}))
	if f.installs != 1 {
		t.Fatalf("accepting must run npm install once, ran %d", f.installs)
	}
	edits := awaitMsg(t, f.msgs, func(msg tea.Msg) bool { _, ok := msg.(reformatBufferEditsMsg); return ok })
	f.update(edits)
	if got := strings.TrimSpace(f.m.activeEditor().Text()); got != "A: 1" {
		t.Fatalf("after the install the unsaved buffer must be reformatted with prettier, got %q", got)
	}
}

// TestReformatPathSynthetic pins the path rules: a real path wins, a typed
// buffer gets root + the language path's base name, an untyped one none.
func TestReformatPathSynthetic(t *testing.T) {
	if p, fb, ok := reformatPath("/x/a.go", "/x/a.go", "/root"); p != "/x/a.go" || !fb || !ok {
		t.Errorf("saved file: got %q %v %v", p, fb, ok)
	}
	if p, fb, ok := reformatPath("", "buffer.ts", "/root"); p != filepath.Join("/root", "buffer.ts") || fb || !ok {
		t.Errorf("typed buffer: got %q %v %v", p, fb, ok)
	}
	if p, fb, ok := reformatPath("", "Dockerfile", "/root"); p != filepath.Join("/root", "Dockerfile") || fb || !ok {
		t.Errorf("base-name language: got %q %v %v", p, fb, ok)
	}
	if _, _, ok := reformatPath("", "", "/root"); ok {
		t.Error("an untyped buffer has no reformat path")
	}
}
