package app

import (
	"testing"

	"ike/internal/lang"
)

// registerTestLang registers l for one test and restores what the registry
// held before when the test ends: the id's previous entry and the previous
// owner of every extension l claims. lang.Register replaces an entry
// wholesale, so a stand-in left behind strips the real plugin's grammar from
// every later test in the binary — "go-archive-test" took Go's and the json
// stand-ins took JSON's, which is what made the complexity, http-highlight
// and phpindex tests fail only in a full package run (#2997).
func registerTestLang(t *testing.T, l lang.Language) {
	t.Helper()
	prev := map[string]lang.Language{}
	if p, ok := lang.ByID(l.ID); ok {
		prev[p.ID] = p
	}
	for _, e := range l.Extensions {
		if p, ok := lang.ByExt(e); ok {
			prev[p.ID] = p
		}
	}
	lang.Register(l)
	t.Cleanup(func() {
		for _, p := range prev {
			lang.Register(p)
		}
	})
}

// ensureLang registers the bare stand-in l only when no language with its id
// is registered yet. The tests that call it need the id-to-extension
// association, not a grammar; where the real plugin is already in the binary
// it must stay, grammar and all.
func ensureLang(l lang.Language) {
	if _, ok := lang.ByID(l.ID); ok {
		return
	}
	lang.Register(l)
}

// withTestExtension re-registers the language id with ext added to its
// extensions for one test, keeping its grammar and everything else, and
// registers a bare entry when the id is unknown.
func withTestExtension(t *testing.T, id, ext string) {
	t.Helper()
	l, ok := lang.ByID(id)
	if !ok {
		l = lang.Language{ID: id}
	}
	l.Extensions = append(append([]string(nil), l.Extensions...), ext)
	registerTestLang(t, l)
}
