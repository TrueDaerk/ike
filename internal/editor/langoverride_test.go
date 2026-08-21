package editor

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
)

func init() {
	// Private test languages so the override tests never depend on the
	// compiled-in language plugins (which live under plugins/ and are not
	// imported here). "lotest" is recognized by base name only — the shape a
	// Dockerfile-like language has.
	lang.Register(lang.Language{ID: "lotest", Filenames: []string{"Lofile"}})
	lang.Register(lang.Language{ID: "lotest-bare"})
}

// fileless is an empty buffer with no path — a fresh tab, a split, the target
// of a paste — seeded with content.
func fileless(t *testing.T, content string) Model {
	t.Helper()
	m := New()
	m.SetSize(80, 20)
	m.SetFocused(true)
	if content != "" {
		m.ApplyTextEdits([]TextEdit{{Text: content}})
	}
	return m
}

// TestLangOverridePathShapes guards the synthetic name every path-keyed
// lookup resolves: the first extension on a "buffer" stem, the exact base
// name for a language recognized by one, and "" for a language a path lookup
// could never match.
func TestLangOverridePathShapes(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	cases := map[string]string{
		"markdown":    "buffer.md",
		"lotest":      "Lofile",
		"lotest-bare": "",
		"nope":        "",
		"":            "",
	}
	for id, want := range cases {
		if got := langOverridePath(id); got != want {
			t.Errorf("langOverridePath(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestSetLangOverrideResolvesLanguage guards the core of #2033: a file-less
// buffer told to be markdown resolves as markdown everywhere the editor asks
// — LangID for the intention context, langPath for every path-keyed lookup —
// while its Path stays empty.
func TestSetLangOverrideResolvesLanguage(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := fileless(t, "# Title\n")
	if m.LangID() != "" {
		t.Fatalf("a typeless buffer must resolve no language, got %q", m.LangID())
	}
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("setting a registered language on a file-less buffer must succeed")
	}
	if got := m.LangID(); got != "markdown" {
		t.Fatalf("LangID = %q, want markdown", got)
	}
	if got := m.LangPath(); got != "buffer.md" {
		t.Fatalf("LangPath = %q, want buffer.md", got)
	}
	if m.Path() != "" || m.HasFile() {
		t.Fatalf("the override must not invent a file, path = %q", m.Path())
	}
	if got := m.LangOverrideTitle(); got != "markdown" {
		t.Fatalf("status-line title = %q, want markdown", got)
	}
}

// TestSetLangOverrideIsChangeableAndClearable: the pick is a live choice —
// switching to another language and back to plain text both take effect.
func TestSetLangOverrideIsChangeableAndClearable(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	lang.Register(lang.Language{ID: "ovcsv", Extensions: []string{"ovcsv"}})
	m := fileless(t, "a,b\n")
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	if _, ok := m.SetLangOverride("ovcsv"); !ok {
		t.Fatal("switching the language must apply")
	}
	if got := m.LangID(); got != "ovcsv" {
		t.Fatalf("LangID = %q, want ovcsv", got)
	}
	if _, ok := m.SetLangOverride(""); !ok {
		t.Fatal("clearing back to plain text must apply")
	}
	if got := m.LangID(); got != "" {
		t.Fatalf("cleared LangID = %q, want empty", got)
	}
	if got := m.LangPath(); got != "" {
		t.Fatalf("cleared LangPath = %q, want empty", got)
	}
}

// TestSetLangOverrideRefusesUnusable guards the two refusals: a buffer with a
// file (its path classifies it) and an id no path lookup could resolve.
func TestSetLangOverrideRefusesUnusable(t *testing.T) {
	m := fileless(t, "x\n")
	if _, ok := m.SetLangOverride("no-such-language"); ok {
		t.Fatal("an unregistered id must be refused")
	}
	if _, ok := m.SetLangOverride("lotest-bare"); ok {
		t.Fatal("a language no path lookup can match must be refused")
	}
	if got := m.LangOverride(); got != "" {
		t.Fatalf("a refused pick must leave no override, got %q", got)
	}

	withFile := loadedExt(t, "ctest", "alpha\n")
	if _, ok := withFile.SetLangOverride("markdown"); ok {
		t.Fatal("a buffer with a file must refuse the override — its path decides")
	}
	if got := withFile.LangOverride(); got != "" {
		t.Fatalf("a buffer with a file reports no override, got %q", got)
	}
}

// TestLangOverrideDropsWhenBufferGetsAPath guards the acceptance rule that
// saving hands the classification back to the file name (#2033): the override
// is gone, and the path — not the chosen language — resolves the buffer.
func TestLangOverrideDropsWhenBufferGetsAPath(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := fileless(t, "# Title\n")
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	path := filepath.Join(t.TempDir(), "notes.ctest")
	if err := m.saveAs(path); err != nil {
		t.Fatal(err)
	}
	if got := m.LangOverride(); got != "" {
		t.Fatalf("a saved buffer keeps no override, got %q", got)
	}
	if got := m.LangPath(); got != path {
		t.Fatalf("LangPath = %q, want the saved path %q", got, path)
	}
	if got := m.LangID(); got != "ctest" {
		t.Fatalf("LangID = %q, want the path's language ctest", got)
	}
}

// TestLangOverrideClearedByLoad: opening a real file into the same view drops
// a previously chosen type too.
func TestLangOverrideClearedByLoad(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := fileless(t, "# Title\n")
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	path := filepath.Join(t.TempDir(), "f.ctest")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if got := m.LangOverride(); got != "" {
		t.Fatalf("a loaded file keeps no override, got %q", got)
	}
	if got := m.LangID(); got != "ctest" {
		t.Fatalf("LangID = %q, want ctest", got)
	}
}

// TestLangOverrideDrivesCommentToggle proves the override reaches a consumer
// that is nowhere near the highlighter: comment toggling resolves its marker
// through langPath, so an overridden buffer comments like a file of the type.
func TestLangOverrideDrivesCommentToggle(t *testing.T) {
	m := fileless(t, "alpha\nbeta\n")
	if _, ok := m.SetLangOverride("ctest"); !ok {
		t.Fatal("ctest must apply")
	}
	m, _ = m.runAction("comment_line")
	if got := line(m, 0); got != "// alpha" {
		t.Fatalf("comment: %q", got)
	}
}

// TestLangOverrideDrivesMarkdownRendering guards the rendering half of the
// acceptance list: with markdown chosen, the markdown layer detects the
// buffer's tables — the same probe an opened .md file passes.
func TestLangOverrideDrivesMarkdownRendering(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := fileless(t, "| a | b |\n|---|---|\n| 1 | 2 |\n")
	if got := m.tableBlocks(); len(got) != 0 {
		t.Fatalf("a typeless buffer must render no markdown tables, got %d", len(got))
	}
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	if got := m.tableBlocks(); len(got) != 1 {
		t.Fatalf("table blocks = %d, want 1", len(got))
	}
}

// TestLangOverrideDrivesSVTable guards the CSV half: the separator-table
// layer picks the buffer up once it is treated as a *sv language.
func TestLangOverrideDrivesSVTable(t *testing.T) {
	lang.Register(lang.Language{ID: "csv", Extensions: []string{"csv"}})
	m := fileless(t, "a,b\n1,2\n")
	if got := m.svLangID(); got != "" {
		t.Fatalf("a typeless buffer is no table, got %q", got)
	}
	if _, ok := m.SetLangOverride("csv"); !ok {
		t.Fatal("csv must apply")
	}
	if got := m.svLangID(); got != "csv" {
		t.Fatalf("svLangID = %q, want csv", got)
	}
}

// TestLangOverrideTravelsWithSplit: a split is a second view of the same
// document, so it inherits the chosen type like the encoding and the EOL
// flavour — otherwise one half of a split would lose the highlighting.
func TestLangOverrideTravelsWithSplit(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	src := fileless(t, "# Title\n")
	if _, ok := src.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	dst := New()
	dst.SetSize(80, 20)
	dst.ShareDocumentWith(&src)
	if got := dst.LangID(); got != "markdown" {
		t.Fatalf("the second view resolves %q, want markdown", got)
	}
}

// TestParseKeyTellsFilelessViewsApart guards the routing identity (#2033): a
// saved buffer answers to its path, a file-less one to a tag of its own, and
// no two views share a tag — a parse result must never land in the wrong
// buffer.
func TestParseKeyTellsFilelessViewsApart(t *testing.T) {
	a, b := fileless(t, "one"), fileless(t, "two")
	if a.ParseKey() == "" || a.ParseKey() == b.ParseKey() {
		t.Fatalf("file-less views must carry distinct keys, got %q and %q", a.ParseKey(), b.ParseKey())
	}
	if !IsBufferKey(a.ParseKey()) {
		t.Fatalf("%q must be recognizable as a view key", a.ParseKey())
	}
	withFile := loadedExt(t, "ctest", "alpha\n")
	if got := withFile.ParseKey(); got != withFile.Path() || IsBufferKey(got) {
		t.Fatalf("a saved buffer answers to its path, got %q", got)
	}
}

// TestFilelessParseAppliesToItsOwnView: the parse of a file-less buffer is
// accepted under the view's key and rejected under anything else — the empty
// path every such buffer used to share included, which is why routing by path
// left a chosen language unhighlighted.
func TestFilelessParseAppliesToItsOwnView(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md"}})
	m := fileless(t, "**bold** x")
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("markdown must apply")
	}
	spans := []highlight.Span{{Line: 0, StartCol: 2, EndCol: 6, Capture: "string"}}

	dropped, _ := m.Update(highlight.SpansMsg{Path: "", Version: m.docVersion, Spans: spans})
	if got := dropped.SyntaxCapture(0, 2); got != "" {
		t.Fatalf("a result under the empty path must be dropped, got %q", got)
	}
	other := fileless(t, "**bold** x")
	dropped, _ = m.Update(highlight.SpansMsg{Path: other.ParseKey(), Version: m.docVersion, Spans: spans})
	if got := dropped.SyntaxCapture(0, 2); got != "" {
		t.Fatalf("another view's result must be dropped, got %q", got)
	}

	m, _ = m.Update(highlight.SpansMsg{Path: m.ParseKey(), Version: m.docVersion, Spans: spans})
	if got := m.SyntaxCapture(0, 2); got != "string" {
		t.Fatalf("the view's own parse must apply, capture = %q", got)
	}
}
