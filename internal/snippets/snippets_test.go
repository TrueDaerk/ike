package snippets

import (
	"context"
	"testing"

	"ike/internal/complete"
	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/lsp/protocol"
)

var _ complete.Source = Source{} // popup-merge seam: registers with the local engine

// withConfig installs entries as the process config for the test.
func withConfig(t *testing.T, entries []config.SnippetEntry) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Snippets = entries
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// register a throwaway language for scoping tests.
func regLang(t *testing.T, id, ext string) {
	t.Helper()
	lang.Register(lang.Language{ID: id, Extensions: []string{ext}})
}

func TestBuiltinLookupByLanguage(t *testing.T) {
	withConfig(t, nil)
	regLang(t, "go", "go")
	body, ok := Lookup("/x/main.go", "iferr")
	if !ok || body != "if err != nil {\n\t$1\n}" {
		t.Fatalf("builtin iferr: %q %v", body, ok)
	}
	if _, ok := Lookup("/x/notes.txt", "iferr"); ok {
		t.Fatal("go-scoped builtin must not fire in an unknown-language buffer")
	}
}

func TestUserOverridesBuiltinSameTriggerLanguage(t *testing.T) {
	regLang(t, "go", "go")
	withConfig(t, []config.SnippetEntry{
		{Trigger: "iferr", Language: "go", Body: "if err != nil { return err }"},
	})
	body, ok := Lookup("/x/main.go", "iferr")
	if !ok || body != "if err != nil { return err }" {
		t.Fatalf("user entry must shadow the builtin, got %q %v", body, ok)
	}
}

func TestLanguageScopedBeatsGlobal(t *testing.T) {
	regLang(t, "sniptestlang", "sniptl")
	withConfig(t, []config.SnippetEntry{
		{Trigger: "hdr", Body: "GLOBAL $1"},
		{Trigger: "hdr", Language: "sniptestlang", Body: "SCOPED $1"},
	})
	if body, _ := Lookup("/x/a.sniptl", "hdr"); body != "SCOPED $1" {
		t.Fatalf("language-scoped entry must win, got %q", body)
	}
	if body, _ := Lookup("/x/other.unknownext", "hdr"); body != "GLOBAL $1" {
		t.Fatalf("other buffers get the global entry, got %q", body)
	}
}

func TestGlobalEntryAppliesEverywhere(t *testing.T) {
	withConfig(t, []config.SnippetEntry{{Trigger: "sig", Body: "-- $1"}})
	if _, ok := Lookup("/x/readme.nolang", "sig"); !ok {
		t.Fatal("global entry must resolve without a language")
	}
}

func TestConfigReloadAppliesLive(t *testing.T) {
	withConfig(t, nil)
	if _, ok := Lookup("/x/a.txt", "brb"); ok {
		t.Fatal("unexpected match before the reload")
	}
	// A config reload publishes a fresh *Config via config.Set; the store
	// reads Get() per lookup, so the new entries apply immediately.
	c := *config.Get()
	c.Snippets = []config.SnippetEntry{{Trigger: "brb", Body: "be right back"}}
	config.Set(&c)
	if body, ok := Lookup("/x/a.txt", "brb"); !ok || body != "be right back" {
		t.Fatalf("reloaded entry must resolve, got %q %v", body, ok)
	}
}

func TestForDedupesByTrigger(t *testing.T) {
	regLang(t, "go", "go")
	withConfig(t, []config.SnippetEntry{
		{Trigger: "main", Language: "go", Body: "user main"},
	})
	seen := 0
	for _, e := range For("/x/main.go") {
		if e.Trigger == "main" {
			seen++
			if e.Body != "user main" {
				t.Fatalf("dedup kept the wrong entry: %q", e.Body)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("trigger listed %d times, want 1", seen)
	}
}

func TestSourceCompleteItems(t *testing.T) {
	regLang(t, "go", "go")
	withConfig(t, nil)
	items, err := NewSource().Complete(context.Background(), complete.Request{Path: "/x/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.Label != "iferr" {
			continue
		}
		found = true
		if !it.IsSnippet || it.Kind != protocol.KindSnippet {
			t.Fatalf("template items must be snippet items: %+v", it)
		}
		if it.Detail == "" || it.Detail[:8] != "template" {
			t.Fatalf("detail must mark the item as a template: %q", it.Detail)
		}
	}
	if !found {
		t.Fatal("builtin iferr missing from the source items")
	}
}
