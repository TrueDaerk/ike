package editor

import tea "charm.land/bubbletea/v2"

// macro.go implements vim's macro recording & replay (#58): `q{a-z}` records
// every following keypress (mode-agnostic — the tap sits in Update), `q` stops,
// `@{a-z}` replays, `@@` repeats the last replay, and a count multiplies
// (`5@a`). The payload is the keystroke list itself, kept per view like the
// register store.

// maxReplayDepth caps nested @-replays. A macro may invoke another macro (or
// itself) like in vim, but without vim's stop-on-error semantics a recursive
// macro would never terminate — the cap is the recursion guard.
const maxReplayDepth = 100

// macroRegister reports whether r names a macro register (`a`-`z`).
func macroRegister(r rune) bool { return r >= 'a' && r <= 'z' }

// startRecording begins capturing keys into register reg.
func (m *Model) startRecording(reg rune) {
	m.recordReg = reg
	m.recordKeys = nil
}

// stopRecording stores the captured keys as the macro. The stopping `q` was
// already appended by the Update tap, so it is dropped from the tail first.
func (m *Model) stopRecording() {
	keys := m.recordKeys
	if n := len(keys); n > 0 {
		keys = keys[:n-1]
	}
	if m.macros == nil {
		m.macros = map[rune][]tea.KeyPressMsg{}
	}
	m.macros[m.recordReg] = keys
	m.recordReg = 0
	m.recordKeys = nil
}

// Recording reports the register an active recording captures into, 0 when
// idle — the status line's "recording @a" seam.
func (m Model) Recording() rune { return m.recordReg }

// playMacro replays the macro in reg count times by feeding its recorded keys
// back through Update. replayDepth guards recursion (nested @ inside a macro)
// and keeps replayed keys out of an active recording.
func (m Model) playMacro(reg rune, count int) (Model, tea.Cmd) {
	keys := m.macros[reg]
	if len(keys) == 0 || m.replayDepth >= maxReplayDepth {
		return m, nil
	}
	m.lastMacro = reg
	m.replayDepth++
	var cmds []tea.Cmd
	for i := 0; i < count; i++ {
		for _, k := range keys {
			var cmd tea.Cmd
			m, cmd = m.Update(k)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	m.replayDepth--
	return m, tea.Batch(cmds...)
}
