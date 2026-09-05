// Package mdns is a small multicast-DNS / DNS-SD responder (#2522): it
// announces one service instance on the local link — "_ike._tcp.local" for
// the network deep-link endpoint — and answers the browse and resolve queries
// a Bonjour / Avahi client sends, so a phone or a script finds a running IKE
// without being told its address.
//
// The package is a leaf: the wire format is golang.org/x/net/dns/dnsmessage,
// the sockets are the standard library's multicast listeners. It implements
// the responder half of RFC 6762 / 6763 that discovery needs — PTR / SRV /
// TXT / A / AAAA answers, unsolicited announcements on start, a goodbye on
// stop, unicast replies to legacy and QU questions — and deliberately not
// probing or conflict resolution: a second IKE on the same host names its
// instance differently ([network].name) rather than negotiating.
package mdns

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Service is one announced instance.
type Service struct {
	// Instance is the human label ("geants-mac"); dots are folded to
	// dashes since the wire format has no escape for them. Empty selects
	// the host's short name.
	Instance string
	// Type is the DNS-SD service type, "_ike._tcp".
	Type string
	// Port is the TCP port the SRV record points at.
	Port int
	// TXT holds the key=value strings of the TXT record.
	TXT []string
	// Host is the host name the SRV record targets, without the ".local."
	// suffix; empty selects os.Hostname's short form.
	Host string
	// IPs are the addresses advertised in A / AAAA records. Nil advertises
	// every non-loopback address of every interface that is up, resolved
	// afresh for each answer so an address change needs no restart.
	IPs []net.IP
}

// The multicast groups and port of RFC 6762.
var (
	groupV4 = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	groupV6 = &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}
)

// TTLs: the shared PTR records live long (a browser polls them anyway); the
// per-instance records short, so a stale host disappears within minutes.
const (
	ttlShared = 4500
	ttlUnique = 120
)

// Responder is one running announcer.
type Responder struct {
	svc   Service
	conns []*net.UDPConn
	once  sync.Once
	done  chan struct{}
}

// Announce joins the mDNS groups, sends the initial announcements and
// answers queries until Close. It fails only when neither the IPv4 nor the
// IPv6 group could be joined.
func Announce(svc Service) (*Responder, error) {
	svc = normalize(svc)
	if svc.Type == "" || svc.Port < 1 || svc.Port > 65535 {
		return nil, errors.New("mdns: a service type and a port are required")
	}
	r := &Responder{svc: svc, done: make(chan struct{})}
	if c, err := net.ListenMulticastUDP("udp4", nil, groupV4); err == nil {
		r.conns = append(r.conns, c)
	}
	if c, err := net.ListenMulticastUDP("udp6", nil, groupV6); err == nil {
		r.conns = append(r.conns, c)
	}
	if len(r.conns) == 0 {
		return nil, errors.New("mdns: cannot join the multicast group on any interface")
	}
	for _, c := range r.conns {
		go r.serve(c)
	}
	// Two announcements a second apart (RFC 6762 §8.3): the first may be
	// lost to a browser that was mid-refresh.
	go func() {
		r.multicast(Announcement(r.svc, r.addrs(), true))
		select {
		case <-time.After(time.Second):
			r.multicast(Announcement(r.svc, r.addrs(), true))
		case <-r.done:
		}
	}()
	return r, nil
}

// Service is the announced instance after normalisation — what a browser
// sees.
func (r *Responder) Service() Service { return r.svc }

// Close sends the goodbye (every record with TTL 0, so browsers drop the
// instance at once instead of waiting the TTL out) and stops answering.
func (r *Responder) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		close(r.done)
		r.multicast(Announcement(r.svc, r.addrs(), false))
		for _, c := range r.conns {
			_ = c.Close()
		}
	})
}

// serve answers the queries arriving on one group socket.
func (r *Responder) serve(c *net.UDPConn) {
	buf := make([]byte, 9000)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		reply, unicast := Respond(r.svc, r.addrs(), buf[:n], from.Port != groupV4.Port)
		if reply == nil {
			continue
		}
		if unicast {
			_, _ = c.WriteToUDP(reply, from)
			continue
		}
		_, _ = c.WriteToUDP(reply, group(c))
	}
}

// multicast sends one message to every joined group.
func (r *Responder) multicast(msg []byte) {
	for _, c := range r.conns {
		_, _ = c.WriteToUDP(msg, group(c))
	}
}

// group is the multicast destination of a socket, by its family.
func group(c *net.UDPConn) *net.UDPAddr {
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok && a.IP.To4() == nil && len(a.IP) == net.IPv6len {
		return groupV6
	}
	return groupV4
}

