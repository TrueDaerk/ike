package app

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/deeplink"
	"ike/internal/netlink"
	"ike/internal/version"
)

// notified reports whether a notification containing text was raised —
// either still queued on the host (raised outside Update) or already drained
// into the history ring by an Update pass.
func notified(m Model, text string) bool {
	for _, n := range m.host.DrainNotifications() {
		if strings.Contains(n.Text, text) {
			return true
		}
	}
	return hasNotice(m, text)
}

// testChallenge builds a live challenge that expires in ttl.
func testChallenge(ttl time.Duration) netlink.Challenge {
	now := time.Now()
	return netlink.Challenge{Code: netlink.Generate(), Issued: now, Expires: now.Add(ttl),
		Client: "phone", Addr: "10.0.0.2:5000", Reason: "new"}
}

// TestNetCodeLayouts: with room the PIN is drawn as block art — every digit
// 1-9 has a shape, netDigitArtRows rows tall, every row the same width, the
// two groups parted by a middle dot — and a narrow budget falls back to one
// bold "4 8 1 · 9 3 6" line. Neither layout contains a zero glyph.
func TestNetCodeLayouts(t *testing.T) {
	code, err := netlink.ParseCode("481936")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []byte(netlink.CodeDigits) {
		art, ok := netDigitArt[d]
		if !ok {
			t.Fatalf("digit %c has no block art", d)
		}
		for r, row := range art {
			if w := lipgloss.Width(row); w != netDigitArtCols {
				t.Fatalf("%c art row %d is %d cells wide, want %d", d, r, w, netDigitArtCols)
			}
		}
	}
	accent := lipgloss.Color("#ffffff")
	big := renderNetCode(code, netCodeBigWidth, accent)
	bigLines := strings.Split(big, "\n")
	if len(bigLines) != netDigitArtRows {
		t.Fatalf("big layout has %d lines, want %d:\n%s", len(bigLines), netDigitArtRows, big)
	}
	for i, l := range bigLines {
		if w := lipgloss.Width(l); w != netCodeBigWidth {
			t.Fatalf("big layout row %d is %d cells wide, want %d", i, w, netCodeBigWidth)
		}
	}
	if !strings.Contains(bigLines[netDigitArtRows/2], "·") || strings.Contains(bigLines[0], "·") {
		t.Fatalf("the group separator sits on the centre row only:\n%s", big)
	}
	if strings.Contains(big, "4") {
		t.Fatalf("big layout must draw block art, not the plain digit:\n%s", big)
	}
	compact := renderNetCode(code, netCodeBigWidth-1, accent)
	if len(strings.Split(compact, "\n")) != 1 || !strings.Contains(compact, "4 8 1 · 9 3 6") {
		t.Fatalf("narrow budget must fall back to the one-line form:\n%s", compact)
	}
}

// TestNetPairPopupShowsCodeAndCounts: a challenge opens the popup with the
// device, the PIN and a bar; a tick keeps it open while time remains and
// closes it (with a notice) once the code expired.
func TestNetPairPopupShowsCodeAndCounts(t *testing.T) {
	m := switchModel(t)
	c := testChallenge(time.Minute)
	out, cmd := m.Update(netChallengeMsg{c: c})
	m = out.(Model)
	if !m.netPairOpen() || cmd == nil {
		t.Fatal("a challenge must open the popup and arm the countdown")
	}
	body := m.shell.View()
	for _, want := range []string{"phone", "10.0.0.2:5000", "refuse", "PIN"} {
		if !strings.Contains(body, want) {
			t.Fatalf("popup lacks %q:\n%s", want, body)
		}
	}
	// The popup is wide enough for the block art, so the digits appear as
	// shapes; the top row of every digit's art must be present.
	for _, d := range c.Code {
		if !strings.Contains(body, netDigitArt[d][0]) {
			t.Fatalf("popup must draw digit %c:\n%s", d, body)
		}
	}
	if !strings.Contains(body, "█") {
		t.Fatalf("popup lacks the countdown bar:\n%s", body)
	}
	// A tick of the live generation re-arms; a stale one is dropped.
	out, cmd = m.Update(netPairTickMsg{gen: m.nlPair.gen})
	m = out.(Model)
	if !m.netPairOpen() || cmd == nil {
		t.Fatal("a live tick keeps the popup and re-arms")
	}
	if _, cmd := m.Update(netPairTickMsg{gen: m.nlPair.gen - 1}); cmd != nil {
		t.Fatal("a stale tick must not re-arm")
	}
	// Expiry: the popup closes and says so.
	m.nlPair.c.Expires = time.Now().Add(-time.Second)
	out, cmd = m.Update(netPairTickMsg{gen: m.nlPair.gen})
	m = out.(Model)
	if m.netPairOpen() {
		t.Fatal("an expired code closes the popup")
	}
	if !notified(m, "expired") {
		t.Fatal("expiry must be announced")
	}
}

