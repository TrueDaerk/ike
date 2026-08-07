package peminfo

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// now is the fixed clock every expiry assertion is made against.
var now = time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

// certOpts is the shape of a test certificate.
type certOpts struct {
	cn         string
	issuer     string
	notBefore  time.Time
	notAfter   time.Time
	dnsNames   []string
	ips        []net.IP
	ed25519Key bool
	ecdsaKey   bool
}

// makeCert builds a self-signed certificate and returns its PEM lines.
func makeCert(t *testing.T, o certOpts) []string {
	t.Helper()
	var pub, priv any
	switch {
	case o.ed25519Key:
		p, s, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("ed25519 key: %v", err)
		}
		pub, priv = p, s
	case o.ecdsaKey:
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa key: %v", err)
		}
		pub, priv = &k.PublicKey, k
	default:
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("rsa key: %v", err)
		}
		pub, priv = &k.PublicKey, k
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: o.cn},
		NotBefore:    o.notBefore,
		NotAfter:     o.notAfter,
		DNSNames:     o.dnsNames,
		IPAddresses:  o.ips,
	}
	// x509.CreateCertificate takes the issuer from the parent's subject, so a
	// distinct issuer needs a real CA to sign with; without one the
	// certificate is self-signed and issuer == subject.
	parent, signer := tmpl, priv
	if o.issuer != "" {
		parent, signer = makeCA(t, o.issuer, o.notBefore, o.notAfter)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pemLines(t, "CERTIFICATE", der)
}

// makeCA builds a self-signed CA certificate to sign leaves with.
func makeCA(t *testing.T, cn string, notBefore, notAfter time.Time) (*x509.Certificate, any) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatalf("create ca: %v", err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}
	return ca, k
}

// pemLines encodes der as a PEM block and splits it into buffer lines.
func pemLines(t *testing.T, typ string, der []byte) []string {
	t.Helper()
	text := string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// one scans lines and requires exactly one block.
func one(t *testing.T, lines []string) Block {
	t.Helper()
	blocks := ScanAt(lines, now)
	if len(blocks) != 1 {
		t.Fatalf("ScanAt found %d blocks; want 1", len(blocks))
	}
	return blocks[0]
}

// TestCertificateSummary: a healthy certificate reads out its subject CN,
// validity window, issuer, key type and SANs, and stays severity-free.
func TestCertificateSummary(t *testing.T) {
	lines := makeCert(t, certOpts{
		cn:        "example.com",
		issuer:    "Example CA",
		notBefore: now.Add(-30 * 24 * time.Hour),
		notAfter:  now.Add(300 * 24 * time.Hour),
		dnsNames:  []string{"example.com", "www.example.com"},
		ips:       []net.IP{net.ParseIP("10.0.0.1")},
	})
	b := one(t, lines)
	if b.Start != 0 || b.End != len(lines)-1 {
		t.Errorf("block range = [%d, %d]; want [0, %d]", b.Start, b.End, len(lines)-1)
	}
	for _, want := range []string{
		"certificate", "CN=example.com", "issuer=Example CA", "RSA-2048",
		"2026-07-08→2027-06-03", "SAN=example.com, www.example.com, 10.0.0.1",
	} {
		if !strings.Contains(b.Summary, want) {
			t.Errorf("summary %q missing %q", b.Summary, want)
		}
	}
	if b.Severity != SevNone {
		t.Errorf("severity = %v; want SevNone for a certificate valid for 300 days", b.Severity)
	}
}

// TestCertificateExpiryVerdicts: the expiry state and its severity are the
// point of the whole feature — expired and not-yet-valid are errors, an expiry
// inside WarnWindow is a warning, and a healthy window says nothing.
func TestCertificateExpiryVerdicts(t *testing.T) {
	cases := []struct {
		name      string
		notBefore time.Time
		notAfter  time.Time
		state     string
		sev       Severity
	}{
		{"expired", now.Add(-400 * 24 * time.Hour), now.Add(-24 * time.Hour), "expired", SevError},
		{"not yet valid", now.Add(24 * time.Hour), now.Add(400 * 24 * time.Hour), "not yet valid", SevError},
		{"expiring", now.Add(-24 * time.Hour), now.Add(12 * 24 * time.Hour), "expires in 12d", SevWarn},
		{"expiring in hours", now.Add(-24 * time.Hour), now.Add(5 * time.Hour), "expires in 5h", SevWarn},
		{"edge of the window", now.Add(-24 * time.Hour), now.Add(WarnWindow - time.Hour), "expires in 29d", SevWarn},
		{"healthy", now.Add(-24 * time.Hour), now.Add(WarnWindow + time.Hour), "", SevNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := one(t, makeCert(t, certOpts{cn: "example.com", notBefore: c.notBefore, notAfter: c.notAfter}))
			if b.Severity != c.sev {
				t.Errorf("severity = %v; want %v (summary %q)", b.Severity, c.sev, b.Summary)
			}
			if c.state == "" {
				for _, bad := range []string{"expired", "not yet valid", "expires in"} {
					if strings.Contains(b.Summary, bad) {
						t.Errorf("summary %q must carry no verdict for a healthy window", b.Summary)
					}
				}
				return
			}
			if !strings.Contains(b.Summary, c.state) {
				t.Errorf("summary %q missing state %q", b.Summary, c.state)
			}
		})
	}
}

