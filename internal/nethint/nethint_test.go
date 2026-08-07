package nethint

import "testing"

// TestDescribeCIDRv4 covers the IPv4 readings, including the point-to-point
// prefixes where the network/broadcast subtraction does not apply.
func TestDescribeCIDRv4(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"10.0.0.0/8", "10.0.0.0–10.255.255.255, 16,777,214 hosts"},
		{"192.168.1.0/24", "192.168.1.0–192.168.1.255, 254 hosts"},
		{"172.16.0.0/12", "172.16.0.0–172.31.255.255, 1,048,574 hosts"},
		{"0.0.0.0/0", "0.0.0.0–255.255.255.255, 4,294,967,294 hosts"},
		{"10.0.0.0/30", "10.0.0.0–10.0.0.3, 2 hosts"},
		{"10.0.0.0/31", "10.0.0.0–10.0.0.1, 2 hosts"}, // RFC 3021
		{"10.0.0.7/32", "10.0.0.7, 1 host"},
		// Host bits set: the hint describes the network the address falls in.
		{"10.1.2.3/8", "10.0.0.0–10.255.255.255, 16,777,214 hosts"},
	}
	for _, c := range cases {
		got, ok := DescribeCIDR(c.text)
		if !ok {
			t.Errorf("DescribeCIDR(%q) reported no hint", c.text)
			continue
		}
		if got != c.want {
			t.Errorf("DescribeCIDR(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestDescribeCIDRv6 covers the IPv6 readings: address counts, not host
// counts, with the huge spans left as a power of two.
func TestDescribeCIDRv6(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"2001:db8::/32", "2001:db8::–2001:db8:ffff:ffff:ffff:ffff:ffff:ffff, 2^96 addresses"},
		{"::/0", "::–ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff, 2^128 addresses"},
		{"fe80::/127", "fe80::–fe80::1, 2 addresses"},
		{"2001:db8::1/128", "2001:db8::1, 1 address"},
		{"2001:db8::/112", "2001:db8::–2001:db8::ffff, 65,536 addresses"},
	}
	for _, c := range cases {
		got, ok := DescribeCIDR(c.text)
		if !ok {
			t.Errorf("DescribeCIDR(%q) reported no hint", c.text)
			continue
		}
		if got != c.want {
			t.Errorf("DescribeCIDR(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestDescribeCIDRRejects: anything net/netip does not parse as a prefix gets
// no hint — no hint beats a wrong one.
func TestDescribeCIDRRejects(t *testing.T) {
	for _, text := range []string{
		"10.0.0.0", "10.0.0.0/", "/8", "10.0.0.0/33", "999.0.0.0/8",
		"2001:db8::/129", "not/8", "1/2", "v1.2/3",
	} {
		if hint, ok := DescribeCIDR(text); ok {
			t.Errorf("DescribeCIDR(%q) = %q, want no hint", text, hint)
		}
	}
}

// TestDecodeIDN: punycode round-trips to its Unicode form, and only a label
// mixing scripts is marked.
func TestDecodeIDN(t *testing.T) {
	cases := []struct {
		host      string
		want      string
		wantMixed bool
	}{
		{"xn--mnchen-3ya.de", "münchen.de", false},
		{"xn--e1afmkfd.xn--p1ai", "пример.рф", false},
		{"xn--80ak6aa92e.com", "аррӏе.com", true}, // Cyrillic look-alike of apple.com
		{"xn--fsq.com", "例.com", false},
		{"www.xn--mnchen-3ya.de", "www.münchen.de", false},
	}
	for _, c := range cases {
		got, mixed, ok := DecodeIDN(c.host)
		if !ok {
			t.Errorf("DecodeIDN(%q) reported no hint", c.host)
			continue
		}
		if got != c.want {
			t.Errorf("DecodeIDN(%q) = %q, want %q", c.host, got, c.want)
		}
		if mixed != c.wantMixed {
			t.Errorf("DecodeIDN(%q) mixed = %v, want %v", c.host, mixed, c.wantMixed)
		}
	}
}

// TestDecodeIDNRejects: a name without an ACE label, or one whose punycode
// does not decode, carries no hint.
func TestDecodeIDNRejects(t *testing.T) {
	for _, host := range []string{
		"example.com", "xn--", "xn--.com", "xn--!!!.com", "münchen.de", "",
	} {
		if got, _, ok := DecodeIDN(host); ok {
			t.Errorf("DecodeIDN(%q) = %q, want no hint", host, got)
		}
	}
}

// TestConfusableLabel: a non-Latin label spelled entirely in Latin
// look-alikes is a homograph even though it mixes nothing.
func TestConfusableLabel(t *testing.T) {
	cases := []struct {
		label string
		want  bool
	}{
		{"аррӏе", true},    // all-Cyrillic "apple"
		{"пример", false},  // ordinary Cyrillic word
		{"рф", false},      // ф has no Latin look-alike
		{"apple", false},   // plain ASCII
		{"münchen", false}, // Latin with a diacritic
		{"о", false},       // one letter spells nothing
		{"日本語", false},
	}
	for _, c := range cases {
		if got := confusableLabel(c.label); got != c.want {
			t.Errorf("confusableLabel(%q) = %v, want %v", c.label, got, c.want)
		}
	}
}

// TestMixedLabelScriptPairs: the script pairs ordinary names combine are not
// homographs — Japanese writes Han with the kana, Korean Han with Hangul.
func TestMixedLabelScriptPairs(t *testing.T) {
	cases := []struct {
		label string
		want  bool
	}{
		{"日本語", false},
		{"の日本", false},     // Hiragana + Han
		{"한국어漢字", false},   // Hangul + Han
		{"münchen", false}, // Latin only
		{"пример", false},  // Cyrillic only
		{"аpple", true},    // Cyrillic а + Latin
		{"gοogle", true},   // Greek omicron + Latin
		{"test123-x", false},
	}
	for _, c := range cases {
		if got := mixedLabel(c.label); got != c.want {
			t.Errorf("mixedLabel(%q) = %v, want %v", c.label, got, c.want)
		}
	}
}

// TestGroup covers the thousands separator a host count is read in.
func TestGroup(t *testing.T) {
	cases := map[uint64]string{0: "0", 254: "254", 1000: "1,000", 16777214: "16,777,214", 4294967294: "4,294,967,294"}
	for n, want := range cases {
		if got := group(n); got != want {
			t.Errorf("group(%d) = %q, want %q", n, got, want)
		}
	}
}
