package palette

import (
	"path"
	"sort"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/fuzzy"
)

// classFixture is a corpus in which one query meets several match classes at
// once: siblings sharing a long stem, a foo.ext / foo_test.ext / test_foo.ext
// triple, the same name in several directories, names with separators and
// camelCase, and directories named like the files.
func classFixture() []string {
	return []string{
		"internal/palette/file_mode.go",
		"internal/palette/file_mode_test.go",
		"internal/palette/file_mode_humps_test.go",
		"internal/palette/file_mode_long_test.go",
		"internal/palette/mode_humps_extra_test.go",
		"internal/app/session_store.go",
		"internal/app/session_store_test.go",
		"internal/app/test_session_store.go",
		"internal/app/session_legacy_store.go",
		"web/SessionStore.tsx",
		"web/SessionStoreView.tsx",
		"web/session-store-utils.ts",
		"internal/settings/keymap_page.go",
		"internal/keymap/keymap_page.go",
		"internal/settings/keymap_page_test.go",
		"internal/keymappage/list.go",
		"keymap/page.go",
		"internal/session_store/codec.go",
		"internal/session_store/codec_test.go",
	}
}

// classFixtureHot are the files opened fifty times: for the queries below
// they match by initials, directory or scattered characters only, so frecency
// pulls them towards the top of whatever it is allowed to reorder.
var classFixtureHot = []string{
	"internal/palette/mode_humps_extra_test.go",
	"internal/app/session_legacy_store.go",
	"internal/app/session_store_test.go",
	"web/session-store-utils.ts",
	"internal/keymappage/list.go",
	"keymap/page.go",
	"internal/session_store/codec.go",
}

// spanningDirectory keys the directory matches stitched across segments,
// which list below the single-segment ones inside the directory class.
const spanningDirectory = "directory (spanning)"

// classCases maps each query to the files it lists, by match class.
var classCases = []struct {
	query   string
	classes map[string][]string
}{
	{"session_stor", map[string][]string{
		"prefix":    {"internal/app/session_store.go", "internal/app/session_store_test.go"},
		"contains":  {"internal/app/test_session_store.go"},
		"initials":  {"internal/app/session_legacy_store.go"},
		"directory": {"internal/session_store/codec.go", "internal/session_store/codec_test.go"},
	}},
	{"session_store", map[string][]string{
		"exact":     {"internal/app/session_store.go"},
		"prefix":    {"internal/app/session_store_test.go"},
		"contains":  {"internal/app/test_session_store.go"},
		"initials":  {"internal/app/session_legacy_store.go"},
		"directory": {"internal/session_store/codec.go", "internal/session_store/codec_test.go"},
	}},
	{"sessionstore", map[string][]string{
		"exact":  {"internal/app/session_store.go", "web/SessionStore.tsx"},
		"prefix": {"web/SessionStoreView.tsx"},
		"initials": {
			"internal/app/session_store_test.go", "internal/app/test_session_store.go",
			"internal/app/session_legacy_store.go", "web/session-store-utils.ts",
		},
		"directory": {"internal/session_store/codec.go", "internal/session_store/codec_test.go"},
	}},
	{"mode_humps_t", map[string][]string{
		"contains": {"internal/palette/file_mode_humps_test.go"},
		"initials": {"internal/palette/mode_humps_extra_test.go"},
	}},
	{"file_mode_humps", map[string][]string{
		"prefix": {"internal/palette/file_mode_humps_test.go"},
	}},
	{"keymappage", map[string][]string{
		"exact":           {"internal/keymap/keymap_page.go", "internal/settings/keymap_page.go"},
		"initials":        {"internal/settings/keymap_page_test.go"},
		"directory":       {"internal/keymappage/list.go"},
		spanningDirectory: {"keymap/page.go"},
	}},
	// Nothing hump-matches: the permissive fallback still tells a name that
	// holds the query from scattered characters.
	{"ession_store", map[string][]string{
		"contains": {
			"internal/app/session_store.go", "internal/app/session_store_test.go",
			"internal/app/test_session_store.go",
		},
		"fuzzy": {
			"internal/app/session_legacy_store.go",
			"internal/session_store/codec.go", "internal/session_store/codec_test.go",
		},
	}},
}

