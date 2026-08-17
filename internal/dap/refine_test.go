package dap

import (
	"encoding/json"
	"testing"
)

// startSession runs the handshake against a fake adapter advertising caps.
func startSession(t *testing.T, caps map[string]any) (*Session, *fakeAdapter) {
	t.Helper()
	pipe, fa := startFake(t)
	fa.caps = caps
	s := NewSession(NewConn(pipe, nil))
	t.Cleanup(s.Close)
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	return s, fa
}

// TestBreakpointRefinementsRoundTrip sends condition, hit count and log
// message to an adapter that advertises all three capabilities (#1914).
func TestBreakpointRefinementsRoundTrip(t *testing.T) {
	s, fa := startSession(t, map[string]any{
		"supportsConditionalBreakpoints":    true,
		"supportsHitConditionalBreakpoints": true,
		"supportsLogPoints":                 true,
	})
	if !s.SupportsConditionalBreakpoints() || !s.SupportsHitConditionalBreakpoints() || !s.SupportsLogPoints() {
		t.Fatal("capabilities not decoded from initialize response")
	}
	_, err := s.SetBreakpoints("/p/a.go", []SourceBreakpoint{
		{Line: 6, Condition: "i > 3", HitCondition: "5", LogMessage: "i is {i}"},
		{Line: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := fa.lastBreakpoints()
	if len(got) != 2 {
		t.Fatalf("adapter saw %d breakpoints, want 2", len(got))
	}
	want := SourceBreakpoint{Line: 7, Condition: "i > 3", HitCondition: "5", LogMessage: "i is {i}"}
	if got[0] != want {
		t.Fatalf("adapter saw %+v, want %+v", got[0], want)
	}
	if got[1] != (SourceBreakpoint{Line: 10}) {
		t.Fatalf("plain breakpoint arrived as %+v", got[1])
	}
}

// TestBreakpointRefinementsStripped verifies unsupported refinements are
// stripped rather than sent — the breakpoint itself still arrives (#1914).
func TestBreakpointRefinementsStripped(t *testing.T) {
	s, fa := startSession(t, map[string]any{"supportsConfigurationDoneRequest": true})
	_, err := s.SetBreakpoints("/p/a.php", []SourceBreakpoint{
		{Line: 3, Condition: "x", HitCondition: "2", LogMessage: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := fa.lastBreakpoints()
	if len(got) != 1 || got[0] != (SourceBreakpoint{Line: 4}) {
		t.Fatalf("adapter saw %+v, want the bare line 4", got)
	}
}

// TestEvaluate drives the watch-expression request (#1914).
func TestEvaluate(t *testing.T) {
	s, fa := startSession(t, nil)
	res, err := s.Evaluate("x+1", 11, "watch")
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != "42" || res.Type != "int" || res.VariablesReference != 0 {
		t.Fatalf("evaluate result = %+v", res)
	}
	var args struct {
		Expression string `json:"expression"`
		FrameID    int    `json:"frameId"`
		Context    string `json:"context"`
	}
	fa.mu.Lock()
	raw := fa.lastEval
	fa.mu.Unlock()
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	if args.Expression != "x+1" || args.FrameID != 11 || args.Context != "watch" {
		t.Fatalf("adapter saw evaluate args %+v", args)
	}
}
