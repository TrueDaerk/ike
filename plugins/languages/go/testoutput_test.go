package langgo

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// sample builds one test2json line.
func ev(action, pkg, test, output string, elapsed float64) string {
	var b strings.Builder
	b.WriteString(`{"Action":"` + action + `","Package":"` + pkg + `"`)
	if test != "" {
		b.WriteString(`,"Test":"` + test + `"`)
	}
	if output != "" {
		b.WriteString(`,"Output":"` + output + `"`)
	}
	if elapsed != 0 {
		b.WriteString(`,"Elapsed":0.12`)
	}
	b.WriteString("}\n")
	return b.String()
}

func TestParseTestJSONStatuses(t *testing.T) {
	out := ev("run", "example/pkg", "TestOK", "", 0) +
		ev("output", "example/pkg", "TestOK", "=== RUN   TestOK\\n", 0) +
		ev("pass", "example/pkg", "TestOK", "", 0.12) +
		ev("run", "example/pkg", "TestBad", "", 0) +
		ev("output", "example/pkg", "TestBad", "    x_test.go:42: boom\\n", 0) +
		ev("fail", "example/pkg", "TestBad", "", 0.12) +
		ev("run", "example/pkg", "TestSkipped", "", 0) +
		ev("skip", "example/pkg", "TestSkipped", "", 0) +
		ev("fail", "example/pkg", "", "", 0.12)
	res := parseTestJSON(out)
	if len(res) != 3 {
		t.Fatalf("results = %+v, want 3", res)
	}
	if res[0].Name != "TestOK" || res[0].Status != lang.TestPass || res[0].Elapsed != 0.12 {
		t.Fatalf("pass result = %+v", res[0])
	}
	if res[1].Name != "TestBad" || res[1].Status != lang.TestFail {
		t.Fatalf("fail result = %+v", res[1])
	}
	if res[1].File != "x_test.go" || res[1].Line != 42 {
		t.Fatalf("failure location = %q:%d, want x_test.go:42", res[1].File, res[1].Line)
	}
	if res[2].Name != "TestSkipped" || res[2].Status != lang.TestSkip {
		t.Fatalf("skip result = %+v", res[2])
	}
	if res[0].Group != "example/pkg" {
		t.Fatalf("group = %q", res[0].Group)
	}
	// The failed package-level event must not add a synthetic node — the
	// per-test failure already explains it.
	for _, r := range res {
		if r.Name == "(package failed)" {
			t.Fatalf("unexpected synthetic package node: %+v", res)
		}
	}
}

func TestParseTestJSONSubtests(t *testing.T) {
	out := ev("run", "p", "TestTable", "", 0) +
		ev("run", "p", "TestTable/case_a", "", 0) +
		ev("output", "p", "TestTable/case_a", "    x_test.go:7: nope\\n", 0) +
		ev("fail", "p", "TestTable/case_a", "", 0) +
		ev("fail", "p", "TestTable", "", 0.12)
	res := parseTestJSON(out)
	if len(res) != 2 {
		t.Fatalf("results = %+v, want 2", res)
	}
	var sub lang.TestResult
	for _, r := range res {
		if strings.Contains(r.Name, "/") {
			sub = r
		}
	}
	if sub.Name != "TestTable/case_a" || sub.RerunID != "TestTable" {
		t.Fatalf("subtest = %+v: rerun must anchor the top-level function", sub)
	}
	if sub.File != "x_test.go" || sub.Line != 7 {
		t.Fatalf("subtest location = %q:%d", sub.File, sub.Line)
	}
	if sub.Output == "" || !strings.Contains(sub.Output, "nope") {
		t.Fatalf("subtest output = %q", sub.Output)
	}
}

func TestParseTestJSONBuildFailure(t *testing.T) {
	// A compile error: non-JSON diagnostics, then a package-level fail with
	// no per-test events.
	out := "# example/broken\n" +
		"broken_test.go:5:2: undefined: nope\n" +
		ev("fail", "example/broken", "", "", 0)
	res := parseTestJSON(out)
	if len(res) != 1 {
		t.Fatalf("results = %+v, want one synthetic failure", res)
	}
	r := res[0]
	if r.Status != lang.TestFail || r.Group != "example/broken" || r.Name != "(package failed)" {
		t.Fatalf("synthetic result = %+v", r)
	}
	if !strings.Contains(r.Output, "undefined: nope") {
		t.Fatalf("output must carry the compiler diagnostics: %q", r.Output)
	}
}

func TestParseTestJSONGarbage(t *testing.T) {
	if res := parseTestJSON("total garbage\nnothing json\n"); res != nil {
		t.Fatalf("garbage must parse to nil (raw fallback), got %+v", res)
	}
	if res := parseTestJSON(""); res != nil {
		t.Fatalf("empty must parse to nil, got %+v", res)
	}
}