// addrs is the address list to advertise: the configured one, else the
// live interface addresses.
func (r *Responder) addrs() []net.IP {
	if r.svc.IPs != nil {
		return r.svc.IPs
	}
	return InterfaceIPs()
}

// InterfaceIPs lists every non-loopback unicast address of the interfaces
// that are up — IPv4 first, then IPv6 (global and link-local; a link-local
// IPv6 address is exactly what a browser on the same link can use).
func InterfaceIPs() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var v4, v6 []net.IP
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsMulticast() || ipn.IP.IsUnspecified() {
				continue
			}
			if ip4 := ipn.IP.To4(); ip4 != nil {
				v4 = append(v4, ip4)
			} else if ipn.IP.IsGlobalUnicast() || ipn.IP.IsLinkLocalUnicast() {
				v6 = append(v6, ipn.IP)
			}
		}
	}
	return append(v4, v6...)
}

// normalize fills the defaults and folds the names into wire-safe labels.
func normalize(svc Service) Service {
	host := svc.Host
	if host == "" {
		host, _ = os.Hostname()
	}
	host = label(shortHost(host), "ike")
	if svc.Instance == "" {
		svc.Instance = host
	}
	svc.Instance = label(svc.Instance, host)
	svc.Host = host
	svc.Type = strings.Trim(strings.TrimSpace(svc.Type), ".")
	return svc
}

// shortHost strips a domain ("mac.local", "box.example.com") to its first
// label.
func shortHost(h string) string {
	if i := strings.IndexByte(h, '.'); i > 0 {
		return h[:i]
	}
	return h
}

// label makes s a single DNS label: trimmed, dots folded to dashes, control
// characters dropped, at most 63 bytes; empty falls back to def.
func label(s, def string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == '.':
			b.WriteByte('-')
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	for len(out) > 63 {
		_, size := lastRune(out)
		out = out[:len(out)-size]
	}
	if out == "" {
		return def
	}
	return out
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xC0 != 0x80 {
			r := []rune(s[i:])
			return r[0], len(s) - i
		}
	}
	return 0, 1
}

// Names of the announced records.
func (svc Service) serviceName() string  { return svc.Type + ".local." }
func (svc Service) instanceName() string { return svc.Instance + "." + svc.Type + ".local." }
func (svc Service) hostName() string     { return svc.Host + ".local." }

const enumerationName = "_services._dns-sd._udp.local."

// Records builds every resource record of the instance: the enumeration and
// service PTRs, the SRV, the TXT and one A / AAAA per address. alive=false
// zeroes the TTLs for a goodbye.
func Records(svc Service, ips []net.IP, alive bool) []dnsmessage.Resource {
	shared, unique := uint32(ttlShared), uint32(ttlUnique)
	if !alive {
		shared, unique = 0, 0
	}
	name := func(s string) dnsmessage.Name { n, _ := dnsmessage.NewName(s); return n }
	hdr := func(n string, t dnsmessage.Type, ttl uint32, flush bool) dnsmessage.ResourceHeader {
		h := dnsmessage.ResourceHeader{Name: name(n), Type: t, Class: dnsmessage.ClassINET, TTL: ttl}
		if flush {
			h.Class |= 1 << 15 // cache-flush: this record replaces cached ones
		}
		return h
	}
	svc = normalize(svc) // idempotent: a Responder's service is already normal
	rr := []dnsmessage.Resource{
		{Header: hdr(enumerationName, dnsmessage.TypePTR, shared, false), Body: &dnsmessage.PTRResource{PTR: name(svc.serviceName())}},
		{Header: hdr(svc.serviceName(), dnsmessage.TypePTR, shared, false), Body: &dnsmessage.PTRResource{PTR: name(svc.instanceName())}},
		{Header: hdr(svc.instanceName(), dnsmessage.TypeSRV, unique, true), Body: &dnsmessage.SRVResource{Port: uint16(svc.Port), Target: name(svc.hostName())}},
		{Header: hdr(svc.instanceName(), dnsmessage.TypeTXT, unique, true), Body: &dnsmessage.TXTResource{TXT: txtStrings(svc.TXT)}},
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			var a [4]byte
			copy(a[:], ip4)
			rr = append(rr, dnsmessage.Resource{Header: hdr(svc.hostName(), dnsmessage.TypeA, unique, true), Body: &dnsmessage.AResource{A: a}})
		} else if len(ip) == net.IPv6len {
			var a [16]byte
			copy(a[:], ip)
			rr = append(rr, dnsmessage.Resource{Header: hdr(svc.hostName(), dnsmessage.TypeAAAA, unique, true), Body: &dnsmessage.AAAAResource{AAAA: a}})
		}
	}
	return rr
}

