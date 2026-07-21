package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestAffordanceGlyphs guards #889: each widget type announces how it edits.
func TestAffordanceGlyphs(t *testing.T) {
	for _, tc := range []struct {
		e    Entry
		val  string
		want string
	}{
		{Entry{Type: Bool}, "true", "[x]"},
		{Entry{Type: Bool}, "false", "[ ]"},
		{Entry{Type: Enum}, "dark", "‹ dark ›"},
		{Entry{Type: Int}, "4", "4 ±"},
		{Entry{Type: String}, "abc", "abc ✎"},
	} {
		if got := affordanceValue(tc.e, tc.val); got != tc.want {
			t.Errorf("affordance(%v, %q) = %q, want %q", tc.e.Type, tc.val, got, tc.want)
		}
	}
}

// TestIntStepperClampsWithNotice guards #889: +/- step the value, the range
// clamp is visible instead of silent.
func TestIntStepperClampsWithNotice(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.focus = formColumn
	m.sel = 1 // editor.tab_width, Min 1 Max 16
	apply(t, m.Update(key("+")))
	after := value("editor.tab_width")
	if after == "" {
		t.Fatal("stepper must write a value")
	}
	// Step far past the max: the write clamps and says so.
	for i := 0; i < 20; i++ {
		if cmd := m.Update(key("+")); cmd != nil {
			apply(t, cmd)
		}
	}
	if got := value("editor.tab_width"); got != "16" {
		t.Fatalf("value = %q, want the max 16", got)
	}
	m.Update(key("+")) // at the cap: no write, visible notice
	if m.notice == "" || !strings.Contains(m.notice, "16") {
		t.Fatalf("clamp must be visible, notice = %q", m.notice)
	}
}

// TestIntCommitClampNotice: typing past the range commits the clamped value
// and says so.
func TestIntCommitClampNotice(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.focus = formColumn
	m.sel = 1
	m.Update(key("enter"))
	m.edit = newTextField("99")
	apply(t, m.Update(key("enter")))
	if got := config.Get().Editor.TabWidth; got != 16 {
		t.Fatalf("committed = %d, want 16", got)
	}
	if !strings.Contains(m.notice, "clamped to 16") {
		t.Fatalf("notice = %q", m.notice)
	}
}
