package jqplay

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// vars_test.go covers the variables line (#2786): its parsing (JSON with a
// string fallback, blanks inside JSON values, the errors), the binding into
// gojq runs and compile checks for jq and yq, and its persistence with the
// last program and the saved filter.

func TestParseVarsJSONWithStringFallback(t *testing.T) {
	vs, err := ParseVars(`id=42 name="alice" bare=bob tags=["a", "b"] ok=true none=null obj={"k": 1} $d=x`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, v := range vs {
		got[v.Name] = v.Value
	}
	want := map[string]any{
		"id":   json.Number("42"),
		"name": "alice",
		"bare": "bob",
		"tags": []any{"a", "b"},
		"ok":   true,
		"none": nil,
		"obj":  map[string]any{"k": json.Number("1")},
		"d":    "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %#v\nwant   %#v", got, want)
	}
	if names := vs.Names(); names[0] != "$id" || len(names) != len(want) {
		t.Errorf("names = %v", names)
	}
}

func TestParseVarsFallbacks(t *testing.T) {
	cases := map[string]any{
		`x=[abc`:   "[abc", // malformed JSON is --arg's string
		`x="a"b`:   `"a"b`, // JSON that does not end the word is a word
		`x=`:       "",     // an empty value is the empty string
		`x=4.5e1`:  json.Number("4.5e1"),
		`x=hello!`: "hello!",
	}
	for line, want := range cases {
		vs, err := ParseVars(line)
		if err != nil || len(vs) != 1 {
			t.Fatalf("%s: %v %v", line, vs, err)
		}
		if !reflect.DeepEqual(vs[0].Value, want) {
			t.Errorf("%s = %#v, want %#v", line, vs[0].Value, want)
		}
	}
	if vs, err := ParseVars("   "); err != nil || len(vs) != 0 {
		t.Errorf("a blank line is no variables, got %v %v", vs, err)
	}
}

func TestParseVarsErrors(t *testing.T) {
	for line, want := range map[string]string{
		`id`:           `"id" is not name=value`,
		`id=1 oops`:    `"oops" is not name=value`,
		`1x=2`:         `"1x" is not a valid variable name`,
		`a-b=2`:        `"a-b" is not a valid variable name`,
		`=2`:           `"" is not a valid variable name`,
		`ENV=1`:        `"ENV" is not a valid variable name`,
		`a=1 a=2`:      `$a is set twice`,
		`a=1 $a="x y"`: `$a is set twice`,
	} {
		_, err := ParseVars(line)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.HasPrefix(err.Error(), "variables: ") {
			t.Errorf("%s: err = %v, want %q", line, err, want)
		}
	}
}

func TestVarsEnvironText(t *testing.T) {
	vs, err := ParseVars(`name="alice smith" id=42 tags=["a"]`)
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(vs.Environ(), "\n")
	for _, want := range []string{"\nname=alice smith", "\nid=42", "\ntags=[\"a\"]"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %q", want)
		}
	}
}

func TestVarTokens(t *testing.T) {
	line := `id=42 n="a b" junk`
	toks := VarTokens(line)
	kinds := []Kind{KindVariable, KindOperator, KindNumber, KindVariable, KindOperator, KindString}
	if len(toks) != len(kinds) {
		t.Fatalf("tokens = %+v", toks)
	}
	for i, k := range kinds {
		if toks[i].Kind != k {
			t.Errorf("token %d kind %v, want %v", i, toks[i].Kind, k)
		}
	}
	if s := toks[5]; string([]rune(line)[s.Start:s.End]) != `"a b"` {
		t.Errorf("the string value spans %d..%d", s.Start, s.End)
	}
}

// runVars runs program over text in dialect d with the variables line vars.
func runVars(t *testing.T, d Dialect, program, text, vars string) Result {
	t.Helper()
	in, err := d.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return RunWith(context.Background(), program, in, Options{Vars: vars})
}

