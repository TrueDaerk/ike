package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
)

// --- scanner (#1655) -------------------------------------------------------

func spanOf(t *testing.T, line string) []linkSpan {
	t.Helper()
	return scanLinks([]rune(line))
}

func TestScanLinksBareURL(t *testing.T) {
	spans := spanOf(t, "see https://example.com/a?b=1 here")
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want one", spans)
	}
	s := spans[0]
	if s.start != 4 || s.url != "https://example.com/a?b=1" {
		t.Fatalf("span = %+v", s)
	}
	if got := s.end - s.start; got != len(s.url) {
		t.Fatalf("span covers %d cols, url is %d runes", got, len(s.url))
	}
}

func TestScanLinksTrimsTrailingPunctuation(t *testing.T) {
	for line, want := range map[string]string{
		"go to https://example.com.":                        "https://example.com",
		"go to https://example.com/a, then":                 "https://example.com/a",
		"(see https://example.com/a)":                       "https://example.com/a",
		"is it https://example.com/a?":                      "https://example.com/a",
		"quote 'https://example.com/a'":                     "https://example.com/a",
		"https://en.wikipedia.org/wiki/Go_(language) stays": "https://en.wikipedia.org/wiki/Go_(language)",
		"<https://example.com/auto>":                        "https://example.com/auto",
	} {
		spans := scanLinks([]rune(line))
		if len(spans) != 1 || spans[0].url != want {
			t.Fatalf("line %q: spans = %+v, want url %q", line, spans, want)
		}
	}
}

func TestScanLinksSchemeNeedsBoundaryAndHost(t *testing.T) {
	if spans := spanOf(t, "xhttps://example.com"); len(spans) != 0 {
		t.Fatalf("mid-word scheme: spans = %+v, want none", spans)
	}
	if spans := spanOf(t, "empty https:// nothing"); len(spans) != 0 {
		t.Fatalf("empty authority: spans = %+v, want none", spans)
	}
}

func TestScanLinksStopsAtControlAndNonASCII(t *testing.T) {
	spans := spanOf(t, "https://example.com/a\x07bad")
	if len(spans) != 1 || spans[0].url != "https://example.com/a" {
		t.Fatalf("spans = %+v, want the URL cut before the BEL", spans)
	}
	spans = spanOf(t, "https://example.com/aä")
	if len(spans) != 1 || spans[0].url != "https://example.com/a" {
		t.Fatalf("spans = %+v, want the URL cut before the non-ASCII rune", spans)
	}
}

func TestScanLinksMarkdownLabel(t *testing.T) {
	line := "read [the docs](https://example.com/docs) now"
	spans := scanLinks([]rune(line))
	// The label span plus the bare-URL span over the destination itself.
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want label + destination", spans)
	}
	label := spans[0]
	if got := string([]rune(line)[label.start:label.end]); got != "the docs" {
		t.Fatalf("label span covers %q, want %q", got, "the docs")
	}
	if label.url != "https://example.com/docs" {
		t.Fatalf("label url = %q", label.url)
	}
}

func TestScanLinksMarkdownTitleAndAngles(t *testing.T) {
	spans := spanOf(t, `[l](https://example.com "the title")`)
	if len(spans) == 0 || spans[0].url != "https://example.com" {
		t.Fatalf("title: spans = %+v", spans)
	}
	spans = spanOf(t, "[l](<https://example.com/x>)")
	if len(spans) == 0 || spans[0].url != "https://example.com/x" {
		t.Fatalf("angle wrapper: spans = %+v", spans)
	}
}

func TestScanLinksMarkdownRelativeTargetSkipped(t *testing.T) {
	spans := spanOf(t, "see [the wiki](./wiki/index.md) too")
	if len(spans) != 0 {
		t.Fatalf("relative target: spans = %+v, want none (OSC 8 needs a URI)", spans)
	}
}

func TestScanLinksMarkdownMailto(t *testing.T) {
	spans := spanOf(t, "[mail me](mailto:a@b.c)")
	if len(spans) != 1 || spans[0].url != "mailto:a@b.c" {
		t.Fatalf("spans = %+v, want the mailto label link", spans)
	}
}

// --- render (#1655) --------------------------------------------------------

// linkPairs counts OSC 8 opens (non-empty URI) and closes (empty URI) in a
// rendered row.
func linkPairs(row string) (opens, closes int) {
	for _, part := range strings.Split(row, "\x1b]8;")[1:] {
		// part = "params;uri BEL ..."; an empty URI after the second ';'
		// is a close.
		rest := part[strings.IndexByte(part, ';')+1:]
		if strings.HasPrefix(rest, "\x07") {
			closes++
		} else {
			opens++
		}
	}
	return opens, closes
}

