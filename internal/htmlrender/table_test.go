package htmlrender

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"

	"ike/internal/theme"
)

// TestTableLinksInCells pins the link index inside a grid: every link in a
// cell is indexed with the rendered lines its label landed on (a label that
// wraps inside its narrow column spans two lines), and its spans sit on
// those lines at the label's columns.
func TestTableLinksInCells(t *testing.T) {
	src := `<table>
<tr><th>Name</th><th>Doc</th></tr>
<tr><td>alpha</td><td><a href="a.html">first</a></td></tr>
<tr><td>beta gamma delta</td><td>see <a href="b.html">the long second label</a> here</td></tr>
</table>`
	doc := Render([]byte(src), Options{Width: 30})
	lines := plainLines(doc)
	if len(doc.Links) != 2 {
		t.Fatalf("links = %+v\n%s", doc.Links, strings.Join(lines, "\n"))
	}
	for _, l := range doc.Links {
		for line := l.FirstLine; line <= l.LastLine; line++ {
			if !strings.HasPrefix(lines[line], "│") {
				t.Errorf("link %q line %d is not a grid row: %q", l.Href, line, lines[line])
			}
		}
	}
	first, second := doc.Links[0], doc.Links[1]
	if first.Label != "first" || first.FirstLine != first.LastLine || !strings.Contains(lines[first.FirstLine], "first") {
		t.Errorf("first link = %+v, line %q", first, lines[first.FirstLine])
	}
	if second.Label != "the long second label" || second.LastLine <= second.FirstLine {
		t.Errorf("second link = %+v (want a wrapped label)\n%s", second, strings.Join(lines, "\n"))
	}
	for _, s := range doc.LinkSpans {
		plain := ansi.Strip(doc.Lines[s.Line])
		label := ansi.Strip(doc.Lines[s.Line][s.Start:s.End])
		if label == "" || !strings.Contains(plain, label) {
			t.Errorf("span %+v covers %q in %q", s, label, plain)
		}
		if got := ansi.Cut(doc.Lines[s.Line], s.Col, s.EndCol); ansi.Strip(got) != label {
			t.Errorf("span %+v cells %q, bytes %q", s, ansi.Strip(got), label)
		}
	}
}

// TestTableSourceMap pins the cursor sync through a grid: a source line with
// a row maps to that row, the <table> line to the top border.
func TestTableSourceMap(t *testing.T) {
	src := "<p>before</p>\n<table>\n<tr><td>one</td><td>1</td></tr>\n<tr><td>two</td><td>2</td></tr>\n</table>\n<p>after</p>\n"
	doc := Render([]byte(src), Options{Width: 40})
	lines := plainLines(doc)
	for srcLine, want := range map[int]string{1: "┌", 2: "one", 3: "two", 5: "after"} {
		got, ok := doc.LineForSourceLine(srcLine)
		if !ok || !strings.Contains(lines[got], want) {
			t.Errorf("source line %d -> rendered %d %q, want %q", srcLine, got, lines[got], want)
		}
	}
	for i, l := range lines {
		if strings.Contains(l, "two") {
			if s, _ := doc.SourceLine(i); s != 3 {
				t.Errorf("row 'two' maps back to source line %d, want 3", s)
			}
		}
	}
}

// TestTableAnchors pins ids on and inside a table: the table's id lands on
// its first line, a cell's (and an empty cell's) on its row.
func TestTableAnchors(t *testing.T) {
	src := `<p>x</p><table id="t"><tr><td id="c1">one</td></tr><tr><td><span id="c2"></span></td><td>two</td></tr></table>`
	doc := Render([]byte(src), Options{Width: 40})
	lines := plainLines(doc)
	if l := lines[doc.Anchors["t"]]; !strings.HasPrefix(l, "┌") {
		t.Errorf("table anchor on %q", l)
	}
	if l := lines[doc.Anchors["c1"]]; !strings.Contains(l, "one") {
		t.Errorf("cell anchor on %q", l)
	}
	if l := lines[doc.Anchors["c2"]]; !strings.Contains(l, "two") {
		t.Errorf("empty cell anchor on %q", l)
	}
}

