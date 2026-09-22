package settings

// nb_imagecap_test.go covers the Settings-UI half of the notebook image cap
// (#2683): notebook.image_max_cols is a real, bounded entry on the Notebook
// Viewer page that refuses nonsense, persists and renders.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// imageCapEntry returns the schema entry for notebook.image_max_cols.
func imageCapEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "notebook.image_max_cols" {
				if p.Title != "Notebook Viewer" {
					t.Fatalf("entry lives on page %q, want Notebook Viewer", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("notebook.image_max_cols must be configurable in the settings UI")
	return Entry{}
}

func TestNotebookImageCapEntryBounded(t *testing.T) {
	e := imageCapEntry(t)
	if e.Type != Int {
		t.Fatalf("type = %v, want Int", e.Type)
	}
	if e.Min != 0 || e.Max != config.NotebookImageMaxColsMax {
		t.Fatalf("range = %d–%d, want 0–%d", e.Min, e.Max, config.NotebookImageMaxColsMax)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
	if e.ValidateInt == nil || e.ValidateInt(-1) == "" {
		t.Fatal("a negative column count must be refused with a message")
	}
	if msg := e.ValidateInt(0); msg != "" {
		t.Fatalf("0 lifts the cap and must be accepted, got %q", msg)
	}
}

func TestNotebookImageCapValidatesAndPersists(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Notebook Viewer", Entries: []Entry{imageCapEntry(t)}}}, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected inline, without a write.
	ed.tf.Set("wide")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid width must not write (err=%q)", ed.err)
	}
	// A negative width is refused too — it must not read as the cap-lifting 0.
	ed.tf.Set("-20")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("negative width must not write (err=%q)", ed.err)
	}
	// A sane value persists and shows up in the list rendering.
	ed.tf.Set("120")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Notebook.ImageMaxCols; got != 120 {
		t.Fatalf("image_max_cols = %d, want 120", got)
	}
	if view := m.View(); !strings.Contains(view, "120") {
		t.Fatalf("the entry list must show the value:\n%s", view)
	}
	// 0 is a legal value: it turns the cap off.
	m.Update(key("enter"))
	m.editor.(*intEditor).tf.Set("0")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Notebook.ImageMaxCols; got != 0 {
		t.Fatalf("image_max_cols = %d, want the cap lifted (0)", got)
	}
}
