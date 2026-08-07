// Package nethint explains the two network literals that are unreadable by
// eye (#1653): a CIDR prefix and a punycode hostname.
//
// A CIDR literal names a range nobody computes in their head — `10.0.0.0/8`
// is sixteen million addresses ending at `10.255.255.255`, `2001:db8::/32` is
// 2^96 of them — so the editor draws the range and the count after the
// literal. A punycode label (`xn--…`) hides the name it stands for, which is
// both a readability problem and a phishing one: `xn--80ak6aa92e.com` is
// `аррӏе.com` in Cyrillic, not `apple.com`. The decoded form draws after the
// literal, and a label mixing scripts — the shape a homograph attack needs —
// carries its own capture so it draws in the warning colour.
//
// Both are stand-ins on the #1585 conceal channel, display-only and revealed
// under the caret (#1594) like every other hint family; the span producers
// live in spans.go. It is a leaf package — pure Go plus golang.org/x/net/idna
// over internal/lang's span type — so any language plugin can call it.
package nethint

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// The highlight captures carried by the hint spans. The editor treats each as
// a conceal family: the two IDN captures share the editor.idn_hints toggle
// (they are one family drawn in two colours), the CIDR one has
// editor.cidr_hints of its own.
const (
	CIDRCapture = "net.cidr"
	// IDNCapture is a punycode name that decodes to a single script.
	IDNCapture = "net.idn"
	// IDNMixedCapture is a punycode name whose decoded form carries the
	// homograph shape: scripts mixed inside one label, or a whole label of
	// Latin look-alikes. Drawn in the warning colour.
	IDNMixedCapture = "net.idn.mixed"
)

// Gap is the separator drawn between the literal and its hint inside the
// stand-in — the same two spaces the cron and number hints use.
const Gap = "  "

// DescribeCIDR renders a prefix as its address range and size:
// `10.0.0.0/8` as "10.0.0.0–10.255.255.255, 16,777,214 hosts". It reports
// false when text is not a CIDR prefix.
//
// Host counts follow what the protocol actually leaves usable. IPv4 subtracts
// the network and broadcast addresses, except on the point-to-point prefixes
// where there are none to subtract: /31 carries two hosts (RFC 3021) and /32
// one. IPv6 has no broadcast address and no reserved host, so its prefixes
// count addresses rather than hosts.
func DescribeCIDR(text string) (string, bool) {
	p, err := netip.ParsePrefix(strings.TrimSpace(text))
	if err != nil {
		return "", false
	}
	// Host bits set (`10.0.0.1/8`) is legal input and a common slip; the hint
	// describes the network the address falls in, which is the useful reading.
	p = p.Masked()
	first := p.Addr()
	last := lastAddr(p)
	rangeText := first.String()
	if last != first {
		rangeText = first.String() + "–" + last.String()
	}
	return rangeText + ", " + sizeText(p), true
}

// lastAddr returns the highest address in the prefix — the broadcast address
// of an IPv4 network — by setting every host bit.
func lastAddr(p netip.Prefix) netip.Addr {
	bytes := p.Addr().AsSlice()
	host := len(bytes)*8 - p.Bits()
	for i := len(bytes) - 1; i >= 0 && host > 0; i-- {
		n := host
		if n > 8 {
			n = 8
		}
		bytes[i] |= byte(1<<n - 1)
		host -= n
	}
	addr, _ := netip.AddrFromSlice(bytes)
	return addr.Unmap()
}

// sizeText renders the prefix size: usable hosts for IPv4, addresses for
// IPv6, with the huge IPv6 spans left as a power of two — "2^96 addresses"
// says what 79 octillion never will.
func sizeText(p netip.Prefix) string {
	width := 32
	if p.Addr().Is6() {
		width = 128
	}
	host := width - p.Bits()
	if p.Addr().Is4() {
		switch {
		case host == 0:
			return "1 host"
		case host == 1:
			return "2 hosts" // RFC 3021: a /31 has no network or broadcast address
		default:
			return group(uint64(1)<<host-2) + " hosts"
		}
	}
	switch {
	case host == 0:
		return "1 address"
	case host <= 20:
		return group(uint64(1)<<host) + " addresses"
	default:
		return fmt.Sprintf("2^%d addresses", host)
	}
}

