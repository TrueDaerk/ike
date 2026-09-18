package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/intention"
	ilsp "ike/internal/lsp"
	"ike/internal/telemetry"
)

// codeactions_telemetry_test.go covers the kinds half of the palette funnel
// (#2635): a third of the intention popup's opens ended in esc, and the log
// could not say what had been on offer — two low-value entries or a real
// choice. The "!" mode's events now carry the offered kinds and, on a pick,
// the chosen row's kind; no other mode does.

// actionSecrets are the strings from the offer that must never surface in an
// event: the titles and the file path. The kind identifiers are the only part
// of an action that is a closed vocabulary rather than the user's code.
var actionSecrets = []string{"Organize imports", "Fix undeclared", "Copy file path", "userSecret", "a.go", "/proj"}

// kindedOffer is a mixed offer: two server quickfixes (one preferred) and an
// organize-imports, with titles chosen so a leak is unmistakable.
func kindedOffer() ilsp.CodeActionsMsg {
	return ilsp.CodeActionsMsg{
		Path: "/proj/a.go",
		Actions: []ilsp.CodeActionChoice{
			{Title: "Organize imports", Kind: "source.organizeImports"},
			{Title: "Fix undeclared name userSecret", Kind: "quickfix", Preferred: true},
			{Title: "Fix undeclared name userSecret (all)", Kind: "quickfix"},
		},
		Apply: func(int) tea.Cmd { return nil },
	}
}

// kindedActionsModel opens the intention popup on the mixed offer plus one of
// ike's own intentions, on an isolated telemetry model.
func kindedActionsModel(t *testing.T) Model {
	t.Helper()
	m := telemetryModel(t, host.MapConfig{})
	m.actions.SetMerged(kindedOffer(), []intention.Item{
		{Title: "Copy file path", Kind: "copy", CommandID: "tm.fire"},
	})
	m.palette.SetSize(100, 40)
	m.palette.OpenLocked(m.paletteContext(), actionsPrefix)
	return m
}

// wantKinds is the summary the mixed offer must produce: sorted, counted, and
// built from kind identifiers only.
const wantKinds = "builtin,quickfix*2,source.organizeImports"

// TestActionsDismissRecordsOfferedKinds is the acceptance criterion for the
// dismiss half: esc out of the popup says which kinds were rejected.
func TestActionsDismissRecordsOfferedKinds(t *testing.T) {
	m := kindedActionsModel(t)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)

	got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
	}
	d := got[0].Data
	if d["mode"] != string(actionsPrefix) {
		t.Errorf("mode = %q, want %q", d["mode"], string(actionsPrefix))
	}
	if d["kinds"] != wantKinds {
		t.Errorf("kinds = %q, want %q", d["kinds"], wantKinds)
	}
	if _, ok := d["picked_kind"]; ok {
		t.Errorf("a dismissal picked nothing, so it must not carry picked_kind: %v", d)
	}
	assertNoActionText(t, d)
}

// TestActionsPickRecordsPickedKind is the pick half: the chosen row's own kind
// travels next to the offer's, so "which kinds get picked out of which offers"
// becomes answerable.
func TestActionsPickRecordsPickedKind(t *testing.T) {
	m := kindedActionsModel(t)
	// The second row: the preferred quickfix sorts first, so this is the
	// organize-imports action — provably not a constant.
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)

	got := eventsOf(usageEvents(t, m), telemetry.TypePalettePick)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePalettePick, got)
	}
	d := got[0].Data
	if d["kinds"] != wantKinds {
		t.Errorf("kinds = %q, want %q", d["kinds"], wantKinds)
	}
	if d["picked_kind"] != "source.organizeImports" {
		t.Errorf("picked_kind = %q, want source.organizeImports", d["picked_kind"])
	}
	if d["rank"] != "1" {
		t.Errorf("rank = %q, want \"1\" — the second row was picked", d["rank"])
	}
	assertNoActionText(t, d)
}