// TestNetPairReplacementAndClear: a second challenge replaces the first in
// place (new generation); cleared / paired close the popup.
func TestNetPairReplacementAndClear(t *testing.T) {
	m := switchModel(t)
	out, _ := m.Update(netChallengeMsg{c: testChallenge(time.Minute)})
	m = out.(Model)
	first := m.nlPair.gen
	wrong := testChallenge(time.Minute)
	wrong.Reason = "wrong"
	out, _ = m.Update(netChallengeMsg{c: wrong})
	m = out.(Model)
	if m.nlPair.gen == first || !strings.Contains(m.shell.View(), "wrong") {
		t.Fatal("a replacement code must bump the generation and say why")
	}
	out, _ = m.Update(netChallengeClearedMsg{})
	m = out.(Model)
	if m.netPairOpen() {
		t.Fatal("cleared must close the popup")
	}
	out, _ = m.Update(netChallengeMsg{c: testChallenge(time.Minute)})
	m = out.(Model)
	out, _ = m.Update(netPairedMsg{c: netlink.Client{Name: "phone", Addr: "10.0.0.2:5000"}})
	m = out.(Model)
	if m.netPairOpen() || !notified(m, "paired with phone") {
		t.Fatal("pairing closes the popup and announces the device")
	}
}

// TestNetPairEscRefuses: esc closes the popup and, with a live server,
// cancels the challenge so the device's next request is refused.
func TestNetPairEscRefuses(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	cfg := config.Get()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", 0
	if err := m.startNetLink(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.nlServer.Close)
	c, err := m.nlServer.Pairing().Begin("phone", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(netChallengeMsg{c: c})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.netPairOpen() || !notified(m, "refused") {
		t.Fatal("esc must close the popup and say the pairing was refused")
	}
	if _, ok := m.nlServer.Pairing().Current(); ok {
		t.Fatal("esc must cancel the live challenge")
	}
	if _, err := m.nlServer.Pairing().Begin("phone", "127.0.0.1:2"); err == nil {
		t.Fatal("the refused host must be kept waiting")
	}
}

// TestNetLinkDeliversAsDeepLink: an authenticated open on the wire lands in
// the Update loop as a DeepLinkMsg carrying the assembled ike:// URL.
func TestNetLinkDeliversAsDeepLink(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	var got []tea.Msg
	m.host.SetSender(func(msg tea.Msg) { got = append(got, msg) })
	cfg := config.Get()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", 0
	if err := m.startNetLink(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.nlServer.Close)
	// Pair straight through the store (the popup's job is tested above).
	token, _, err := m.nlServer.Store().Issue("test", "127.0.0.1:1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", m.nlServer.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req, _ := json.Marshal(netlink.Request{Cmd: "open", Token: token, Project: "ike", File: "x.go", Line: 3})
	_, _ = conn.Write(append(req, '\n'))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, `"type":"ok"`) {
		t.Fatalf("reply %q err %v", line, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(got) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("delivered %d messages, want 1", len(got))
	}
	dl, ok := got[0].(DeepLinkMsg)
	if !ok {
		t.Fatalf("delivered %T, want DeepLinkMsg", got[0])
	}
	link, err := deeplink.Parse(dl.URL)
	if err != nil || link.Project != "ike" || link.File != "x.go" || link.Line != 3 {
		t.Fatalf("link %+v err %v", link, err)
	}
}

// TestReconfigureNetwork: enabling starts the listener, an unchanged config
// leaves it alone, a new port restarts it, disabling stops it.
func TestReconfigureNetwork(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	cfg := config.Get()
	cfg.Network.Enabled = false
	m.reconfigureNetwork(cfg)
	if m.nlServer != nil {
		t.Fatal("disabled must not listen")
	}
	free := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		return ln.Addr().(*net.TCPAddr).Port
	}
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", free()
	m.reconfigureNetwork(cfg)
	if m.nlServer == nil || !notified(m, "listening on 127.0.0.1:") {
		t.Fatal("enabling must start the listener and say where")
	}
	t.Cleanup(func() { m.nlServer.Close() })
	first := m.nlServer
	m.reconfigureNetwork(cfg)
	if m.nlServer != first {
		t.Fatal("an unchanged config must keep the listener")
	}
	cfg.Network.Port = free()
	m.reconfigureNetwork(cfg)
	if m.nlServer == first || m.nlServer == nil {
		t.Fatal("a new port must restart the listener")
	}
	if _, err := net.Dial("tcp", first.Addr().String()); err == nil {
		t.Fatal("the old listener must be closed")
	}
	cfg.Network.Enabled = false
	m.reconfigureNetwork(cfg)
	if m.nlServer != nil || !notified(m, "stopped") {
		t.Fatal("disabling must stop the listener")
	}
}

// TestNetworkForgetClients: the command empties the store, listener or not.
func TestNetworkForgetClients(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	store, err := netlink.OpenStore(netlink.DefaultStorePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Issue("phone", "1:1", time.Now()); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(NetworkForgetClientsMsg{})
	m = out.(Model)
	if !notified(m, "forgot 1 paired client") {
		t.Fatal("the command must report what it forgot")
	}
	again, _ := netlink.OpenStore(netlink.DefaultStorePath())
	if len(again.Clients()) != 0 {
		t.Fatal("the store must be empty afterwards")
	}
}

// TestNetListenAddr: IPv6 binds are bracketed, empty binds every interface.
func TestNetListenAddr(t *testing.T) {
	for bind, want := range map[string]string{"": ":4530", "0.0.0.0": "0.0.0.0:4530", "::1": "[::1]:4530", "[::]": "[::]:4530"} {
		cfg := &config.Config{Network: config.Network{Port: 4530, Bind: bind}}
		if got := netListenAddr(cfg); got != want {
			t.Errorf("bind %q: %q, want %q", bind, got, want)
		}
	}
}

// TestNetLinkSurvivesProjectSwitch: the listener rides across the model
// rebuild a switch performs — it is neither closed nor bound twice.
func TestNetLinkSurvivesProjectSwitch(t *testing.T) {
	_, dst, _ := deepLinkFixture(t)
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	cfg := config.Get()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port = true, "127.0.0.1", 0
	if err := m.startNetLink(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.nlServer.Close)
	srv := m.nlServer
	out, cmd := m.Update(deepLinkResolvedMsg{link: deeplink.Link{Project: "dst"},
		res: deeplink.Resolution{Kind: deeplink.KindSwitch, Path: dst}})
	m = stepCmd(out.(Model), cmd)
	if m.nlServer != srv || m.nlAddr == "" {
		t.Fatal("the listener must ride across the switch")
	}
	if conn, err := net.Dial("tcp", srv.Addr().String()); err != nil {
		t.Fatalf("the listener must still accept: %v", err)
	} else {
		conn.Close()
	}
}

// TestNetDiscoverable: announcing (#2522) needs the endpoint on, the switch
// on and a bind another device can reach — loopback is skipped, the
// unspecified and a LAN address pass.
func TestNetDiscoverable(t *testing.T) {
	cfg := config.Get()
	t.Cleanup(netResetConfig(cfg))
	cfg.Network.Enabled, cfg.Network.MDNS = true, true
	for bind, want := range map[string]bool{"": true, "0.0.0.0": true, "[::]": true, "192.168.1.20": true, "127.0.0.1": false, "::1": false} {
		cfg.Network.Bind = bind
		if got := netDiscoverable(cfg); got != want {
			t.Errorf("bind %q: discoverable %v, want %v", bind, got, want)
		}
	}
	cfg.Network.Bind = ""
	cfg.Network.MDNS = false
	if netDiscoverable(cfg) {
		t.Error("mdns=false must not announce")
	}
	cfg.Network.MDNS, cfg.Network.Enabled = true, false
	if netDiscoverable(cfg) || netDiscoverable(nil) {
		t.Error("a disabled endpoint must not announce")
	}
}

// TestNetService: the announced instance carries the configured name, the
// port, the version TXT, and — for a bind to one address — that address
// only; the unspecified bind leaves the addresses to the interfaces.
func TestNetService(t *testing.T) {
	cfg := config.Get()
	t.Cleanup(netResetConfig(cfg))
	cfg.Network.Port, cfg.Network.Name, cfg.Network.Bind = 4531, " desk ", "192.168.1.20"
	svc := netService(cfg)
	if svc.Instance != "desk" || svc.Type != "_ike._tcp" || svc.Port != 4531 {
		t.Errorf("service %+v", svc)
	}
	if len(svc.IPs) != 1 || !svc.IPs[0].Equal(net.ParseIP("192.168.1.20")) {
		t.Errorf("a bind to one address must advertise that address: %v", svc.IPs)
	}
	if strings.Join(svc.TXT, " ") != "v="+version.Short()+" proto=1 name=ike" {
		t.Errorf("TXT %v", svc.TXT)
	}
	cfg.Network.Bind = "0.0.0.0"
	if svc = netService(cfg); svc.IPs != nil {
		t.Errorf("the unspecified bind must leave the addresses to the interfaces: %v", svc.IPs)
	}
}

// TestReconfigureNetworkDiscovery: the discovery switch and the announced
// name are part of the restart key, and a loopback bind runs the listener
// without an announcer.
func TestReconfigureNetworkDiscovery(t *testing.T) {
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	cfg := config.Get()
	t.Cleanup(netResetConfig(cfg))
	cfg.Network.Name = ""
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg.Network.Enabled, cfg.Network.Bind, cfg.Network.Port, cfg.Network.MDNS = true, "127.0.0.1", port, true
	m.reconfigureNetwork(cfg)
	if m.nlServer == nil {
		t.Fatal("enabling must start the listener")
	}
	t.Cleanup(func() { m.CloseNetLink() })
	if m.nlMDNS != nil {
		t.Fatal("a loopback bind must not announce")
	}
	first := m.nlServer
	m.reconfigureNetwork(cfg)
	if m.nlServer != first {
		t.Fatal("an unchanged config must keep the listener")
	}
	cfg.Network.Name = "desk"
	m.reconfigureNetwork(cfg)
	if m.nlServer == first || m.nlServer == nil {
		t.Fatal("a new announced name must restart the endpoint")
	}
	second := m.nlServer
	cfg.Network.MDNS = false
	m.reconfigureNetwork(cfg)
	if m.nlServer == second || m.nlServer == nil {
		t.Fatal("flipping the discovery switch must restart the endpoint")
	}
	cfg.Network.Enabled = false
	m.reconfigureNetwork(cfg)
	if m.nlServer != nil || m.nlMDNS != nil || m.nlKey != "" {
		t.Fatal("disabling must clear listener, announcer and key")
	}
}

// netResetConfig restores the shared config's [network] section after a
// test that mutated it — config.Get is one process-wide value.
func netResetConfig(cfg *config.Config) func() {
	saved := cfg.Network
	return func() { cfg.Network = saved }
}