// TestCertificateKeyTypes: ECDSA and Ed25519 subjects name their own algorithm.
func TestCertificateKeyTypes(t *testing.T) {
	base := certOpts{cn: "example.com", notBefore: now.Add(-time.Hour), notAfter: now.Add(400 * 24 * time.Hour)}
	ec, ed := base, base
	ec.ecdsaKey, ed.ed25519Key = true, true

	if got := one(t, makeCert(t, ec)).Summary; !strings.Contains(got, "ECDSA-P-256") {
		t.Errorf("summary %q must name the ECDSA curve", got)
	}
	if got := one(t, makeCert(t, ed)).Summary; !strings.Contains(got, "Ed25519") {
		t.Errorf("summary %q must name Ed25519", got)
	}
}

// TestSANOverflowCounted: a certificate with many SANs spells out the first
// few and counts the rest, so the expiry verdict never gets pushed off the row.
func TestSANOverflowCounted(t *testing.T) {
	b := one(t, makeCert(t, certOpts{
		cn: "a.example.com", notBefore: now.Add(-time.Hour), notAfter: now.Add(400 * 24 * time.Hour),
		dnsNames: []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com", "e.example.com"},
	}))
	if !strings.Contains(b.Summary, "SAN=a.example.com, b.example.com, c.example.com +2 more") {
		t.Errorf("summary %q must cap the SAN list", b.Summary)
	}
}

// TestPrivateKeyNeverPrintsMaterial: a private key gets a label and nothing
// else — no base64, no key size, no algorithm beyond the PEM type name.
func TestPrivateKeyNeverPrintsMaterial(t *testing.T) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	cases := []struct {
		typ  string
		der  []byte
		want string
	}{
		{"RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k), "private key (rsa)"},
		{"PRIVATE KEY", mustPKCS8(t, k), "private key"},
		{"ENCRYPTED PRIVATE KEY", []byte("not really encrypted"), "private key (encrypted)"},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			lines := pemLines(t, c.typ, c.der)
			b := one(t, lines)
			if b.Summary != c.want {
				t.Errorf("summary = %q; want %q", b.Summary, c.want)
			}
			if b.Severity != SevNone {
				t.Errorf("severity = %v; want SevNone", b.Severity)
			}
			// Nothing from the encoded body may reach the summary.
			for _, l := range lines[1 : len(lines)-1] {
				if strings.Contains(b.Summary, strings.TrimSpace(l)) {
					t.Fatalf("summary %q leaked key material", b.Summary)
				}
			}
		})
	}
}

func mustPKCS8(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	return der
}

// TestPublicKeyAndCSR: the other parseable types read out their key and
// subject facts.
func TestPublicKeyAndCSR(t *testing.T) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatalf("pkix: %v", err)
	}
	if got := one(t, pemLines(t, "PUBLIC KEY", pubDER)).Summary; got != "public key  RSA-2048" {
		t.Errorf("public key summary = %q; want \"public key  RSA-2048\"", got)
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "example.com"},
		DNSNames: []string{"example.com"},
	}, k)
	if err != nil {
		t.Fatalf("csr: %v", err)
	}
	got := one(t, pemLines(t, "CERTIFICATE REQUEST", csrDER)).Summary
	for _, want := range []string{"certificate request", "CN=example.com", "RSA-2048", "SAN=example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("csr summary %q missing %q", got, want)
		}
	}
}

// TestUnparseableFallsBack: garbage inside valid markers still summarises,
// as a label saying so — a wrong summary would be worse than none.
func TestUnparseableFallsBack(t *testing.T) {
	b := one(t, []string{
		"-----BEGIN CERTIFICATE-----",
		"bm90IGEgY2VydGlmaWNhdGU=",
		"-----END CERTIFICATE-----",
	})
	if b.Summary != "certificate  (unparseable)" {
		t.Errorf("summary = %q; want \"certificate  (unparseable)\"", b.Summary)
	}
	if b.Severity != SevNone {
		t.Errorf("severity = %v; want SevNone — unreadable is not an error verdict", b.Severity)
	}

	// Body that is not even base64: pem.Decode itself fails.
	b = one(t, []string{"-----BEGIN CERTIFICATE-----", "!!!!", "-----END CERTIFICATE-----"})
	if !strings.Contains(b.Summary, "unparseable") {
		t.Errorf("summary = %q; want an unparseable label", b.Summary)
	}
}

