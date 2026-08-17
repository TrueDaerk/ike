package debug

import "testing"

// TestMetaRoundTrip persists refinements and loads them back (#1914).
func TestMetaRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	b := NewBreakpoints()
	b.Toggle("a.go", 5)
	b.Toggle("a.go", 9)
	b.SetMeta("a.go", 5, Meta{Condition: "i > 3", HitCondition: "5"})
	b.SetMeta("a.go", 9, Meta{LogMessage: "x = {x}"})
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if m := got.MetaAt("a.go", 5); m != (Meta{Condition: "i > 3", HitCondition: "5"}) {
		t.Fatalf("line 5 meta = %+v", m)
	}
	if m := got.MetaAt("a.go", 9); m != (Meta{LogMessage: "x = {x}"}) {
		t.Fatalf("line 9 meta = %+v", m)
	}
}

// TestMetaLifecycle covers set-on-missing, zero-clears and clear-on-remove.
func TestMetaLifecycle(t *testing.T) {
	b := NewBreakpoints()
	b.SetMeta("a.go", 5, Meta{Condition: "x"})
	if !b.MetaAt("a.go", 5).IsZero() {
		t.Fatal("meta stuck to a line without a breakpoint")
	}
	b.Toggle("a.go", 5)
	b.SetMeta("a.go", 5, Meta{Condition: "x"})
	b.SetMeta("a.go", 5, Meta{})
	if !b.MetaAt("a.go", 5).IsZero() {
		t.Fatal("zero meta did not clear the entry")
	}
	b.SetMeta("a.go", 5, Meta{LogMessage: "hi"})
	b.Toggle("a.go", 5) // remove the breakpoint
	b.Toggle("a.go", 5) // re-add: a fresh breakpoint is plain
	if !b.MetaAt("a.go", 5).IsZero() {
		t.Fatal("meta survived breakpoint removal")
	}
}

// TestMetaFollowsAdjustEdit shifts a refined breakpoint with an insertion
// above it — the meta must travel with the line like the disabled flag.
func TestMetaFollowsAdjustEdit(t *testing.T) {
	b := NewBreakpoints()
	b.Toggle("a.go", 10)
	b.SetMeta("a.go", 10, Meta{Condition: "n == 1"})
	b.AdjustEdit("a.go", 2, 3) // 3 lines inserted ending at line 2
	if b.Has("a.go", 10) {
		t.Fatal("breakpoint did not shift")
	}
	if m := b.MetaAt("a.go", 13); m != (Meta{Condition: "n == 1"}) {
		t.Fatalf("meta after shift = %+v, want it on line 13", m)
	}
}

// TestEnabledSpecs carries refinements only for enabled lines, sorted.
func TestEnabledSpecs(t *testing.T) {
	b := NewBreakpoints()
	b.Toggle("a.go", 7)
	b.Toggle("a.go", 3)
	b.Toggle("a.go", 5)
	b.SetMeta("a.go", 3, Meta{LogMessage: "log"})
	b.SetMeta("a.go", 5, Meta{Condition: "c"})
	b.SetEnabled("a.go", 5, false)
	specs := b.EnabledSpecs("a.go")
	if len(specs) != 2 {
		t.Fatalf("specs = %+v, want 2", specs)
	}
	if specs[0].Line != 3 || specs[0].LogMessage != "log" || specs[1].Line != 7 || !specs[1].Meta.IsZero() {
		t.Fatalf("specs = %+v", specs)
	}
}
