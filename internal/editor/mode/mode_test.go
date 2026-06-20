package mode

import "testing"

func TestModeStringAndPredicates(t *testing.T) {
	cases := []struct {
		m         Mode
		s         string
		visual    bool
		capturing bool
	}{
		{Normal, "NORMAL", false, false},
		{Insert, "INSERT", false, true},
		{Visual, "VISUAL", true, false},
		{VisualLine, "V-LINE", true, false},
		{VisualBlock, "V-BLOCK", true, false},
		{CommandLine, "COMMAND", false, true},
		{Replace, "REPLACE", false, true},
	}
	for _, c := range cases {
		if c.m.String() != c.s {
			t.Errorf("%d String=%q want %q", c.m, c.m.String(), c.s)
		}
		if c.m.IsVisual() != c.visual {
			t.Errorf("%s IsVisual=%v want %v", c.s, c.m.IsVisual(), c.visual)
		}
		if c.m.Capturing() != c.capturing {
			t.Errorf("%s Capturing=%v want %v", c.s, c.m.Capturing(), c.capturing)
		}
	}
}

func TestPendingCountAndOperator(t *testing.T) {
	var p Pending
	if !p.Empty() {
		t.Fatal("fresh pending should be empty")
	}
	p.PushDigit(2)
	p.PushDigit(3)
	if p.EffectiveCount() != 23 {
		t.Fatalf("count=%d want 23", p.EffectiveCount())
	}
	p.SetOperator('d')
	if !p.HasOperator() || p.Empty() {
		t.Fatal("operator not recorded")
	}
	p.Reset()
	if !p.Empty() || p.EffectiveCount() != 1 {
		t.Fatalf("reset failed: %+v eff=%d", p, p.EffectiveCount())
	}
}

func TestPendingRegisterCapture(t *testing.T) {
	var p Pending
	p.BeginRegister()
	if !p.AwaitingRegister() {
		t.Fatal("should await register after BeginRegister")
	}
	p.SetRegister('a')
	if p.AwaitingRegister() || p.Register != 'a' {
		t.Fatalf("register=%q awaiting=%v", p.Register, p.AwaitingRegister())
	}
}
