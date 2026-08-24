package settings

import (
	"os"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/theme"
)

// probe_panel.go is the keymap doctor's settings surface (#2080): a sub-panel
// off the Keymap page that lists the stored per-terminal probe runs — which
// chords probed missing, with the collapse evidence — and offers running the
// doctor and clearing a stored run. The store itself lives in the keymap
// package; the panel reads and writes it directly, like the JetBrains import
// reads its XML.

type probePanel struct {
	page   *KeymapPage
	host   SubPanelHost
	pal    *theme.Palette
	launch func() tea.Cmd // dispatches keymap.doctor in the app; nil hides the button
	store  keymap.ProbeStore
	// current is the running terminal's identity: clearing it must also
	// uninstall the live verdicts, not just rewrite the file.
	current string
	sel     int // selected terminal in TerminalIDs() order
}

func newProbePanel(page *KeymapPage, host SubPanelHost, launch func() tea.Cmd) *probePanel {
	return &probePanel{
		page:    page,
		host:    host,
		pal:     page.theme(),
		launch:  launch,
		store:   keymap.LoadProbeStore(keymap.ProbeStorePath()),
		current: keymap.TerminalID(os.Getenv),
	}
}

func (p *probePanel) Title() string   { return "Keymap Doctor" }
func (p *probePanel) Capturing() bool { return false }

func (p *probePanel) Buttons() []Button {
	terms := p.store.TerminalIDs()
	return []Button{
		{Label: "Run Probe", Key: "p", Do: p.run, Disabled: p.launch == nil},
		{Label: "Clear Results", Key: "c", Do: p.clear, Disabled: len(terms) == 0},
		{Label: "Close", Do: func() tea.Cmd { p.host.Pop(); return nil }},
	}
}

// run pops the whole settings flow out of the way and launches the doctor —
// the overlay is full-screen and must own every raw key.
func (p *probePanel) run() tea.Cmd {
	if p.launch == nil {
		return nil
	}
	p.host.Pop()
	return p.launch()
}

// clear drops the selected terminal's stored run. Clearing the running
// terminal also uninstalls the live verdicts; the config reload rebuilds the
// binding table so Fragile flags fall back to the static classes.
func (p *probePanel) clear() tea.Cmd {
	terms := p.store.TerminalIDs()
	if p.sel < 0 || p.sel >= len(terms) {
		return nil
	}
	term := terms[p.sel]
	p.store.Clear(term)
	if err := p.store.Save(keymap.ProbeStorePath()); err != nil {
		return nil
	}
	if term == p.current {
		keymap.SetProbeVerdicts(nil)
	}
	if p.sel >= len(p.store.TerminalIDs()) {
		p.sel = len(p.store.TerminalIDs()) - 1
	}
	opts := p.page.opts
	return func() tea.Msg {
		cfg, diags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: cfg, Diags: diags}
	}
}

func (p *probePanel) Update(key tea.KeyPressMsg) tea.Cmd {
	n := len(p.store.TerminalIDs())
	switch key.String() {
	case "j", "down":
		if p.sel < n-1 {
			p.sel++
		}
	case "k", "up":
		if p.sel > 0 {
			p.sel--
		}
	}
	return nil
}

func (p *probePanel) View(width, height int) string {
	sec := lipgloss.NewStyle().Foreground(p.pal.Secondary)
	warn := lipgloss.NewStyle().Foreground(p.pal.Warning)
	selStyle := lipgloss.NewStyle().Background(p.pal.Selection).Foreground(p.pal.SelectionText).Bold(true)
	clip := lipgloss.NewStyle().MaxWidth(width)
	terms := p.store.TerminalIDs()
	var lines []string
	lines = append(lines, clip.Render(sec.Render("probe chord delivery in this terminal ("+p.current+"); saved runs override the static reachability table")))
	lines = append(lines, "")
	if len(terms) == 0 {
		lines = append(lines, clip.Render(sec.Render("no stored probe runs — press p to run the doctor")))
		return strings.Join(padTo(lines, height), "\n")
	}
	for i, t := range terms {
		run := p.store.Terminals[t]
		delivered, missing := 0, 0
		for _, r := range run.Results {
			if r.Delivered {
				delivered++
			} else {
				missing++
			}
		}
		label := " " + t
		if t == p.current {
			label += " (this terminal)"
		}
		label += " — ✓ " + strconv.Itoa(delivered) + " · ✗ " + strconv.Itoa(missing)
		if run.Probed != "" {
			label += " · " + run.Probed
		}
		style := lipgloss.NewStyle()
		if i == p.sel {
			style = selStyle
		}
		lines = append(lines, clip.Render(style.Render(label)))
	}
	// The selected run's missing chords, with the collapse evidence — the
	// part worth reading before rebinding anything.
	if p.sel >= 0 && p.sel < len(terms) {
		var missing []string
		for _, r := range p.store.Terminals[terms[p.sel]].Results {
			if r.Delivered {
				continue
			}
			s := r.Chord
			if r.Got != "" {
				s += " (arrives as " + r.Got + ")"
			}
			missing = append(missing, s)
		}
		sort.Strings(missing)
		lines = append(lines, "")
		if len(missing) == 0 {
			lines = append(lines, clip.Render(sec.Render(" every probed chord was delivered")))
		} else {
			lines = append(lines, clip.Render(warn.Render(" probed missing:")))
			for _, s := range missing {
				if len(lines) >= height-1 {
					lines = append(lines, clip.Render(sec.Render(" …")))
					break
				}
				lines = append(lines, clip.Render(warn.Render("   ✗ "+s)))
			}
		}
	}
	return strings.Join(padTo(lines, height), "\n")
}
