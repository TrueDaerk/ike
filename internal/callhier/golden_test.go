package callhier

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
	root := entry("Greet", "/proj/a.go", 3)
	root.Detail = "func(name string)"
	m.Open(ilsp.CallHierarchyMsg{
		Path:  "/proj/a.go",
		Roots: []ilsp.CallHierarchyEntry{root},
		Fetch: rec.fetch,
	})
	var kids []ilsp.CallHierarchyEntry
	for i := 1; i <= 10; i++ {
		kids = append(kids, entry(fmt.Sprintf("caller%02d", i), fmt.Sprintf("/proj/c%d.go", i), i*10))
	}
	m.Apply(ilsp.CallHierarchyCallsMsg{ReqID: rec.reqs[0].reqID, Incoming: true, Calls: kids})
	m.Update(key("down"))  // caller01
	m.Update(key("right")) // fetch → leaf
	m.Apply(ilsp.CallHierarchyCallsMsg{ReqID: rec.reqs[1].reqID, Incoming: true})
	m.Update(key("left"))  // collapse the leaf: "·"
	m.Update(key("down"))  // caller02
	m.Update(key("right")) // fetch → grandchild, then collapse
	m.Apply(ilsp.CallHierarchyCallsMsg{ReqID: rec.reqs[2].reqID, Incoming: true,
		Calls: []ilsp.CallHierarchyEntry{entry("deep", "/proj/d.go", 0)}})
	m.Update(key("left"))  // collapse caller02
	m.Update(key("down"))  // caller03
	m.Update(key("right")) // in flight
	m.Update(key("G"))     // caller10: window scrolls
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

const goldenTop = "\x1b[38;2;95;135;255m╭────────────────────────────────────────────╮\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[1;4;4mC\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4ml\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mH\x1b[m\x1b[1;4;4mi\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mc\x1b[m\x1b[1;4;4mh\x1b[m\x1b[1;4;4my\x1b[m\x1b[4m \x1b[m\x1b[1;4;4m—\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mC\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ms\x1b[m                   \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[48;2;44;44;44m▾ Greet func(name string)  a.go:4\x1b[m          \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  · \x1b[m\x1b[1mcaller01\x1b[m\x1b[2m  c1.go:11\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller02\x1b[m\x1b[2m  c2.go:21\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  … \x1b[m\x1b[1mcaller03\x1b[m\x1b[2m  c3.go:31\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller04\x1b[m\x1b[2m  c4.go:41\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller05\x1b[m\x1b[2m  c5.go:51\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller06\x1b[m\x1b[2m  c6.go:61\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2menter jumps · space expands · tab\x1b[m          \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2mcallers/callees · alt+p/ctrl+e preview ·\x1b[m   \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2mesc closes\x1b[m                                 \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m╰────────────────────────────────────────────╯\x1b[m"
const goldenScrolled = "\x1b[38;2;95;135;255m╭────────────────────────────────────────────╮\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[1;4;4mC\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4ml\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mH\x1b[m\x1b[1;4;4mi\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4mc\x1b[m\x1b[1;4;4mh\x1b[m\x1b[1;4;4my\x1b[m\x1b[4m \x1b[m\x1b[1;4;4m—\x1b[m\x1b[4m \x1b[m\x1b[1;4;4mC\x1b[m\x1b[1;4;4ma\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4ml\x1b[m\x1b[1;4;4me\x1b[m\x1b[1;4;4mr\x1b[m\x1b[1;4;4ms\x1b[m                   \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller04\x1b[m\x1b[2m  c4.go:41\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller05\x1b[m\x1b[2m  c5.go:51\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller06\x1b[m\x1b[2m  c6.go:61\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller07\x1b[m\x1b[2m  c7.go:71\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller08\x1b[m\x1b[2m  c8.go:81\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2m  ▸ \x1b[m\x1b[1mcaller09\x1b[m\x1b[2m  c9.go:91\x1b[m                     \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[48;2;44;44;44m  ▸ caller10  c10.go:101\x1b[m                   \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m                                            \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2menter jumps · space expands · tab\x1b[m          \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2mcallers/callees · alt+p/ctrl+e preview ·\x1b[m   \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m│\x1b[m \x1b[2mesc closes\x1b[m                                 \x1b[38;2;95;135;255m│\x1b[m\n\x1b[38;2;95;135;255m╰────────────────────────────────────────────╯\x1b[m"
