package htmlrender

import (
	"strings"
	"testing"
)

// imageblock_test.go guards Options.ImageBlock (0530/5, #2743): the hook the
// HTML preview uses to stand Kitty placeholder rows in for an <img>.

// blockFor returns an ImageBlock hook drawing every image whose src is in
// srcs as rows lines of "#", and recording the widths it was offered.
func blockFor(rows, cols int, offered *[]int, srcs ...string) func(Image, int) ([]string, int) {
	return func(img Image, maxCols int) ([]string, int) {
		*offered = append(*offered, maxCols)
		for _, s := range srcs {
			if img.Src == s {
				c := min(cols, maxCols)
				out := make([]string, rows)
				for i := range out {
					out[i] = strings.Repeat("#", c)
				}
				return out, c
			}
		}
		return nil, 0
	}
}

// TestImageBlockReplacesPlaceholder: a block stands on its own lines at the
// image's place, the text around it keeps flowing on lines of its own, the
// index records where the block starts and how tall it is, and the source
// map points every block line at the <img>.
func TestImageBlockReplacesPlaceholder(t *testing.T) {
	src := "<p>before <img src=\"a.png\" alt=\"A\"> after</p>\n<p id=\"next\">next</p>"
	var offered []int
	doc := Render([]byte(src), Options{Width: 30, ImageBlock: blockFor(3, 8, &offered, "a.png")})
	lines := plainLines(doc)
	want := []string{"before", "########", "########", "########", "after", "", "next"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if len(offered) != 1 || offered[0] != 30 {
		t.Fatalf("offered widths = %v, want [30]", offered)
	}
	if img := doc.Images[0]; img.Line != 1 || img.Rows != 3 || img.Src != "a.png" {
		t.Fatalf("image index = %+v, want line 1, 3 rows", img)
	}
	if doc.Anchors["next"] != 6 {
		t.Fatalf("anchor after the block = %d, want 6", doc.Anchors["next"])
	}
	for row := 1; row <= 3; row++ {
		if l, ok := doc.SourceLine(row); !ok || l != 0 {
			t.Fatalf("block line %d maps to source line %d (%v), want 0", row, l, ok)
		}
	}
	if l, ok := doc.LineForSourceLine(1); !ok || l != 6 {
		t.Fatalf("source line 1 renders at %d (%v), want 6", l, ok)
	}
}

// TestImageBlockNilKeepsPlaceholder: an image the hook declines renders as
// its bracketed alt text, and a block is never torn into a table row.
func TestImageBlockNilKeepsPlaceholder(t *testing.T) {
	src := `<p><img src="http://x/a.png" alt="remote"></p>
<table><tr><td><img src="a.png" alt="cell"></td><td>b</td></tr></table>`
	var offered []int
	doc := Render([]byte(src), Options{Width: 40, ImageBlock: blockFor(2, 4, &offered, "a.png")})
	text := strings.Join(plainLines(doc), "\n")
	if !strings.Contains(text, "[remote]") || !strings.Contains(text, "[cell] │ b") {
		t.Fatalf("declined and in-table images must keep [alt]:\n%s", text)
	}
	if strings.Contains(text, "#") {
		t.Fatalf("no block may be drawn:\n%s", text)
	}
	for _, img := range doc.Images {
		if img.Rows != 0 {
			t.Fatalf("placeholder image carries rows: %+v", img)
		}
	}
}

// TestImageBlockIndentAndLink: inside an indent the hook is offered the
// width left beside it, the block lines carry the indent, and a linked image
// keeps its link on every block line.
func TestImageBlockIndentAndLink(t *testing.T) {
	src := `<blockquote><a href="big.png"><img src="a.png"></a></blockquote>`
	var offered []int
	doc := Render([]byte(src), Options{Width: 20, ImageBlock: blockFor(2, 50, &offered, "a.png")})
	if len(offered) != 1 || offered[0] >= 20 {
		t.Fatalf("offered widths = %v, want less than the full 20", offered)
	}
	lines := plainLines(doc)
	for _, l := range lines {
		if strings.Contains(l, "#") && !strings.HasPrefix(l, "│") {
			t.Fatalf("block line lost the blockquote bar: %q", l)
		}
	}
	if len(doc.Links) != 1 || doc.Links[0].FirstLine != 0 || doc.Links[0].LastLine != 1 {
		t.Fatalf("linked block = %+v, want lines 0-1", doc.Links)
	}
}

// TestPictureAndSVG: <picture> shows its <img> fallback (the <source>s are
// skipped), an inline <svg> shows "[svg]" and none of its markup text.
func TestPictureAndSVG(t *testing.T) {
	src := `<picture><source srcset="a.webp"><img src="a.png" alt="fallback"></picture>
<p>logo <svg><title>secret</title><text>inner</text></svg> end</p>`
	doc := Render([]byte(src), Options{Width: 40})
	text := strings.Join(plainLines(doc), "\n")
	if !strings.Contains(text, "[fallback]") || !strings.Contains(text, "logo [svg] end") {
		t.Fatalf("picture/svg placeholders missing:\n%s", text)
	}
	if strings.Contains(text, "inner") || strings.Contains(text, "secret") {
		t.Fatalf("svg markup text leaked:\n%s", text)
	}
}
