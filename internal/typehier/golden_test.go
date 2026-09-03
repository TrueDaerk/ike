package typehier

import (
	"fmt"
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// goldenModel builds a tree exercising every row state the renderer knows —
// an expanded root with a detail, a loaded leaf ("·"), a node whose fetch is
// in flight ("…"), a collapsed loaded node ("▸") — over enough siblings for
// the render window to scroll, with the cursor at the end of the list.
func goldenModel(t *testing.T) *Model {
	t.Helper()
	m := New()
	m.SetSize(60, 24) // listHeight = 7
	m.SetDisplayPath(func(p string) string { return strings.TrimPrefix(p, "/proj/") })
	rec := &fetchRecorder{}
	root := entry("Circle", "/proj/a.go", 3)
	root.Detail = "struct"
	m.Open(ilsp.TypeHierarchyMsg{
		Path:  "/proj/a.go",
		Roots: []ilsp.TypeHierarchyEntry{root},
		Fetch: rec.fetch,
	})
	var kids []ilsp.TypeHierarchyEntry
	for i := 1; i <= 10; i++ {
		kids = append(kids, entry(fmt.Sprintf("Shape%02d", i), fmt.Sprintf("/proj/s%d.go", i), i*10))
	}
	m.Apply(ilsp.TypeHierarchyItemsMsg{ReqID: rec.reqs[0].reqID, Supertypes: true, Items: kids})
	m.Update(key("down"))  // Shape01
	m.Update(key("right")) // fetch → leaf
	m.Apply(ilsp.TypeHierarchyItemsMsg{ReqID: rec.reqs[1].reqID, Supertypes: true})
	m.Update(key("left"))  // collapse the leaf: "·"
	m.Update(key("down"))  // Shape02
	m.Update(key("right")) // fetch → grandchild, then collapse
	m.Apply(ilsp.TypeHierarchyItemsMsg{ReqID: rec.reqs[2].reqID, Supertypes: true,
		Items: []ilsp.TypeHierarchyEntry{entry("Deep", "/proj/d.go", 0)}})
	m.Update(key("left"))  // collapse Shape02
	m.Update(key("down"))  // Shape03
	m.Update(key("right")) // in flight
	m.Update(key("G"))     // Shape10: window scrolls
	return m
}

// TestGoldenViewTop pins the rendered overlay byte-for-byte with the cursor
// on the first row, and TestGoldenViewScrolled with the window scrolled to
// the end — the refactor into hiertree must keep both identical.
func TestGoldenViewTop(t *testing.T) {
	m := goldenModel(t)
	m.Update(key("g"))
	if got := m.View(); got != goldenTop {
		t.Fatalf("view drifted:\n got %q\nwant %q", got, goldenTop)
	}
}

func TestGoldenViewScrolled(t *testing.T) {
	m := goldenModel(t)
	if got := m.View(); got != goldenScrolled {
		t.Fatalf("view drifted:\n got %q\nwant %q", got, goldenScrolled)
	}
}

const goldenTop = "\x1b[38;2;95;135;255m╭────────────────────────────────────────────╮\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[1;4;4mT\x1b[m\x1b[1;4;4my\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mH\x1b[m\x1b[1;4;4mi\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mc\x1b[m\x1b[1;4;4mh\x1b[m\x1b[1;4;4my\x1b[m\x1b[4m \x1b[m\x1b[1;4;4m—\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mS\x1b[m\x1b[1;4;4mu\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mt\x1b[m\x1b[1;4;4my\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4ms\x1b[m                \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[48;2;44;44;44m▾ Circle struct  a.go:4\x1b[m                    \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  · \x1b[m\x1b[1mShape01\x1b[m\x1b[2m  s1.go:11\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape02\x1b[m\x1b[2m  s2.go:21\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  … \x1b[m\x1b[1mShape03\x1b[m\x1b[2m  s3.go:31\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape04\x1b[m\x1b[2m  s4.go:41\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape05\x1b[m\x1b[2m  s5.go:51\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape06\x1b[m\x1b[2m  s6.go:61\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2menter jumps · space expands · tab\x1b[m          \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2msupertypes/subtypes · esc closes\x1b[m           \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m╰────────────────────────────────────────────╯\x1b[m"
const goldenScrolled = "\x1b[38;2;95;135;255m╭────────────────────────────────────────────╮\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[1;4;4mT\x1b[m\x1b[1;4;4my\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mH\x1b[m\x1b[1;4;4mi\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mc\x1b[m\x1b[1;4;4mh\x1b[m\x1b[1;4;4my\x1b[m\x1b[4m \x1b[m\x1b[1;4;4m—\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mS\x1b[m\x1b[1;4;4mu\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mt\x1b[m\x1b[1;4;4my\x1b[m\x1b[1;4;4mp\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4ms\x1b[m                \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape04\x1b[m\x1b[2m  s4.go:41\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape05\x1b[m\x1b[2m  s5.go:51\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape06\x1b[m\x1b[2m  s6.go:61\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape07\x1b[m\x1b[2m  s7.go:71\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape08\x1b[m\x1b[2m  s8.go:81\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mShape09\x1b[m\x1b[2m  s9.go:91\x1b[m                      \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[48;2;44;44;44m  ▸ Shape10  s10.go:101\x1b[m                    \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2menter jumps · space expands · tab\x1b[m          \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2msupertypes/subtypes · esc closes\x1b[m           \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m╰────────────────────────────────────────────╯\x1b[m"
