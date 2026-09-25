package htmlrender

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

func plainLines(doc Document) []string {
	out := make([]string, len(doc.Lines))
	for i, l := range doc.Lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

// TestLinkIndex pins labels, hrefs and line ranges, including a label that
// wraps across lines, and that every line a link occupies carries its OSC 8
// sequence (opened and closed on the same line, like glamour's output the
// markdown preview scans).
func TestLinkIndex(t *testing.T) {
	src := `<p>See <a href="https://example.com/a">first link</a> and then
<a href="guide.html#install">a label that is long enough to wrap</a>.</p>
<p><a name="anchor-only">not a link</a> <a href="#top"><b>bold</b> <i>mixed</i></a></p>
<p><a href="empty.html"></a>empty links are dropped</p>`
	doc := Render([]byte(src), Options{Width: 24})
	lines := plainLines(doc)
	want := []Link{
		{Label: "first link", Href: "https://example.com/a", FirstLine: 0, LastLine: 0},
		{Label: "a label that is long enough to wrap", Href: "guide.html#install", FirstLine: 1, LastLine: 2},
		{Label: "bold mixed", Href: "#top", FirstLine: 4, LastLine: 4},
	}
	if len(doc.Links) != len(want) {
		t.Fatalf("links = %+v, want %+v\n%s", doc.Links, want, strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if doc.Links[i] != w {
			t.Errorf("link %d = %+v, want %+v\n%s", i, doc.Links[i], w, strings.Join(lines, "\n"))
		}
	}
	for i, l := range doc.Links {
		for row := l.FirstLine; row <= l.LastLine; row++ {
			// Link ids count every <a href> in order; the dropped empty
			// link comes last, so the indexed ones keep their position.
			open := ansi.SetHyperlink(l.Href, fmt.Sprintf("id=ike-%d", i))
			line := doc.Lines[row]
			o := strings.Index(line, open)
			c := strings.LastIndex(line, ansi.ResetHyperlink())
			if o < 0 || c < o {
				t.Errorf("line %d lacks the OSC 8 span for %q: %q", row, l.Href, line)
			}
		}
	}
	if !strings.Contains(lines[0], "See first link and then") {
		t.Errorf("line 0 = %q", lines[0])
	}
	if _, ok := doc.Anchors["anchor-only"]; !ok {
		t.Errorf("named anchor missing: %v", doc.Anchors)
	}
}

// TestLinkSpans pins the per-line label pieces (#2741): every span's bytes
// are exactly its label text and its cells are where that text is drawn; a
// wrapped label has one span per line, and spans name the Document.Links
// index even when an earlier <a href> was dropped.
func TestLinkSpans(t *testing.T) {
	src := `<p><a href="gone.html"></a>See <a href="a.html">first link</a> and then
<a href="b.html">a label that is long enough to wrap</a>.</p>
<ul><li><a href="c.html">item</a></li></ul>`
	doc := Render([]byte(src), Options{Width: 24})
	lines := plainLines(doc)
	pieces := map[int][]string{}
	for _, s := range doc.LinkSpans {
		label := ansi.Strip(doc.Lines[s.Line][s.Start:s.End])
		pieces[s.Link] = append(pieces[s.Link], label)
		if got := ansi.Cut(doc.Lines[s.Line], s.Col, s.EndCol); ansi.Strip(got) != label {
			t.Errorf("span %+v covers cells %q, want %q", s, ansi.Strip(got), label)
		}
		if s.Line < doc.Links[s.Link].FirstLine || s.Line > doc.Links[s.Link].LastLine {
			t.Errorf("span %+v outside its link's lines %+v", s, doc.Links[s.Link])
		}
	}
	want := map[int]string{0: "first link", 1: "a label that is long enough to wrap", 2: "item"}
	for i, w := range want {
		if got := strings.Join(pieces[i], " "); got != w {
			t.Errorf("link %d pieces %q, want %q\n%s", i, pieces[i], w, strings.Join(lines, "\n"))
		}
	}
	if len(pieces[1]) < 2 {
		t.Errorf("the wrapped label should have a span per line, got %q", pieces[1])
	}
}

// sourceMapFixture has one construct per source line so the expected
// mapping is readable off the line numbers.
const sourceMapFixture = `<h1>Title</h1>
<p>First paragraph
continues here</p>
<ul>
  <li>One</li>
  <li>Two</li>
</ul>
<pre>a
b</pre>
<p>Last</p>`

func TestSourceMap(t *testing.T) {
	doc := Render([]byte(sourceMapFixture), Options{Width: 80})
	lines := plainLines(doc)
	wantLines := []string{" Title", "", "First paragraph continues here", "", "• One", "• Two", "", "  a", "  b", "", "Last"}
	if strings.Join(lines, "\n") != strings.Join(wantLines, "\n") {
		t.Fatalf("rendered:\n%s", strings.Join(lines, "\n"))
	}
	// rendered → source; blank separators take the line above them.
	for rendered, src := range map[int]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 4, 5: 5, 6: 5, 7: 7, 8: 8, 10: 9} {
		if got, ok := doc.SourceLine(rendered); !ok || got != src {
			t.Errorf("SourceLine(%d) = %d,%v, want %d", rendered, got, ok, src)
		}
	}
	// source → rendered; markup-only lines map to the content before them.
	for src, rendered := range map[int]int{0: 0, 1: 2, 2: 2, 3: 2, 4: 4, 5: 5, 6: 5, 7: 7, 8: 8, 9: 10, 99: 10} {
		if got, ok := doc.LineForSourceLine(src); !ok || got != rendered {
			t.Errorf("LineForSourceLine(%d) = %d,%v, want %d", src, got, ok, rendered)
		}
	}
	// A byte offset inside a word maps to that word's line.
	off := strings.Index(sourceMapFixture, "Two") + 1
	if got, _ := doc.LineForOffset(off); got != 5 {
		t.Errorf("LineForOffset(inside Two) = %d, want 5", got)
	}
	if got, _ := doc.LineForOffset(0); got != 0 {
		t.Errorf("LineForOffset(0) = %d, want 0", got)
	}
	if got, ok := doc.SourceOffset(4); !ok || got != strings.Index(sourceMapFixture, "One") {
		t.Errorf("SourceOffset(4) = %d, want offset of One", got)
	}
}

// TestSourceMapWrapped checks the line accuracy the pane's cursor sync needs:
// a wrapped paragraph maps each source line to the rendered line its words
// landed on, and back.
func TestSourceMapWrapped(t *testing.T) {
	doc := Render([]byte(sourceMapFixture), Options{Width: 20})
	lines := plainLines(doc)
	if lines[2] != "First paragraph" || lines[3] != "continues here" {
		t.Fatalf("expected the paragraph to wrap after its first source line:\n%s", strings.Join(lines, "\n"))
	}
	if got, _ := doc.LineForSourceLine(2); got != 3 {
		t.Errorf("LineForSourceLine(2) = %d, want 3", got)
	}
	if got, _ := doc.SourceLine(3); got != 2 {
		t.Errorf("SourceLine(3) = %d, want 2", got)
	}
}

func TestSourceMapEmpty(t *testing.T) {
	doc := Render([]byte("<script>x</script>"), Options{Width: 40})
	if len(doc.Lines) != 0 {
		t.Fatalf("lines = %q", doc.Lines)
	}
	if _, ok := doc.SourceOffset(0); ok {
		t.Error("SourceOffset on an empty document should report !ok")
	}
	if _, ok := doc.LineForSourceLine(0); ok {
		t.Error("LineForSourceLine on an empty document should report !ok")
	}
}

func TestImageIndex(t *testing.T) {
	src := `<p>one</p><p>An <img src="a/b.png" alt="the  alt"> inline.</p><p><img src="c.jpg?x=1"></p>`
	doc := Render([]byte(src), Options{Width: 40})
	want := []Image{{Src: "a/b.png", Alt: "the alt", Line: 2}, {Src: "c.jpg?x=1", Alt: "", Line: 4}}
	if len(doc.Images) != len(want) {
		t.Fatalf("images = %+v", doc.Images)
	}
	for i, w := range want {
		if doc.Images[i] != w {
			t.Errorf("image %d = %+v, want %+v", i, doc.Images[i], w)
		}
	}
	if got := plainLines(doc)[4]; got != "[image: c.jpg]" {
		t.Errorf("alt-less placeholder = %q", got)
	}
}

func TestAnchors(t *testing.T) {
	src := `<h2 id="intro">Intro</h2><p>text</p><div id="empty"></div><p>after <span id="mid">mid</span></p><a name="n"></a>`
	doc := Render([]byte(src), Options{Width: 40})
	for id, line := range map[string]int{"intro": 0, "empty": 4, "mid": 4, "n": 4} {
		if got, ok := doc.Anchors[id]; !ok || got != line {
			t.Errorf("anchor %q = %d,%v, want %d", id, got, ok, line)
		}
	}
}

// TestStyling checks the palette reaches the output: headings in the accent,
// inline emphasis as SGR attributes, code on the surface colour.
func TestStyling(t *testing.T) {
	pal := theme.DefaultPalette()
	doc := Render([]byte(`<h2>Head</h2><p><b>b</b> <i>i</i> <u>u</u> <s>s</s> <code>c</code> <mark>m</mark></p>`), Options{Width: 40, Palette: pal})
	head := style{fg: rgb(pal.Accent), attrs: attrBold}.sgr()
	if !strings.Contains(doc.Lines[0], head+"## Head") {
		t.Errorf("heading not accent-bold: %q", doc.Lines[0])
	}
	body := doc.Lines[2]
	for _, want := range []string{
		"\x1b[1mb", "\x1b[3mi", "\x1b[4mu", "\x1b[9ms",
		style{fg: rgb(pal.Warning), bg: rgb(pal.Surface)}.sgr() + "c",
		style{fg: rgb(pal.Background), bg: rgb(pal.Warning)}.sgr() + "m",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %q", want, body)
		}
	}
}

