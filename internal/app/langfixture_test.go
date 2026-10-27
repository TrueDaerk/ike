package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
)

// registerTestLang registers l for one test and puts the whole registry back
// when the test ends (lang.Snapshot, #3056). lang.Register replaces an entry
// wholesale and has no inverse, so a stand-in left behind strips the real
// plugin's grammar from every later test in the binary — "go-archive-test"
// took Go's and the json stand-ins took JSON's, which is what made the
// complexity, http-highlight and phpindex tests fail only in a full package
// run (#2997), and re-registering the previous owner did not give the
// extensions back, which is what made them fail on a repeated run (-count=2).
func registerTestLang(t *testing.T, l lang.Language) {
	t.Helper()
	t.Cleanup(lang.Snapshot())
	lang.Register(l)
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

// TestNoRawLangRegisterInTests is the sweep guard (#3056): a test registers
// languages through registerTestLang, never lang.Register, so the registry is
// the same for every test whatever ran before it. Process-wide fixtures in
// init() are the exception — they are in place before the first test and stay
// constant.
func TestNoRawLangRegisterInTests(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "langfixture_test.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fn := ""
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "func ") {
				fn = line
			}
			if strings.Contains(line, "lang.Register(") && !strings.HasPrefix(fn, "func init()") && !strings.Contains(line, "process-wide fixture") && !strings.Contains(line, "registerTestLang's snapshot") {
				t.Errorf("%s:%d: use registerTestLang(t, l) instead of lang.Register — it restores the registry when the test ends", f, i+1)
			}
		}
	}
}