// setMatchOrder installs search.match_order for the duration of the test.
func setMatchOrder(t *testing.T, order ...string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Search.MatchOrder = order
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// classFinder is the fixture finder with its hot files recorded.
func classFinder(cx Context) *FileMode {
	f := fileMode(classFixture()...)
	opens := make(map[string]int, len(classFixtureHot))
	for _, p := range classFixtureHot {
		opens[p] = 50
	}
	f.SetFrecency(frecStore(cx.Root, opens))
	return f
}

// assertClassGroups checks that got lists the case's classes as blocks in the
// given order, each block holding exactly its files in any order.
func assertClassGroups(t *testing.T, query string, got []string, classes map[string][]string, order []string) {
	t.Helper()
	var blocks []string
	for _, name := range order {
		blocks = append(blocks, name)
		if name == "directory" {
			blocks = append(blocks, spanningDirectory)
		}
	}
	at := 0
	for _, name := range blocks {
		want := append([]string(nil), classes[name]...)
		if at+len(want) > len(got) {
			t.Fatalf("%q: list ends before the %s block %v: %v", query, name, want, got)
		}
		have := append([]string(nil), got[at:at+len(want)]...)
		sort.Strings(want)
		sort.Strings(have)
		if strings.Join(have, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%q: %s block = %v, want %v; list: %v", query, name, have, want, got)
		}
		at += len(want)
	}
	if at != len(got) {
		t.Fatalf("%q: %d rows beyond the expected classes: %v", query, len(got)-at, got[at:])
	}
}

// TestFileModeRanksByMatchClass: with the default order a file whose name
// holds the query verbatim lists above every file that matches by initials,
// directory or scattered characters only, however often those were opened,
// and a name that equals the query lists first.
func TestFileModeRanksByMatchClass(t *testing.T) {
	setCase(t, "first_letter")
	setMatchOrder(t, fuzzy.ClassNames()...)
	cx := Context{Root: "/proj"}
	f := classFinder(cx)
	for _, tc := range classCases {
		if n := len([]rune(tc.query)); n < 10 || n > 15 {
			t.Fatalf("%q: the fixture is about 10–15 character queries", tc.query)
		}
		got := titles(f.Results(tc.query, cx))
		assertClassGroups(t, tc.query, got, tc.classes, fuzzy.ClassNames())

		// The same claim without the table: no row whose name lacks the
		// query may list above one whose name holds it.
		exact := map[string]bool{}
		for _, p := range tc.classes["exact"] {
			exact[p] = true
		}
		lastVerbatim, firstOther := -1, len(got)
		for i, p := range got {
			switch {
			case exact[p]:
				if i >= len(tc.classes["exact"]) {
					t.Errorf("%q: exact match %s at rank %d, want it first; list: %v", tc.query, p, i, got)
				}
			case strings.Contains(strings.ToLower(path.Base(p)), strings.ToLower(tc.query)):
				lastVerbatim = i
			case i < firstOther:
				firstOther = i
			}
		}
		if lastVerbatim > firstOther {
			t.Errorf("%q: %s lists above %s, whose name holds the query; list: %v", tc.query, got[firstOther], got[lastVerbatim], got)
		}
	}
}

// TestFileModeFrecencyStaysInsideAClass: the hot file of a class leads that
// class and nothing above it.
func TestFileModeFrecencyStaysInsideAClass(t *testing.T) {
	setCase(t, "first_letter")
	setMatchOrder(t, fuzzy.ClassNames()...)
	cx := Context{Root: "/proj"}
	got := titles(classFinder(cx).Results("session_stor", cx))
	if got[0] != "internal/app/session_store_test.go" {
		t.Errorf("the hot prefix match should lead its class, got %v", got)
	}
	if r := indexOf(got, "internal/session_store/codec.go"); r != 4 {
		t.Errorf("the hot directory match should lead the directory class at rank 4, got %d: %v", r, got)
	}
}

// TestFileModeFollowsConfiguredMatchOrder: the list follows
// search.match_order, read live for every query.
func TestFileModeFollowsConfiguredMatchOrder(t *testing.T) {
	setCase(t, "first_letter")
	cx := Context{Root: "/proj"}
	f := classFinder(cx)
	orders := [][]string{
		{"directory", "initials", "exact", "prefix", "contains", "fuzzy"},
		{"fuzzy", "contains", "prefix", "exact", "initials", "directory"},
	}
	for _, order := range orders {
		setMatchOrder(t, order...)
		for _, tc := range classCases {
			got := titles(f.Results(tc.query, cx))
			assertClassGroups(t, tc.query, got, tc.classes, order)
		}
	}
	// With directory first the hot directory hit is the first row.
	setMatchOrder(t, orders[0]...)
	if got := titles(f.Results("session_store", cx)); got[0] != "internal/session_store/codec.go" {
		t.Errorf("directory first: %v", got)
	}
}

// TestFileModeInvalidMatchOrderRanksByDefault: a list the validator would
// reject never reorders the finder.
func TestFileModeInvalidMatchOrderRanksByDefault(t *testing.T) {
	setCase(t, "first_letter")
	setMatchOrder(t, "fuzzy", "exact")
	cx := Context{Root: "/proj"}
	f := classFinder(cx)
	for _, tc := range classCases {
		assertClassGroups(t, tc.query, titles(f.Results(tc.query, cx)), tc.classes, fuzzy.ClassNames())
	}
}

// TestFileModeSlashQueryIsClassedByItsNamePart: a directory-and-name query
// requires the directory and ranks by how the name part matches the name.
func TestFileModeSlashQueryIsClassedByItsNamePart(t *testing.T) {
	setCase(t, "first_letter")
	cx := Context{Root: "/proj"}
	f := fileMode(
		"internal/app/store.go", "internal/app/store_test.go", "internal/app/session_store.go",
		"internal/app/slow_tore.go", "lib/other/store.go",
	)
	cases := []struct {
		order []string
		want  []string
	}{
		{fuzzy.ClassNames(), []string{
			"internal/app/store.go", "internal/app/store_test.go",
			"internal/app/session_store.go", "internal/app/slow_tore.go",
		}},
		{[]string{"initials", "contains", "prefix", "exact", "directory", "fuzzy"}, []string{
			"internal/app/slow_tore.go", "internal/app/session_store.go",
			"internal/app/store_test.go", "internal/app/store.go",
		}},
	}
	for _, tc := range cases {
		setMatchOrder(t, tc.order...)
		got := titles(f.Results("app/store", cx))
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("order %v: app/store = %v, want %v", tc.order, got, tc.want)
		}
	}
}

// TestFileModeShortQueryIgnoresMatchOrder: up to two characters frecency
// leads the list whatever the classes say.
func TestFileModeShortQueryIgnoresMatchOrder(t *testing.T) {
	setCase(t, "first_letter")
	setMatchOrder(t, fuzzy.ClassNames()...)
	cx := Context{Root: "/proj"}
	f := fileMode("no.go", "xx-note.go")
	f.SetFrecency(frecStore(cx.Root, map[string]int{"xx-note.go": 2}))
	if got := titles(f.Results("no", cx)); got[0] != "xx-note.go" {
		t.Fatalf("short query = %v, want the hot file first", got)
	}
}
