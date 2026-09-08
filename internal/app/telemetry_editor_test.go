package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/telemetry"
)

// telemetryEditorModel opens a file in an editor pane on an isolated model, so
// key presses land in a focused editor.
func telemetryEditorModel(t *testing.T) Model {
	t.Helper()
	m := telemetryModel(t, host.MapConfig{})
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("alpha bravo charlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	return drainCmd(tm.(Model), cmd)
}

// unboundChords lists the chords logged as unbound in m's usage log.
func unboundChords(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["status"] == "unbound" {
			out = append(out, ev.Data["chord"])
		}
	}
	return out
}

// TestTelemetryEditorOwnedChordNotUnbound (#2303): a chord the editor handles
// itself never reaches the keymap table, but it is bound as far as the user is
// concerned — it must not be logged as a missing keybind. alt+delete (the
// macOS word kill) was the top "unbound" chord in the telemetry before this.
func TestTelemetryEditorOwnedChordNotUnbound(t *testing.T) {
	for _, mode := range []string{"normal", "insert"} {
		t.Run(mode, func(t *testing.T) {
			m := telemetryEditorModel(t)
			if mode == "insert" {
				m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
			}
			m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})

			if got := unboundChords(t, m); len(got) != 0 {
				t.Fatalf("editor-owned alt+delete logged as unbound: %v", got)
			}
			if ed := m.focusedEditor(); ed == nil || ed.Text() != "bravo charlie" {
				t.Fatalf("alt+delete did not kill the word in %s mode", mode)
			}
		})
	}
}

// TestWordChordsLanguageScopedEditor (#2313): the whole word-wise chord family
// — motion (alt+left/right), selection (alt+shift+left/right) and deletion
// (alt+backspace/alt+delete) — is editor-owned in every buffer regardless of
// its language classification, in normal, insert and visual mode. The editor
// consumes each chord and a telemetry replay records none of them as unbound.
// (The unbound events that motivated #2313 were recorded by a pre-#2303 build,
// which logged editor-owned chords as unbound even when they worked.)
func TestWordChordsLanguageScopedEditor(t *testing.T) {
	bufferLangLangs() // registers the http language, so a.http classifies
	chords := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"alt+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}},
		{"alt+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}},
		{"alt+shift+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt | tea.ModShift}},
		{"alt+shift+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt | tea.ModShift}},
		{"alt+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}},
		{"alt+delete", tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt}},
	}
	for _, mode := range []string{"normal", "insert", "visual"} {
		for _, c := range chords {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				m := telemetryModel(t, host.MapConfig{})
				path := filepath.Join(t.TempDir(), "a.http")
				if err := os.WriteFile(path, []byte("GET https://example.com/foo\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				tm, cmd := m.openPath(path, false)
				m = drainCmd(tm.(Model), cmd)
				if got := m.keyContext(); string(got) != "editor[http]" {
					t.Fatalf("keyContext=%s want editor[http]", got)
				}
				switch mode {
				case "insert":
					m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
				case "visual":
					m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"})
				}
				m = drainKey(m, c.msg)
				if !m.focusedEditor().HandledLastKey() {
					t.Errorf("editor declined %s in %s mode", c.name, mode)
				}
				if got := unboundChords(t, m); len(got) != 0 {
					t.Errorf("%s in %s mode recorded unbound: %v", c.name, mode, got)
				}
			})
		}
	}
}

// TestTelemetryUnboundChordInEditorStillRecorded keeps the missing-keybind
// signal alive: a chord neither the keymap nor the editor wants is still
// reported, even with an editor focused.
func TestTelemetryUnboundChordInEditorStillRecorded(t *testing.T) {
	m := telemetryEditorModel(t)
	// ctrl+alt+0 is bound to nothing in any preset and means nothing to the
	// editor either.
	m = drainKey(m, tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt})

	found := false
	for _, c := range unboundChords(t, m) {
		if c == "ctrl+alt+0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unbound chord not recorded in an editor; got %v", unboundChords(t, m))
	}
}

// TestTelemetryUnboundNamesDroppedDefault (#2539): a default chord the user's
// config unbound still records as unbound — the key did nothing — but the
// event names the default that used to own it, so a missing-keybind report
// can tell "never bound" from "removed by an override". The #2539 telemetry
// had editor.caret.addAbove's chord unbound in editor[json] while the default
// table binds it there in every language scope, and this is the one way the
// resolver can produce that.
func TestTelemetryUnboundNamesDroppedDefault(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.alt+shift+up": ""})
	path := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(path, []byte("{\"a\": 1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	m = drainCmd(tm.(Model), cmd)
	if m.onboardingOpen() { // the first-start LSP dialog eats keys on hosts missing a server
		m = m.closeOnboarding().(Model)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt | tea.ModShift})

	var got []telemetry.Event
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["chord"] == "alt+shift+up" {
			got = append(got, ev)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one alt+shift+up key event, got %v", got)
	}
	ev := got[0]
	if ev.Data["status"] != "unbound" || ev.Data["command"] != "editor.caret.addAbove" {
		t.Fatalf("want unbound naming the dropped default, got %v", ev.Data)
	}
	if ev.Data["context"] != "editor[json]" {
		t.Fatalf("want the language-scoped editor context, got %q", ev.Data["context"])
	}

	// A chord no default ever bound keeps a bare unbound event.
	m = drainKey(m, tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt})
	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["chord"] == "ctrl+alt+0" && ev.Data["command"] != "" {
			t.Fatalf("a never-bound chord must not name a command, got %v", ev.Data)
		}
	}
}

// TestEditorDefaultResolvesInLanguageScope pins the #2539 baseline: with no
// override, the Editor-context default is found under the language-scoped
// key context of a classified buffer — the resolver, not a host, is what the
// telemetry's editor[json] names. (The test registry carries no editor
// commands, so the lookup is asserted on the table rather than via dispatch.)
func TestEditorDefaultResolvesInLanguageScope(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	path := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(path, []byte("{\"a\": 1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.openPath(path, false)
	m = drainCmd(tm.(Model), cmd)
	if got := m.keyContext(); got != keymap.WithLang(keymap.Editor, "json") {
		t.Fatalf("key context = %q, want editor[json]", got)
	}
	chord := keymap.MustParseChord("alt+shift+up")
	b, ok := m.bindings.Table().Lookup(chord, m.keyContext())
	if !ok || b.Command != "editor.caret.addAbove" {
		t.Fatalf("alt+shift+up in editor[json] = %+v, %v; want editor.caret.addAbove", b, ok)
	}
	if m.droppedDefault(chord.Steps[0]) != "" {
		t.Fatal("nothing is dropped without an override")
	}
}
