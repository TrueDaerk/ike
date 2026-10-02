package settings

// warmup_notice_roots_test.go covers the Settings-UI half of the muted
// silent-server notice (#2886): lsp.warmup_notice_muted_roots is a validated
// list on the Language Support page that persists, renders and lets the user
// remove entries again.

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// mutedRootsEntry returns the schema entry for lsp.warmup_notice_muted_roots.
func mutedRootsEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages(nil, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "lsp.warmup_notice_muted_roots" {
				if p.Title != "Language Support" {
					t.Fatalf("entry lives on page %q, want Language Support", p.Title)
				}
				return e
			}
		}
	}
	t.Fatal("lsp.warmup_notice_muted_roots must be configurable in the settings UI")
	return Entry{}
}

func TestMutedRootValidate(t *testing.T) {
	for _, ok := range []string{"/home/me/project", " /srv/app/ "} {
		if msg := mutedRootValidate(nil, ok); msg != "" {
			t.Fatalf("%q rejected: %s", ok, msg)
		}
	}
	for _, bad := range []string{"", "   ", "project", "./rel/dir", "/a,b"} {
		if msg := mutedRootValidate(nil, bad); msg == "" {
			t.Fatalf("%q accepted, want a rejection message", bad)
		}
	}
}

func TestMutedRootsEntryShape(t *testing.T) {
	e := mutedRootsEntry(t)
	if e.Type != List || e.ValidateEntry == nil {
		t.Fatalf("entry = %+v, want a List with element validation", e)
	}
	if e.Scope != config.UserScope || e.Title == "" || e.Description == "" {
		t.Fatalf("entry needs a user-scoped title and description: %#v", e)
	}
}

// TestMutedRootsValidatesPersistsRemoves drives the list editor: a relative
// path is rejected in place with a message, an absolute root persists and is
// listed, and deleting it writes the empty list back.
func TestMutedRootsValidatesPersistsRemoves(t *testing.T) {
	restoreConfig(t)
	m := New([]Page{{Title: "Language Support", Entries: []Entry{mutedRootsEntry(t)}}}, testOpts(t))
	m.SetSize(120, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed, ok := m.editor.(*listEditor)
	if !ok {
		t.Fatalf("editor = %T, want *listEditor", m.editor)
	}
	if len(ed.items) != 0 {
		t.Fatalf("default list = %v, want empty", ed.items)
	}

	ed.idx = len(ed.items) // the "+ add value…" row
	m.Update(key("enter"))
	ed.tf.Set("relative/project")
	if cmd := m.Update(key("enter")); cmd != nil || !strings.Contains(ed.err, "absolute") {
		t.Fatalf("a relative root must be rejected in place (err=%q)", ed.err)
	}

	root := "/srv/never-has-a-server"
	ed.tf.Set(root)
	m.Update(key("enter"))
	apply(t, m.applyChanges())
	if got := config.Get().LSP.WarmupNoticeMutedRoots; len(got) != 1 || got[0] != root {
		t.Fatalf("muted roots = %v, want [%s]", got, root)
	}
	if !config.Get().LSP.WarmupNoticeMuted(root + "/") {
		t.Error("a persisted root must mute the notice for that project")
	}
	if view := m.View(); !strings.Contains(view, root) {
		t.Fatalf("the list must show the muted root:\n%s", view)
	}

	ed.idx = 0
	m.Update(key("d"))
	apply(t, m.applyChanges())
	if got := config.Get().LSP.WarmupNoticeMutedRoots; len(got) != 0 {
		t.Fatalf("muted roots = %v after removal, want empty", got)
	}
}
