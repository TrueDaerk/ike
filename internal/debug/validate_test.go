package debug

import "testing"

// TestMetaKind classifies the three breakpoint shapes the gutter and the list
// draw distinct glyphs for (#2245).
func TestMetaKind(t *testing.T) {
	cases := []struct {
		name string
		meta Meta
		want Kind
	}{
		{"plain", Meta{}, KindPlain},
		{"condition", Meta{Condition: "i > 3"}, KindConditional},
		{"hit count", Meta{HitCondition: "%2"}, KindConditional},
		{"logpoint", Meta{LogMessage: "i={i}"}, KindLogpoint},
		{"conditional logpoint logs", Meta{Condition: "i > 3", LogMessage: "hi"}, KindLogpoint},
		{"blank fields are no refinement", Meta{Condition: "  ", LogMessage: "\t"}, KindPlain},
	}
	for _, c := range cases {
		if got := c.meta.Kind(); got != c.want {
			t.Errorf("%s: kind = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestKindLines splits a file's breakpoints into the subsets the gutter
// queries, disabled ones included (#2245).
func TestKindLines(t *testing.T) {
	b := NewBreakpoints()
	for _, l := range []int{1, 2, 3, 4} {
		b.Toggle("a.go", l)
	}
	b.SetMeta("a.go", 2, Meta{Condition: "x > 0"})
	b.SetMeta("a.go", 3, Meta{HitCondition: "5"})
	b.SetMeta("a.go", 4, Meta{LogMessage: "x={x}"})
	b.SetEnabled("a.go", 4, false)
	if got := b.ConditionalLines("a.go"); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("conditional lines = %v, want [2 3]", got)
	}
	if got := b.LogpointLines("a.go"); len(got) != 1 || got[0] != 4 {
		t.Fatalf("logpoint lines = %v, want [4] (a disabled logpoint keeps its shape)", got)
	}
}

// TestValidateHitCondition accepts the VS Code hit-count forms and rejects
// what an adapter would answer to with an opaque error (#2245).
func TestValidateHitCondition(t *testing.T) {
	for _, ok := range []string{"", "5", ">3", ">=3", "<10", "<=10", "==2", "=2", "%2", " > 3 "} {
		if err := ValidateHitCondition(ok); err != nil {
			t.Errorf("ValidateHitCondition(%q) = %v, want accepted", ok, err)
		}
	}
	for _, bad := range []string{"   ", "many", ">", "%", "3x", ">3x", "!=3"} {
		if err := ValidateHitCondition(bad); err == nil {
			t.Errorf("ValidateHitCondition(%q) accepted, want rejected", bad)
		}
	}
}

// TestValidateLogMessage checks the placeholder rules (#2245).
func TestValidateLogMessage(t *testing.T) {
	for _, ok := range []string{"", "plain text", "i is {i}", "{a} and {b}"} {
		if err := ValidateLogMessage(ok); err != nil {
			t.Errorf("ValidateLogMessage(%q) = %v, want accepted", ok, err)
		}
	}
	for _, bad := range []string{"  ", "i is {i", "closed }", "{{i}}", "empty {} here"} {
		if err := ValidateLogMessage(bad); err == nil {
			t.Errorf("ValidateLogMessage(%q) accepted, want rejected", bad)
		}
	}
}

// TestValidateConditionBlank rejects the empty-but-enabled field: a condition
// of spaces would reach the adapter and fail the whole breakpoint (#2245).
func TestValidateConditionBlank(t *testing.T) {
	if err := ValidateCondition(""); err != nil {
		t.Fatalf("cleared condition rejected: %v", err)
	}
	if err := ValidateCondition("i > 3"); err != nil {
		t.Fatalf("condition rejected: %v", err)
	}
	if err := ValidateCondition("   "); err == nil {
		t.Fatal("blank condition accepted, want rejected")
	}
}

// TestMetaValidate reports the first offending field of the whole set.
func TestMetaValidate(t *testing.T) {
	if err := (Meta{Condition: "ok", HitCondition: "5", LogMessage: "x={x}"}).Validate(); err != nil {
		t.Fatalf("valid meta rejected: %v", err)
	}
	if err := (Meta{HitCondition: "lots"}).Validate(); err == nil {
		t.Fatal("bad hit count accepted")
	}
}
