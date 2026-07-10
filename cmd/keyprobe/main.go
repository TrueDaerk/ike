// keyprobe is the terminal reality probe (Roadmap 0081/10): run it in a
// terminal, press the listed chords, and quit with ctrl+d (delivered
// everywhere). On exit it prints one machine-parseable PROBE line per target
// chord — delivered or missing, with the actually-received key when it
// differs — feeding the reachability table in internal/keymap.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

type model struct {
	targets []string
	hit     map[string]string // chord -> what arrived ("" until seen)
	last    string
	done    bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+d" {
		m.done = true
		return m, tea.Quit
	}
	k, ok := keymap.FromKeyMsg(key)
	if !ok {
		return m, nil
	}
	m.last = k.String()
	if _, want := m.hit[m.last]; want {
		m.hit[m.last] = m.last
	}
	// Collapse evidence: a shifted chord arriving as its unshifted twin (the
	// classic ctrl+shift+z → ctrl+z) is recorded against the shifted target —
	// in addition to any direct hit, since the receiver cannot tell them apart.
	for _, t := range m.targets {
		if t != m.last && m.hit[t] == "" && strings.Replace(t, "shift+", "", 1) == m.last {
			m.hit[t] = m.last
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString("ike key probe — press each chord; ctrl+d finishes\n\n")
	for _, t := range m.targets {
		mark := "  ·  "
		switch got := m.hit[t]; {
		case got == t:
			mark = "  ✓  "
		case got != "":
			mark = "  ≈  " // arrived collapsed as another key
		}
		b.WriteString(mark + t + "\n")
	}
	b.WriteString("\nlast key: " + m.last + "\n")
	v := tea.NewView(b.String())
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}

func main() {
	targets := keymap.ProbeTargets()
	m := model{targets: targets, hit: map[string]string{}}
	for _, t := range targets {
		m.hit[t] = ""
	}
	out, err := tea.NewProgram(m).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "keyprobe:", err)
		os.Exit(1)
	}
	final := out.(model)
	for _, t := range final.targets {
		r := keymap.ProbeResult{Chord: t, Delivered: final.hit[t] == t}
		if got := final.hit[t]; got != "" && got != t {
			r.Got = got
		}
		fmt.Println(keymap.FormatProbeResult(r))
	}
}
