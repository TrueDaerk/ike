package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// TestAffordanceGlyphs guards #889/#1295: each type announces its editor with
// the wireframes' value marker.
func TestAffordanceGlyphs(t *testing.T) {
	for _, tc := range []struct {
		e    Entry
		val  string
		want string
	}{
		{Entry{Type: Bool}, "true", "true ◉"},
		{Entry{Type: Bool}, "false", "false ◉"},
		{Entry{Type: Enum}, "dark", "dark ▸"},
		{Entry{Type: Int}, "4", "4 ‹›"},
		{Entry{Type: String}, "abc", "abc ✎"},
		{Entry{Type: Chord}, "", "(unbound) ⌨"},
		{Entry{Type: List}, "[80 120]", "80, 120 ≡"},
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
	m.Update(key("+"))
	if m.value("editor.tab_width") == "" {
		t.Fatal("stepper must stage a value")
	}
	// Step far past the max: the staged value clamps and says so.
	for i := 0; i < 20; i++ {
		m.Update(key("+"))
	}
	if got := m.value("editor.tab_width"); got != "16" {
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
	m.Update(key("enter")) // focus the detail column's stepper
	m.editor.(*intEditor).tf.Set("99")
	m.Update(key("enter"))
	if !strings.Contains(m.notice, "clamped to 16") {
		t.Fatalf("notice = %q", m.notice)
	}
	apply(t, m.applyChanges())
	if got := config.Get().Editor.TabWidth; got != 16 {
		t.Fatalf("committed = %d, want 16", got)
	}
}
