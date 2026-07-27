package lsp

import "testing"

func diagFor(source, code, msg string) Diagnostic {
	return Diagnostic{Source: source, Code: code, Message: msg}
}

func TestIgnoreRulesMatch(t *testing.T) {
	cases := []struct {
		name string
		rule string
		diag Diagnostic
		want bool
	}{
		{"bare code", "P1006", diagFor("intelephense", "P1006", "Expected type 'array'. Found 'null'."), true},
		{"bare code case-insensitive", "p1006", diagFor("intelephense", "P1006", "x"), true},
		{"bare code mismatch", "P1006", diagFor("intelephense", "P1005", "x"), false},
		{"explicit code", "code=reportGeneralTypeIssues", diagFor("pyright", "reportGeneralTypeIssues", "x"), true},
		{"source and code", "source=intelephense code=P1006", diagFor("intelephense", "P1006", "x"), true},
		{"source mismatch", "source=pyright code=P1006", diagFor("intelephense", "P1006", "x"), false},
		{"code glob", "code=report*", diagFor("pyright", "reportUnusedImport", "x"), true},
		{"msg glob", "msg=*has no type*", diagFor("s", "c", "variable $x has no type specified"), true},
		{"msg glob mismatch", "msg=*has no type*", diagFor("s", "c", "undefined variable"), false},
		{"msg keeps spaces", "code=P1006 msg=*Found 'null'*", diagFor("intelephense", "P1006", "Expected type 'array'. Found 'null'."), true},
		{"msg condition unmet", "code=P1006 msg=*Found 'null'*", diagFor("intelephense", "P1006", "Expected type 'int'. Found 'string'."), false},
		{"missing code never matches code rule", "code=P1006", diagFor("intelephense", "", "x"), false},
		{"empty rule matches nothing", "   ", diagFor("s", "c", "m"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := CompileIgnoreRules([]string{tc.rule})
			if got := rs.Match(tc.diag); got != tc.want {
				t.Fatalf("rule %q vs %+v: got %v, want %v", tc.rule, tc.diag, got, tc.want)
			}
		})
	}
}

func TestIgnoreRulesFilter(t *testing.T) {
	rs := CompileIgnoreRules([]string{"source=intelephense code=P1006"})
	in := []Diagnostic{
		diagFor("intelephense", "P1006", "a"),
		diagFor("intelephense", "P1005", "b"),
		diagFor("gopls", "P1006", "c"),
	}
	out := rs.Filter(in)
	if len(out) != 2 || out[0].Message != "b" || out[1].Message != "c" {
		t.Fatalf("Filter kept %+v", out)
	}
	// No rules: same slice back, no copy.
	var none IgnoreRules
	if got := none.Filter(in); len(got) != 3 {
		t.Fatalf("empty rules filtered: %+v", got)
	}
	// No hits: input returned unchanged.
	miss := CompileIgnoreRules([]string{"code=nope"})
	if got := miss.Filter(in); len(got) != 3 {
		t.Fatalf("miss rules filtered: %+v", got)
	}
}

func TestIgnoreRuleForDiagnostic(t *testing.T) {
	cases := []struct {
		diag Diagnostic
		want string
	}{
		{diagFor("intelephense", "P1006", "m"), "source=intelephense code=P1006"},
		{diagFor("", "P1006", "m"), "code=P1006"},
		{diagFor("intelephense", "", "Expected type 'array'. Found 'null'."), "source=intelephense msg=Expected type 'array'. Found 'null'."},
		{diagFor("", "", "boom"), "msg=boom"},
	}
	for _, tc := range cases {
		got := IgnoreRuleFor(tc.diag)
		if got != tc.want {
			t.Fatalf("IgnoreRuleFor(%+v) = %q, want %q", tc.diag, got, tc.want)
		}
		rs := CompileIgnoreRules([]string{got})
		if !rs.Match(tc.diag) {
			t.Fatalf("generated rule %q does not match its own diagnostic", got)
		}
	}
}