// txtStrings caps each string at the 255 bytes a TXT character-string
// holds; an empty list becomes the single empty string DNS-SD requires.
func txtStrings(in []string) []string {
	if len(in) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if len(s) > 255 {
			s = s[:255]
		}
		out = append(out, s)
	}
	return out
}

// Announcement packs every record into one unsolicited response — the
// start-up announcement (alive) or the goodbye.
func Announcement(svc Service, ips []net.IP, alive bool) []byte {
	return pack(0, Records(svc, ips, alive), nil)
}

// Respond answers one query packet. It returns nil when the packet is not a
// query or asks for nothing of ours. legacy says the query came from a port
// other than 5353 (a one-shot resolver such as dig), which — like a question
// with the QU bit — wants its answer unicast; the second result says so.
func Respond(svc Service, ips []net.IP, query []byte, legacy bool) (reply []byte, unicast bool) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil || h.Response || h.OpCode != 0 {
		return nil, false
	}
	qs, err := p.AllQuestions()
	if err != nil {
		return nil, false
	}
	all := Records(svc, ips, true)
	var answers, extra []dnsmessage.Resource
	seen := map[string]bool{}
	add := func(list *[]dnsmessage.Resource, r dnsmessage.Resource) {
		k := r.Header.Name.String() + "/" + r.Header.Type.String()
		if r.Body != nil {
			k += "/" + r.Body.GoString()
		}
		if !seen[k] {
			seen[k] = true
			*list = append(*list, r)
		}
	}
	for _, q := range qs {
		if q.Class&0x7fff != dnsmessage.ClassINET && q.Class&0x7fff != 255 {
			continue
		}
		if q.Class&(1<<15) != 0 {
			unicast = true
		}
		qn := strings.ToLower(q.Name.String())
		for _, r := range all {
			if strings.ToLower(r.Header.Name.String()) != qn {
				continue
			}
			if q.Type != dnsmessage.TypeALL && q.Type != r.Header.Type {
				continue
			}
			add(&answers, r)
			// DNS-SD §12: a PTR answer carries the SRV/TXT/addresses it
			// leads to, an SRV its addresses, so one round trip resolves.
			switch r.Header.Type {
			case dnsmessage.TypePTR:
				for _, x := range all {
					if x.Header.Type == dnsmessage.TypeSRV || x.Header.Type == dnsmessage.TypeTXT || x.Header.Type == dnsmessage.TypeA || x.Header.Type == dnsmessage.TypeAAAA {
						add(&extra, x)
					}
				}
			case dnsmessage.TypeSRV:
				for _, x := range all {
					if x.Header.Type == dnsmessage.TypeA || x.Header.Type == dnsmessage.TypeAAAA {
						add(&extra, x)
					}
				}
			}
		}
	}
	if len(answers) == 0 {
		return nil, false
	}
	// A record in the answer section is not repeated as additional.
	var extras []dnsmessage.Resource
	for _, x := range extra {
		dup := false
		for _, a := range answers {
			if a.Header.Name == x.Header.Name && a.Header.Type == x.Header.Type && a.Body.GoString() == x.Body.GoString() {
				dup = true
				break
			}
		}
		if !dup {
			extras = append(extras, x)
		}
	}
	id := uint16(0)
	if legacy {
		id = h.ID // a legacy resolver matches the answer by ID
		unicast = true
	}
	return pack(id, answers, extras), unicast
}

// pack serialises an authoritative response with the given sections.
func pack(id uint16, answers, extras []dnsmessage.Resource) []byte {
	b := dnsmessage.NewBuilder(make([]byte, 0, 512), dnsmessage.Header{ID: id, Response: true, Authoritative: true})
	b.EnableCompression()
	_ = b.StartQuestions()
	_ = b.StartAnswers()
	for _, r := range answers {
		_ = putResource(&b, r)
	}
	_ = b.StartAdditionals()
	for _, r := range extras {
		_ = putResource(&b, r)
	}
	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}

func putResource(b *dnsmessage.Builder, r dnsmessage.Resource) error {
	switch body := r.Body.(type) {
	case *dnsmessage.PTRResource:
		return b.PTRResource(r.Header, *body)
	case *dnsmessage.SRVResource:
		return b.SRVResource(r.Header, *body)
	case *dnsmessage.TXTResource:
		return b.TXTResource(r.Header, *body)
	case *dnsmessage.AResource:
		return b.AResource(r.Header, *body)
	case *dnsmessage.AAAAResource:
		return b.AAAAResource(r.Header, *body)
	}
	return nil
}
