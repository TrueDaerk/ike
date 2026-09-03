package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/nbview"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/watch"
)

// nbfiles_test.go covers the notebook viewer's app side (#2425): the .ipynb
// handler, the re-render on an external change, the chooser's targets, and
// the three requests the pane routes back out — clipboard, scratch, image.

// testNotebook is a minimal but complete nbformat 4 document: one markdown
// cell, one code cell with a stream output.
const testNotebook = `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Analysis\n"]},
  {"cell_type": "code", "execution_count": 1, "metadata": {},
   "source": ["print(\"hi\")\n"],
   "outputs": [{"output_type": "stream", "name": "stdout", "text": ["hi\n"]}]}
 ],
 "metadata": {"language_info": {"name": "python", "file_extension": ".py"}},
 "nbformat": 4, "nbformat_minor": 5
}`

// writeTestNotebook writes a notebook fixture into a temp dir.
func writeTestNotebook(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// notebookPane returns the open notebook viewer instance, or nil.
func notebookPane(m Model) *pane.Instance {
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool { return c.Kind() == pane.KindNotebook })
	if !ok {
		return nil
	}
	return inst
}

// TestIpynbOpensInNotebookPane: an .ipynb opens as cells, never as a raw JSON
// buffer.
func TestIpynbOpensInNotebookPane(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("an .ipynb must open in a notebook pane")
	}
	if inst.Notebook().Path() != canonicalPath(path) {
		t.Fatalf("pane bound to %q, want %q", inst.Notebook().Path(), canonicalPath(path))
	}
	if len(inst.Notebook().Cells()) != 2 {
		t.Fatalf("cells = %d, want 2", len(inst.Notebook().Cells()))
	}
	if m.editorWithFile(canonicalPath(path)) != "" {
		t.Fatal("the notebook must not additionally land in a JSON buffer")
	}
}

// TestOpenNotebookAsTextGivesTheJSON: the chooser's Text editor target still
// opens the document itself, which is the escape hatch the viewer needs.
func TestOpenNotebookAsTextGivesTheJSON(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m.openAsPath = canonicalPath(path)
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsEditor})
	m = settle(t, out.(Model), cmd)
	if notebookPane(m) != nil {
		t.Fatal("the Text editor target must bypass the notebook handler")
	}
	if m.editorWithFile(canonicalPath(path)) == "" {
		t.Fatal("the notebook must open in a text buffer")
	}
}

// TestOpenAsNotebookRejectsNonNotebooks: an invalid pick notifies and leaves
// the layout untouched, like every other content-contract target.
func TestOpenAsNotebookRejectsNonNotebooks(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.openAsPath = writeTestNotebook(t, "plain.json", `{"hello": 1}`)
	leaves := len(m.leafOrder())
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsNotebook})
	m = settle(t, out.(Model), cmd)
	if notebookPane(m) != nil {
		t.Fatal("a non-notebook must not open a notebook pane")
	}
	if got := len(m.leafOrder()); got != leaves {
		t.Fatalf("leaf count changed %d → %d — the layout must stay untouched", leaves, got)
	}
	notes := m.host.DrainNotifications()
	if len(notes) == 0 || !strings.Contains(notes[0].Text, "not a Jupyter notebook") {
		t.Fatalf("an invalid pick must notify, got %v", notes)
	}
}

// TestOpenAsNotebookForcesAJSONFile: a notebook saved under another extension
// still opens as cells through the chooser.
func TestOpenAsNotebookForcesAJSONFile(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.openAsPath = writeTestNotebook(t, "renamed.json", testNotebook)
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsNotebook})
	m = settle(t, out.(Model), cmd)
	if notebookPane(m) == nil {
		t.Fatal("Open file as… → Notebook must open the notebook pane")
	}
}

// TestNotebookRerendersOnExternalChange: a kernel rewriting the .ipynb
// re-renders the pane — no buffer exists that the editor routing would catch.
func TestNotebookRerendersOnExternalChange(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	if notebookPane(m) == nil {
		t.Fatal("the notebook pane did not open")
	}
	updated := strings.Replace(testNotebook, `"# Analysis\n"`, `"# Analysis\n", "\n", "second run\n"`, 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	out, cmd := m.Update(watch.EventMsg{Path: canonicalPath(path), Kind: watch.FileChanged})
	m = settle(t, out.(Model), cmd)
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("the notebook pane vanished on the change")
	}
	if !strings.Contains(strings.Join(inst.Notebook().Rows(), "\n"), "second run") {
		t.Fatalf("the pane did not re-render:\n%s", strings.Join(inst.Notebook().Rows(), "\n"))
	}
}

