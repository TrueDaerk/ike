// keyprobe is the terminal reality probe (Roadmap 0081/10): run it in a
// terminal, press the listed chords, and quit with ctrl+d (delivered
// everywhere). On exit it prints one machine-parseable PROBE line per target
// chord — delivered or missing, with the actually-received key when it
// differs — feeding the reachability table in internal/keymap. The matching
// itself lives in keymap.ProbeSession, shared with the in-app keymap doctor
// (#2080), whose store can import a captured run of this binary.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

type model struct {
	sess *keymap.ProbeSession
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The mouse navigation buttons (#816) are probe targets like any chord:
	// they are only reachable when the terminal reports SGR extended buttons,
	// which is exactly what the probe exists to establish — without this the
	// mouse targets always reported "missing", regardless of the terminal.
	if click, isClick := msg.(tea.MouseClickMsg); isClick {
		if k, isNav := keymap.FromMouseButton(click.Button); isNav {
			m.sess.HandleKey(k)
		}
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+d" {
		return m, tea.Quit
	}
	if k, ok := keymap.FromKeyMsg(key); ok {
		m.sess.HandleKey(k)
	}
	return m, nil
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString("ike key probe — press each chord (mouse-back/mouse-forward: click the button); ctrl+d finishes\n\n")
	for _, t := range m.sess.Targets() {
		mark := "  ·  "
		switch got := m.sess.Hit(t); {
		case got == t:
			mark = "  ✓  "
		case got != "":
			mark = "  ≈  " // arrived collapsed as another key
		}
		b.WriteString(mark + t + "\n")
	}
	b.WriteString("\nlast key: " + m.sess.Last() + "\n")
	v := tea.NewView(b.String())
	v.KeyboardEnhancements.ReportEventTypes = true
	// Mouse reporting on (same mode the editor uses), so the mouse-back /
	// mouse-forward targets can be probed by clicking them (#816).
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func main() {
	m := model{sess: keymap.NewProbeSession(keymap.ProbeTargets())}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "keyprobe:", err)
		os.Exit(1)
	}
	for _, r := range m.sess.Results() {
		fmt.Println(keymap.FormatProbeResult(r))
	}
}
