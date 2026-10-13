package cli

import (
	"reflect"
	"testing"
)

func TestParseGrammar(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Invocation
	}{
		{"zero args", nil, Invocation{}},
		{"plain path", []string{"file.go"}, Invocation{Targets: []Target{{Path: "file.go"}}}},
		{"path with line", []string{"file.go:42"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"path with line and col", []string{"file.go:42:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42, Col: 7}}}},
		{"colon inside path", []string{"weird:name.txt"}, Invocation{Targets: []Target{{Path: "weird:name.txt"}}}},
		{"colon path with line", []string{"a:b.txt:12"}, Invocation{Targets: []Target{{Path: "a:b.txt", Line: 12}}}},
		{"three numeric segments keep leftmost in path", []string{"a:12:5:7"}, Invocation{Targets: []Target{{Path: "a:12", Line: 5, Col: 7}}}},
		{"trailing colon is a plain path", []string{"file.go:"}, Invocation{Targets: []Target{{Path: "file.go:"}}}},
		{"trailing colon after number", []string{"file.go:42:"}, Invocation{Targets: []Target{{Path: "file.go:42:"}}}},
		{"zero line stays in path", []string{"file.go:0"}, Invocation{Targets: []Target{{Path: "file.go:0"}}}},
		{"signed suffix stays in path", []string{"file.go:+5"}, Invocation{Targets: []Target{{Path: "file.go:+5"}}}},
		{"leading colon stays in path", []string{":42"}, Invocation{Targets: []Target{{Path: ":42"}}}},
		{"vim-style +N", []string{"+42", "file.go"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"suffix wins over +N", []string{"+42", "file.go:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 7}}}},
		{"+N skips only the next path", []string{"+42", "a.go", "b.go"}, Invocation{Targets: []Target{{Path: "a.go", Line: 42}, {Path: "b.go"}}}},
		{"stdin only", []string{"-"}, Invocation{Stdin: true}},
		{"stdin with files keeps order", []string{"a.go", "-", "b.go:3"}, Invocation{Stdin: true, Targets: []Target{{Path: "a.go"}, {Path: "b.go", Line: 3}}}},
		{"multiple targets preserve order", []string{"b.go", "a.go:1"}, Invocation{Targets: []Target{{Path: "b.go"}, {Path: "a.go", Line: 1}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.args)
			if err != nil {
				t.Fatalf("Parse(%v) error: %v", c.args, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%v) = %+v, want %+v", c.args, got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"dangling +N", []string{"file.go", "+42"}},
		{"bare +", []string{"+"}},
		{"non-numeric +x", []string{"+abc", "file.go"}},
		{"double sign", []string{"++42", "file.go"}},
		{"zero line flag", []string{"+0", "file.go"}},
		{"duplicate stdin", []string{"-", "-"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.args); err == nil {
				t.Fatalf("Parse(%v) must error", c.args)
			}
		})
	}
}

// TestParseVersionFlag covers `ike --version` / `ike -v` (#1214): the flag
// short-circuits, so it wins over anything else on the line — including an
// otherwise-invalid invocation, since the version banner never needs the rest
// of the arguments to be sound.
func TestParseVersionFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"long", []string{"--version"}},
		{"short", []string{"-v"}},
		{"before a path", []string{"-v", "file.go"}},
		{"after a path", []string{"file.go", "--version"}},
		{"wins over a malformed line", []string{"--version", "+abc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv, err := Parse(c.args)
			if err != nil {
				t.Fatalf("Parse(%v) errored: %v", c.args, err)
			}
			if !inv.Version {
				t.Fatalf("Parse(%v).Version = false, want true", c.args)
			}
		})
	}
}

// TestParseVersionFlagIsExact guards the neighbours: a lone "-" is stdin, and
// "-v" only counts as the flag when it is the whole argument.
func TestParseVersionFlagIsExact(t *testing.T) {
	for _, arg := range []string{"-", "-vv", "--versions", "v"} {
		inv, err := Parse([]string{arg})
		if err != nil {
			t.Fatalf("Parse([%q]) errored: %v", arg, err)
		}
		if inv.Version {
			t.Fatalf("Parse([%q]).Version = true, want false", arg)
		}
	}
}

// TestParseDeepLinkURL covers the ike:// routing arguments (#2396).
func TestParseDeepLinkURL(t *testing.T) {
	inv, err := Parse([]string{"ike://open?remote=git@github.com:a/b"})
	if err != nil || inv.URL != "ike://open?remote=git@github.com:a/b" || inv.URLSendOnly {
		t.Fatalf("got %+v, %v", inv, err)
	}
	inv, err = Parse([]string{"--url-send-only", "ike://open?project=x"})
	if err != nil || !inv.URLSendOnly || inv.URL != "ike://open?project=x" {
		t.Fatalf("got %+v, %v", inv, err)
	}
	if _, err = Parse([]string{"ike://open?project=a", "ike://open?project=b"}); err == nil {
		t.Error("duplicate URL accepted")
	}
	if _, err = Parse([]string{"--url-send-only"}); err == nil {
		t.Error("--url-send-only without URL accepted")
	}
	// A path that merely contains "ike:" mid-string stays a file target.
	inv, err = Parse([]string{"dir/ike:notes.txt"})
	if err != nil || inv.URL != "" || len(inv.Targets) != 1 {
		t.Fatalf("got %+v, %v", inv, err)
	}
}

// TestParseAgentHook covers `ike agent-hook <event>` (#2843): the subcommand
// only counts as the first argument and takes exactly one event name.
func TestParseAgentHook(t *testing.T) {
	inv, err := Parse([]string{"agent-hook", "SessionStart"})
	if err != nil || inv.AgentHook != "SessionStart" || len(inv.Targets) != 0 {
		t.Errorf("Parse(agent-hook SessionStart) = %+v, %v", inv, err)
	}
	for _, args := range [][]string{{"agent-hook"}, {"agent-hook", ""}, {"agent-hook", "A", "B"}} {
		if _, err := Parse(args); err == nil {
			t.Errorf("Parse(%q) accepted", args)
		}
	}
	inv, err = Parse([]string{"main.go", "agent-hook"})
	if err != nil || inv.AgentHook != "" || len(inv.Targets) != 2 {
		t.Errorf("agent-hook after a path must stay a path: %+v, %v", inv, err)
	}
}

// TestParseComplexity covers `ike complexity …` (#2922): flags in both the
// `--flag N` and `--flag=N` forms, --diff's optional `=REV`, any number of
// paths (#2936), and the usage errors cmd/ike turns into exit 2.
func TestParseComplexity(t *testing.T) {
	cases := []struct {
		args []string
		want ComplexityArgs
	}{
		{nil, ComplexityArgs{}},
		{[]string{"."}, ComplexityArgs{Paths: []string{"."}}},
		{[]string{"--json", "src"}, ComplexityArgs{JSON: true, Paths: []string{"src"}}},
		{[]string{"--diff"}, ComplexityArgs{Diff: true}},
		{[]string{"--diff=origin/main", "--json"}, ComplexityArgs{Diff: true, Rev: "origin/main", JSON: true}},
		{[]string{"--diff", "src"}, ComplexityArgs{Diff: true, Paths: []string{"src"}}},
		// Several paths, files and directories mixed, in order (#2936).
		{[]string{"a.go", "b.go"}, ComplexityArgs{Paths: []string{"a.go", "b.go"}}},
		{[]string{"--json", "pkg/", "--min", "3", "main.go", "-"}, ComplexityArgs{JSON: true, Min: 3, Paths: []string{"pkg/", "main.go", "-"}}},
		{[]string{"--diff=main", "x.go", "x.go"}, ComplexityArgs{Diff: true, Rev: "main", Paths: []string{"x.go", "x.go"}}},
		{[]string{"--diff=main", "--by", "file"}, ComplexityArgs{Diff: true, Rev: "main", By: "file"}},
		{[]string{"--min", "20"}, ComplexityArgs{Min: 20}},
		{[]string{"--min=20", "--ccn", "10", "--cog=12"}, ComplexityArgs{Min: 20, CCN: 10, Cog: 12}},
		{[]string{"--cmt", "40", "--json"}, ComplexityArgs{Cmt: 40, JSON: true}},
		{[]string{"--cmt=100"}, ComplexityArgs{Cmt: 100}},
		{[]string{"--by", "class"}, ComplexityArgs{By: "class"}},
		{[]string{"--by=file", "--json", "src"}, ComplexityArgs{By: "file", JSON: true, Paths: []string{"src"}}},
		// --brief[=class] (#2938): the level only after "=".
		{[]string{"--brief"}, ComplexityArgs{Brief: "file"}},
		{[]string{"--brief=class", "--json"}, ComplexityArgs{Brief: "class", JSON: true}},
		{[]string{"--brief=file", "--min", "5"}, ComplexityArgs{Brief: "file", Min: 5}},
		{[]string{"--brief", "class"}, ComplexityArgs{Brief: "file", Paths: []string{"class"}}},
		{[]string{"--no-tests", "--brief"}, ComplexityArgs{NoTests: true, Brief: "file"}},
		{[]string{"--sites", "--json"}, ComplexityArgs{Sites: true, JSON: true}},
		// --churn[=DAYS] (#2957): the window only after "=".
		{[]string{"--churn"}, ComplexityArgs{Churn: true}},
		{[]string{"--churn=30", "--by", "file"}, ComplexityArgs{Churn: true, ChurnDays: 30, By: "file"}},
		{[]string{"--churn", "30"}, ComplexityArgs{Churn: true, Paths: []string{"30"}}},
		// --baseline[=PATH] / --no-baseline / --write-baseline (#2958): the
		// path only after "=".
		{[]string{"--baseline"}, ComplexityArgs{Baseline: true}},
		{[]string{"--baseline=debt.json", "--json"}, ComplexityArgs{Baseline: true, BaselinePath: "debt.json", JSON: true}},
		{[]string{"--baseline", "debt.json"}, ComplexityArgs{Baseline: true, Paths: []string{"debt.json"}}},
		{[]string{"--no-baseline", "--by", "file"}, ComplexityArgs{NoBaseline: true, By: "file"}},
		{[]string{"--write-baseline", "--baseline=debt.json"}, ComplexityArgs{WriteBaseline: true, Baseline: true, BaselinePath: "debt.json"}},
		// --pins (#2959).
		{[]string{"--pins", "--json"}, ComplexityArgs{Pins: true, JSON: true}},
		{[]string{"--help"}, ComplexityArgs{Help: true}},
		{[]string{"-h"}, ComplexityArgs{Help: true}},
	}
	for _, c := range cases {
		inv, err := Parse(append([]string{"complexity"}, c.args...))
		if err != nil {
			t.Errorf("Parse(complexity %q) error: %v", c.args, err)
			continue
		}
		if inv.Complexity == nil || !reflect.DeepEqual(*inv.Complexity, c.want) || len(inv.Targets) != 0 {
			t.Errorf("Parse(complexity %q) = %+v, want %+v", c.args, inv.Complexity, c.want)
		}
	}
	for _, args := range [][]string{
		{"--min"}, {"--min", "x"}, {"--min=0"}, {"--ccn=-1"}, {"--cog", ""}, {"--cmt=0"}, {"--cmt", "101"}, {"--cmt"},
		{"--diff="}, {"--bogus"}, {"a", "--bogus"},
		{"--by"}, {"--by", "method"}, {"--by="}, {"--by=class", "--diff"}, {"--diff=main", "--by", "class"},
		{"--brief=method"}, {"--brief="},
		{"--churn=0"}, {"--churn="}, {"--churn=x"}, {"--churn=3651"},
		{"--baseline="}, {"--baseline=  "},
	} {
		if _, err := Parse(append([]string{"complexity"}, args...)); err == nil {
			t.Errorf("Parse(complexity %q) accepted", args)
		}
	}
	for _, c := range []struct {
		args []string
		want error
	}{
		{[]string{"--brief", "--by", "file"}, ErrBriefWithBy},
		{[]string{"--by=class", "--brief=class"}, ErrBriefWithBy},
		{[]string{"--brief", "--diff"}, ErrBriefWithDiff},
		{[]string{"--diff=main", "--brief=class"}, ErrBriefWithDiff},
		{[]string{"--churn", "--diff"}, ErrChurnWithDiff},
		{[]string{"--diff=main", "--churn=30"}, ErrChurnWithDiff},
		{[]string{"--baseline", "--no-baseline"}, ErrBaselineWithNoBaseline},
		{[]string{"--no-baseline", "--baseline=x.json"}, ErrBaselineWithNoBaseline},
		{[]string{"--write-baseline", "--diff"}, ErrWriteBaselineWithDiff},
		{[]string{"--diff=main", "--write-baseline"}, ErrWriteBaselineWithDiff},
		{[]string{"--write-baseline", "--no-baseline"}, ErrWriteBaselineWithNoBaseline},
		{[]string{"--pins", "--diff"}, ErrPinsWithDiff},
		{[]string{"--by", "file", "--pins"}, ErrPinsWithDiff},
		{[]string{"--pins", "--brief"}, ErrPinsWithDiff},
	} {
		if _, err := Parse(append([]string{"complexity"}, c.args...)); err != c.want {
			t.Errorf("Parse(complexity %q) error %v, want %v", c.args, err, c.want)
		}
	}
	inv, err := Parse([]string{"main.go", "complexity"})
	if err != nil || inv.Complexity != nil || len(inv.Targets) != 2 {
		t.Errorf("complexity after a path must stay a path: %+v, %v", inv, err)
	}
}
