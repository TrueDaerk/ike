package app

import (
	"fmt"
	"image/color"
	"net"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/mdns"
	"ike/internal/netlink"
	"ike/internal/ui"
	"ike/internal/version"
)

// netlink.go is the root-model side of the network deep-link endpoint
// (#2519): the TCP listener's lifecycle (start on launch when enabled,
// restart on a [network] settings change), the pairing popup — six coloured
// suit glyphs with a countdown bar — and the "forget paired clients"
// command. The protocol, the pairing state machine and the token store live
// in internal/netlink; an accepted link arrives here as a DeepLinkMsg and
// runs the very same pipeline an OS-delivered ike:// click does.

// NetworkForgetClientsMsg runs network.forgetClients: every paired device
// loses its token and has to pair again.
type NetworkForgetClientsMsg struct{}

// netChallengeMsg carries a new (or replacement) pairing code into the
// Update loop; the popup shows it.
type netChallengeMsg struct{ c netlink.Challenge }

// netChallengeClearedMsg says the live code ended without a replacement
// (paired, or the address got blocked): the popup closes.
type netChallengeClearedMsg struct{}

// netPairedMsg announces a device that just paired.
type netPairedMsg struct{ c netlink.Client }

// netPairTickMsg drives the popup's countdown bar once a second; gen drops
// ticks of a popup that has since closed or been replaced.
type netPairTickMsg struct{ gen int }

// netPairing is the open pairing popup. content is the shell content it
// installed — the identity check behind netPairOpen, so another dialog
// taking the shared shell over (or the shell being closed underneath) can
// never leave a stale claim on the keyboard.
type netPairing struct {
	c       netlink.Challenge
	gen     int
	content *ui.ModelContent
}

// netEvents bridges the server's goroutine-side events into the Update
// loop through host.Send — the one bridge a background goroutine may use.
type netEvents struct{ h *host.Host }

func (e netEvents) ChallengeIssued(c netlink.Challenge) { e.h.Send(netChallengeMsg{c: c}) }
func (e netEvents) ChallengeCleared()                   { e.h.Send(netChallengeClearedMsg{}) }
func (e netEvents) Paired(c netlink.Client)             { e.h.Send(netPairedMsg{c: c}) }

// StartNetLink opens the TCP endpoint when [network].enabled is on and
// returns the model holding it. A failure to listen (port taken, bad bind)
// is a notification the model shows once it runs; the rest of the IDE is
// unaffected. The mDNS announcement (#2522) follows the listener; its own
// failure is a warning that leaves the endpoint up.
func (m Model) StartNetLink() Model {
	if err := m.startNetLink(config.Get()); err != nil {
		m.host.Notify(host.Warn, err.Error())
	} else if err := m.startNetDiscovery(config.Get()); err != nil {
		m.host.Notify(host.Warn, err.Error())
	}
	return m
}

// startNetLink starts the listener for cfg (a no-op while disabled).
func (m *Model) startNetLink(cfg *config.Config) error {
	m.nlKey = netConfigKey(cfg)
	if cfg == nil || !cfg.Network.Enabled {
		return nil
	}
	store, err := netlink.OpenStore(netlink.DefaultStorePath())
	if err != nil {
		return fmt.Errorf("network links: cannot read the paired-clients file: %v", err)
	}
	h := m.host
	srv, err := netlink.Serve(netlink.Options{
		Addr:    netListenAddr(cfg),
		Store:   store,
		Version: version.Short(),
		Deliver: func(url string) { h.Send(DeepLinkMsg{URL: url}) },
		Events:  netEvents{h: h},
	})
	if err != nil {
		return fmt.Errorf("network links: cannot listen on %s: %v", netListenAddr(cfg), err)
	}
	m.nlServer = srv
	m.nlAddr = netListenAddr(cfg)
	return nil
}

// netServiceType is the DNS-SD service type IKE announces (#2522).
const netServiceType = "_ike._tcp"

// netDiscoverable reports whether an enabled endpoint is worth announcing:
// a loopback bind is reachable from this machine only, so a browser on the
// LAN would find an address it cannot connect to.
func netDiscoverable(cfg *config.Config) bool {
	if cfg == nil || !cfg.Network.Enabled || !cfg.Network.MDNS {
		return false
	}
	bind := strings.Trim(strings.TrimSpace(cfg.Network.Bind), "[]")
	if bind == "" {
		return true
	}
	ip := net.ParseIP(bind)
	return ip != nil && !ip.IsLoopback()
}

// netService is the DNS-SD instance the endpoint announces: the configured
// name (or the host's), the port, a TXT record naming the protocol and the
// version, and — for a bind to one address — exactly that address, else
// every interface's.
func netService(cfg *config.Config) mdns.Service {
	svc := mdns.Service{
		Instance: strings.TrimSpace(cfg.Network.Name),
		Type:     netServiceType,
		Port:     cfg.Network.Port,
		TXT:      []string{"v=" + version.Short(), "proto=1", "name=ike"},
	}
	if ip := net.ParseIP(strings.Trim(strings.TrimSpace(cfg.Network.Bind), "[]")); ip != nil && !ip.IsUnspecified() {
		svc.IPs = []net.IP{ip}
	}
	return svc
}

