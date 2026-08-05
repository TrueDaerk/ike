package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/register"
)

// shared_registers_test.go covers the app-wide register store (#1540): two
// editors handed the same store via SetRegisters share yanks, the history
// ring and named registers, vim style.

func TestSharedRegistersAcrossEditors(t *testing.T) {
	shared := register.New()
	a, _ := loaded(t, "alpha\n")
	a.SetRegisters(shared)
	b, _ := loaded(t, "beta\n")
	b.SetRegisters(shared)

	// yy in a lands in the shared unnamed register and history…
	for _, k := range keys("yy") {
		a, _ = a.Update(k)
	}
	// …so p in b pastes a's line.
	for _, k := range keys("p") {
		b, _ = b.Update(k)
	}
	if !strings.Contains(b.Text(), "alpha") {
		t.Fatalf("paste in b must see a's yank, text = %q", b.Text())
	}
	if h := b.RegisterHistory(); len(h) == 0 || !strings.HasPrefix(h[0].Text, "alpha") {
		t.Fatalf("b must see the shared history, got %v", h)
	}

	// Named registers share the same way: "qyy in b, "qp in a.
	b.SetCursor(0, 0)
	for _, k := range keys(`"qyy`) {
		b, _ = b.Update(k)
	}
	for _, k := range keys(`"qp`) {
		a, _ = a.Update(k)
	}
	if strings.Count(a.Text(), "beta") != 1 {
		t.Fatalf(`"q must carry b's yank into a, text = %q`, a.Text())
	}
}

func TestSetRegistersNilKeepsPrivateStore(t *testing.T) {
	m, _ := loaded(t, "x\n")
	before := m.regs
	m.SetRegisters(nil)
	if m.regs != before {
		t.Fatal("nil must keep the private store")
	}
}
