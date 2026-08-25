package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// forge_test.go covers the Forge settings page (#2085): the interval entry is
// present and editable, 0 (polling off) commits, a value inside the 1–9 hole
// is rejected in the form with the rule spelled out, and the steppers jump
// that hole instead of stopping inside it.

// forgePages is the real Forge page, isolated for the form tests.
func forgePages() []Page {
	for _, p := range BasePages(nil, nil, nil) {
		if p.Title == "Forge" {
			return []Page{p}
		}
	}
	return nil
}

func TestForgePageExposesThePollInterval(t *testing.T) {
	pages := forgePages()
	if len(pages) != 1 {
		t.Fatal("the settings schema has no Forge page")
	}
	e := pages[0].Entries[0]
	if e.Key != "forge.poll_interval_seconds" {
		t.Fatalf("entry = %q, want forge.poll_interval_seconds", e.Key)
	}
	if e.Type != Int {
		t.Errorf("type = %v, want Int", e.Type)
	}
	if e.Min != 0 || e.Max != config.ForgePollMaxSeconds {
		t.Errorf("range = %d–%d, want 0–%d", e.Min, e.Max, config.ForgePollMaxSeconds)
	}
	if e.ValidateInt == nil {
		t.Fatal("the entry needs the strict form check for the 0 / 10+ split")
	}
	if msg := e.ValidateInt(0); msg != "" {
		t.Errorf("0 rejected with %q, want it accepted as \"polling off\"", msg)
	}
	if msg := e.ValidateInt(5); msg == "" {
		t.Error("5 must be rejected: it is neither off nor a sane interval")
	}
	if msg := e.ValidateInt(config.ForgePollMinSeconds); msg != "" {
		t.Errorf("the floor itself was rejected with %q", msg)
	}
	if !strings.Contains(e.Description, "0 turns polling off") {
		t.Errorf("the description must document the 0 = off rule: %q", e.Description)
	}
}

// forgePanel opens a panel on the real Forge page with its entry selected in
// the detail column.
func forgePanel(t *testing.T) *Model {
	t.Helper()
	restoreConfig(t)
	m := New(forgePages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.focus = formColumn
	for i, r := range m.rows() {
		if r.kind == rowEntry && r.entry.Key == "forge.poll_interval_seconds" {
			m.sel = i
		}
	}
	m.Update(key("enter")) // focus the detail column's stepper
	if _, ok := m.editor.(*intEditor); !ok {
		t.Fatalf("editor = %T, want the int stepper", m.editor)
	}
	return m
}

func TestForgeIntervalCommits(t *testing.T) {
	m := forgePanel(t)
	m.editor.(*intEditor).tf.Set("45")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Forge.PollIntervalSeconds; got != 45 {
		t.Fatalf("committed = %d, want 45", got)
	}
}

func TestForgeIntervalZeroDisablesPolling(t *testing.T) {
	m := forgePanel(t)
	m.editor.(*intEditor).tf.Set("0")
	m.Update(key("enter"))
	if ed := m.editor.(*intEditor); ed.err != "" {
		t.Fatalf("0 rejected with %q — it is the documented off switch", ed.err)
	}
	commit(t, m)
	if got := config.Get().Forge.PollIntervalSeconds; got != 0 {
		t.Fatalf("committed = %d, want 0 (polling off)", got)
	}
}

func TestForgeIntervalBelowFloorRejectedInTheForm(t *testing.T) {
	m := forgePanel(t)
	ed := m.editor.(*intEditor)
	ed.tf.Set("5")
	m.Update(key("enter"))
	if ed.err == "" {
		t.Fatal("a 5s interval must be refused in the form, not silently snapped")
	}
	if !strings.Contains(ed.err, "10") {
		t.Errorf("the rejection must name the floor: %q", ed.err)
	}
	if len(m.changes) != 0 {
		t.Errorf("a rejected value must not stage a change (%d staged)", len(m.changes))
	}
}

func TestForgeIntervalStepperJumpsTheHole(t *testing.T) {
	m := forgePanel(t)
	ed := m.editor.(*intEditor)
	ed.tf.Set("0")
	m.Update(key("right"))
	if got := ed.tf.text; got != "10" {
		t.Fatalf("stepping up from 0 landed on %q, want the 10s floor", got)
	}
	m.Update(key("left"))
	if got := ed.tf.text; got != "0" {
		t.Fatalf("stepping down from 10 landed on %q, want 0 (off)", got)
	}
	ed.tf.Set("10")
	m.Update(key("right"))
	if got := ed.tf.text; got != "11" {
		t.Fatalf("stepping above the floor landed on %q, want 11", got)
	}
}

func TestSnapIntLeavesPlainEntriesAlone(t *testing.T) {
	plain := Entry{Key: "editor.tab_width", Type: Int, Min: 1, Max: 16}
	if got := snapInt(plain, 5, 1); got != 5 {
		t.Errorf("snapInt without a hook = %d, want 5 unchanged", got)
	}
	gapped := Entry{Type: Int, Min: 0, Max: 3600, ValidateInt: forgePollValidate}
	if got := snapInt(gapped, 5, 0); got != 5 {
		t.Errorf("a zero delta = %d, want 5 unchanged", got)
	}
}
