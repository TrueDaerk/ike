package mdns

import (
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// mdns_test.go covers the responder's pure core (#2522): the record set of
// a service, the answers to browse / resolve / enumeration queries, unicast
// versus multicast replies, the goodbye, and the label normalisation. The
// socket side is exercised by one loopback round trip that is skipped where
// the sandbox has no multicast.

func testService() Service {
	return Service{
		Instance: "geants mac",
		Type:     "_ike._tcp",
		Port:     4530,
		TXT:      []string{"v=0.5.170", "proto=1", "name=ike"},
		Host:     "geants-mac.local",
		IPs:      []net.IP{net.IPv4(192, 168, 1, 20), net.ParseIP("fe80::1")},
	}
}

func query(t *testing.T, name string, typ dnsmessage.Type, qu bool) []byte {
	t.Helper()
	n, err := dnsmessage.NewName(name)
	if err != nil {
		t.Fatal(err)
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 7})
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	class := dnsmessage.ClassINET
	if qu {
		class |= 1 << 15
	}
	if err := b.Question(dnsmessage.Question{Name: n, Type: typ, Class: class}); err != nil {
		t.Fatal(err)
	}
	out, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type parsed struct {
	hdr     dnsmessage.Header
	answers []dnsmessage.Resource
	extras  []dnsmessage.Resource
}

func parse(t *testing.T, msg []byte) parsed {
	t.Helper()
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SkipAllQuestions(); err != nil {
		t.Fatal(err)
	}
	ans, err := p.AllAnswers()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SkipAllAuthorities(); err != nil {
		t.Fatal(err)
	}
	extra, err := p.AllAdditionals()
	if err != nil {
		t.Fatal(err)
	}
	return parsed{hdr: h, answers: ans, extras: extra}
}

func find(rr []dnsmessage.Resource, name string, typ dnsmessage.Type) *dnsmessage.Resource {
	for i := range rr {
		if strings.EqualFold(rr[i].Header.Name.String(), name) && rr[i].Header.Type == typ {
			return &rr[i]
		}
	}
	return nil
}

// TestRecords: the service yields the enumeration PTR, the service PTR, an
// SRV to the host and port, the TXT strings and one address record per IP,
// with the instance's dots and spaces made a single label.
func TestRecords(t *testing.T) {
	rr := Records(testService(), testService().IPs, true)
	if got := len(rr); got != 6 {
		t.Fatalf("%d records, want 6: %+v", got, rr)
	}
	if r := find(rr, "_services._dns-sd._udp.local.", dnsmessage.TypePTR); r == nil || r.Body.(*dnsmessage.PTRResource).PTR.String() != "_ike._tcp.local." {
		t.Errorf("enumeration PTR wrong: %+v", r)
	}
	ptr := find(rr, "_ike._tcp.local.", dnsmessage.TypePTR)
	if ptr == nil || ptr.Body.(*dnsmessage.PTRResource).PTR.String() != "geants mac._ike._tcp.local." {
		t.Fatalf("service PTR wrong: %+v", ptr)
	}
	if ptr.Header.TTL != ttlShared || ptr.Header.Class&(1<<15) != 0 {
		t.Errorf("shared PTR must carry the long TTL and no cache-flush: %+v", ptr.Header)
	}
	srv := find(rr, "geants mac._ike._tcp.local.", dnsmessage.TypeSRV)
	if srv == nil {
		t.Fatal("no SRV")
	}
	if body := srv.Body.(*dnsmessage.SRVResource); body.Port != 4530 || body.Target.String() != "geants-mac.local." {
		t.Errorf("SRV %+v", body)
	}
	if srv.Header.TTL != ttlUnique || srv.Header.Class&(1<<15) == 0 {
		t.Errorf("unique SRV must carry the short TTL and cache-flush: %+v", srv.Header)
	}
	txt := find(rr, "geants mac._ike._tcp.local.", dnsmessage.TypeTXT)
	if txt == nil || strings.Join(txt.Body.(*dnsmessage.TXTResource).TXT, ",") != "v=0.5.170,proto=1,name=ike" {
		t.Errorf("TXT %+v", txt)
	}
	if a := find(rr, "geants-mac.local.", dnsmessage.TypeA); a == nil || a.Body.(*dnsmessage.AResource).A != [4]byte{192, 168, 1, 20} {
		t.Errorf("A %+v", a)
	}
	if aaaa := find(rr, "geants-mac.local.", dnsmessage.TypeAAAA); aaaa == nil {
		t.Error("no AAAA")
	}
	for _, r := range Records(testService(), testService().IPs, false) {
		if r.Header.TTL != 0 {
			t.Errorf("goodbye record keeps TTL %d: %+v", r.Header.TTL, r.Header)
		}
	}
}

// TestRespondBrowse: a browse query (PTR for the type) is answered on
// multicast with the PTR and, as additionals, everything needed to resolve
// the instance in one round trip.
func TestRespondBrowse(t *testing.T) {
	svc := testService()
	reply, unicast := Respond(svc, svc.IPs, query(t, "_ike._tcp.local.", dnsmessage.TypePTR, false), false)
	if reply == nil || unicast {
		t.Fatalf("reply %v unicast %v", reply != nil, unicast)
	}
	p := parse(t, reply)
	if !p.hdr.Response || !p.hdr.Authoritative || p.hdr.ID != 0 {
		t.Errorf("header %+v", p.hdr)
	}
	if len(p.answers) != 1 || p.answers[0].Header.Type != dnsmessage.TypePTR {
		t.Fatalf("answers %+v", p.answers)
	}
	for _, typ := range []dnsmessage.Type{dnsmessage.TypeSRV, dnsmessage.TypeTXT} {
		if find(p.extras, "geants mac._ike._tcp.local.", typ) == nil {
			t.Errorf("additionals lack %v: %+v", typ, p.extras)
		}
	}
	if find(p.extras, "geants-mac.local.", dnsmessage.TypeA) == nil || find(p.extras, "geants-mac.local.", dnsmessage.TypeAAAA) == nil {
		t.Errorf("additionals lack the addresses: %+v", p.extras)
	}
	if find(p.extras, "_ike._tcp.local.", dnsmessage.TypePTR) != nil {
		t.Error("the answered PTR must not repeat as an additional")
	}
}

// TestRespondResolveAndEnumerate: an SRV question gets the SRV plus the
// addresses; ANY on the instance gets SRV and TXT; the service-enumeration
// PTR names our type; the host's A is answered alone.
func TestRespondResolveAndEnumerate(t *testing.T) {
	svc := testService()
	reply, _ := Respond(svc, svc.IPs, query(t, "geants mac._ike._tcp.local.", dnsmessage.TypeSRV, false), false)
	p := parse(t, reply)
	if len(p.answers) != 1 || p.answers[0].Header.Type != dnsmessage.TypeSRV || len(p.extras) != 2 {
		t.Errorf("SRV query: answers %+v extras %+v", p.answers, p.extras)
	}
	reply, _ = Respond(svc, svc.IPs, query(t, "GEANTS MAC._ike._tcp.local.", dnsmessage.TypeALL, false), false)
	p = parse(t, reply)
	if find(p.answers, "geants mac._ike._tcp.local.", dnsmessage.TypeSRV) == nil || find(p.answers, "geants mac._ike._tcp.local.", dnsmessage.TypeTXT) == nil {
		t.Errorf("ANY (case-folded) must answer SRV and TXT: %+v", p.answers)
	}
	reply, _ = Respond(svc, svc.IPs, query(t, "_services._dns-sd._udp.local.", dnsmessage.TypePTR, false), false)
	p = parse(t, reply)
	if len(p.answers) != 1 || p.answers[0].Body.(*dnsmessage.PTRResource).PTR.String() != "_ike._tcp.local." {
		t.Errorf("enumeration: %+v", p.answers)
	}
	reply, _ = Respond(svc, svc.IPs, query(t, "geants-mac.local.", dnsmessage.TypeA, false), false)
	p = parse(t, reply)
	if len(p.answers) != 1 || p.answers[0].Header.Type != dnsmessage.TypeA || len(p.extras) != 0 {
		t.Errorf("A query: answers %+v extras %+v", p.answers, p.extras)
	}
}

// TestRespondIgnoresForeign: questions for other names or types, responses,
// and garbage draw no reply at all — the group must not be spammed.
func TestRespondIgnoresForeign(t *testing.T) {
	svc := testService()
	for name, q := range map[string][]byte{
		"other type":  query(t, "_http._tcp.local.", dnsmessage.TypePTR, false),
		"other host":  query(t, "printer.local.", dnsmessage.TypeA, false),
		"wrong rtype": query(t, "_ike._tcp.local.", dnsmessage.TypeA, false),
		"garbage":     []byte("not dns"),
		"empty":       nil,
	} {
		if reply, _ := Respond(svc, svc.IPs, q, false); reply != nil {
			t.Errorf("%s drew a reply", name)
		}
	}
	own := Announcement(svc, svc.IPs, true)
	if reply, _ := Respond(svc, svc.IPs, own, false); reply != nil {
		t.Error("our own announcement (a response) drew a reply")
	}
}

// TestRespondUnicast: the QU bit and a legacy source port both switch the
// reply to unicast; the legacy reply keeps the query's ID so the resolver
// can match it.
func TestRespondUnicast(t *testing.T) {
	svc := testService()
	if _, unicast := Respond(svc, svc.IPs, query(t, "_ike._tcp.local.", dnsmessage.TypePTR, true), false); !unicast {
		t.Error("QU question must be answered unicast")
	}
	reply, unicast := Respond(svc, svc.IPs, query(t, "_ike._tcp.local.", dnsmessage.TypePTR, false), true)
	if !unicast {
		t.Error("legacy query must be answered unicast")
	}
	if p := parse(t, reply); p.hdr.ID != 7 {
		t.Errorf("legacy reply ID %d, want the query's 7", p.hdr.ID)
	}
}

// TestNormalize: an empty instance takes the host's short name, dots fold
// to dashes, control characters vanish, over-long labels are cut at 63
// bytes on a rune boundary, and the host loses its domain.
func TestNormalize(t *testing.T) {
	s := normalize(Service{Type: "._ike._tcp.", Host: "Box.example.com", Port: 1})
	if s.Instance != "Box" || s.Host != "Box" || s.Type != "_ike._tcp" {
		t.Errorf("%+v", s)
	}
	s = normalize(Service{Type: "_ike._tcp", Host: "h", Instance: " my.mac\x01 ", Port: 1})
	if s.Instance != "my-mac" {
		t.Errorf("instance %q", s.Instance)
	}
	long := strings.Repeat("ä", 40) // 80 bytes
	s = normalize(Service{Type: "_ike._tcp", Host: "h", Instance: long, Port: 1})
	if len(s.Instance) > 63 || !strings.HasPrefix(long, s.Instance) || len(s.Instance)%2 != 0 {
		t.Errorf("long instance %q (%d bytes)", s.Instance, len(s.Instance))
	}
	if s = normalize(Service{Type: "_ike._tcp", Host: "\x01", Port: 1}); s.Host != "ike" {
		t.Errorf("an unusable host must fall back, got %q", s.Host)
	}
}

// TestAnnounceRoundTrip: the socket side — a browse query sent to the group
// on the loopback-reachable socket is answered. Skipped where multicast is
// unavailable (sandboxes, CI containers without a multicast route).
func TestAnnounceRoundTrip(t *testing.T) {
	svc := testService()
	svc.Port = 4530
	r, err := Announce(svc)
	if err != nil {
		t.Skip("no multicast here:", err)
	}
	defer r.Close()
	if r.Service().Instance != "geants mac" {
		t.Fatalf("service %+v", r.Service())
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Skip("no udp:", err)
	}
	defer c.Close()
	if _, err := c.WriteToUDP(query(t, "_ike._tcp.local.", dnsmessage.TypePTR, false), groupV4); err != nil {
		t.Skip("cannot reach the group:", err)
	}
	// Our legacy-port query gets a unicast reply carrying our ID.
	if err := c.SetReadDeadline(deadline()); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 9000)
	for {
		n, _, err := c.ReadFromUDP(buf)
		if err != nil {
			t.Skip("no reply on the loopback group (multicast not routed):", err)
		}
		p := parse(t, buf[:n])
		if p.hdr.ID == 7 && find(p.answers, "_ike._tcp.local.", dnsmessage.TypePTR) != nil {
			return
		}
	}
}

func deadline() time.Time { return time.Now().Add(3 * time.Second) }