// A built-in intention reports the "builtin" marker rather than ike's own kind
// vocabulary, which would drown the server kinds the ranking question is about.
func TestActionsPickOfBuiltinRecordsMarker(t *testing.T) {
	m := kindedActionsModel(t)
	for i := 0; i < 3; i++ {
		tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = tm.(Model)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)

	got := eventsOf(usageEvents(t, m), telemetry.TypePalettePick)
	if len(got) != 1 {
		t.Fatalf("want one %s event, got %v", telemetry.TypePalettePick, got)
	}
	if k := got[0].Data["picked_kind"]; k != "builtin" {
		t.Errorf("picked_kind = %q, want builtin", k)
	}
}

// A server action with no kind at all stays visible as "none": "offered
// something unclassified" must not read as "offered nothing". A kind that is
// not an identifier — a server building one out of the user's code — collapses
// to "other" instead of travelling in clear text.
func TestActionsKindTokensAreAClosedVocabulary(t *testing.T) {
	for name, tc := range map[string]struct {
		kind string
		want string
	}{
		"kindless": {kind: "", want: "none"},
		"freetext": {kind: "fix userSecret in a.go", want: "other"},
		"overlong": {kind: strings.Repeat("x", kindLeakProbeLen), want: "other"},
	} {
		t.Run(name, func(t *testing.T) {
			m := telemetryModel(t, host.MapConfig{})
			m.actions.SetMerged(ilsp.CodeActionsMsg{
				Path:    "/proj/a.go",
				Actions: []ilsp.CodeActionChoice{{Title: "Fix undeclared name userSecret", Kind: tc.kind}},
				Apply:   func(int) tea.Cmd { return nil },
			}, nil)
			m.palette.SetSize(100, 40)
			m.palette.OpenLocked(m.paletteContext(), actionsPrefix)
			tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			m = tm.(Model)

			got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
			if len(got) != 1 {
				t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
			}
			if k := got[0].Data["kinds"]; k != tc.want {
				t.Errorf("kinds = %q, want %q", k, tc.want)
			}
			assertNoActionText(t, got[0].Data)
		})
	}
}

// kindLeakProbeLen is longer than any kind identifier the recorder lets
// through verbatim, so the overlong case provably hits the cap.
const kindLeakProbeLen = 64

// Every other palette mode is unchanged (#2635): its rows carry no Kind, so
// neither field is written at all — a reader can tell "no kinds recorded" from
// "this mode has none".
func TestOtherPaletteModesRecordNoKinds(t *testing.T) {
	for _, prefix := range []rune{':', '@'} {
		t.Run(string(prefix), func(t *testing.T) {
			m := telemetryModel(t, host.MapConfig{})
			m.palette.SetSize(100, 40)
			m.palette.OpenLocked(m.paletteContext(), prefix)
			tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			m = tm.(Model)

			got := eventsOf(usageEvents(t, m), telemetry.TypePaletteDismiss)
			if len(got) != 1 {
				t.Fatalf("want one %s event, got %v", telemetry.TypePaletteDismiss, got)
			}
			d := got[0].Data
			if _, ok := d["kinds"]; ok {
				t.Errorf("mode %q must record no kinds, got %v", string(prefix), d)
			}
			if _, ok := d["picked_kind"]; ok {
				t.Errorf("mode %q must record no picked_kind, got %v", string(prefix), d)
			}
		})
	}
}

// assertNoActionText is the clear-text scan: no row title, no file path and no
// identifier out of the offer may appear anywhere in the payload.
func assertNoActionText(t *testing.T, data map[string]string) {
	t.Helper()
	for k, v := range data {
		for _, secret := range actionSecrets {
			if strings.Contains(strings.ToLower(v), strings.ToLower(secret)) {
				t.Errorf("payload %q = %q leaks %q", k, v, secret)
			}
		}
	}
}