// TestURLRendersAsHyperlink: every cell of the URL carries a self-contained
// OSC 8 pair; the stripped row is byte-identical to the plain text and one
// rune still occupies one display cell (#1469's invariant).
func TestURLRendersAsHyperlink(t *testing.T) {
	m, _ := loaded(t, "see https://example.com/x ok\n")
	row := strings.SplitN(m.View(), "\n", 2)[0]
	opens, closes := linkPairs(row)
	url := "https://example.com/x"
	if opens != len(url) || closes != len(url) {
		t.Fatalf("opens=%d closes=%d, want %d self-contained pairs", opens, closes, len(url))
	}
	if !strings.Contains(row, "\x1b]8;id=ike-0-4;"+url+"\x07") {
		t.Fatalf("row %q missing the OSC 8 open for %q", row, url)
	}
	got := strings.TrimRight(ansi.Strip(row), " ")
	if got != "see https://example.com/x ok" {
		t.Fatalf("stripped row = %q, sequences must be zero-width", got)
	}
	if w := ansi.StringWidth(row); w != len([]rune(got)) {
		t.Fatalf("display width = %d, want %d", w, len([]rune(got)))
	}
}

// TestHyperlinkSurvivesHorizontalClip: a span starting inside the URL still
// emits balanced pairs — every visible URL cell opens and closes its own.
func TestHyperlinkSurvivesHorizontalClip(t *testing.T) {
	m, _ := loaded(t, "see https://example.com/long/path ok\n")
	m.view.Left = 10 // clip into the middle of the URL
	row := strings.SplitN(m.View(), "\n", 2)[0]
	opens, closes := linkPairs(row)
	if opens == 0 || opens != closes {
		t.Fatalf("opens=%d closes=%d, want balanced non-zero pairs", opens, closes)
	}
	if !strings.Contains(row, "https://example.com/long/path\x07") {
		t.Fatalf("row %q: clipped cells must still target the full URL", row)
	}
}

// TestHyperlinkDisabled: the config switch removes every sequence.
func TestHyperlinkDisabled(t *testing.T) {
	m, _ := loaded(t, "see https://example.com/x ok\n")
	m.hyperlinks = false
	m.bumpRender()
	if row := strings.SplitN(m.View(), "\n", 2)[0]; strings.Contains(row, "\x1b]8;") {
		t.Fatalf("row %q carries OSC 8 with editor.hyperlinks off", row)
	}
}

// TestMarkdownLabelCarriesTarget: the rendered label cells link to the
// destination, so the label stays clickable while the target is concealed.
func TestMarkdownLabelCarriesTarget(t *testing.T) {
	m, _ := loaded(t, "read [the docs](https://example.com/docs) now\n")
	row := strings.SplitN(m.View(), "\n", 2)[0]
	if !strings.Contains(row, "https://example.com/docs\x07t") {
		t.Fatalf("row %q: label cell 't' must open with the link target", row)
	}
	if o, c := linkPairs(row); o != c {
		t.Fatalf("opens=%d closes=%d, want balanced", o, c)
	}
}

// TestClickAfterURL: the sequences are zero-width, so clicking a character
// right of the URL still lands the caret on it.
func TestClickAfterURL(t *testing.T) {
	m, _ := loaded(t, "see https://example.com/x ok\n")
	col := strings.LastIndex(m.buf.Line(0), "ok")
	gutter := m.view.GutterWidth(m.buf.LineCount())
	m.MouseClick(gutter+col, 0)
	if m.cursor != (buffer.Position{Line: 0, Col: col}) {
		t.Fatalf("cursor = %v, want {0 %d}", m.cursor, col)
	}
}

// TestHyperlinkOnCursorLine: the cursor cell sitting inside the URL keeps the
// pairs balanced and the characters intact.
func TestHyperlinkOnCursorLine(t *testing.T) {
	m, _ := loaded(t, "see https://example.com/x ok\n")
	m.cursor = buffer.Position{Line: 0, Col: 10} // inside the URL
	row := strings.SplitN(m.View(), "\n", 2)[0]
	if o, c := linkPairs(row); o != c || o == 0 {
		t.Fatalf("opens=%d closes=%d, want balanced non-zero pairs", o, c)
	}
	got := strings.TrimRight(ansi.Strip(row), " ")
	if got != "see https://example.com/x ok" {
		t.Fatalf("stripped row = %q", got)
	}
}
