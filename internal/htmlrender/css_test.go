package htmlrender

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"
)

// TestCSSStyleGolden pins the styling the CSS subset produces: every
// testdata/css_*.html fixture rendered at width 80 with its escape
// sequences made visible (\e), against testdata/golden/<name>.sgr.txt. The
// plain-text goldens (TestGolden) cover what is shown; these cover how.
func TestCSSStyleGolden(t *testing.T) {
	fixtures, _ := filepath.Glob("testdata/css_*.html")
	if len(fixtures) == 0 {
		t.Fatal("no css fixtures")
	}
	for _, path := range fixtures {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".html")
		t.Run(name, func(t *testing.T) {
			doc := Render(src, Options{Width: 80})
			got := strings.ReplaceAll(strings.Join(doc.Lines, "\n"), "\x1b", `\e`) + "\n"
			golden := filepath.Join("testdata", "golden", name+".sgr.txt")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden (run with -update): %v", err)
			}
			if got != string(want) {
				t.Errorf("styled render mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

// styleOf returns the SGR sequence directly in front of word in line, "" for
// an unstyled word.
func styleOf(t *testing.T, doc Document, word string) string {
	t.Helper()
	for _, l := range doc.Lines {
		i := strings.Index(l, word)
		if i < 0 {
			continue
		}
		j := strings.LastIndex(l[:i], "\x1b[")
		if j < 0 || strings.Contains(l[j:i], "\x1b[0m") || strings.HasPrefix(l[j:], "\x1b[0m") {
			return ""
		}
		return l[j:i]
	}
	t.Fatalf("%q not rendered:\n%s", word, strings.Join(plainLines(doc), "\n"))
	return ""
}

// TestCSSEmphasisAndColour pins the attribute and colour mapping.
func TestCSSEmphasisAndColour(t *testing.T) {
	src, _ := os.ReadFile("testdata/css_style.html")
	doc := Render(src, Options{Width: 80})
	cases := []struct{ word, want string }{
		{"bold", "\x1b[1m"},
		{"w700", "\x1b[1m"},
		{"b-normal", ""},
		{"strong-normal", ""},
		{"italic", "\x1b[3m"},
		{"underline", "\x1b[4m"},
		{"strike", "\x1b[9m"},
		{"both", "\x1b[4;9m"},
		{"red", "\x1b[38;2;255;0;0m"},
		{"hex", "\x1b[38;2;0;170;136m"},
		{"hex6", "\x1b[38;2;51;102;153m"},
		{"rgb", "\x1b[38;2;255;128;0m"},
		{"pct", "\x1b[38;2;255;0;128m"},
		{"clear", ""},
		{"bad", ""},
		{"inline", "\x1b[1;38;2;255;0;0m"},
	}
	for _, c := range cases {
		if got := styleOf(t, doc, c.word); got != c.want {
			t.Errorf("%s: style %q, want %q", c.word, got, c.want)
		}
	}
	// text-decoration:none drops the link underline; color:inherit keeps the
	// link colour (inherit is not a colour the subset reads).
	link, plain := styleOf(t, doc, "link"), styleOf(t, doc, "plain")
	if !strings.Contains(link, "4;") || strings.Contains(plain, "4;") || !strings.Contains(plain, "38;2;") {
		t.Errorf("link style %q, plain link style %q", link, plain)
	}
}

// TestCSSTextAlign pins centring and right alignment against the width.
func TestCSSTextAlign(t *testing.T) {
	src, _ := os.ReadFile("testdata/css_style.html")
	const w = 40
	lines := plainLines(Render(src, Options{Width: w}))
	find := func(s string) string {
		for _, l := range lines {
			if strings.Contains(l, s) {
				return l
			}
		}
		t.Fatalf("%q missing:\n%s", s, strings.Join(lines, "\n"))
		return ""
	}
	lead := func(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }
	if l := find("centered text"); lead(l) != (w-len("centered text"))/2 {
		t.Errorf("centred line %q", l)
	}
	if l := find("right text"); len(l) != w {
		t.Errorf("right-aligned line %q does not end at the width", l)
	}
	if l := find("inherited centre"); lead(l) != (w-len("inherited centre"))/2 {
		t.Errorf("inherited centre %q", l)
	}
	if l := find("left again"); lead(l) != 0 {
		t.Errorf("text-align:left does not reset: %q", l)
	}
	if l := find("inline span"); lead(l) == 0 || len(l) == w {
		t.Errorf("inline span line %q: want the div's centring, not its own right alignment", l)
	}
	if l := find("right item"); ansi.StringWidth(l) != w || !strings.HasPrefix(l, "•") {
		t.Errorf("right-aligned list item %q", l)
	}
}

// TestCSSAlignedLinkSpans checks that alignment padding moves the link
// spans' cells with the text.
func TestCSSAlignedLinkSpans(t *testing.T) {
	doc := Render([]byte(`<p style="text-align:right"><a href="a.html">go</a></p>`), Options{Width: 20})
	if len(doc.LinkSpans) != 1 {
		t.Fatalf("spans %+v", doc.LinkSpans)
	}
	s := doc.LinkSpans[0]
	if s.Col != 18 || s.EndCol != 20 || ansi.Strip(doc.Lines[s.Line][s.Start:s.End]) != "go" {
		t.Errorf("span %+v on %q", s, doc.Lines[s.Line])
	}
}

// TestCSSNoStyleNoCascade pins the fast path: a document without CSS builds
// no cascade.
func TestCSSNoStyleNoCascade(t *testing.T) {
	if c := newCascade(parse([]byte(`<p class="x">plain</p>`))); c != nil {
		t.Errorf("cascade %v for a document without CSS", c)
	}
}

func TestParseSheet(t *testing.T) {
	for _, tc := range []struct {
		sheet string
		rules int
		ok    bool
	}{
		{"", 0, true},
		{"p{color:red} a , b { x: y }", 2, true},
		{"<!-- p{color:red} -->", 1, true},
		{"@media screen { p { color: red } } p{}", 1, true},
		{"@import 'x.css'; @charset \"utf-8\"; p{}", 1, true},
		{"p:hover{} p::after{} p{}", 2, true}, // :hover parses (never matching)
		{"p{color:red", 0, false},
		{"p{color:red}}", 0, false},
		{"/* open", 0, false},
		{"p{content:'unterminated}", 0, false},
		{"p", 0, false},
		{"p;", 0, false},
		{"p\\", 0, false}, // a trailing escape (fuzz: css_trailing_escape)
	} {
		rules, ok := parseSheet(tc.sheet)
		if ok != tc.ok || len(rules) != tc.rules {
			t.Errorf("parseSheet(%q) = %d rules, ok %v; want %d, %v", tc.sheet, len(rules), ok, tc.rules, tc.ok)
		}
	}
}

func TestParseDecls(t *testing.T) {
	if d := parseDecls(`a:b\`); len(d) != 1 {
		t.Errorf("trailing escape: %+v", d)
	}
	got := parseDecls(`color: red ; FONT-WEIGHT:Bold!important; ;bogus; content: "a;b"; empty:`)
	want := []decl{
		{prop: "color", val: "red"},
		{prop: "font-weight", val: "Bold", important: true},
		{prop: "content", val: `"a;b"`},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("decl %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint32
		ok   bool
	}{
		{"red", 0xff0000, true},
		{"rebeccapurple", 0x663399, true},
		{"#fff", 0xffffff, true},
		{"#fff8", 0xffffff, true},
		{"#12345678", 0x123456, true},
		{"#12345600", 0, false},
		{"#12", 0, false},
		{"#ggg", 0, false},
		{"rgb(1,2,3)", 0x010203, true},
		{"rgba(1, 2, 3, 0.5)", 0x010203, true},
		{"rgb(0 0 0 / 0%)", 0, false},
		{"rgb(300, -5, 50%)", 0xff0080, true},
		{"rgb(1,2)", 0, false},
		{"rgb(a,b,c)", 0, false},
		{"transparent", 0, false},
		{"currentcolor", 0, false},
		{"hsl(0, 100%, 50%)", 0, false},
	} {
		got, ok := parseColor(tc.in)
		if ok != tc.ok || (ok && got != 1<<24|tc.want) {
			t.Errorf("parseColor(%q) = %#x, %v; want %#x, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestCSSImportantOrder pins the !important layer: an important stylesheet
// declaration beats a normal inline one, an important inline one beats it.
func TestCSSImportantOrder(t *testing.T) {
	src := `<style>.a{display:none !important} .b{display:none !important}</style>
<p class="a" style="display:block">one</p><p class="b" style="display:block !important">two</p>`
	got := strings.Join(plainLines(Render([]byte(src), Options{Width: 40})), "\n")
	if strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Errorf("render:\n%s", got)
	}
}

// styledDocument is largeDocument (about n bytes) behind a stylesheet of
// rules rules — class, descendant and id selectors, the shape of a real
// site's CSS.
func styledDocument(n, rules int) []byte {
	var b strings.Builder
	b.WriteString("<style>")
	for i := 0; i < rules; i++ {
		fmt.Fprintf(&b, ".c%d{color:#%06x} section .c%d p{font-weight:bold} #s%d li{display:block}\n", i, i*997%0xffffff, i, i)
	}
	b.WriteString("</style>")
	b.Write(largeDocument(n))
	return []byte(b.String())
}

// TestLargeStylesheet guards the cascade against pathological cost: a big
// stylesheet over a big document renders in well under a second.
func TestLargeStylesheet(t *testing.T) {
	src := styledDocument(256<<10, 1000)
	start := time.Now()
	Render(src, Options{Width: 80})
	elapsed := time.Since(start)
	t.Logf("rendered %d bytes with 3000 rules in %v", len(src), elapsed)
	if elapsed > 5*time.Second {
		t.Fatalf("styled render took %v", elapsed)
	}
}

func BenchmarkRenderStyled(b *testing.B) {
	src := styledDocument(256<<10, 1000)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Render(src, Options{Width: 80})
	}
}

// TestSelectorIndexMatchesQueryAll checks the index's shortcuts against
// plain cascadia matching: every selector matches the same elements both
// ways, whatever combinators, escapes and pseudo-classes it uses.
func TestSelectorIndexMatchesQueryAll(t *testing.T) {
	doc := `<div id="main" class="a b"><section class="b"><p class="x y">one <span class="a:b">two</span></p>
<ul><li id="i1">x</li><li class="x">y</li></ul></section><p id="p2">three</p><em>e</em><P CLASS="Up">up</P></div>
<table><tr><td class="c">1</td><td>2</td></tr></table>`
	sels := []string{
		"p", "P", "*", ".a", ".b .x", "#main p", "div > section p", "section > p", "ul li",
		"#main li.x", "li + li", "p ~ em", "section + p", "div p ~ em", "#main .b > .x span",
		`.a\:b`, `#i1`, "li:not(.x)", ":not(p) > li", "[class~=x]", "div [id]", "td.c",
		"tr td:first-child", "p:has(span)", ".b ul > li:nth-child(2)", ".Up", "p.Up",
		"body p", "html li", "em, .x", "#nope li", ".zz", `[title="a b"] p`, ".x.y", "#main.a",
	}
	tr := parse([]byte(doc))
	for _, src := range sels {
		g, err := cascadia.ParseGroup(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		for _, sel := range g {
			var idx selIndex
			idx.add(indexedSel{sel: sel})
			got := map[*html.Node]bool{}
			idx.match(tr.root, func(n *html.Node, _ *indexedSel) { got[n] = true })
			want := cascadia.QueryAll(tr.root, sel)
			if len(got) != len(want) {
				t.Errorf("%s: index matched %d elements, cascadia %d", sel, len(got), len(want))
				continue
			}
			for _, n := range want {
				if !got[n] {
					t.Errorf("%s: index missed a <%s>", sel, n.Data)
				}
			}
		}
	}
}

func TestSelectorKeys(t *testing.T) {
	for _, tc := range []struct {
		sel, subject string
		anc          []string
	}{
		{"p", "p", nil},
		{"*", "", nil},
		{".a.b", ".a", nil},
		{"p.a#x", "#x", nil},
		{"#main  .b  >  p", "p", []string{".b", "#main"}},
		{"a + b c", "c", []string{"b"}},
		{"a ~ b > c", "c", []string{"b"}},
		{`.a\:b`, "", nil},
		{`#x\:y.c`, ".c", nil},
		{"li:not(.x)", "li", nil},
		{"[class~=x] p", "p", nil},
		{`[title="a b"] p`, "p", nil},
		{":has(> a) b", "b", nil},
	} {
		subject, anc := selectorKeys(tc.sel)
		if subject != tc.subject || strings.Join(anc, ",") != strings.Join(tc.anc, ",") {
			t.Errorf("selectorKeys(%q) = %q %v, want %q %v", tc.sel, subject, anc, tc.subject, tc.anc)
		}
	}
}
