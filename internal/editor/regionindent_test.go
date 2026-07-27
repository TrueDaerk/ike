package editor

import (
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/lang"
)

// regionindent_test.go covers #1304: inside an embedded region the editor
// indents by the region's language, not the host's. A private host language
// stands in for .http so this test never depends on the compiled-in plugins.
func init() {
	lang.Register(lang.Language{ID: "braceregion", Extensions: []string{"braceregion"}, IndentAfter: []string{"{", "["}})
	lang.Register(lang.Language{ID: "plainregion", Extensions: []string{"plainregion"}})
	lang.Register(lang.Language{
		ID:          "regionhost",
		Extensions:  []string{"rgnhost"},
		IndentAfter: []string{":"}, // the host's own rule, deliberately different
		Regions: func(lines []string) []lang.Region {
			// Everything after the first blank line is the "body"; its language
			// is named on the line before it, mimicking a Content-Type header.
			for i, l := range lines {
				if l != "" {
					continue
				}
				if i == 0 || i+1 >= len(lines) {
					return nil
				}
				return []lang.Region{{Lang: lines[i-1], StartLine: i + 1, EndLine: len(lines) - 1}}
			}
			return nil
		},
	})
}

// enterAt puts the caret at the end of line and presses Enter, returning the
// buffer text afterwards.
func enterAt(t *testing.T, m *Model, line int) string {
	t.Helper()
	m.cursor.Line = line
	m.cursor.Col = m.buf.RuneLen(line)
	m.insertNewline()
	return m.buf.String()
}

// TestEnterIndentsByTheRegionLanguage: a `{` at the end of an embedded
// brace-language line opens an indented line, even though the host language
// would not indent after `{`.
func TestEnterIndentsByTheRegionLanguage(t *testing.T) {
	cfg := host.MapConfig{"editor.auto_indent": "true", "editor.use_spaces": "true", "editor.tab_width": "2"}
	src := strings.Join([]string{"POST /x", "braceregion", "", "{"}, "\n") + "\n"
	m, _ := loadedInDir(t, cfg, "", "x.rgnhost", src)

	got := enterAt(t, &m, 3)
	lines := strings.Split(got, "\n")
	if len(lines) < 5 || lines[4] != "  " {
		t.Fatalf("enter after `{` in the region must indent one level, got %q", got)
	}
}

// TestEnterInAPlainRegionDoesNotInheritTheHost: a region whose language has no
// indent rules falls back to copy-indent instead of borrowing the host's.
func TestEnterInAPlainRegionDoesNotInheritTheHost(t *testing.T) {
	cfg := host.MapConfig{"editor.auto_indent": "true", "editor.use_spaces": "true", "editor.tab_width": "2"}
	src := strings.Join([]string{"POST /x", "plainregion", "", "note:"}, "\n") + "\n"
	m, _ := loadedInDir(t, cfg, "", "x.rgnhost", src)

	got := enterAt(t, &m, 3)
	lines := strings.Split(got, "\n")
	if len(lines) < 5 || lines[4] != "" {
		t.Fatalf("a rule-less region must not inherit the host's openers, got %q", got)
	}
}

// TestEnterOutsideARegionUsesTheHostLanguage: the host keeps deciding above
// the region.
func TestEnterOutsideARegionUsesTheHostLanguage(t *testing.T) {
	cfg := host.MapConfig{"editor.auto_indent": "true", "editor.use_spaces": "true", "editor.tab_width": "2"}
	src := strings.Join([]string{"head:", "braceregion", "", "{"}, "\n") + "\n"
	m, _ := loadedInDir(t, cfg, "", "x.rgnhost", src)

	got := enterAt(t, &m, 0)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 || lines[1] != "  " {
		t.Fatalf("the host's own rule must still apply outside a region, got %q", got)
	}
}

// TestRegionSplitBlock: `{`+enter with the caret between a pair opens the
// three-line block at the region language's indent width.
func TestRegionSplitBlock(t *testing.T) {
	cfg := host.MapConfig{"editor.auto_indent": "true", "editor.use_spaces": "true", "editor.tab_width": "2"}
	src := strings.Join([]string{"POST /x", "braceregion", "", "{}"}, "\n") + "\n"
	m, _ := loadedInDir(t, cfg, "", "x.rgnhost", src)

	m.cursor.Line, m.cursor.Col = 3, 1 // between { and }
	m.insertNewline()
	lines := strings.Split(m.buf.String(), "\n")
	if len(lines) < 6 || lines[3] != "{" || lines[4] != "  " || lines[5] != "}" {
		t.Fatalf("block split must use the region's indent, got %q", m.buf.String())
	}
}
