package htmlrender

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// update rewrites the golden files instead of comparing against them:
// go test ./internal/htmlrender -run Golden -update
var update = flag.Bool("update", false, "rewrite the golden files")

// goldenWidths are the pane widths every fixture is rendered at: a narrow
// pane that forces wrapping and hard breaks, and a common wide one.
var goldenWidths = []int{40, 80}

// TestGolden renders every testdata/*.html fixture at each golden width and
// compares the printable text (escape sequences stripped — styling and
// hyperlinks have their own tests) against testdata/golden/<name>.w<N>.txt.
func TestGolden(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/*.html")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, path := range fixtures {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".html")
		for _, w := range goldenWidths {
			t.Run(fmt.Sprintf("%s/w%d", name, w), func(t *testing.T) {
				doc := Render(src, Options{Width: w})
				got := plainText(doc)
				golden := filepath.Join("testdata", "golden", fmt.Sprintf("%s.w%d.txt", name, w))
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
					t.Errorf("render mismatch for %s at width %d\n--- got ---\n%s\n--- want ---\n%s", name, w, got, want)
				}
			})
		}
	}
}

// TestLinesFitWidth pins the layout invariant the pane relies on: no rendered
// line is wider than the width it was rendered at, whatever the fixture.
func TestLinesFitWidth(t *testing.T) {
	fixtures, _ := filepath.Glob("testdata/*.html")
	for _, path := range fixtures {
		src, _ := os.ReadFile(path)
		for w := minWidth; w <= 100; w += 7 {
			doc := Render(src, Options{Width: w})
			for i, l := range doc.Lines {
				if lw := ansi.StringWidth(l); lw > w {
					t.Errorf("%s at width %d: line %d is %d cells: %q", path, w, i, lw, ansi.Strip(l))
				}
			}
		}
	}
}

// FuzzRender feeds arbitrary bytes through the renderer: it must never panic
// and never exceed the width, whatever the markup. The fixtures seed it.
func FuzzRender(f *testing.F) {
	fixtures, _ := filepath.Glob("testdata/*.html")
	for _, path := range fixtures {
		src, _ := os.ReadFile(path)
		f.Add(src, 30)
	}
	f.Add([]byte("<pre>\t\x00<a href=x>\xff\xfe</pre><ol start=-99999999999><li>"), 5)
	f.Fuzz(func(t *testing.T, src []byte, w int) {
		w = minWidth + abs(w%120)
		doc := Render(src, Options{Width: w})
		for i, l := range doc.Lines {
			if lw := ansi.StringWidth(l); lw > w {
				t.Fatalf("width %d: line %d is %d cells: %q", w, i, lw, l)
			}
		}
		for i := range doc.Lines {
			doc.SourceLine(i)
		}
		for i := 0; i < 5; i++ {
			doc.LineForSourceLine(i)
		}
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// plainText is the document as the golden files hold it: the title header,
// then the printable lines with trailing blanks trimmed.
func plainText(doc Document) string {
	var b strings.Builder
	fmt.Fprintf(&b, "title: %q\n", doc.Title)
	b.WriteString("----\n")
	for _, l := range doc.Lines {
		b.WriteString(strings.TrimRight(ansi.Strip(l), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
