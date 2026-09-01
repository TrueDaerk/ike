package testdata

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// dsl_test.go covers the DSL (#2392): the parser's happy paths and error
// classes, the dependency-ordered evaluator with {field} references and
// template strings, and weighted(...) sampling.

// mustParse parses a body or fails the test.
func mustParse(t *testing.T, dsl string) *Program {
	t.Helper()
	p, err := ParseDSL(dsl)
	if err != nil {
		t.Fatalf("ParseDSL(%q) = %v", dsl, err)
	}
	return p
}

// rows evaluates the spec's first n rows or fails the test.
func rows(t *testing.T, spec Spec, n int) [][]any {
	t.Helper()
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	out := make([][]any, n)
	for i := range out {
		if out[i], err = g.Row(i); err != nil {
			t.Fatalf("Row(%d): %v", i, err)
		}
	}
	return out
}

func TestParseHappyPaths(t *testing.T) {
	cases := []struct {
		name string
		dsl  string
	}{
		{"plain calls", "id = id()\nname = first_name()"},
		{"call with range", "n = int(1..1000)\nx = float(0.5..2)\nd = date(2020-01-01..2026-01-01)"},
		{"call with list", "c = from_list(red, green, blue)"},
		{"call with domain", "e = email(example.com)"},
		{"comments and blanks", "# people\n\nid = id()\n   # tail comment line\nname = last_name()\n"},
		{"template string", `id = id()` + "\n" + `url = "https://example.com/api/{id}"`},
		{"plain literal string", `s = "hello, world"`},
		{"empty literal string", `s = ""`},
		{"escapes", `s = "say \"hi\" \{not a ref\} \\ two\nlines"`},
		{"ref as argument", "domain = domain()\nhost = hostname({domain})"},
		{"forward reference", `url = "https://{host}/"` + "\nhost = hostname()"},
		{"weighted literals", `state = weighted(60: "active", 20: "inactive", 20: "banned")`},
		{"weighted mixed", "domain = domain()\nmail = weighted(70: email({domain}), 30: \"\")"},
		{"weighted nested", `x = weighted(50: weighted(1: "a", 1: "b"), 50: int(1..9))`},
		{"fractional weights", `x = weighted(0.5: "a", 1.5: "b")`},
		{"loose spacing", "  spaced   =   int( 1..9 )  "},
		{"dotted and dashed names", "a.b = id()\nc-d = uuid()\nx = \"{a.b}/{c-d}\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustParse(t, tc.dsl)
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		dsl  string
		want string // substring of the error, which always carries "line N"
		line int
	}{
		{"missing equals", "id = id()\nbroken line", "missing '='", 2},
		{"empty name", "= id()", "field name is required", 1},
		{"bad name", "bad name = id()", "invalid field name", 1},
		{"duplicate name", "a = id()\na = uuid()", "duplicate field", 2},
		{"empty expression", "a = ", "an expression is required", 1},
		{"unknown generator", "a = wat()", `unknown generator "wat"`, 1},
		{"generator without parens", "a = uuid", "must be called with parentheses", 1},
		{"unknown bare word", "a = hello", `unknown value "hello"`, 1},
		{"bare reference", "a = id()\nb = {a}", "not an expression", 2},
		{"unclosed call", "a = int(1..9", "missing ')'", 1},
		{"unclosed quote", `a = "oops`, "missing closing quote", 1},
		{"unknown escape", `a = "\x"`, "unknown escape", 1},
		{"unclosed ref in string", `a = "{oops"`, "missing '}'", 1},
		{"unclosed ref in arg", "a = domain()\nb = hostname({a", "missing ')'", 2},
		{"empty ref", `a = "{}"`, "field name is required", 1},
		{"trailing garbage", "a = id() extra", `unexpected "extra"`, 1},
		{"argument on paramless kind", "a = uuid(4)", "uuid() takes no argument", 1},
		{"ref argument on paramless kind", "a = id()\nb = uuid({a})", "uuid() takes no argument", 2},
		{"bad int range", "a = int(9..1)", "above max", 1},
		{"malformed range", "a = int(1-9)", "min..max", 1},
		{"bad date", "a = date(yesterday..today)", "not a date", 1},
		{"bad domain", "a = email(http://x.com)", "not a domain name", 1},
		{"empty from_list", "a = from_list( , ,)", "at least one entry", 1},
		{"weighted empty", "a = weighted()", "at least one branch", 1},
		{"weighted without parens", "a = weighted", "needs parentheses", 1},
		{"weighted bad weight", `a = weighted(lots: "x")`, "is not a weight", 1},
		{"weighted negative weight", `a = weighted(-1: "x")`, "is not a weight", 1},
		{"weighted zero weight", `a = weighted(0: "x")`, "must be positive", 1},
		{"weighted missing colon", `a = weighted(60 "x")`, "expected ':'", 1},
		{"weighted unclosed", `a = weighted(60: "x"`, "missing ')'", 1},
		{"weighted bad separator", `a = weighted(60: "x" 40: "y")`, "expected ',' or ')'", 1},
		{"unknown reference", "a = id()\nb = \"{c}\"", "reference {c} does not match any field", 2},
		{"self reference", `a = "{a}"`, "cycle: a → a", 1},
		{"two-field cycle", "a = \"{b}\"\nb = \"x{a}\"", "cycle: a → b → a", 1},
		{"cycle behind a chain", "top = id()\na = \"{top}{b}\"\nb = hostname({c})\nc = \"{b}\"", "cycle: b → c → b", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDSL(tc.dsl)
			if err == nil {
				t.Fatalf("ParseDSL(%q) = nil, want an error containing %q", tc.dsl, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
			var pe *ParseError
			if ok := errorsAs(err, &pe); !ok {
				t.Fatalf("error %T is not a *ParseError", err)
			}
			if pe.Line != tc.line {
				t.Fatalf("error line = %d, want %d (%s)", pe.Line, tc.line, err)
			}
		})
	}
}

// errorsAs is errors.As without the import dance for the one type used here.
func errorsAs(err error, target **ParseError) bool {
	for err != nil {
		if pe, ok := err.(*ParseError); ok {
			*target = pe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestProgramOrder pins definition order for output and dependency order for
// evaluation.
func TestProgramOrder(t *testing.T) {
	p := mustParse(t, "url = \"https://{host}/\"\nhost = hostname({domain})\ndomain = domain()")
	if got := strings.Join(p.Names(), ","); got != "url,host,domain" {
		t.Fatalf("Names() = %q, want definition order", got)
	}
	lines := p.FieldLines()
	if lines["url"] != 1 || lines["domain"] != 3 {
		t.Fatalf("FieldLines() = %v", lines)
	}
	spec := dslSpec(FormatCSV, 3, 9, "url = \"https://{host}/\"\nhost = hostname({domain})\ndomain = domain()")
	for _, r := range rows(t, spec, 3) {
		u, h, d := r[0].(string), r[1].(string), r[2].(string)
		if !strings.HasSuffix(h, "."+d) {
			t.Fatalf("host %q not under domain %q", h, d)
		}
		if u != "https://"+h+"/" {
			t.Fatalf("url %q not built from host %q", u, h)
		}
	}
}

// TestTemplateStrings covers interpolation: values of every type render into
// the string, id() stays the row number, escapes are literal.
func TestTemplateStrings(t *testing.T) {
	spec := dslSpec(FormatCSV, 4, 5, strings.Join([]string{
		`id    = id()`,
		`n     = int(7..7)`,
		`ok    = bool()`,
		`plain = "x"`,
		`built = "row {id}: n={n} ok={ok} {plain}\n\"quoted\" \{brace\}"`,
	}, "\n"))
	for i, r := range rows(t, spec, 4) {
		got := r[4].(string)
		want := "row " + strconv.Itoa(i+1) + ": n=7 ok=" + strconv.FormatBool(r[2].(bool)) + " x\n\"quoted\" {brace}"
		if got != want {
			t.Fatalf("row %d template = %q, want %q", i, got, want)
		}
	}
}

// TestReferenceAsArgument is the issue's host({domain}) / email({domain})
// case: every generated value stays inside the referenced field's value.
func TestReferenceAsArgument(t *testing.T) {
	spec := dslSpec(FormatCSV, 20, 12, "domain = domain()\nhost = hostname({domain})\nmail = email({domain})")
	for _, r := range rows(t, spec, 20) {
		d, h, m := r[0].(string), r[1].(string), r[2].(string)
		if !strings.HasSuffix(h, "."+d) {
			t.Fatalf("host %q not under %q", h, d)
		}
		if !strings.HasSuffix(m, "@"+d) {
			t.Fatalf("email %q not at %q", m, d)
		}
	}
}

// TestDynamicArgumentValidatedPerRow: an interpolated argument that fails its
// kind's grammar is a generation error naming the field, not a panic.
func TestDynamicArgumentValidatedPerRow(t *testing.T) {
	spec := dslSpec(FormatCSV, 2, 1, "a = sentence()\nb = email({a})")
	g, err := NewGenerator(spec)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	_, err = g.Row(0)
	if err == nil || !strings.Contains(err.Error(), `field "b"`) || !strings.Contains(err.Error(), "not a domain name") {
		t.Fatalf("Row = %v, want a per-row domain error naming field b", err)
	}
	if _, err := Render(spec); err == nil {
		t.Fatal("Render must surface the per-row error")
	}
}

// TestWeightedDistribution is the weights sanity check: 60/20/20 over enough
// rows lands near 60/20/20, and scaling all weights (they need not sum to
// 100) does not change the distribution.
func TestWeightedDistribution(t *testing.T) {
	count := func(dsl string) map[string]int {
		spec := dslSpec(FormatCSV, 0, 77, dsl)
		spec.Rows = 3000
		got := map[string]int{}
		for _, r := range rows(t, spec, 3000) {
			got[r[0].(string)]++
		}
		return got
	}
	got := count(`state = weighted(60: "a", 20: "b", 20: "c")`)
	if got["a"]+got["b"]+got["c"] != 3000 {
		t.Fatalf("unexpected values: %v", got)
	}
	for val, share := range map[string]float64{"a": 0.6, "b": 0.2, "c": 0.2} {
		frac := float64(got[val]) / 3000
		if frac < share-0.05 || frac > share+0.05 {
			t.Fatalf("share of %q = %.3f, want ≈%.2f (%v)", val, frac, share, got)
		}
	}
	// The same weights scaled by 3 draw identically under the same seed —
	// normalization, not "sums to 100".
	scaled := count(`state = weighted(180: "a", 60: "b", 60: "c")`)
	for k, v := range got {
		if scaled[k] != v {
			t.Fatalf("scaled weights changed the draw: %v vs %v", scaled, got)
		}
	}
}

// TestWeightedOverExpressions mixes a generator branch with a literal branch,
// per the issue's email example.
func TestWeightedOverExpressions(t *testing.T) {
	spec := dslSpec(FormatCSV, 200, 3, "domain = domain()\nmail = weighted(70: email({domain}), 30: \"\")")
	empty, filled := 0, 0
	for _, r := range rows(t, spec, 200) {
		m := r[1].(string)
		switch {
		case m == "":
			empty++
		case strings.HasSuffix(m, "@"+r[0].(string)):
			filled++
		default:
			t.Fatalf("mail %q neither empty nor at %q", m, r[0])
		}
	}
	if empty == 0 || filled == 0 {
		t.Fatalf("both branches must occur (empty %d, filled %d)", empty, filled)
	}
}

// TestWeightedDeterminism: the branch draw comes from the seeded instance
// faker, so the same seed picks the same branches and values.
func TestWeightedDeterminism(t *testing.T) {
	spec := dslSpec(FormatNDJSON, 100, 42, "domain = domain()\nmail = weighted(50: email({domain}), 30: \"\", 20: sentence())")
	a, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("the same seed produced different weighted draws")
	}
}

// fullDSL exercises every DSL feature at once — the spec the cross-format
// determinism and preview tests run.
const fullDSL = `id     = id()
domain = domain()
host   = hostname({domain})
url    = "https://{host}/api/{id}"
state  = weighted(60: "active", 20: "inactive", 20: "banned")
email  = weighted(70: email({domain}), 30: "")
n      = int(1..1000)
when   = date(2020-01-01..2026-01-01)
color  = from_list(red, green, blue)
`

// TestDSLSeedDeterminismAllFormats is the acceptance criterion "same seed +
// same spec → byte-identical output" for the full feature set, per format.
func TestDSLSeedDeterminismAllFormats(t *testing.T) {
	for _, f := range Formats() {
		t.Run(string(f), func(t *testing.T) {
			spec := dslSpec(f, 25, 4242, fullDSL)
			a, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			b, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("two runs of seed %d differ", spec.Seed)
			}
			spec.Seed = 4243
			c, err := Render(spec)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if bytes.Equal(a, c) {
				t.Fatal("a different seed produced identical output")
			}
		})
	}
}

// TestPreviewMatchesGeneration: the preview pipeline is the generation
// pipeline. For the line-per-row formats the preview is literally the head of
// the full file; for every format it equals a Render capped to the same rows.
func TestPreviewMatchesGeneration(t *testing.T) {
	spec := dslSpec(FormatNDJSON, 100, 7, fullDSL)
	prev, err := Preview(spec)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	full, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(full, prev) {
		t.Fatalf("preview is not the head of the generated file:\n%s\n---\n%s", prev, full[:len(prev)])
	}
	for _, f := range Formats() {
		spec := dslSpec(f, 100, 7, fullDSL)
		prev, err := Preview(spec)
		if err != nil {
			t.Fatalf("Preview(%s): %v", f, err)
		}
		capped := spec
		capped.Rows = PreviewRows
		want, err := Render(capped)
		if err != nil {
			t.Fatalf("Render(%s): %v", f, err)
		}
		if !bytes.Equal(prev, want) {
			t.Fatalf("%s preview differs from a %d-row generation", f, PreviewRows)
		}
	}
	// A preview below the cap renders all rows.
	small := dslSpec(FormatCSV, 2, 7, fullDSL)
	prevSmall, err := Preview(small)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	fullSmall, err := Render(small)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Equal(prevSmall, fullSmall) {
		t.Fatal("a 2-row preview must equal the 2-row generation")
	}
}

// TestBuiltinTemplatesParse: every shipped template body parses; the curated
// minimum set exists; each renders under a seed.
func TestBuiltinTemplatesParse(t *testing.T) {
	want := map[string]bool{"Person": false, "Address": false, "Order": false, "URL / Web": false, "Server log": false}
	for _, tpl := range BuiltinTemplates() {
		if !tpl.BuiltIn {
			t.Fatalf("template %q not marked built-in", tpl.Name)
		}
		if _, err := ParseDSL(tpl.DSL); err != nil {
			t.Fatalf("template %q does not parse: %v", tpl.Name, err)
		}
		if _, err := Render(dslSpec(FormatCSV, 3, 1, tpl.DSL)); err != nil {
			t.Fatalf("template %q does not render: %v", tpl.Name, err)
		}
		if _, ok := want[tpl.Name]; ok {
			want[tpl.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("built-in template %q missing", name)
		}
	}
}

// TestWeightedInTemplateString: a weighted field interpolates like any other.
func TestWeightedInTemplateString(t *testing.T) {
	spec := dslSpec(FormatCSV, 50, 8, "state = weighted(1: \"on\", 1: \"off\")\nmsg = \"switch is {state}\"")
	for _, r := range rows(t, spec, 50) {
		if want := "switch is " + r[0].(string); r[1].(string) != want {
			t.Fatalf("msg = %q, want %q", r[1], want)
		}
	}
}

func TestParseErrorFormatting(t *testing.T) {
	_, err := ParseDSL("a = id()\nb = wat()")
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "line 2: ") {
		t.Fatalf("error %q must lead with its line", got)
	}
}

func TestDefaultDSLIsValid(t *testing.T) {
	p := mustParse(t, DefaultDSL)
	if got := fmt.Sprint(p.Names()); got != "[id first_name last_name email]" {
		t.Fatalf("default fields = %v", got)
	}
}
