package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/nethint"
)

// nethints_test.go covers the editor half of the network-literal hints
// (#1653): each family rides the stand-in channel, so it draws off the caret,
// disappears while the literal is edited, and switches on its own toggle. The
// spans are synthetic; the parsing and context detection live in
// internal/nethint.

const (
	cidrHint = "10.0.0.0–10.255.255.255, 16,777,214 hosts"
	idnHint  = "münchen.de"
)

// netHinted loads a buffer whose first line carries a CIDR prefix and whose
// second carries a punycode host, and delivers both hint spans.
func netHinted(t *testing.T, idnCapture string) Model {
	t.Helper()
	m, path := mdLoaded(t, "net = 10.0.0.0/8\nhost = xn--mnchen-3ya.de\nplain\n")
	m.cursor = buffer.Position{Line: 2}
	spans := []highlight.Span{
		{
			Line: 0, StartCol: 6, EndCol: 16,
			Capture: nethint.CIDRCapture, Replace: "10.0.0.0/8" + nethint.Gap + cidrHint,
		},
		{
			Line: 1, StartCol: 7, EndCol: 24,
			Capture: idnCapture, Replace: "xn--mnchen-3ya.de" + nethint.Gap + idnHint,
		},
	}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

// TestNetHintsRender: off the caret line both literals draw with their
// reading appended, the raw literal still visible.
func TestNetHintsRender(t *testing.T) {
	view := plainView(netHinted(t, nethint.IDNCapture))
	for _, want := range []string{"10.0.0.0/8", cidrHint, "xn--mnchen-3ya.de", idnHint} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

// TestNetHintsCaretReveals (#1594 mechanic): the caret inside a literal drops
// that literal's hint and leaves the other one alone.
func TestNetHintsCaretReveals(t *testing.T) {
	m := netHinted(t, nethint.IDNCapture)
	m.cursor = buffer.Position{Line: 0, Col: 8}
	view := plainView(m)
	if strings.Contains(view, cidrHint) {
		t.Error("the caret inside the prefix must hide its hint")
	}
	if !strings.Contains(view, idnHint) {
		t.Error("the caret on another line must not hide the IDN hint")
	}
}

// TestNetHintsToggles: the two families switch independently of each other.
func TestNetHintsToggles(t *testing.T) {
	m := netHinted(t, nethint.IDNCapture)
	m, _ = m.Update(ActionMsg{Action: "toggle_cidr_hints"})
	view := plainView(m)
	if strings.Contains(view, cidrHint) {
		t.Error("toggle off must drop the CIDR hint")
	}
	if !strings.Contains(view, idnHint) {
		t.Error("the CIDR toggle must not reach the IDN family")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_idn_hints"})
	if view := plainView(m); strings.Contains(view, idnHint) {
		t.Error("toggle off must drop the IDN hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_cidr_hints"})
	if view := plainView(m); !strings.Contains(view, cidrHint) {
		t.Error("toggling back on must restore the CIDR hint")
	}
}

// TestNetHintsMixedSharesToggle: the homograph capture is the same family
// drawn in the warning colour, so editor.idn_hints gates it too.
func TestNetHintsMixedSharesToggle(t *testing.T) {
	m := netHinted(t, nethint.IDNMixedCapture)
	if view := plainView(m); !strings.Contains(view, idnHint) {
		t.Error("a homograph hint must render like any other IDN hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_idn_hints"})
	if view := plainView(m); strings.Contains(view, idnHint) {
		t.Error("the IDN toggle must gate the homograph capture too")
	}
}

// TestNetHintsMixedWarnColor: a homograph draws in the theme's warning
// colour, an ordinary decode does not.
func TestNetHintsMixedWarnColor(t *testing.T) {
	m := netHinted(t, nethint.IDNMixedCapture)
	warn := lipgloss.NewStyle().Foreground(m.theme().Warning).GetForeground()
	st, ok := m.styleAt(1, 8)
	if !ok {
		t.Fatal("the homograph capture must resolve to a style")
	}
	if st.GetForeground() != warn {
		t.Errorf("homograph foreground = %v, want the warning colour %v", st.GetForeground(), warn)
	}
	plain := netHinted(t, nethint.IDNCapture)
	if st, ok := plain.styleAt(1, 8); ok && st.GetForeground() == warn {
		t.Error("an ordinary IDN decode must not draw in the warning colour")
	}
}

// TestNetHintsConfigDefaults: editor.cidr_hints / editor.idn_hints drive the
// initial state, and a view toggle overrides them from then on.
func TestNetHintsConfigDefaults(t *testing.T) {
	m := netHinted(t, nethint.IDNCapture)
	m.Configure(host.MapConfig{"editor.cidr_hints": "false", "editor.idn_hints": "false"})
	if view := plainView(m); strings.Contains(view, cidrHint) || strings.Contains(view, idnHint) {
		t.Error("the config defaults must hide both hints")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_cidr_hints"})
	m, _ = m.Update(ActionMsg{Action: "noop"})
	view := plainView(m)
	if !strings.Contains(view, cidrHint) {
		t.Error("the view toggle must win over the config default, and survive a refresh")
	}
	if strings.Contains(view, idnHint) {
		t.Error("editor.idn_hints=false must still hide the IDN hint")
	}
}

// TestNetHintsIndependentOfOtherToggles: the families ride their own channels
// — neither the markdown nor the cron switch reaches them.
func TestNetHintsIndependentOfOtherToggles(t *testing.T) {
	m := netHinted(t, nethint.IDNCapture)
	m, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"})
	m, _ = m.Update(ActionMsg{Action: "toggle_cron_hints"})
	if view := plainView(m); !strings.Contains(view, cidrHint) || !strings.Contains(view, idnHint) {
		t.Error("the markdown/cron toggles must not gate the network hints")
	}
}