// group renders n with thousands separators, the form a host count is read
// in: 16777214 as "16,777,214".
func group(n uint64) string {
	digits := strconv.FormatUint(n, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

// DecodeIDN decodes the punycode labels of a hostname to their Unicode form,
// reporting whether any decoded label carries the homograph shape — mixed
// scripts in one label, or a non-Latin label written entirely in Latin
// look-alikes. It reports false when
// the name carries no punycode label, when decoding fails, or when the result
// would not be readable text — no hint beats a wrong one.
func DecodeIDN(host string) (decoded string, mixed, ok bool) {
	if !hasPunycode(host) {
		return "", false, false
	}
	decoded, err := idna.Punycode.ToUnicode(host)
	if err != nil || decoded == host || !readable(decoded) {
		return "", false, false
	}
	for _, label := range strings.Split(decoded, ".") {
		if mixedLabel(label) || confusableLabel(label) {
			return decoded, true, true
		}
	}
	return decoded, false, true
}

// hasPunycode reports whether any label of host carries the ACE prefix.
func hasPunycode(host string) bool {
	for _, label := range strings.Split(host, ".") {
		if len(label) > 4 && strings.EqualFold(label[:4], "xn--") {
			return true
		}
	}
	return false
}

// readable rejects a decoded form that is not text a reader can see —
// unprintable runes, control characters and the bidi/format characters an
// attack would hide a name behind (IsPrint excludes all three).
func readable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// mixedLabel reports whether one label mixes scripts — the homograph shape,
// where a Latin word carries a Cyrillic or Greek look-alike. Script-neutral
// runes (digits, the hyphen, combining marks) are ignored, and the script
// pairs that ordinary names combine are allowed: Japanese writes Han with the
// kana, Korean Han with Hangul, Traditional Chinese Han with Bopomofo.
func mixedLabel(label string) bool {
	scripts := map[string]bool{}
	for _, r := range label {
		if r < 0x80 && !unicode.IsLetter(r) {
			continue // ASCII digits, the hyphen: script-neutral
		}
		if s := scriptOf(r); s != "" {
			scripts[s] = true
		}
	}
	if len(scripts) <= 1 {
		return false
	}
	for _, allowed := range []map[string]bool{
		{"Han": true, "Hiragana": true, "Katakana": true},
		{"Han": true, "Hangul": true},
		{"Han": true, "Bopomofo": true},
	} {
		if subset(scripts, allowed) {
			return false
		}
	}
	return true
}

// latinLookAlikes are the Cyrillic and Greek letters that render as an ASCII
// letter in nearly every font. A label written entirely in them is the other
// half of the homograph shape: `аррӏе.com` is not *mixed* — it is Cyrillic
// throughout — but it reads as `apple.com`, which is the whole point of it.
// The table is deliberately small: the letters that carry the attack, not the
// full Unicode confusables data.
var latinLookAlikes = map[rune]bool{
	// Cyrillic
	'а': true, 'в': true, 'е': true, 'ё': true, 'з': true, 'и': true, 'к': true,
	'м': true, 'н': true, 'о': true, 'р': true, 'с': true, 'т': true, 'у': true,
	'х': true, 'ѕ': true, 'і': true, 'ј': true, 'ԁ': true, 'ԛ': true, 'ԝ': true,
	'ӏ': true, 'ү': true, 'ӕ': true,
	// Greek
	'α': true, 'β': true, 'γ': true, 'ε': true, 'η': true, 'ι': true, 'κ': true,
	'ν': true, 'ο': true, 'ρ': true, 'σ': true, 'τ': true, 'υ': true, 'χ': true,
	'ϲ': true, 'ϳ': true, 'ϵ': true,
}

// LatinLookAlike reports whether r is one of the Cyrillic/Greek letters in the
// table above — exported for the buffer-wide confusable scan (#1654), which
// flags the same letters when they hide inside an otherwise-ASCII identifier.
func LatinLookAlike(r rune) bool { return latinLookAlikes[r] }

// confusableLabel reports whether a single-script non-Latin label is written
// entirely in letters that look Latin — the whole-script homograph. Labels of
// one letter are not enough to spell anything, so they do not count.
func confusableLabel(label string) bool {
	letters := 0
	for _, r := range label {
		if r < 0x80 {
			if unicode.IsLetter(r) {
				return false // a real Latin letter: this is the mixed case, not this one
			}
			continue
		}
		if !latinLookAlikes[r] {
			return false
		}
		letters++
	}
	return letters >= 2
}

// subset reports whether every script in have is listed in allowed.
func subset(have, allowed map[string]bool) bool {
	for s := range have {
		if !allowed[s] {
			return false
		}
	}
	return true
}

// scriptOf names the Unicode script a rune belongs to, or "" for the
// script-neutral categories (Common, Inherited) and unassigned runes.
func scriptOf(r rune) string {
	for name, table := range unicode.Scripts {
		if name == "Common" || name == "Inherited" {
			continue
		}
		if unicode.Is(table, r) {
			return name
		}
	}
	return ""
}
