package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCommonPrefix (#2534): the longest common prefix of candidate texts,
// case-sensitive so candidates differing in case only share no extension.
func TestCommonPrefix(t *testing.T) {
	cands := func(s ...string) []candidate {
		out := make([]candidate, len(s))
		for i, t := range s {
			out[i] = candidate{text: t}
		}
		return out
	}
	cases := []struct {
		in   []candidate
		want string
	}{
		{nil, ""},
		{cands("abc_x"), "abc_x"},
		{cands("abc_x", "abc_y", "abc_z"), "abc_"},
		{cands("abc_x", "abd_y"), "ab"},
		{cands("Abc_x", "abc_y"), ""},
		{cands("./Abc_x", "./abc_y"), "./"},
		{cands("éa", "éb"), "é"},
		{cands("xyz", "abc"), ""},
	}
	for _, c := range cases {
		if got := commonPrefix(c.in); got != c.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// prefixTestModel starts a shell in a directory holding files, types
// `ls ./`+typed and opens the auto-suggest popup on it.
func prefixTestModel(t *testing.T, files []string, typed string) *Model {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &collector{}
	m := startShModelIn(t, c, dir)
	for _, r := range "ls ./" + typed {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, "echo of typed word", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "./"+typed
	})
	m.OnOutput()
	if !m.comp.open || m.comp.focused {
		t.Fatalf("auto-suggest must open an unfocused popup, got %+v", m.comp)
	}
	return m
}

// TestTabInsertsCommonPrefix (#2534): tab on an unfocused popup types the
// candidates' common prefix instead of the first row; the popup stays open,
// refilters on the new word and still accepts a selected row on tab.
func TestTabInsertsCommonPrefix(t *testing.T) {
	m := prefixTestModel(t, []string{"abc_x", "abc_y", "abc_z"}, "a")
	if len(m.comp.items) != 3 {
		t.Fatalf("candidates = %v, want three", m.comp.items)
	}
	if !m.completionKey("tab") {
		t.Fatal("tab must be consumed")
	}
	if !m.comp.open {
		t.Fatal("prefix insert must keep the popup open")
	}
	waitFor(t, "common prefix inserted", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "./abc_"
	})
	if !m.pendingSuggest {
		t.Fatal("prefix insert must arm the refresh")
	}
	m.OnOutput()
	if !m.comp.open || m.comp.word != "./abc_" || len(m.comp.items) != 3 || m.comp.focused {
		t.Fatalf("popup after refilter = %+v, want open, unfocused, three items on ./abc_", m.comp)
	}
	// Still operable: down selects, tab accepts the selected row.
	if !m.completionKey("down") || !m.comp.focused {
		t.Fatal("down must focus the popup")
	}
	m.comp.sel = 1
	if !m.completionKey("tab") || m.comp.open {
		t.Fatal("tab on a selected row must accept and close")
	}
	waitFor(t, "selected row accepted", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "./abc_y ")
	})
}

// TestTabNoCommonExtensionAcceptsFirst (#2534): when the common prefix adds
// nothing to the typed word, tab falls back to accepting the first row.
func TestTabNoCommonExtensionAcceptsFirst(t *testing.T) {
	m := prefixTestModel(t, []string{"abc_x", "abd_y"}, "ab")
	if !m.completionKey("tab") || m.comp.open {
		t.Fatal("tab without a common extension must accept and close")
	}
	waitFor(t, "first row accepted", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "./abc_x ")
	})
}

// TestTabSingleCandidateAccepts (#2534): a lone candidate is accepted as a
// finished token, unchanged from before.
func TestTabSingleCandidateAccepts(t *testing.T) {
	m := prefixTestModel(t, []string{"only", "zzz"}, "o")
	if len(m.comp.items) != 1 {
		t.Fatalf("candidates = %v, want [./only]", m.comp.items)
	}
	if !m.completionKey("tab") || m.comp.open {
		t.Fatal("tab on a single candidate must accept and close")
	}
	waitFor(t, "single candidate accepted", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), "./only ")
	})
}

// TestTabCaseOnlyDifferenceNoExtension (#2534, #968): candidates whose shared
// part differs only in case have no common extension, so tab accepts the
// first row (case-corrected) instead of inserting a mixed prefix.
func TestTabCaseOnlyDifferenceNoExtension(t *testing.T) {
	m := prefixTestModel(t, []string{"Abc_x", "abc_y"}, "a")
	if len(m.comp.items) != 2 {
		t.Fatalf("candidates = %v, want two", m.comp.items)
	}
	first := m.comp.items[0].text
	if !m.completionKey("tab") || m.comp.open {
		t.Fatal("tab without a common extension must accept and close")
	}
	waitFor(t, "first row accepted", func() bool {
		return strings.HasSuffix(m.lineBeforeCursor(), first+" ")
	})
}

// TestTabPrefixCaseCorrects (#2534, #968): a typed word matching the common
// prefix only case-insensitively is retyped in the candidates' spelling.
func TestTabPrefixCaseCorrects(t *testing.T) {
	m := prefixTestModel(t, []string{"abc_x", "abc_y"}, "A")
	if !m.completionKey("tab") || !m.comp.open {
		t.Fatal("tab must insert the prefix and keep the popup open")
	}
	waitFor(t, "case-corrected prefix", func() bool {
		_, word := parseCmdline(m.lineBeforeCursor())
		return word == "./abc_"
	})
}
