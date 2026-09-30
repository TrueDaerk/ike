package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/finder"
	ilsp "ike/internal/lsp"
	"ike/internal/search"
)

// TestFindInPathSequenceFromPlaygroundSurvives replays the #2836 telemetry
// sequence end to end and asserts nothing panics along it: a project switch
// back onto a workspace whose jq playground holds the focus (inline or with
// the result split out, keyboard on the query line or in the result buffer),
// find in path opened from there, a scan's results streamed in, a hit opened
// into a Python buffer, a go-to-definition jump into another file, then the
// in-file search (cmd+f) committed. Every step renders a frame, since the
// crash could as well have been View's.
//
// The chord itself is dispatched as its command message: the test registry
// carries no project.* commands, so cmd+shift+f from the playground would
// resolve and then find nothing to run (production registers them).
func TestFindInPathSequenceFromPlaygroundSurvives(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, viaSwitch := range []bool{false, true} {
			for _, buf := range []bool{false, true} {
				name := "inline"
				if split {
					name = "split"
				}
				if viaSwitch {
					name += "-switch"
				}
				if buf {
					name += "-buf"
				}
				t.Run(name, func(t *testing.T) {
					m, b := playSwitchModel(t, `{"foo":[1,2,3]}`, ".foo[]")
					a := cwd(t)
					if split {
						tm, cmd := m.Update(SplitPlayResultMsg{})
						m = drainCmd(tm.(Model), cmd)
					}
					py1 := filepath.Join(a, "one.py")
					py2 := filepath.Join(a, "two.py")
					for p, body := range map[string]string{py1: "import two\nneedle = two.f()\n", py2: "def f():\n    return 'needle'\n"} {
						if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if viaSwitch {
						m = switchTo(t, m, b)
						m = switchTo(t, m, a)
						m = dismissOnboarding(m)
					}
					if buf {
						m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
						if !m.play.bufFocus {
							t.Fatal("precondition: the result buffer holds the keyboard")
						}
					}
					if !m.playFocused() {
						t.Fatal("precondition: the playground holds the focus")
					}
					tm, cmd := m.Update(OpenFindInPathMsg{})
					m = drainCmd(tm.(Model), cmd)
					if !m.finder.IsOpen() {
						t.Fatal("find in path must open from the playground")
					}
					_ = m.render()
					m = typeKeys(m, "needle")
					gen := m.searcher.Gen()
					tm, _ = m.Update(search.BatchMsg{Gen: gen, Matches: []search.Match{
						{Path: py1, Line: 2, Text: "needle = two.f()", StartCol: 0, EndCol: 6},
						{Path: py2, Line: 2, Text: "    return 'needle'", StartCol: 12, EndCol: 18},
					}})
					m = tm.(Model)
					tm, _ = m.Update(search.DoneMsg{Gen: gen, Total: 2})
					m = tm.(Model)
					_ = m.render()
					m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
					_ = m.render()
					if m.finder.IsOpen() {
						tm, cmd := m.Update(finder.OpenLocationMsg{Path: py1, Line: 2, Col: 0})
						m = drainCmd(tm.(Model), cmd)
					}
					if ed := m.activeEditor(); ed == nil || filepath.Base(ed.Path()) != "one.py" {
						t.Fatalf("the hit must open one.py, active editor = %v", ed)
					}
					_ = m.render()
					tm, cmd = m.Update(ilsp.DefinitionMsg{Path: py2, Line: 0, Col: 4})
					m = drainCmd(tm.(Model), cmd)
					_ = m.render()
					if ed := m.activeEditor(); ed == nil || filepath.Base(ed.Path()) != "two.py" {
						t.Fatalf("the definition must open two.py, active editor = %v", ed)
					}
					m = drainKey(m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModSuper})
					_ = m.render()
					m = typeKeys(m, "nee")
					_ = m.render()
					m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
					_ = m.render()
					if !m.playOpen() {
						t.Error("the playground stays mounted behind the opened files")
					}
				})
			}
		}
	}
}