// TestNoEscapeInjection: document text and attributes can never put a raw
// escape sequence on the terminal — only the renderer's own SGR and OSC 8.
func TestNoEscapeInjection(t *testing.T) {
	src := "<p>a\x1b[31mred\x1b]8;;http://evil\x07x &#27;[2J \u009b</p><a href=\"x\x1b]0;t\x07\">l</a><img alt=\"\x1b[5m\">"
	doc := Render([]byte(src), Options{Width: 80})
	for _, l := range doc.Lines {
		rest := l
		for _, ok := range []string{ansi.ResetHyperlink(), sgrReset} {
			rest = strings.ReplaceAll(rest, ok, "")
		}
		for _, lk := range doc.Links {
			rest = strings.ReplaceAll(rest, ansi.SetHyperlink(lk.Href, "id=ike-0"), "")
		}
		for i := 0; i < len(rest); i++ {
			if rest[i] == 0x1b && (i+1 >= len(rest) || rest[i+1] != '[') {
				t.Fatalf("foreign escape in %q", l)
			}
		}
		if strings.Contains(ansi.Strip(l), "\x1b") || strings.ContainsRune(l, 0x9b) {
			t.Fatalf("control character survived in %q", l)
		}
	}
	if strings.ContainsAny(doc.Links[0].Href, "\x1b\x07") {
		t.Fatalf("href not sanitised: %q", doc.Links[0].Href)
	}
}

