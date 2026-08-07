// Package peminfo turns PEM blocks into a one-line summary (#1652). A
// certificate in an editor is a wall of base64: forty lines that say nothing
// about who the certificate is for, who signed it, or whether it is still
// valid. The facts a reader actually wants are in the DER underneath, so this
// package decodes them and renders the answer as a single line.
//
// It is a leaf package — standard library only — so every consumer can call it
// without a dependency cycle: the editor collapses a block into its BEGIN line
// plus the summary (pemsummary.go), and the summary reveals positionally like
// every other stand-in family (#1585, #1594) — put the caret inside the block
// and the raw base64 is back.
//
// Scope of the decoding, deliberately asymmetric:
//
//   - CERTIFICATE decodes fully: subject CN, issuer CN, key type, validity
//     window, subject alternative names, and an expiry verdict that drives the
//     severity (expired or not-yet-valid is an error, an expiry inside
//     WarnWindow is a warning).
//   - CERTIFICATE REQUEST and PUBLIC KEY decode to their subject/key facts.
//   - Private keys are NEVER parsed. They get a type label and nothing else —
//     no key size, no algorithm beyond what the PEM type name already states,
//     and above all no key material. A summary that renders a secret defeats
//     the point of the file being opaque in the first place.
//   - Anything else, and anything that fails to parse, falls back to a plain
//     label. A wrong summary is worse than no summary.
package peminfo

import (
	"crypto/dsa" //nolint:staticcheck // only used to name the key type
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// WarnWindow is how close to its notAfter a certificate has to be before the
// summary reads as a warning. Thirty days is the usual renewal lead time: ACME
// clients renew at that mark, so a certificate still inside the window is one
// nobody has renewed yet.
const WarnWindow = 30 * 24 * time.Hour

// Severity is how loudly a block's summary should read.
type Severity int

const (
	// SevNone is an ordinary summary — nothing is wrong with the block.
	SevNone Severity = iota
	// SevWarn is a certificate expiring inside WarnWindow.
	SevWarn
	// SevError is a certificate that is expired or not yet valid.
	SevError
)

// Block is one PEM block found in a buffer: the inclusive line range from its
// BEGIN marker through its END marker, the PEM type as written, and the
// rendered summary with the severity it should be drawn in.
type Block struct {
	Start, End int
	Type       string
	Summary    string
	Severity   Severity
}

// sanLimit is how many subject alternative names the summary spells out before
// it counts the rest. A certificate with eighty SANs must not push the expiry
// verdict off the row.
const sanLimit = 3

// Scan returns the PEM blocks of lines, summarised against the current time.
func Scan(lines []string) []Block { return ScanAt(lines, time.Now()) }

// ScanAt is Scan with an explicit clock, so the expiry verdicts are testable.
func ScanAt(lines []string, now time.Time) []Block {
	var out []Block
	for i := 0; i < len(lines); i++ {
		typ, ok := beginType(lines[i])
		if !ok {
			continue
		}
		end, ok := findEnd(lines, i+1, typ)
		if !ok {
			// An unterminated block is not a block: without its END marker
			// there is no telling where the base64 stops, and collapsing to the
			// buffer end on a half-typed marker would be hostile.
			continue
		}
		summary, sev := summarize(typ, blockText(lines, i, end), now)
		out = append(out, Block{Start: i, End: end, Type: typ, Summary: summary, Severity: sev})
		i = end
	}
	return out
}

// beginType reads the PEM type out of a `-----BEGIN X-----` line. Leading and
// trailing whitespace is tolerated, so a block indented inside a YAML block
// scalar is recognised the same as one at the margin.
func beginType(line string) (string, bool) { return markerType(line, "-----BEGIN ") }

// endType is beginType for the closing marker.
func endType(line string) (string, bool) { return markerType(line, "-----END ") }

// markerType matches one PEM marker line and returns the type between the
// prefix and the trailing dashes. The type must be non-empty and hold only the
// characters RFC 7468 allows in a label, so a prose line that happens to start
// with the prefix is not claimed.
func markerType(line, prefix string) (string, bool) {
	t := strings.TrimSpace(line)
	rest, ok := strings.CutPrefix(t, prefix)
	if !ok {
		return "", false
	}
	typ, ok := strings.CutSuffix(rest, "-----")
	if !ok || typ == "" {
		return "", false
	}
	for _, r := range typ {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '-') {
			return "", false
		}
	}
	return typ, true
}