// TestRunBindsVariables is the issue's acceptance case: select(.id == $id)
// with id=42 matches; without the variable gojq's compile error comes back.
func TestRunBindsVariables(t *testing.T) {
	doc := `[{"id":41,"n":"a"},{"id":42,"n":"b"}]`
	res := runVars(t, DialectJQ, `.[] | select(.id == $id) | .n`, doc, "id=42")
	if res.Err != "" || res.Text() != `"b"` {
		t.Fatalf("result = %q err %q", res.Text(), res.Err)
	}
	if res.Options().Vars != "id=42" {
		t.Errorf("the result must carry the variables it ran with, got %q", res.Options().Vars)
	}
	res = runVars(t, DialectJQ, `.[] | select(.id == $id)`, doc, "")
	if !strings.Contains(res.Err, "variable not defined: $id") {
		t.Errorf("missing variable err = %q", res.Err)
	}
	// A string binding, and $ENV still what gojq makes it.
	res = runVars(t, DialectJQ, `[$who, ($ENV | type)]`, `null`, `who=alice`)
	if got := strings.Join(strings.Fields(res.Text()), ""); got != `["alice","object"]` {
		t.Errorf("string + $ENV = %s (%s)", got, res.Err)
	}
	// A bad line fails the run with its message.
	if res := runVars(t, DialectJQ, `.`, `1`, "oops"); !strings.HasPrefix(res.Err, "variables: ") {
		t.Errorf("bad line err = %q", res.Err)
	}
}

func TestRunBindsVariablesYAML(t *testing.T) {
	res := runVars(t, DialectYQ, `.items[] | select(.tag == $t) | .name`, "items:\n  - {name: a, tag: x}\n  - {name: b, tag: y}\n", `t=y`)
	if res.Err != "" || strings.TrimSpace(res.Text()) != "b" {
		t.Fatalf("yq result = %q err %q", res.Text(), res.Err)
	}
}

// TestRunExportsVariablesToXMQ: xmq has no $name, so the line reaches the
// CLI as environment variables — a string as its characters, JSON as written.
func TestRunExportsVariablesToXMQ(t *testing.T) {
	fakeXMQ(t, `printf '%s|%s' "$who" "$tags"`)
	res := runVars(t, DialectXMQ, "", "<a/>", `who="alice smith" tags=["x"]`)
	if res.Err != "" || res.Text() != `alice smith|["x"]` {
		t.Fatalf("xmq saw %q (err %q)", res.Text(), res.Err)
	}
	if res.Options().Vars == "" {
		t.Error("the xmq result must carry the variables it ran with")
	}
}

func TestCheckWithVariables(t *testing.T) {
	if d := CheckWith(DialectJQ, `select(.id == $id)`, "id=1"); d.Msg != "" {
		t.Errorf("bound variable must compile, got %q", d.Msg)
	}
	d := CheckWith(DialectJQ, `select(.id == $id)`, "")
	if !strings.Contains(d.Msg, "variable not defined: $id") || !d.HasSpan() {
		t.Errorf("unbound variable = %+v, want gojq's error on $id", d)
	}
	d = CheckWith(DialectJQ, `.`, "x")
	if !strings.HasPrefix(d.Msg, "variables: ") || d.HasSpan() {
		t.Errorf("bad line = %+v, want the line's error without a program span", d)
	}
	if d := CheckWith(DialectXMQ, `select /a`, "x"); !strings.HasPrefix(d.Msg, "variables: ") {
		t.Errorf("xmq must judge the line too, got %+v", d)
	}
}

func TestLastProgramsRememberVars(t *testing.T) {
	file := filepath.Join(t.TempDir(), "last.json")
	l := NewLastPrograms(file)
	l.SetWithOptions("jq:file:/a.json", "select(.id == $id)", Options{Raw: true, Vars: " id=42 "})
	re := NewLastPrograms(file)
	if o := re.Options("jq:file:/a.json"); !o.Raw || o.Vars != "id=42" {
		t.Fatalf("reloaded options = %+v", o)
	}
	re.SetWithFlags("jq:file:/a.json", ".", "")
	if o := re.Options("jq:file:/a.json"); o.Vars != "" {
		t.Errorf("a later program without variables must drop them, got %q", o.Vars)
	}
}

func TestFilterVarsPersistAndCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	lib := LoadLibrary(path)
	if err := lib.Set("by id", ".[] | select(.id == $id) | .n"); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetVars("by id", "id=2"); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetSample("by id", SelfTest{Input: `[{"id":1,"n":"a"},{"id":2,"n":"b"}]`, Expect: `"b"`}); err != nil {
		t.Fatal(err)
	}
	if err := lib.Save(path); err != nil {
		t.Fatal(err)
	}
	f, ok := LoadLibrary(path).Get("by id")
	if !ok || f.Vars != "id=2" {
		t.Fatalf("reloaded filter = %+v", f)
	}
	if st := CheckFilter(context.Background(), DialectJQ, f); st != CheckPass {
		t.Errorf("the self-test must run with the saved variables, got %v", st)
	}
	if err := lib.SetVars("missing", "x=1"); err != ErrNotFound {
		t.Errorf("SetVars on a missing name = %v", err)
	}
}