// startNetDiscovery announces the running endpoint (a no-op while not
// discoverable). The announcer is closed with the listener.
func (m *Model) startNetDiscovery(cfg *config.Config) error {
	if !netDiscoverable(cfg) || m.nlServer == nil {
		return nil
	}
	r, err := mdns.Announce(netService(cfg))
	if err != nil {
		return fmt.Errorf("network links: cannot announce over mDNS: %v", err)
	}
	m.nlMDNS = r
	return nil
}

// netConfigKey folds every [network] field the running endpoint and its
// announcement depend on into one comparable string; "" while disabled.
func netConfigKey(cfg *config.Config) string {
	if cfg == nil || !cfg.Network.Enabled {
		return ""
	}
	return netListenAddr(cfg) + "|mdns=" + strconv.FormatBool(cfg.Network.MDNS) + "|name=" + strings.TrimSpace(cfg.Network.Name)
}

// netListenAddr renders the [network] bind/port pair as a listen address.
func netListenAddr(cfg *config.Config) string {
	bind := strings.Trim(strings.TrimSpace(cfg.Network.Bind), "[]")
	if strings.Contains(bind, ":") {
		bind = "[" + bind + "]"
	}
	return bind + ":" + strconv.Itoa(cfg.Network.Port)
}

// CloseNetLink stops the endpoint and its announcement (the goodbye goes
// out first, so browsers drop the instance at once); cmd/ike defers it
// around Run.
func (m Model) CloseNetLink() {
	m.nlMDNS.Close()
	m.nlServer.Close()
}

// reconfigureNetwork applies a [network] settings change live: the
// listener stops when disabled, starts when enabled, and restarts — with
// its announcement — when the address, the discovery switch or the
// announced name changed. An unchanged configuration leaves a running
// listener — and its live pairing — alone.
func (m *Model) reconfigureNetwork(cfg *config.Config) {
	if cfg == nil {
		return
	}
	want := netConfigKey(cfg)
	if want == m.nlKey && (want == "" || m.nlServer != nil) {
		return
	}
	if m.nlServer != nil {
		m.closeNetPair()
		m.nlMDNS.Close()
		m.nlServer.Close()
		m.nlServer, m.nlAddr, m.nlMDNS, m.nlKey = nil, "", nil, ""
		if want == "" {
			m.host.Notify(host.Info, "network links: endpoint stopped")
		}
	}
	if want == "" {
		m.nlKey = ""
		return
	}
	if err := m.startNetLink(cfg); err != nil {
		m.host.Notify(host.Warn, err.Error())
		return
	}
	note := "network links: listening on " + m.nlAddr
	if err := m.startNetDiscovery(cfg); err != nil {
		m.host.Notify(host.Warn, err.Error())
	} else if m.nlMDNS != nil {
		note += ", announced as " + m.nlMDNS.Service().Instance + "." + netServiceType + ".local"
	}
	m.host.Notify(host.Info, note)
}

// handleNetChallenge shows (or refreshes) the pairing popup for a code and
// arms its countdown.
func (m Model) handleNetChallenge(c netlink.Challenge) (tea.Model, tea.Cmd) {
	m.nlTickGen++
	m.nlPair = &netPairing{c: c, gen: m.nlTickGen}
	m.renderNetPair()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return m, m.netPairTick(m.nlTickGen)
}

// netPairTick schedules the next countdown tick for popup generation gen.
func (m Model) netPairTick(gen int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return netPairTickMsg{gen: gen} })
}

// handleNetPairTick redraws the bar, or ends the popup once the code is
// out of time: the machine forgets the code so a late guess finds nothing.
func (m Model) handleNetPairTick(msg netPairTickMsg) (tea.Model, tea.Cmd) {
	p := m.nlPair
	if p == nil || msg.gen != p.gen || !m.shell.IsOpen() {
		return m, nil
	}
	if !time.Now().Before(p.c.Expires) {
		if m.nlServer != nil {
			m.nlServer.Pairing().Expire()
		}
		m.closeNetPair()
		m.host.Notify(host.Info, "network links: the pairing code expired — the device can ask for a new one")
		return m, nil
	}
	m.renderNetPair()
	return m, m.netPairTick(p.gen)
}

// closeNetPair dismisses the popup without touching the machine.
func (m *Model) closeNetPair() {
	if m.nlPair == nil {
		return
	}
	m.nlPair = nil
	m.shell.Close()
}

// netPairOpen reports whether the pairing popup owns the keyboard: the shell
// is open and still shows this popup's content.
func (m Model) netPairOpen() bool {
	return m.nlPair != nil && m.nlPair.content != nil && m.shell.IsOpen() &&
		m.shell.Content() == ui.Content(m.nlPair.content)
}