// findEnd locates the `-----END typ-----` line at or after from. A second BEGIN
// marker before it ends the search: the first block was never closed, and
// swallowing the next one would misreport both.
func findEnd(lines []string, from int, typ string) (int, bool) {
	for i := from; i < len(lines); i++ {
		if t, ok := endType(lines[i]); ok && t == typ {
			return i, true
		}
		if _, ok := beginType(lines[i]); ok {
			return 0, false
		}
	}
	return 0, false
}

// blockText reassembles the block's lines into parseable PEM. Each line is
// trimmed, which is what makes an indented block inside YAML decode at all —
// pem.Decode rejects leading whitespace on the marker lines.
func blockText(lines []string, start, end int) []byte {
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(strings.TrimSpace(lines[i]))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// summarize renders one block. The PEM type decides how far the decoding goes;
// see the package comment for why private keys stop at their label.
func summarize(typ string, text []byte, now time.Time) (string, Severity) {
	if isPrivateKey(typ) {
		return privateLabel(typ), SevNone
	}
	blk, _ := pem.Decode(text)
	if blk == nil {
		return label(typ) + "  (unparseable)", SevNone
	}
	switch typ {
	case "CERTIFICATE", "TRUSTED CERTIFICATE", "X509 CERTIFICATE":
		return certSummary(typ, blk.Bytes, now)
	case "CERTIFICATE REQUEST", "NEW CERTIFICATE REQUEST":
		return csrSummary(typ, blk.Bytes)
	case "PUBLIC KEY":
		return pubSummary(typ, blk.Bytes)
	}
	return label(typ), SevNone
}

// certSummary decodes a certificate into its one-line reading. The order is by
// what a reader looks for first — who it is for, then whether it is still good,
// then who signed it and what it is made of — which is also the order that
// survives a narrow pane: the consumer truncates from the right, so the CN and
// the expiry verdict are the last things to go.
func certSummary(typ string, der []byte, now time.Time) (string, Severity) {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return label(typ) + "  (unparseable)", SevNone
	}
	parts := []string{label(typ), "CN=" + commonName(c.Subject)}
	state, sev := expiryState(c.NotBefore, c.NotAfter, now)
	if state != "" {
		parts = append(parts, state)
	}
	parts = append(parts, validity(c.NotBefore, c.NotAfter), "issuer="+commonName(c.Issuer))
	if kt := keyType(c.PublicKey); kt != "" {
		parts = append(parts, kt)
	}
	if san := sanList(c); san != "" {
		parts = append(parts, "SAN="+san)
	}
	return strings.Join(parts, "  "), sev
}

// csrSummary decodes a certificate signing request: the same subject and key
// facts as a certificate, minus the validity window a request does not have.
func csrSummary(typ string, der []byte) (string, Severity) {
	r, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return label(typ) + "  (unparseable)", SevNone
	}
	parts := []string{label(typ), "CN=" + commonName(r.Subject)}
	if kt := keyType(r.PublicKey); kt != "" {
		parts = append(parts, kt)
	}
	if san := names(r.DNSNames, r.IPAddresses, r.EmailAddresses); san != "" {
		parts = append(parts, "SAN="+san)
	}
	return strings.Join(parts, "  "), SevNone
}

// pubSummary names a public key's algorithm and size. A public key is public;
// unlike its private counterpart there is nothing here worth hiding.
func pubSummary(typ string, der []byte) (string, Severity) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return label(typ) + "  (unparseable)", SevNone
	}
	if kt := keyType(pub); kt != "" {
		return label(typ) + "  " + kt, SevNone
	}
	return label(typ), SevNone
}

