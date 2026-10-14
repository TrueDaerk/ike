package complexitycli

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"ike/internal/cli"
	"ike/internal/complexity"
)

// width_test.go covers the text tables' width limit (#2992): --width,
// the terminal / 120 default, the truncation and the alignment.

const longRel = "internal/very/deeply/nested/package/directory/structure/goes/here/longfile.go"

const longGo = `package p

type SomeLongReceiverTypeName struct{}

func (s *SomeLongReceiverTypeName) someVeryLongMethodNameThatKeepsGoingAndGoing(a, b int) int {
	if a > 0 {
		if b > 0 {
			return 1
		}
	}
	return 0
}
`

// longName is the long method's measured name.
const longName = "SomeLongReceiverTypeName.someVeryLongMethodNameThatKeepsGoingAndGoing"

func longFixture(t *testing.T) string {
	t.Helper()
	dir := fixture(t)
	write(t, dir, longRel, longGo)
	return dir
}

// tableLines are out's lines up to the first blank one — the table.
func tableLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "" {
			break
		}
		lines = append(lines, l)
	}
	return lines
}

var gap = regexp.MustCompile(`\S  +`)

// fieldStart is the cell offset of line's n-th two-space-separated field.
func fieldStart(line string, n int) int {
	locs := gap.FindAllStringIndex(line, -1)
	if n-1 >= len(locs) {
		return -1
	}
	return lipgloss.Width(line[:locs[n-1][1]])
}

func TestWidthLimitsProjectTable(t *testing.T) {
	requireTrees(t)
	dir := longFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Width: 100, WidthSet: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	lines := tableLines(out)
	var long string
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 100 {
			t.Errorf("line is %d cells wide: %q", w, l)
		}
		if strings.Contains(l, "longfile.go") {
			long = l
		}
	}
	if !strings.HasPrefix(long, "…") || !strings.Contains(long, "/longfile.go") {
		t.Fatalf("the long path must be cut from the left, keeping the file name: %q", long)
	}
	if !strings.Contains(long, "…  ") || !strings.Contains(long, "SomeLongReceiver") {
		t.Fatalf("the long name must be cut from the right: %q", long)
	}
	// The metric columns stay aligned: LINE starts at the same cell on
	// every line.
	want := fieldStart(lines[0], 2)
	for _, l := range lines[1:] {
		if got := fieldStart(l, 2); got != want {
			t.Errorf("LINE column at %d, header at %d: %q", got, want, l)
		}
	}
}

func TestWidthZeroIsUnlimited(t *testing.T) {
	requireTrees(t)
	dir := longFixture(t)
	plain, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	zero, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, WidthSet: true})
	if code != ExitOK || zero != plain {
		t.Fatalf("--width 0 must be today's output:\n%s\nvs\n%s", zero, plain)
	}
	if !strings.Contains(zero, longRel) || strings.Contains(tableLines(zero)[0], "…") {
		t.Fatalf("unlimited output must keep the full path:\n%s", zero)
	}
}

func TestWidthRejectsNarrow(t *testing.T) {
	_, stderr, code := run(t, cli.ComplexityArgs{Width: 20, WidthSet: true})
	if code != ExitError || !strings.Contains(stderr, "--width") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestWidthLeavesJSONAlone(t *testing.T) {
	requireTrees(t)
	dir := longFixture(t)
	a, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	b, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, Width: 40, WidthSet: true})
	if a != b || !strings.Contains(b, longRel) {
		t.Fatalf("--json must ignore --width")
	}
}

func TestWidthLimitsEveryTable(t *testing.T) {
	requireTrees(t)
	dir := longFixture(t)
	p := &complexity.Pins{Version: complexity.PinsVersion}
	p.Toggle(longRel, complexity.Function{Name: longName, CCN: 9, Cognitive: 9}, 0, time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC))
	if err := p.Save(complexity.PinsPath(dir)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]cli.ComplexityArgs{
		"by file":  {By: "file"},
		"by class": {By: "class"},
		"brief":    {Brief: "class"},
		"pins":     {Pins: true},
	}
	for name, a := range cases {
		a.Paths, a.Width, a.WidthSet = []string{dir}, 110, true
		out, stderr, code := run(t, a)
		if code != ExitOK {
			t.Fatalf("%s: exit %d, stderr %q", name, code, stderr)
		}
		for _, l := range tableLines(out) {
			if w := lipgloss.Width(l); w > 110 {
				t.Errorf("%s: line is %d cells wide: %q", name, w, l)
			}
		}
	}
}

func TestWidthLimitsDiffTables(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	write(t, dir, longRel, longGo)
	g := exec.Command("git", "add", "-A")
	g.Dir = dir
	if out, err := g.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	for _, by := range []string{"", "file"} {
		out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, By: by, Width: 100, WidthSet: true})
		if code != ExitOK {
			t.Fatalf("by %q: exit %d, stderr %q", by, code, stderr)
		}
		if !strings.Contains(out, "longfile.go") {
			t.Fatalf("by %q: the long file must be listed:\n%s", by, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "  ") && lipgloss.Width(l) > 100 {
				t.Errorf("by %q: table line is %d cells wide: %q", by, lipgloss.Width(l), l)
			}
		}
	}
}

func TestResolveWidth(t *testing.T) {
	a := cli.ComplexityArgs{}
	ResolveWidth(&a, 0)
	if a.Width != cli.DefaultWidth {
		t.Fatalf("no terminal: width %d, want %d", a.Width, cli.DefaultWidth)
	}
	a = cli.ComplexityArgs{}
	ResolveWidth(&a, 211)
	if a.Width != 211 {
		t.Fatalf("terminal: width %d, want 211", a.Width)
	}
	a = cli.ComplexityArgs{WidthSet: true}
	ResolveWidth(&a, 211)
	if a.Width != 0 {
		t.Fatalf("--width 0 must stay unlimited, got %d", a.Width)
	}
}

func TestTableDefaultWidthLimitsLines(t *testing.T) {
	requireTrees(t)
	dir := longFixture(t)
	// A long-enough path to overflow 120 together with the name.
	write(t, dir, strings.Repeat("abcdefghij/", 8)+"x.go", longGo)
	a := cli.ComplexityArgs{Paths: []string{dir}}
	ResolveWidth(&a, 0) // stdout is not a terminal
	out, _, _ := run(t, a)
	for _, l := range tableLines(out) {
		if w := lipgloss.Width(l); w > cli.DefaultWidth {
			t.Errorf("line is %d cells wide: %q", w, l)
		}
	}
}

func TestTruncCell(t *testing.T) {
	if got := truncCell(colPath, "  a/b/c/d/e/f/g/file.go", 12); got != "  …g/file.go" {
		t.Errorf("indented path = %q", got)
	}
	if got := truncCell(colName, "someLongFunctionName"+acceptedMark+pinMark, 14); got != "someLongF…"+acceptedMark+pinMark {
		t.Errorf("marked name = %q", got)
	}
	if got := truncCell(colNum, "12345", 2); got != "12345" {
		t.Errorf("a number must never be cut: %q", got)
	}
}