// TestTableStyling pins the palette look: borders in the border colour,
// header cells bold, and the double rule under the header.
func TestTableStyling(t *testing.T) {
	pal := theme.DefaultPalette()
	doc := Render([]byte(`<table><thead><tr><td>Head</td></tr></thead><tr><td>body</td></tr></table>`), Options{Width: 40, Palette: pal})
	border := "\x1b[38;2;" + rgbParams(rgb(pal.Border)) + "m"
	if !strings.HasPrefix(doc.Lines[0], border+"┌") {
		t.Errorf("top border not in the border colour: %q", doc.Lines[0])
	}
	if !strings.Contains(doc.Lines[1], "\x1b[1mHead") {
		t.Errorf("thead cell not bold: %q", doc.Lines[1])
	}
	if strings.Contains(doc.Lines[3], "\x1b[1m") {
		t.Errorf("body cell bold: %q", doc.Lines[3])
	}
	if !strings.Contains(ansi.Strip(doc.Lines[2]), "╞") {
		t.Errorf("no header rule: %q", ansi.Strip(doc.Lines[2]))
	}
}

// TestColumnWidths pins the distribution rules.
func TestColumnWidths(t *testing.T) {
	row := func(cells ...[2]int) []gridRow {
		var r gridRow
		for i, c := range cells {
			r.cells = append(r.cells, gridCell{n: nodeStub, col: i, span: 1, lo: c[0], hi: c[1]})
		}
		return []gridRow{r}
	}
	for _, tc := range []struct {
		name  string
		rows  []gridRow
		avail int
		want  []int
	}{
		{"all fit", row([2]int{3, 10}, [2]int{2, 5}), 20, []int{10, 5}},
		{"needs fit, rest proportional", row([2]int{4, 20}, [2]int{4, 8}), 16, []int{11, 5}},
		{"fair share keeps short words", row([2]int{30, 30}, [2]int{8, 20}, [2]int{2, 2}), 20, []int{10, 8, 2}},
		{"floors when nothing fits", row([2]int{9, 9}, [2]int{9, 9}), 2, []int{3, 3}},
		{"empty column", row([2]int{0, 0}, [2]int{1, 1}), 10, []int{1, 1}},
	} {
		if got := columnWidths(tc.rows, len(tc.want), tc.avail); fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	// A spanning cell's wants spread over its columns.
	rows := []gridRow{{cells: []gridCell{{n: nodeStub, col: 0, span: 2, lo: 5, hi: 23}}}, row([2]int{1, 1}, [2]int{1, 1})[0]}
	if got := columnWidths(rows, 2, 40); got[0]+got[1]+3 != 23 {
		t.Errorf("colspan widths %v do not hold the spanning cell", got)
	}
}

// TestTableClipsLongWords pins the ellipsis: a word wider than its column is
// cut, never broken onto the next line.
func TestTableClipsLongWords(t *testing.T) {
	doc := Render([]byte(`<table><tr><td>`+strings.Repeat("x", 30)+`</td><td>`+strings.Repeat("y", 30)+`</td></tr></table>`), Options{Width: 24})
	lines := plainLines(doc)
	if len(lines) != 3 || !strings.Contains(lines[1], "x…") || !strings.Contains(lines[1], "y…") {
		t.Errorf("long words not clipped:\n%s", strings.Join(lines, "\n"))
	}
}

// nodeStub stands in for a cell element where only measurements matter.
var nodeStub = &html.Node{Type: html.ElementNode, Data: "td"}

// table500 is a 500-row table with a link in every row.
func table500() []byte {
	var b strings.Builder
	b.WriteString("<table><thead><tr><th>#</th><th>Name</th><th>Description</th><th>Link</th></tr></thead><tbody>\n")
	for i := range 500 {
		fmt.Fprintf(&b, "<tr><td>%d</td><td>row %d</td><td>Some description text for row %d that wraps</td><td><a href=\"r%d.html\">open</a></td></tr>\n", i, i, i, i)
	}
	b.WriteString("</tbody></table>")
	return []byte(b.String())
}

// TestLargeTable renders a 500-row table inside the package's budget and
// checks every row's link was indexed.
func TestLargeTable(t *testing.T) {
	src := table500()
	start := time.Now()
	doc := Render(src, Options{Width: 80})
	elapsed := time.Since(start)
	t.Logf("rendered 500 rows into %d lines in %v", len(doc.Lines), elapsed)
	if elapsed > 2*time.Second {
		t.Fatalf("500-row table took %v", elapsed)
	}
	if len(doc.Links) != 500 {
		t.Fatalf("links = %d, want 500", len(doc.Links))
	}
}

func BenchmarkRenderTable500(b *testing.B) {
	src := table500()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Render(src, Options{Width: 80})
	}
}
