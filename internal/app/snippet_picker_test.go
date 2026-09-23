package app

import (
	"testing"

	"ike/internal/config"
	"ike/internal/palette"
	"ike/internal/snippets"
)

// withAppSnippets installs entries as the process config for the test (#2694).
func withAppSnippets(t *testing.T, entries []config.SnippetEntry) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Snippets = entries
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// TestSnippetPickerOpensWithTheBufferTemplates (#2694): snippets.insert opens
// the palette locked to the picker, carrying the templates the focused
// buffer's language offers — the go-scoped entry stays out of a txt buffer.
func TestSnippetPickerOpensWithTheBufferTemplates(t *testing.T) {
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "\n"))
	withAppSnippets(t, []config.SnippetEntry{
		{Trigger: "every", Body: "ALL"},
		{Trigger: "gonly", Language: "go", Body: "GO"},
	})
	tm, _ := m.Update(SnippetPickerMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("snippets.insert must open the palette")
	}
	for _, e := range m.snippetPicker.entries {
		if e.Trigger == "gonly" {
			t.Fatal("a txt buffer must not be offered the go-scoped template")
		}
	}
	if len(m.snippetPicker.entries) != 1 || m.snippetPicker.entries[0].Trigger != "every" {
		t.Fatalf("entries = %v", m.snippetPicker.entries)
	}
}

// TestSnippetPickerNoTemplatesNotifies (#2694): a buffer whose language has no
// templates gets the notice, not an empty picker.
func TestSnippetPickerNoTemplatesNotifies(t *testing.T) {
	dir := t.TempDir()
	// .txt has no built-ins, so the list is genuinely empty.
	m := openApp(t, writeTemp(t, dir, "a.txt", "\n"))
	withAppSnippets(t, nil)
	tm, _ := m.Update(SnippetPickerMsg{})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("an empty template list must not open the picker")
	}
}

// TestSnippetPickerRowExpandsAtTheCaret (#2694): activating a row expands the
// template with its tab stops into the focused editor, like trigger+tab.
func TestSnippetPickerRowExpandsAtTheCaret(t *testing.T) {
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "\n"))
	withAppSnippets(t, []config.SnippetEntry{{Trigger: "pair", Body: "left($1) right(val)"}})
	tm, _ := m.Update(SnippetPickerMsg{})
	m = tm.(Model)
	items := m.snippetPicker.Results("", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("results = %d, want 1", len(items))
	}
	msg, ok := items[0].Msg.(SnippetPickedMsg)
	if !ok {
		t.Fatalf("row message = %T", items[0].Msg)
	}
	tm, _ = m.Update(msg)
	m = tm.(Model)
	ed := m.focusedEditor()
	if ed == nil {
		t.Fatal("no focused editor")
	}
	if got := ed.Text(); got != "left() right(val)" {
		t.Fatalf("expansion = %q", got)
	}
}

// TestSnippetPickerFiltersByHumps (#2694): the rows filter with the hump
// matcher (#2650) — "if" finds iferr, "ferr" does not (no segment start).
func TestSnippetPickerFiltersByHumps(t *testing.T) {
	mode := newSnippetPickerMode()
	mode.entries = []snippets.Entry{
		{Trigger: "iferr", Language: "go", Body: "if err != nil {\n\t$1\n}"},
		{Trigger: "main", Language: "go", Body: "func main() {}"},
	}
	items := mode.Results("if", palette.Context{})
	if len(items) != 1 || items[0].Title != "iferr" {
		t.Fatalf("hump query \"if\" = %v", items)
	}
	if items[0].Badge != "go" {
		t.Fatalf("a language-scoped row must be badged, got %q", items[0].Badge)
	}
	if items[0].Detail == "" {
		t.Fatal("a row must describe its body")
	}
	if got := mode.Results("ferr", palette.Context{}); len(got) != 0 {
		t.Fatalf("mid-word query must not match: %v", got)
	}
}
