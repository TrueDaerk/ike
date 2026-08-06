package langyaml

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// The detector itself is pure Go, so region extents are testable without cgo;
// highlight.Fragments takes the Regions path for YAML and works here too.

func TestShellRegionsBlockScalar(t *testing.T) {
	lines := []string{
		`jobs:`,
		`  build:`,
		`    steps:`,
		`      - run: |`,
		`          echo building`,
		`          make all`,
		`      - uses: actions/checkout@v4`,
	}
	regions := shellRegions(lines)
	if len(regions) != 1 {
		t.Fatalf("regions = %d, want 1: %+v", len(regions), regions)
	}
	r := regions[0]
	if r.Lang != "shell" {
		t.Errorf("Lang = %q, want shell", r.Lang)
	}
	if r.StartLine != 4 || r.EndLine != 5 {
		t.Errorf("lines = %d..%d, want 4..5", r.StartLine, r.EndLine)
	}
	if r.StartCol != 0 || r.EndCol != len(lines[5]) {
		t.Errorf("cols = %d..%d, want 0..%d", r.StartCol, r.EndCol, len(lines[5]))
	}
}

func TestShellRegionsBlockScalarVariants(t *testing.T) {
	for _, header := range []string{"|", ">", "|-", "|+", ">-", "|2-", "| # comment"} {
		lines := []string{
			`steps:`,
			`  - run: ` + header,
			`      echo hi`,
			`  - run: echo done`,
		}
		regions := shellRegions(lines)
		if len(regions) != 2 {
			t.Fatalf("header %q: regions = %d, want 2: %+v", header, len(regions), regions)
		}
		if regions[0].StartLine != 2 || regions[0].EndLine != 2 {
			t.Errorf("header %q: block region lines = %d..%d, want 2..2", header, regions[0].StartLine, regions[0].EndLine)
		}
	}
}

func TestShellRegionsInlineScalar(t *testing.T) {
	lines := []string{
		`steps:`,
		`  - run: make test`,
		`  - run: 'echo quoted'`,
		`  - run: echo hi # deploy`,
	}
	regions := shellRegions(lines)
	if len(regions) != 3 {
		t.Fatalf("regions = %d, want 3: %+v", len(regions), regions)
	}
	line := func(i int) string { return lines[regions[i].StartLine][regions[i].StartCol:regions[i].EndCol] }
	if got := line(0); got != "make test" {
		t.Errorf("plain scalar = %q, want %q", got, "make test")
	}
	if got := line(1); got != "echo quoted" {
		t.Errorf("quoted scalar = %q, want %q (quotes stripped)", got, "echo quoted")
	}
	if got := line(2); got != "echo hi" {
		t.Errorf("commented scalar = %q, want %q (comment cut)", got, "echo hi")
	}
}

// TestShellRegionsRequiresCI: a run: key in arbitrary YAML stays plain — the
// buffer must contain a steps: line to count as a CI pipeline.
func TestShellRegionsRequiresCI(t *testing.T) {
	lines := []string{
		`tasks:`,
		`  - run: echo not ci`,
	}
	if regions := shellRegions(lines); regions != nil {
		t.Fatalf("non-CI buffer produced regions: %+v", regions)
	}
}

// TestShellRegionsSkipsScriptBody: a "run:" inside an already-consumed block
// scalar (say, a script writing YAML) never opens a second region.
func TestShellRegionsSkipsScriptBody(t *testing.T) {
	lines := []string{
		`steps:`,
		`  - run: |`,
		`      cat <<Y`,
		`      run: echo nested`,
		`      Y`,
	}
	regions := shellRegions(lines)
	if len(regions) != 1 {
		t.Fatalf("regions = %d, want 1: %+v", len(regions), regions)
	}
	if regions[0].StartLine != 2 || regions[0].EndLine != 4 {
		t.Errorf("lines = %d..%d, want 2..4", regions[0].StartLine, regions[0].EndLine)
	}
}

// TestShellRegionsEmptyValueIgnored: `run:` with nothing after it (null value
// or a broken document) yields no region.
func TestShellRegionsEmptyValueIgnored(t *testing.T) {
	lines := []string{
		`steps:`,
		`  - run:`,
		`  - run: |`,
	}
	if regions := shellRegions(lines); regions != nil {
		t.Fatalf("empty values produced regions: %+v", regions)
	}
}

// TestYAMLFragmentsViaRegions: the registry seam end-to-end — Fragments
// resolves YAML through the Go-level detector, no grammar needed.
func TestYAMLFragmentsViaRegions(t *testing.T) {
	lines := []string{
		`steps:`,
		`  - run: |`,
		`      echo one`,
		`      echo two`,
	}
	frags := highlight.Fragments("yaml", lines)
	if len(frags) != 1 {
		t.Fatalf("Fragments = %d, want 1: %+v", len(frags), frags)
	}
	f := frags[0]
	if f.Lang != "shell" {
		t.Errorf("Lang = %q, want shell", f.Lang)
	}
	want := "      echo one\n      echo two"
	if got := strings.Join(f.Lines, "\n"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}