// updateNetPair consumes every key while the popup is open: esc refuses the
// pairing (the device is kept from asking again for a moment); everything
// else is ignored — there is nothing to type, the code is read, not entered.
func (m Model) updateNetPair(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEscape {
		if m.nlServer != nil {
			m.nlServer.Pairing().Cancel()
		}
		m.closeNetPair()
		m.host.Notify(host.Info, "network links: pairing refused")
	}
	return m, nil
}

// renderNetPair (re)fills the shell for the live code: the device asking,
// the six glyphs in their colours with the colour names spelled out
// underneath (a colour-blind reader still gets the code), the countdown bar
// and the esc hint.
func (m *Model) renderNetPair() {
	p := m.nlPair
	if p == nil {
		return
	}
	c := p.c
	pal := m.pal()
	content := &ui.ModelContent{
		Heading: "Pair a network device",
		Body: func() string {
			who := c.Client
			if who == "" {
				who = "a device"
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s at %s wants to connect.\n", who, c.Addr)
			switch c.Reason {
			case "wrong":
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Render("The last code was wrong — this is a new one.") + "\n")
			case "expired":
				b.WriteString(lipgloss.NewStyle().Foreground(pal.Warning).Render("The last code expired — this is a new one.") + "\n")
			}
			b.WriteString("\nEnter this code on the device:\n\n")
			b.WriteString(renderNetCode(c.Code))
			b.WriteString("\n\n")
			b.WriteString(renderNetCountdown(c, time.Now(), 36, pal.Accent, pal.Hint))
			b.WriteString("\n\n" + guardCancel("refuse — the device is turned away"))
			return b.String()
		},
	}
	p.content = content
	m.shell.SetContent(content)
}

// netGlyphChip is the neutral light chip every glyph sits on, so red, black,
// blue and green all read the same on a dark or a light theme.
var netGlyphChip = lipgloss.Color("#e8e8e8")

// renderNetCode draws the six glyphs: a row of coloured suits on chips, a
// row of position numbers, and a row naming each colour.
func renderNetCode(code netlink.Code) string {
	var glyphs, nums, names []string
	for i, g := range code {
		suit, _ := netSuit(g.Suit)
		colour, _ := netColour(g.Colour)
		chip := lipgloss.NewStyle().Background(netGlyphChip).Foreground(lipgloss.Color(colour.Hex)).Bold(true).
			Render(" " + suit.Glyph + " ")
		glyphs = append(glyphs, chip)
		nums = append(nums, centerCell(strconv.Itoa(i+1), 3))
		names = append(names, centerCell(colour.Name, 7))
	}
	// Column pitch is 7 cells: a 3-wide chip plus 4 spaces, so the 7-wide
	// name cells butt up against each other under the same centres.
	return "   " + strings.Join(glyphs, "    ") + "\n" +
		"   " + strings.Join(nums, "    ") + "\n" +
		" " + strings.Join(names, "")
}

// renderNetCountdown draws the remaining-time bar of width cells with the
// seconds left after it.
func renderNetCountdown(c netlink.Challenge, now time.Time, width int, on, off color.Color) string {
	total := c.Expires.Sub(c.Issued)
	left := c.Expires.Sub(now)
	if left < 0 {
		left = 0
	}
	frac := 0.0
	if total > 0 {
		frac = float64(left) / float64(total)
	}
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	bar := lipgloss.NewStyle().Foreground(on).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(off).Render(strings.Repeat("░", width-filled))
	secs := int(left.Round(time.Second) / time.Second)
	return "  " + bar + fmt.Sprintf("  %2ds", secs)
}

// centerCell pads s to width, centred.
func centerCell(s string, width int) string {
	n := lipgloss.Width(s)
	if n >= width {
		return s
	}
	left := (width - n) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-n-left)
}

// netSuit / netColour look up alphabet entries by ID for rendering.
func netSuit(id string) (netlink.Suit, bool) {
	for _, s := range netlink.Suits {
		if s.ID == id {
			return s, true
		}
	}
	return netlink.Suit{}, false
}

func netColour(id string) (netlink.Colour, bool) {
	for _, c := range netlink.Colours {
		if c.ID == id {
			return c, true
		}
	}
	return netlink.Colour{}, false
}

// handleNetworkForgetClients revokes every paired device. Works with the
// endpoint off too — the file is the source of truth, not the listener.
func (m Model) handleNetworkForgetClients() (tea.Model, tea.Cmd) {
	store := (*netlink.Store)(nil)
	if m.nlServer != nil {
		store = m.nlServer.Store()
	} else {
		s, err := netlink.OpenStore(netlink.DefaultStorePath())
		if err != nil {
			m.host.Notify(host.Warn, "network links: cannot read the paired-clients file: "+err.Error())
			return m, nil
		}
		store = s
	}
	n := len(store.Clients())
	if err := store.RevokeAll(); err != nil {
		m.host.Notify(host.Warn, "network links: cannot write the paired-clients file: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, fmt.Sprintf("network links: forgot %d paired client(s) — each has to pair again", n))
	return m, nil
}