// TestIndentedBlockDecodes: a certificate pasted into a YAML block scalar is
// indented, and must summarise exactly like one at the margin.
func TestIndentedBlockDecodes(t *testing.T) {
	cert := makeCert(t, certOpts{cn: "example.com", notBefore: now.Add(-time.Hour), notAfter: now.Add(400 * 24 * time.Hour)})
	lines := []string{"tls:", "  ca.crt: |"}
	for _, l := range cert {
		lines = append(lines, "    "+l)
	}
	b := one(t, lines)
	if b.Start != 2 || b.End != len(lines)-1 {
		t.Errorf("block range = [%d, %d]; want [2, %d]", b.Start, b.End, len(lines)-1)
	}
	if !strings.Contains(b.Summary, "CN=example.com") {
		t.Errorf("summary %q must decode an indented block", b.Summary)
	}
}

// TestMultipleBlocks: a chain file summarises each block separately, and the
// scan resumes past the previous END marker.
func TestMultipleBlocks(t *testing.T) {
	opts := certOpts{notBefore: now.Add(-time.Hour), notAfter: now.Add(400 * 24 * time.Hour)}
	leaf, ca := opts, opts
	leaf.cn, ca.cn = "leaf.example.com", "Example Root CA"
	var lines []string
	lines = append(lines, makeCert(t, leaf)...)
	lines = append(lines, makeCert(t, ca)...)

	blocks := ScanAt(lines, now)
	if len(blocks) != 2 {
		t.Fatalf("ScanAt found %d blocks; want 2", len(blocks))
	}
	if !strings.Contains(blocks[0].Summary, "CN=leaf.example.com") {
		t.Errorf("first summary = %q", blocks[0].Summary)
	}
	if !strings.Contains(blocks[1].Summary, "CN=Example Root CA") {
		t.Errorf("second summary = %q", blocks[1].Summary)
	}
	if blocks[1].Start <= blocks[0].End {
		t.Errorf("blocks overlap: %+v, %+v", blocks[0], blocks[1])
	}
}

// TestUnterminatedBlockIgnored: without an END marker there is no telling
// where the base64 stops, so a half-typed block is not claimed — and a second
// BEGIN never gets swallowed by the first.
func TestUnterminatedBlockIgnored(t *testing.T) {
	if got := ScanAt([]string{"-----BEGIN CERTIFICATE-----", "MIIB", "more text"}, now); len(got) != 0 {
		t.Errorf("ScanAt = %+v; want no block", got)
	}
	cert := makeCert(t, certOpts{cn: "example.com", notBefore: now.Add(-time.Hour), notAfter: now.Add(400 * 24 * time.Hour)})
	lines := append([]string{"-----BEGIN CERTIFICATE-----", "MIIB"}, cert...)
	blocks := ScanAt(lines, now)
	if len(blocks) != 1 || blocks[0].Start != 2 {
		t.Fatalf("ScanAt = %+v; want only the closed block at line 2", blocks)
	}
}

// TestNonMarkerLinesIgnored: prose that merely resembles a marker is not one.
func TestNonMarkerLinesIgnored(t *testing.T) {
	for _, line := range []string{
		"-----BEGIN-----",
		"-----BEGIN certificate-----",
		"-----BEGIN CERTIFICATE",
		"# -----BEGIN CERTIFICATE-----",
		"----- BEGIN CERTIFICATE -----",
	} {
		if got := ScanAt([]string{line, "x", "-----END CERTIFICATE-----"}, now); len(got) != 0 {
			t.Errorf("ScanAt(%q) = %+v; want no block", line, got)
		}
	}
}

// TestUnknownTypeLabelled: a PEM type with no special handling still gets a
// readable label instead of the base64 wall.
func TestUnknownTypeLabelled(t *testing.T) {
	b := one(t, []string{"-----BEGIN DH PARAMETERS-----", "MIIB", "-----END DH PARAMETERS-----"})
	if b.Summary != "Diffie-Hellman parameters" {
		t.Errorf("summary = %q; want \"Diffie-Hellman parameters\"", b.Summary)
	}
	b = one(t, []string{"-----BEGIN SOMETHING ODD-----", "MIIB", "-----END SOMETHING ODD-----"})
	if b.Summary != "something odd" {
		t.Errorf("summary = %q; want \"something odd\"", b.Summary)
	}
}

// TestScanUsesTheWallClock: Scan is ScanAt with time.Now — a certificate that
// expired in the past is an error whatever day the test runs on.
func TestScanUsesTheWallClock(t *testing.T) {
	lines := makeCert(t, certOpts{
		cn:        "example.com",
		notBefore: time.Now().Add(-400 * 24 * time.Hour),
		notAfter:  time.Now().Add(-24 * time.Hour),
	})
	blocks := Scan(lines)
	if len(blocks) != 1 || blocks[0].Severity != SevError {
		t.Fatalf("Scan = %+v; want one expired block", blocks)
	}
}
