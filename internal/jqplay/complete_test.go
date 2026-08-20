package jqplay

import (
	"fmt"
	"strings"
	"testing"

	"github.com/itchyny/gojq"
)

// complete_test.go covers the query line's typing aid (#1979): key extraction
// at a path — nested, through arrays, quoted — builtin listing and filtering,
// and the context detection that keeps completion out of strings, comments
// and computed expressions.

// parseInput builds the snapshot a completion runs against.
func parseInput(t *testing.T, text string) *Input {
	t.Helper()
	in, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// complete runs Complete with the cursor at the end of program.
func complete(t *testing.T, program, input string) ([]Candidate, int) {
	t.Helper()
	return Complete(program, len([]rune(program)), parseInput(t, input), false)
}

// labels flattens candidates for comparison.
func labels(items []Candidate) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

// TestCompleteTopLevelKeys is the issue's first acceptance case: `.` offers
// the top-level keys of the input, sorted, with the value types as details.
func TestCompleteTopLevelKeys(t *testing.T) {
	items, start := complete(t, ".", `{"name":"ike","tags":[1],"meta":{"a":1}}`)
	if got, want := fmt.Sprint(labels(items)), "[meta name tags]"; got != want {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if start != 1 {
		t.Errorf("start = %d, want the position after the dot", start)
	}
	details := map[string]string{}
	for _, it := range items {
		details[it.Label] = it.Detail
	}
	for key, want := range map[string]string{"name": "string", "tags": "array", "meta": "object"} {
		if details[key] != want {
			t.Errorf("detail[%s] = %q, want the value type %q", key, details[key], want)
		}
	}
}

// TestCompleteNestedKeys: `.foo.` offers the keys under foo.
func TestCompleteNestedKeys(t *testing.T) {
	items, _ := complete(t, ".meta.", `{"meta":{"created":1,"updated":2},"other":true}`)
	if got := fmt.Sprint(labels(items)); got != "[created updated]" {
		t.Fatalf("nested keys = %v", got)
	}
}

// TestCompleteThroughArrays: `.items[].` offers the keys of the array's
// element objects — the union across elements.
func TestCompleteThroughArrays(t *testing.T) {
	items, _ := complete(t, ".items[].", `{"items":[{"id":1},{"id":2,"name":"x"}]}`)
	if got := fmt.Sprint(labels(items)); got != "[id name]" {
		t.Fatalf("keys through the array = %v", got)
	}
}

// TestCompleteArrayIndex: `.items[0].` offers exactly that element's keys,
// and a negative index counts from the end like jq's.
func TestCompleteArrayIndex(t *testing.T) {
	const input = `{"items":[{"first":1},{"second":2}]}`
	items, _ := complete(t, ".items[0].", input)
	if got := fmt.Sprint(labels(items)); got != "[first]" {
		t.Fatalf("keys at [0] = %v", got)
	}
	items, _ = complete(t, ".items[-1].", input)
	if got := fmt.Sprint(labels(items)); got != "[second]" {
		t.Fatalf("keys at [-1] = %v", got)
	}
}

// TestCompleteQuotedSegments: both `."a b".` and `.["a b"].` descend through
// a key that is not an identifier.
func TestCompleteQuotedSegments(t *testing.T) {
	const input = `{"a b":{"inner":1}}`
	for _, program := range []string{`."a b".`, `.["a b"].`} {
		items, _ := complete(t, program, input)
		if got := fmt.Sprint(labels(items)); got != "[inner]" {
			t.Errorf("%s keys = %v, want [inner]", program, got)
		}
	}
}

// TestCompleteQuotesNonIdentifierKeys: a key jq cannot take bare inserts its
// quoted form, so accepting it yields a valid program.
func TestCompleteQuotesNonIdentifierKeys(t *testing.T) {
	items, _ := complete(t, ".", `{"a b":1,"plain":2}`)
	inserts := map[string]string{}
	for _, it := range items {
		inserts[it.Label] = it.Insert
	}
	if got := inserts["plain"]; got != "plain" {
		t.Errorf("identifier key inserts %q, want itself", got)
	}
	if got := inserts["a b"]; got != `"a b"` {
		t.Errorf("non-identifier key inserts %q, want its quoted form", got)
	}
	// And the quoted access actually evaluates.
	res := Evaluate(`."a b"`, `{"a b":1,"plain":2}`)
	if res.Err != "" || res.Text() != "1" {
		t.Errorf(`."a b" = (%q, %q), the quoted insert must be valid jq`, res.Text(), res.Err)
	}
}

// TestCompleteKeyPrefixFilter: a partial after the dot narrows the keys.
func TestCompleteKeyPrefixFilter(t *testing.T) {
	items, start := complete(t, ".na", `{"name":"ike","nation":"x","other":1}`)
	if got := fmt.Sprint(labels(items)); got != "[name nation]" {
		t.Fatalf("filtered keys = %v", got)
	}
	if start != 1 {
		t.Errorf("start = %d, want the partial's start after the dot", start)
	}
}

// TestCompleteJSONLStream: a multi-value input offers the union of the
// top-level keys, as the program runs over every value.
func TestCompleteJSONLStream(t *testing.T) {
	items, _ := complete(t, ".", "{\"a\":1}\n{\"b\":2}\n")
	if got := fmt.Sprint(labels(items)); got != "[a b]" {
		t.Fatalf("stream keys = %v", got)
	}
}

// TestCompleteNotRooted: a dot after a computed value — a function call, a
// variable, a bare identifier — offers nothing: the snapshot cannot answer
// for it, and a wrong list is worse than none.
func TestCompleteNotRooted(t *testing.T) {
	for _, program := range []string{"env.", "$v.", "(.a).", `"s".`, "..", "f(.x)."} {
		if items, _ := complete(t, program, `{"a":1}`); len(items) != 0 {
			t.Errorf("%q offered %v, want nothing", program, labels(items))
		}
	}
}

// TestCompleteRootAfterPipe: a bare `.` after a pipe or an operator offers
// the top-level keys — the chain is just the trigger dot.
func TestCompleteRootAfterPipe(t *testing.T) {
	for _, program := range []string{".a | .", "[.], .", "select(."} {
		items, _ := complete(t, program, `{"key":1}`)
		if got := fmt.Sprint(labels(items)); got != "[key]" {
			t.Errorf("%q keys = %v, want the top level", program, got)
		}
	}
}

// TestCompleteInsideLiteralsSilent: a dot or a word inside a string or a
// comment is text; the popup must not open over it.
func TestCompleteInsideLiteralsSilent(t *testing.T) {
	for _, program := range []string{`"abc`, `."abc`, "# map", `.a == "x`} {
		if items, _ := complete(t, program, `{"a":1}`); len(items) != 0 {
			t.Errorf("%q offered %v, want nothing inside a literal", program, labels(items))
		}
	}
}

// TestCompleteOptionalMarker: `.foo?.` sees through the optional marker.
func TestCompleteOptionalMarker(t *testing.T) {
	items, _ := complete(t, ".meta?.", `{"meta":{"x":1}}`)
	if got := fmt.Sprint(labels(items)); got != "[x]" {
		t.Fatalf("keys after ?. = %v", got)
	}
}

// TestCompleteBuiltins: an identifier partial offers the matching builtins
// with their arities, and gojq's own list is the source — `select`, `map`,
// `to_entries`, `group_by` are all in it.
func TestCompleteBuiltins(t *testing.T) {
	items, start := complete(t, "sel", `{"a":1}`)
	if len(items) == 0 || start != 0 {
		t.Fatalf("items = %v, start = %d", labels(items), start)
	}
	if items[0].Label != "select" {
		t.Fatalf("first candidate = %q, want select", items[0].Label)
	}
	if items[0].Detail != "/1" {
		t.Errorf("select detail = %q, want its arity", items[0].Detail)
	}
	if items[0].Doc == "" {
		t.Error("select should carry its one-line doc")
	}
	names := map[string]bool{}
	for _, b := range Builtins() {
		names[b.Name] = true
	}
	for _, want := range []string{"select", "map", "to_entries", "group_by", "range"} {
		if !names[want] {
			t.Errorf("the builtin list must include %s", want)
		}
	}
}

// TestCompleteBuiltinsMergeArities: a builtin defined at several arities is
// one candidate listing them, not duplicates.
func TestCompleteBuiltinsMergeArities(t *testing.T) {
	items, _ := complete(t, "range", `1`)
	var found *Candidate
	count := 0
	for i, it := range items {
		if it.Label == "range" {
			found = &items[i]
			count++
		}
	}
	if count != 1 || found == nil {
		t.Fatalf("range appears %d times, want once", count)
	}
	if found.Detail != "/1 /2 /3" {
		t.Errorf("range detail = %q, want every arity", found.Detail)
	}
}

// TestBuiltinsCompile: every name the completion offers is accepted by the
// engine at its listed arity — the list and the evaluator cannot drift apart,
// since the former is read out of the latter.
func TestBuiltinsCompile(t *testing.T) {
	bs := Builtins()
	if len(bs) < 100 {
		t.Fatalf("builtin list has %d entries, expected gojq's full set", len(bs))
	}
	for _, b := range bs {
		for _, arity := range b.Arities {
			program := b.Name
			if arity > 0 {
				args := strings.TrimRight(strings.Repeat(".;", arity), ";")
				program = fmt.Sprintf("%s(%s)", b.Name, args)
			}
			q, err := gojq.Parse(program)
			if err != nil {
				t.Errorf("%s/%d: parse: %v", b.Name, arity, err)
				continue
			}
			if _, err := gojq.Compile(q); err != nil {
				t.Errorf("%s/%d: compile: %v", b.Name, arity, err)
			}
		}
	}
}

// TestCompleteManualOpensFullList: the explicit request (ctrl+space) opens
// the builtin list on an empty partial, which plain typing never does.
func TestCompleteManualOpensFullList(t *testing.T) {
	in := parseInput(t, `{"a":1}`)
	if items, _ := Complete("", 0, in, false); len(items) != 0 {
		t.Fatalf("an empty line offered %v without a request", labels(items))
	}
	items, start := Complete("", 0, in, true)
	if len(items) == 0 || start != 0 {
		t.Fatalf("manual completion offered nothing")
	}
	if len(items) > MaxCompletionItems {
		t.Errorf("items = %d, over the cap", len(items))
	}
}

// TestCompleteNumbersAndVariablesSilent: number tails, variables and format
// strings are not identifiers to complete.
func TestCompleteNumbersAndVariablesSilent(t *testing.T) {
	for _, program := range []string{"1e", "$va", "@bas"} {
		if items, _ := Complete(program, len([]rune(program)), parseInput(t, `1`), false); len(items) != 0 {
			t.Errorf("%q offered %v, want nothing", program, labels(items))
		}
	}
}

// TestCompleteNilInput: with the snapshot still parsing (or failed) path
// completion stays quiet and builtins still answer — the popup must never
// need a nil check upstream.
func TestCompleteNilInput(t *testing.T) {
	if items, _ := Complete(".", 1, nil, false); len(items) != 0 {
		t.Errorf("keys without an input = %v, want nothing", labels(items))
	}
	if items, _ := Complete("sel", 3, nil, false); len(items) == 0 {
		t.Error("builtins must complete without an input")
	}
}

// TestCompleteBudget: a snapshot larger than the node budget completes with
// the keys seen so far instead of walking everything — the query line must
// not stall on a keystroke.
func TestCompleteBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := range completionNodeBudget * 2 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"k%d":1}`, i)
	}
	b.WriteString(`]}`)
	items, _ := complete(t, ".items[].", b.String())
	if len(items) == 0 {
		t.Fatal("a capped walk should still offer the keys it saw")
	}
	if len(items) > MaxCompletionItems {
		t.Errorf("items = %d, over the cap", len(items))
	}
}

// TestCompleteMidLine: completion works with text after the cursor — the
// replace span is the partial only.
func TestCompleteMidLine(t *testing.T) {
	program := ".na | length"
	items, start := Complete(program, 3, parseInput(t, `{"name":"ike"}`), false)
	if got := fmt.Sprint(labels(items)); got != "[name]" {
		t.Fatalf("mid-line keys = %v", got)
	}
	if start != 1 {
		t.Errorf("start = %d, want 1", start)
	}
}
