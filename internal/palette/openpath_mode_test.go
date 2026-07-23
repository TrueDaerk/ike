package palette

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPathModeListsFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := NewOpenPathMode()
	items := o.Results(dir+string(filepath.Separator), Context{})
	var file, sub bool
	for _, it := range items {
		switch {
		case strings.HasSuffix(it.Title, "note.txt"):
			file = true
			msg, ok := it.Msg.(OpenFileMsg)
			if !ok || msg.Path != filepath.Join(dir, "note.txt") {
				t.Fatalf("file item msg=%v", it.Msg)
			}
		case strings.HasSuffix(it.Title, "sub"+string(filepath.Separator)):
			sub = true
			if _, ok := it.Msg.(OpenPathDescendMsg); !ok {
				t.Fatalf("dir item must descend, msg=%v", it.Msg)
			}
		}
	}
	if !file || !sub {
		t.Fatalf("want file+dir candidates, got %v", items)
	}
}

func TestOpenPathModeEmptyQuerySeedsRoots(t *testing.T) {
	o := NewOpenPathMode()
	items := o.Results("", Context{})
	if len(items) != 2 || items[0].Title != "~/" || items[1].Title != "/" {
		t.Fatalf("empty query must seed ~/ and /, got %v", items)
	}
}

func TestOpenPathModeRawFallback(t *testing.T) {
	o := NewOpenPathMode()
	q := filepath.Join(t.TempDir(), "missing.txt")
	items := o.Results(q, Context{})
	if len(items) != 1 {
		t.Fatalf("want single raw fallback, got %v", items)
	}
	msg, ok := items[0].Msg.(OpenFileMsg)
	if !ok || msg.Path != q {
		t.Fatalf("raw fallback msg=%v", items[0].Msg)
	}
}

func TestOpenPathModeTabCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "unique-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := NewOpenPathMode()
	got := o.Complete(filepath.Join(dir, "uni"))
	want := filepath.Join(dir, "unique-dir") + string(filepath.Separator)
	if got != want {
		t.Fatalf("Complete=%q want %q", got, want)
	}
}