// TestNotebookCopyAndScratch: the pane's clipboard and scratch requests are
// carried out by the root model.
func TestNotebookCopyAndScratch(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	out, _ := m.Update(nbview.CopyMsg{Text: "print(1)", What: "cell 2 source"})
	m = out.(Model)
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "copied cell 2 source") {
		t.Fatalf("copy must notify, got %+v", m.toasts)
	}

	out, cmd := m.Update(nbview.ScratchMsg{Ext: "py", Content: "print(1)\n"})
	m = settle(t, out.(Model), cmd)
	var scratch *editor.Model
	m.contentInstances(func(_ string, _ int, inst *pane.Instance) bool {
		if inst.Kind() != pane.KindEditor {
			return true
		}
		for i := 0; i < inst.TabCount(); i++ {
			if ed := inst.TabEditor(i); ed != nil && filepath.Ext(ed.Path()) == ".py" {
				scratch = ed
				return false
			}
		}
		return true
	})
	if scratch == nil {
		t.Fatal("the cell source did not open as a .py scratch")
	}
	if !strings.Contains(scratch.Text(), "print(1)") {
		t.Fatalf("the scratch does not hold the cell source: %q", scratch.Text())
	}
}

// TestNotebookSaveImage: the save request writes the bytes and never
// overwrites an existing file.
func TestNotebookSaveImage(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	dir := t.TempDir()
	target := filepath.Join(dir, "nb-cell1.png")
	m.saveNotebookImage(nbview.SaveImageMsg{Path: target, Data: []byte("\x89PNG-one")})
	if got, err := os.ReadFile(target); err != nil || string(got) != "\x89PNG-one" {
		t.Fatalf("first save: %q, %v", got, err)
	}
	if notes := m.host.DrainNotifications(); len(notes) == 0 || !strings.Contains(notes[0].Text, "saved") {
		t.Fatalf("save must notify, got %v", notes)
	}
	m.saveNotebookImage(nbview.SaveImageMsg{Path: target, Data: []byte("\x89PNG-two")})
	if got, err := os.ReadFile(target); err != nil || string(got) != "\x89PNG-one" {
		t.Fatalf("the first file was overwritten: %q, %v", got, err)
	}
	second := filepath.Join(dir, "nb-cell1-2.png")
	if got, err := os.ReadFile(second); err != nil || string(got) != "\x89PNG-two" {
		t.Fatalf("second save did not land on %q: %q, %v", second, got, err)
	}
}

// TestNotebookPanePersistence: the pane's identity round-trips through the
// layout snapshot, so a restored session re-reads the notebook.
func TestNotebookPanePersistence(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("the notebook pane did not open")
	}
	id, ok := contentIdentity(inst)
	if !ok || id.Kind != "notebook" || id.Path != canonicalPath(path) {
		t.Fatalf("identity = %+v, ok = %v", id, ok)
	}
	kind, ok := contentKindFromString("notebook")
	if !ok || kind != pane.KindNotebook {
		t.Fatalf("contentKindFromString(notebook) = %v, %v", kind, ok)
	}
	// A key minted for a restore keeps the counter ahead of it.
	reg := m.activeWS().Panes
	restored := reg.AddNotebookKey("notebook:4", canonicalPath(path))
	if restored == nil || restored.Kind() != pane.KindNotebook {
		t.Fatal("AddNotebookKey did not recreate the pane")
	}
	if next := reg.AddNotebookView(canonicalPath(path)); next == "notebook:4" {
		t.Fatal("the mint counter did not advance past the restored key")
	}
}

// TestNotebookPaneRoutesKeys: a focused notebook pane consumes its own
// navigation keys and the shared find chord reaches its search line.
func TestNotebookPaneRoutesKeys(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("the notebook pane did not open")
	}
	if got := inst.ContextID(); got != "notebook" {
		t.Fatalf("context id = %q, want notebook", got)
	}
	s := inst.Searchable()
	if s == nil {
		t.Fatal("a notebook pane must be Searchable (#2409)")
	}
	if !s.OpenSearch() || !inst.Notebook().Searching() {
		t.Fatal("cmd+f must open the notebook's search line")
	}
	inst.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
}
