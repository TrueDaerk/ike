package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/preview"
)

// openMarkdownFile loads a temp markdown file into the active editor and
// returns the model plus the file path.
func openMarkdownFile(t *testing.T, content string) (Model, string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	return tm.(Model), path
}

// previewKeyFor returns the key of the first preview pane bound to path, or "".
func previewKeyFor(m Model, path string) string {
	for _, key := range m.panes.Keys() {
		if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindMarkdown && inst.Preview().Path() == path {
			return key
		}
	}
	return ""
}

// TestMarkdownPreviewOpensSplit guards markdown.preview: the command splits
// the active editor with a preview pane bound to its buffer, keeps focus on
// the editor, and renders the document.
func TestMarkdownPreviewOpensSplit(t *testing.T) {
	m, path := openMarkdownFile(t, "# Hello Preview\n\nbody text\n")
	editorKey := m.panes.Focused()
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	key := previewKeyFor(m, path)
	if key == "" {
		t.Fatal("markdown.preview should open a preview pane bound to the buffer")
	}
	if m.panes.Focused() != editorKey {
		t.Fatalf("focus should stay on the editor, got %q", m.panes.Focused())
	}
	if v := m.render(); !strings.Contains(v, "PREVIEW") || !strings.Contains(v, "Hello Preview") {
		t.Fatal("the rendered workspace should show the titled preview with the document")
	}
}

// TestMarkdownPreviewNeedsMarkdownBuffer guards the guard: a non-markdown
// buffer opens no preview.
func TestMarkdownPreviewNeedsMarkdownBuffer(t *testing.T) {
	m := newSized()
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	for _, key := range m.panes.Keys() {
		if m.panes.Get(key).Kind() == pane.KindMarkdown {
			t.Fatal("a .txt buffer must not open a markdown preview")
		}
	}
}

// TestMarkdownPreviewRefocusesExisting guards the no-duplicate rule: invoking
// the command again focuses the existing preview instead of splitting twice.
func TestMarkdownPreviewRefocusesExisting(t *testing.T) {
	m, path := openMarkdownFile(t, "# Once\n")
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	first := previewKeyFor(m, path)
	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	count := 0
	for _, key := range m.panes.Keys() {
		if m.panes.Get(key).Kind() == pane.KindMarkdown {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("second invocation must not duplicate the pane, got %d previews", count)
	}
	if m.panes.Focused() != first {
		t.Fatalf("second invocation should focus the existing preview, got %q", m.panes.Focused())
	}
}

// TestMarkdownPreviewLiveUpdate guards the change seam: an edit's SyncMsg arms
// the debounce, and the resulting tick re-renders the preview with the new
// buffer text.
func TestMarkdownPreviewLiveUpdate(t *testing.T) {
	m, path := openMarkdownFile(t, "# Draft\n")
	editorKey := m.panes.Focused()
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	// Type new text into the source buffer, then deliver the change broadcast
	// the emitter would send.
	for _, k := range []tea.KeyPressMsg{
		{Code: 'o', Text: "o"},
		{Code: 'U', Text: "U"},
		{Code: 'n', Text: "n"},
		{Code: 'i', Text: "i"},
		{Code: 'q', Text: "q"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, cmd := m.Update(editor.SyncMsg{Path: path, FromKey: editorKey})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("a change to a previewed buffer must arm the debounce tick")
	}
	// Run the batch: the tea.Tick sleeps out the debounce and yields the
	// RenderTickMsg, which drains back into Update like the program loop would.
	m = drainCmd(m, cmd)
	key := previewKeyFor(m, path)
	if v := m.panes.Get(key).View(); !strings.Contains(v, "Uniq") {
		t.Fatalf("preview should re-render the edited text, got:\n%s", v)
	}
}

// TestMarkdownPreviewCursorSync guards scroll sync: a CursorMsg for the bound
// path scrolls the preview toward the cursor's mapped position.
func TestMarkdownPreviewCursorSync(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Top\n\n")
	for i := 0; i < 60; i++ {
		b.WriteString("filler\n")
	}
	b.WriteString("\n# Bottom\n\nend\n")
	m, path := openMarkdownFile(t, b.String())
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	key := previewKeyFor(m, path)
	before := m.panes.Get(key).View()
	tm, _ = m.Update(preview.CursorMsg{Path: path, Line: strings.Count(b.String(), "\n") - 1})
	m = tm.(Model)
	after := m.panes.Get(key).View()
	if before == after {
		t.Fatal("a cursor move to the bottom should scroll the preview")
	}
	if !strings.Contains(after, "Bottom") {
		t.Fatalf("preview should show the section under the cursor, got:\n%s", after)
	}
}

// TestMarkdownPreviewClosesLikeAPane guards close: the focused preview closes
// through the ordinary pane-close path and the layout collapses.
func TestMarkdownPreviewClosesLikeAPane(t *testing.T) {
	m, path := openMarkdownFile(t, "# Bye\n")
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	key := previewKeyFor(m, path)
	m.setFocus(key)
	m.closeFocused()
	if m.panes.Has(key) {
		t.Fatal("closing the focused preview must remove its pane")
	}
}

// TestMarkdownPreviewPersistsAndRestores guards the layout store round trip:
// a saved preview leaf restores as a preview of the same file, re-read from
// disk.
func TestMarkdownPreviewPersistsAndRestores(t *testing.T) {
	m, path := openMarkdownFile(t, "# Persisted\n")
	tm, _ := m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	saveLayout(m.tree, m.panes)
	// A fresh model in the same config dir restores the saved layout.
	m2 := New()
	tm, _ = m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = tm.(Model)
	key := previewKeyFor(m2, path)
	if key == "" {
		t.Fatal("layout restore should rebuild the preview pane")
	}
	if v := m2.panes.Get(key).View(); !strings.Contains(v, "Persisted") {
		t.Fatalf("restored preview should render the file from disk, got:\n%s", v)
	}
}
