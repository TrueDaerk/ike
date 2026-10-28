package palette

import (
	"fmt"
	"path"
	"strings"
	"testing"
)

// longFixture is the tree behind #3079's telemetry: 10–12 character queries
// that leave 15–49 candidates and the picked file at the very end. Under
// internal/app/ the wanted recents.go sits among siblings that share its
// first six letters and continue with a word starting in s — recent_sel.go,
// recent_search.go … — so the scorer, which rewards a word boundary more than
// a consecutive rune, liked splitting "recents" into "recent" plus the s of
// the next word better than the file whose name one typed in full. Golden
// snapshots named after the source (testdata/recents.go.NN.golden) give the
// "basename.ext" query its own crowd, and filterbar/ is the same trap for a
// directory and a name typed without the '/'.
func longFixture() []string {
	paths := []string{"internal/app/recents.go"}
	for _, n := range []string{
		"recents_test.go", "recents_view.go", "recents_view_test.go",
		"recents_mode.go", "recentscope.go", "recent_sel.go",
		"recent_sel_test.go", "recent_search.go", "recent_snapshot.go",
		"recent_store.go", "recent_files_store.go", "recent_sort.go",
		"recent_switch.go", "recent_session.go", "recent_spans_test.go",
	} {
		paths = append(paths, "internal/app/"+n)
	}
	for i := 1; i <= 15; i++ {
		paths = append(paths, fmt.Sprintf("internal/app/testdata/recents.go.%02d.golden", i))
	}
	paths = append(paths,
		"internal/filterbar/filterbar.go", "internal/filterbar/filterbar_test.go",
		"internal/filterbar/filter_text.go", "internal/filterbar/negate_test.go",
		"internal/ui/listview.go", "internal/ui/list_view_test.go",
		"internal/ui/listviewport.go",
	)
	return paths
}

// TestFileModeLongQueryShapesRankTheFileFirst guards #3079's acceptance
// shapes: a query that is the file's base name, its name with the extension,
// and its parent directory plus name each rank that file first among at least
// fifteen hump matches. The parentdir/basename row is the one that failed:
// scored over the whole path, "app/recents" listed recent_sel.go first and
// recents.go below every sibling with an s-word after "recent".
func TestFileModeLongQueryShapesRankTheFileFirst(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode(longFixture()...)
	const want = "internal/app/recents.go"
	for _, query := range []string{"recents", "recents.go", "app/recents", "app/recents.go", "internal/app/recents"} {
		got := titles(f.Results(query, Context{Root: "/proj"}))
		if len(got) < 15 {
			t.Fatalf("%q matched %d files, want ≥ 15 for the fixture to mean anything: %v", query, len(got), got)
		}
		if r := indexOf(got, want); r != 0 {
			t.Errorf("%q: %s at rank %d of %d, want 0; list: %v", query, want, r, len(got), got)
		}
	}
}

// TestFileModeDirNamePrefixIsContiguous: a directory plus the start of a name
// grades the name part as a prefix and scores it contiguously (#3079), so the
// shortest file starting with the typed name leads instead of one whose next
// word happens to begin with the last typed letter.
func TestFileModeDirNamePrefixIsContiguous(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode(
		"internal/app/tool_slots.go", "internal/app/toolhost_layout_test.go",
		"internal/app/tools.go", "internal/app/tools_setup.go", "internal/app/layouts_tools_test.go",
	)
	cases := map[string]string{
		"app/tool":  "internal/app/tools.go",
		"app/tools": "internal/app/tools.go",
		"apptools":  "internal/app/tools.go",
	}
	for query, want := range cases {
		got := titles(f.Results(query, Context{Root: "/proj"}))
		if len(got) == 0 || got[0] != want {
			t.Errorf("%q = %v, want %s first", query, got, want)
		}
	}
}

// TestFileModeDirNameSpellsDirectory: among equal name matches the file whose
// directory one spelled whole ranks first, ahead of a shorter path whose
// directory merely starts with the typed letters (#3079).
func TestFileModeDirNameSpellsDirectory(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode("a/apps/tools.go", "internal/app/tools.go")
	got := titles(f.Results("app/tools", Context{Root: "/proj"}))
	if len(got) != 2 || got[0] != "internal/app/tools.go" {
		t.Fatalf("app/tools = %v, want internal/app/tools.go first", got)
	}
}

// TestFileModeDirNameSpans: the highlight of a directory-and-name match covers
// the directory letters, the typed '/' and the name, indexed in the path.
func TestFileModeDirNameSpans(t *testing.T) {
	setCase(t, "first_letter")
	items := fileMode("internal/app/tools.go").Results("app/tools", Context{Root: "/proj"})
	if len(items) == 0 {
		t.Fatal("app/tools matched nothing")
	}
	want := []int{9, 10, 11, 12, 13, 14, 15, 16, 17}
	if fmt.Sprint(items[0].Spans) != fmt.Sprint(want) {
		t.Fatalf("spans = %v, want %v", items[0].Spans, want)
	}
}

// TestFileModeSeparatorlessNameIsExact: a basename typed without its
// separators is still the whole name (#3079), so "recentsel" ranks
// recent_sel.go above recent_sel_test.go and every looser hump match.
func TestFileModeSeparatorlessNameIsExact(t *testing.T) {
	setCase(t, "first_letter")
	tier, _, ok := matchFile("recentsel", "internal/app/recent_sel.go", 0)
	if !ok || tier != tierFileExact {
		t.Fatalf("recentsel on recent_sel.go = tier %d (ok %v), want tierFileExact", tier, ok)
	}
	got := titles(fileMode(longFixture()...).Results("recentsel", Context{Root: "/proj"}))
	if len(got) == 0 || got[0] != "internal/app/recent_sel.go" {
		t.Fatalf("recentsel = %v, want recent_sel.go first", got)
	}
}

// TestFileModeLongerQueryNeverRanksLower is #3079's monotonicity guard: for
// every file of the fixture and every way of describing it — the name, the
// name with its extension, the parent directory plus the name with and
// without the '/' — typing one more letter of that description never moves
// the file further down than the shorter query had it. A directory-shaped
// description is checked from its first name letter on: before that the query
// is a bare name ("filterbar") or a directory ("filterbar/"), and turning one
// into the other changes what is asked — every file of the directory matches
// "filterbar/" alike — rather than refining it.
func TestFileModeLongerQueryNeverRanksLower(t *testing.T) {
	setCase(t, "first_letter")
	paths := longFixture()
	f := fileMode(paths...)
	for _, p := range paths {
		if strings.Contains(p, "/testdata/") {
			continue // the golden snapshots share one name up to their number
		}
		base := path.Base(p)
		stem := strings.TrimSuffix(base, path.Ext(base))
		dir := path.Base(path.Dir(p))
		descs := []struct {
			text string
			from int // first prefix length checked
		}{
			{stem, shortQueryLen + 1},
			{base, shortQueryLen + 1},
			{dir + "/" + stem, len(dir) + 2},
			{dir + "/" + base, len(dir) + 2},
			{dir + stem, len(dir) + 1},
		}
		for _, d := range descs {
			desc := d.text
			prev, prevQuery := -1, ""
			for n := d.from; n <= len(desc); n++ {
				q := desc[:n]
				r := indexOf(titles(f.Results(q, Context{Root: "/proj"})), p)
				if r < 0 {
					t.Fatalf("%q does not list %s", q, p)
				}
				if prev >= 0 && r > prev {
					t.Errorf("%s: %q ranks it %d, the shorter %q had it at %d", p, q, r, prevQuery, prev)
				}
				prev, prevQuery = r, q
			}
		}
	}
}
