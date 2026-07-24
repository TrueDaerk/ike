package langmake

import (
	"testing"

	"ike/internal/lang"
)

// TestMakeRegistered guards #1136: the exact base names Makefile / makefile /
// GNUmakefile and the .mk extension resolve to the make language, with # line
// comments, no LSP server (none exists), and the tab-indent default (#1137).
func TestMakeRegistered(t *testing.T) {
	for _, path := range []string{
		"/p/Makefile",
		"/p/makefile",
		"/p/GNUmakefile",
		"/p/rules.mk",
	} {
		l, ok := lang.ByPath(path)
		if !ok {
			t.Errorf("%s: no language registered", path)
			continue
		}
		if l.ID != "make" {
			t.Errorf("%s → %s, want make", path, l.ID)
		}
	}

	// A base name that merely contains "Makefile" must not match the
	// exact-name index; the extension path is the only fallback.
	if l, ok := lang.ByPath("/p/Makefile.bak"); ok && l.ID == "make" {
		t.Error("Makefile.bak must not resolve to make")
	}

	l, _ := lang.ByID("make")
	if l.Server != nil {
		t.Errorf("server = %+v, want none (no Makefile language server)", l.Server)
	}
	if l.UseTabs == nil || !*l.UseTabs {
		t.Errorf("UseTabs = %v, want true — recipes require a literal tab (#1137)", l.UseTabs)
	}
	line, _, ok := lang.Comments("/p/Makefile")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}
