package lang

import (
	"testing"

	"ike/internal/config"
)

// setAssociations installs a [files.associations] map for the test and
// restores the previous config afterwards.
func setAssociations(t *testing.T, m map[string]string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Files.Associations = m
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

func init() {
	// Two throwaway languages with unique extensions so the tests never
	// collide with plugin registrations.
	Register(Language{ID: "assoc-a", Extensions: []string{"assoca"}})
	Register(Language{ID: "assoc-b", Extensions: []string{"assocb"}})
}

// TestAssociationExtensionPatternBeatsBuiltin: a *.ext user association wins
// over the extension's own registered language (#1365).
func TestAssociationExtensionPatternBeatsBuiltin(t *testing.T) {
	setAssociations(t, map[string]string{"*.assocb": "assoc-a"})
	l, ok := ByPath("/proj/file.assocb")
	if !ok || l.ID != "assoc-a" {
		t.Fatalf("ByPath = %q, %v; want assoc-a (user association over built-in extension)", l.ID, ok)
	}
	// Unmapped files keep the built-in resolution.
	if l, ok := ByPath("/proj/other.assoca"); !ok || l.ID != "assoc-a" {
		t.Fatalf("unmapped path = %q, %v", l.ID, ok)
	}
}

// TestAssociationExactFilename: an exact base-name key matches, and wins over
// a glob that also covers the name.
func TestAssociationExactFilename(t *testing.T) {
	setAssociations(t, map[string]string{
		"Assocfile1365":   "assoc-a",
		"*ssocfile1365":   "assoc-b",
		"*.assocpattern1": "assoc-b",
	})
	if l, ok := ByPath("/p/Assocfile1365"); !ok || l.ID != "assoc-a" {
		t.Fatalf("exact filename = %q, %v; want assoc-a over the glob", l.ID, ok)
	}
	if l, ok := ByPath("/p/x.assocpattern1"); !ok || l.ID != "assoc-b" {
		t.Fatalf("glob filename = %q, %v", l.ID, ok)
	}
}

// TestAssociationBeatsSniffedPath: the user association outranks a per-path
// entry recorded by content sniffing (#893) — explicit intent beats detection.
func TestAssociationBeatsSniffedPath(t *testing.T) {
	const path = "/p/sniffed.assoc1365"
	AssociatePath(path, "assoc-b")
	setAssociations(t, map[string]string{"*.assoc1365": "assoc-a"})
	if l, ok := ByPath(path); !ok || l.ID != "assoc-a" {
		t.Fatalf("ByPath = %q, %v; want assoc-a over the sniffed association", l.ID, ok)
	}
}

// TestAssociationInvalidLanguage: an unknown target id never matches — the
// file falls back to built-in detection (or nothing) — and the entry is
// reported by InvalidAssociations.
func TestAssociationInvalidLanguage(t *testing.T) {
	setAssociations(t, map[string]string{
		"*.assocb":     "no-such-language",
		"*.assocdead1": "also-missing",
	})
	// Built-in extension still resolves under the broken association.
	if l, ok := ByPath("/p/f.assocb"); !ok || l.ID != "assoc-b" {
		t.Fatalf("fallback = %q, %v; want the built-in assoc-b", l.ID, ok)
	}
	// No built-in either: plain text (no match).
	if l, ok := ByPath("/p/f.assocdead1"); ok {
		t.Fatalf("expected no language, got %q", l.ID)
	}
	got := InvalidAssociations()
	if len(got) != 2 ||
		got[0] != (InvalidAssociation{Pattern: "*.assocb", Lang: "no-such-language"}) ||
		got[1] != (InvalidAssociation{Pattern: "*.assocdead1", Lang: "also-missing"}) {
		t.Fatalf("InvalidAssociations = %+v", got)
	}
}

// TestAssociationLanguageFeatures: language-keyed lookups that resolve via
// ByPath (comment syntax) follow the association too.
func TestAssociationLanguageFeatures(t *testing.T) {
	Register(Language{ID: "assoc-c", LineComment: "##"})
	setAssociations(t, map[string]string{"*.assoccmt": "assoc-c"})
	line, _, ok := Comments("/p/x.assoccmt")
	if !ok || line != "##" {
		t.Fatalf("Comments = %q, %v; want ## via the association", line, ok)
	}
}