// expiryState renders the verdict on a validity window, and the severity that
// goes with it. A certificate comfortably inside its window gets no verdict at
// all — the dates already said so, and a summary that shouts on every healthy
// block teaches the reader to ignore it.
func expiryState(notBefore, notAfter, now time.Time) (string, Severity) {
	switch {
	case now.Before(notBefore):
		return "not yet valid", SevError
	case now.After(notAfter):
		return "expired", SevError
	case notAfter.Sub(now) <= WarnWindow:
		return "expires in " + until(notAfter.Sub(now)), SevWarn
	}
	return "", SevNone
}

// until renders a sub-30-day remaining time in the coarsest unit that still
// says something useful: days down to one, hours below that.
func until(d time.Duration) string {
	if days := int(d / (24 * time.Hour)); days >= 1 {
		return strconv.Itoa(days) + "d"
	}
	if hours := int(d / time.Hour); hours >= 1 {
		return strconv.Itoa(hours) + "h"
	}
	return "<1h"
}

// validity renders the window as two plain dates. The times of day are noise:
// nobody schedules a renewal by the minute.
func validity(notBefore, notAfter time.Time) string {
	const day = "2006-01-02"
	return notBefore.UTC().Format(day) + "→" + notAfter.UTC().Format(day)
}

// commonName returns a subject's CN, falling back to the first organisation
// and finally to a placeholder — some CA certificates carry no CN at all.
func commonName(n pkix.Name) string {
	if n.CommonName != "" {
		return n.CommonName
	}
	if len(n.Organization) > 0 {
		return n.Organization[0]
	}
	return "—"
}

// sanList renders a certificate's subject alternative names.
func sanList(c *x509.Certificate) string {
	return names(c.DNSNames, c.IPAddresses, c.EmailAddresses)
}

// names joins the alternative names, spelling out at most sanLimit of them and
// counting the rest, so a certificate with eighty SANs still fits a row.
func names(dns []string, ips []net.IP, emails []string) string {
	all := make([]string, 0, len(dns)+len(ips)+len(emails))
	all = append(all, dns...)
	for _, ip := range ips {
		all = append(all, ip.String())
	}
	all = append(all, emails...)
	if len(all) == 0 {
		return ""
	}
	if len(all) <= sanLimit {
		return strings.Join(all, ", ")
	}
	return fmt.Sprintf("%s +%d more", strings.Join(all[:sanLimit], ", "), len(all)-sanLimit)
}

// keyType names a public key's algorithm and strength. The empty string means
// an algorithm this build does not know — the summary then simply omits it.
func keyType(pub any) string {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "RSA-" + strconv.Itoa(k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECDSA-" + k.Curve.Params().Name
	case ed25519.PublicKey:
		return "Ed25519"
	case *dsa.PublicKey:
		return "DSA"
	}
	return ""
}

// isPrivateKey reports whether a PEM type names private key material. The test
// is on the suffix so every vendor spelling — RSA, EC, DSA, OPENSSH,
// ENCRYPTED — is caught by one rule and none of them is ever parsed.
func isPrivateKey(typ string) bool { return strings.HasSuffix(typ, "PRIVATE KEY") }

// privateLabel names a private key by its PEM type alone. Nothing is decoded:
// the label is built from the marker text that was already on screen.
func privateLabel(typ string) string {
	kind, _ := strings.CutSuffix(typ, "PRIVATE KEY")
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "private key"
	}
	return "private key (" + strings.ToLower(kind) + ")"
}

// labels are the PEM types with a reading better than their lowercased name.
var labels = map[string]string{
	"CERTIFICATE":             "certificate",
	"TRUSTED CERTIFICATE":     "trusted certificate",
	"X509 CERTIFICATE":        "certificate",
	"CERTIFICATE REQUEST":     "certificate request",
	"NEW CERTIFICATE REQUEST": "certificate request",
	"PUBLIC KEY":              "public key",
	"X509 CRL":                "certificate revocation list",
	"PKCS7":                   "PKCS#7 bundle",
	"CMS":                     "CMS message",
	"DH PARAMETERS":           "Diffie-Hellman parameters",
	"EC PARAMETERS":           "EC parameters",
	"ATTRIBUTE CERTIFICATE":   "attribute certificate",
}

// label is the human reading of a PEM type; an unknown type simply lowercases.
func label(typ string) string {
	if l, ok := labels[typ]; ok {
		return l
	}
	return strings.ToLower(typ)
}