func TestTitleAndDefaults(t *testing.T) {
	doc := Render([]byte("<title>\n A &amp; B </title><p>x</p>"), Options{})
	if doc.Title != "A & B" {
		t.Errorf("title = %q", doc.Title)
	}
	long := strings.Repeat("word ", 40)
	doc = Render([]byte("<p>"+long+"</p>"), Options{})
	for _, l := range doc.Lines {
		if ansi.StringWidth(l) > DefaultWidth {
			t.Fatalf("default width exceeded: %q", l)
		}
	}
}

// largeDocument builds roughly n bytes of mixed markup: every element family
// the renderer handles, repeated.
func largeDocument(n int) []byte {
	section := `<section id="s%d"><h2>Section %d</h2>
<p>Paragraph with <b>bold</b>, <i>italic</i>, <code>code</code> and a <a href="https://example.com/%d">link to somewhere</a> that runs long enough to wrap a few times at typical pane widths &amp; entities.</p>
<ul><li>One<ul><li>Nested <em>item</em></li></ul></li><li>Two</li></ul>
<blockquote><p>Quoted text in a blockquote.</p></blockquote>
<pre>func f() {
	return %d
}</pre>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>
<p><img src="img%d.png" alt="image"></p>
</section>
`
	var b strings.Builder
	b.WriteString("<html><head><title>big</title></head><body>")
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, section, i, i, i, i, i)
	}
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// TestLargeDocument renders a 1 MB document and guards against pathological
// (quadratic) behaviour; the benchmark below measures the real cost.
func TestLargeDocument(t *testing.T) {
	src := largeDocument(1 << 20)
	start := time.Now()
	doc := Render(src, Options{Width: 80})
	elapsed := time.Since(start)
	t.Logf("rendered %d bytes into %d lines in %v", len(src), len(doc.Lines), elapsed)
	if elapsed > 10*time.Second {
		t.Fatalf("1 MB render took %v", elapsed)
	}
	if len(doc.Links) == 0 || len(doc.Images) == 0 {
		t.Fatal("indexes empty")
	}
}

func BenchmarkRender1MB(b *testing.B) {
	src := largeDocument(1 << 20)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Render(src, Options{Width: 80})
	}
}
