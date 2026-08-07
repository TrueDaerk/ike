package editor

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
)

// certDoc renders a self-signed certificate for cn, valid for the window
// [now+from, now+to), as a buffer document.
func certDoc(t *testing.T, cn string, from, to time.Duration) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(from),
		NotAfter:     now.Add(to),
		DNSNames:     []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// pemLoaded builds a focused editor over content, as a .pem file.
func pemLoaded(t *testing.T, content string) Model {
	t.Helper()
	m := New()
	m.buf = buffer.FromString(content)
	m.path = "server.pem"
	m.SetSize(160, 20)
	m.SetFocused(true)
	return m
}

// healthyDoc is a certificate with a comfortable validity window, plus a
// trailing comment line so "past the block" is a real position.
func healthyDoc(t *testing.T) string {
	t.Helper()
	return certDoc(t, "example.com", -24*time.Hour, 400*24*time.Hour) + "# trailing note\n"
}

// TestPemBlockCollapses: the base64 body hides behind the BEGIN marker, which
// carries the decoded summary, and the block costs one row of scrolling.
func TestPemBlockCollapses(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	last := m.buf.LineCount() - 1 // the trailing comment
	m.cursor = buffer.Position{Line: last}

	b, ok := m.pemBlockAt(0)
	if !ok {
		t.Fatal("the BEGIN marker must head a collapsed block")
	}
	if m.lineHidden(0) {
		t.Error("the BEGIN marker itself must stay visible")
	}
	for l := 1; l <= b.End; l++ {
		if !m.lineHidden(l) {
			t.Errorf("line %d must fold into the block", l)
		}
	}
	if m.lineHidden(last) {
		t.Error("a line past the END marker must not fold")
	}
	if !m.hasFolds() {
		t.Error("a collapsed block must engage the fold-aware paths")
	}
	if got, ok := m.visibleStep(0, 1); !ok || got != b.End+1 {
		t.Errorf("visibleStep(0, +1) = %d, %v; want %d, true", got, ok, b.End+1)
	}
}

// TestPemSummaryInView: the collapsed row spells out the certificate's facts
// instead of base64.
func TestPemSummaryInView(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}

	got := m.View()
	for _, want := range []string{"-----BEGIN CERTIFICATE-----", "CN=example.com", "issuer=example.com", "RSA-2048"} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, m.buf.Line(1)) {
		t.Errorf("the base64 body must not render while collapsed:\n%s", got)
	}
}

// TestPemSummaryTruncatesNotDrops: a full certificate reading is wider than an
// eighty-column pane, so the row must truncate it — keeping the CN and the
// expiry verdict, the facts peminfo orders first — rather than leaving the
// feature invisible at the most common width.
func TestPemSummaryTruncatesNotDrops(t *testing.T) {
	m := pemLoaded(t, certDoc(t, "example.com", -24*time.Hour, 10*24*time.Hour)+"# note\n")
	m.SetSize(80, 20)
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}

	got := m.View()
	for _, want := range []string{"CN=example.com", "expires in"} {
		if !strings.Contains(got, want) {
			t.Errorf("an 80-column pane must still show %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "…") {
		t.Errorf("the oversized summary must truncate with an ellipsis:\n%s", got)
	}
	for _, row := range strings.Split(got, "\n") {
		if w := lipgloss.Width(row); w > 80 {
			t.Fatalf("row overflows the pane at %d columns: %q", w, row)
		}
	}
}

// TestPemSummaryDroppedWhenUnusable: a pane too narrow for a meaningful
// summary falls back to the bare marker — five columns of certificate facts
// would only look like damage.
func TestPemSummaryDroppedWhenUnusable(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.SetSize(34, 20)
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}

	if got := m.View(); strings.Contains(got, "CN=") || strings.Contains(got, "certificate ") {
		t.Errorf("a too-narrow pane must drop the summary:\n%s", got)
	}
	if _, ok := m.pemBlockAt(0); !ok {
		t.Error("the block must still collapse even when the summary does not fit")
	}
}

// TestPemRevealsUnderCursor: the block reveals positionally like every other
// stand-in family — the cursor inside it renders all of it raw, and leaving
// collapses it again.
func TestPemRevealsUnderCursor(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.cursor = buffer.Position{Line: 1} // inside the base64 body

	if m.lineHidden(2) {
		t.Error("the block must reveal while the cursor is inside it")
	}
	if _, ok := m.pemBlockAt(0); ok {
		t.Error("a revealed block must not render as a collapsed header")
	}
	// Line 2, not the cursor line: the cursor cell renders styled, so its own
	// line never appears in the view as one contiguous string.
	if got := m.View(); !strings.Contains(got, m.buf.Line(2)) {
		t.Errorf("the raw base64 must be back under the cursor:\n%s", got)
	}

	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	if !m.lineHidden(2) {
		t.Error("leaving the block must collapse it again")
	}
}

// TestPemSeverityColors: an expired certificate draws its summary in the error
// color, one inside the warning window in the warning color, a healthy one
// faint.
func TestPemSeverityColors(t *testing.T) {
	cases := []struct {
		name     string
		from, to time.Duration
		want     string
	}{
		{"expired", -400 * 24 * time.Hour, -24 * time.Hour, "expired"},
		{"not yet valid", 24 * time.Hour, 400 * 24 * time.Hour, "not yet valid"},
		{"expiring", -24 * time.Hour, 10 * 24 * time.Hour, "expires in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pemLoaded(t, certDoc(t, "example.com", c.from, c.to)+"# note\n")
			m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
			b, ok := m.pemBlockAt(0)
			if !ok {
				t.Fatal("the block must collapse")
			}
			if !strings.Contains(b.Summary, c.want) {
				t.Errorf("summary %q missing %q", b.Summary, c.want)
			}
			healthy := m.pemStyle(0)
			if got := m.pemStyle(b.Severity); got.Render("x") == healthy.Render("x") {
				t.Errorf("%s must not draw like a healthy summary", c.name)
			}
		})
	}
}

// TestPemPrivateKeyLabelOnly: a private key collapses to a label; none of its
// base64 reaches the view.
func TestPemPrivateKeyLabelOnly(t *testing.T) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})) + "# note\n"
	m := pemLoaded(t, doc)
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}

	got := m.View()
	if !strings.Contains(got, "private key (rsa)") {
		t.Errorf("view must label the private key:\n%s", got)
	}
	if strings.Contains(got, m.buf.Line(1)) {
		t.Errorf("no key material may reach the view:\n%s", got)
	}
}

// TestPemEmbeddedInYAML: a certificate pasted into a YAML block scalar
// collapses the same way — the layer reads buffer text, not a language.
func TestPemEmbeddedInYAML(t *testing.T) {
	var b strings.Builder
	b.WriteString("apiVersion: v1\ndata:\n  ca.crt: |\n")
	for _, l := range strings.Split(strings.TrimSuffix(certDoc(t, "ca.example.com", -24*time.Hour, 400*24*time.Hour), "\n"), "\n") {
		b.WriteString("    " + l + "\n")
	}
	m := pemLoaded(t, b.String())
	m.path = "secret.yaml"
	m.cursor = buffer.Position{Line: 0}

	blk, ok := m.pemBlockAt(3)
	if !ok {
		t.Fatal("an indented block must collapse too")
	}
	if !strings.Contains(blk.Summary, "CN=ca.example.com") {
		t.Errorf("summary = %q; want the indented certificate's CN", blk.Summary)
	}
	if !m.lineHidden(4) {
		t.Error("the indented base64 must fold")
	}
}

// TestPemToggleOffRendersRaw: view.togglePemSummary shows the untouched file,
// and the override sticks over the config default.
func TestPemToggleOffRendersRaw(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	if !m.lineHidden(1) {
		t.Fatal("precondition: the block collapses")
	}

	m, _ = m.Update(ActionMsg{Action: "toggle_pem_summary"})
	if m.lineHidden(1) {
		t.Error("raw mode must render every line of the block")
	}
	if m.hasPemBlocks() {
		t.Error("raw mode must report no blocks")
	}
	if got := m.View(); !strings.Contains(got, m.buf.Line(1)) {
		t.Errorf("raw mode must show the base64:\n%s", got)
	}

	m.Configure(host.MapConfig{"editor.pem_summary": "true"})
	if m.lineHidden(1) {
		t.Error("a per-view toggle must survive the config refresh")
	}
}

// TestPemConfigDefault: editor.pem_summary drives the initial state.
func TestPemConfigDefault(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	m.Configure(host.MapConfig{"editor.pem_summary": "false"})
	if m.lineHidden(1) {
		t.Error("editor.pem_summary=false must render the block raw")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_pem_summary"})
	if !m.lineHidden(1) {
		t.Error("the toggle must switch it back on")
	}
}

// TestPemFollowsEdits: the block map is keyed by document version, so breaking
// the END marker un-folds the body.
func TestPemFollowsEdits(t *testing.T) {
	m := pemLoaded(t, healthyDoc(t))
	m.cursor = buffer.Position{Line: m.buf.LineCount() - 1}
	if !m.lineHidden(1) {
		t.Fatal("precondition: the block collapses")
	}

	lines := m.buf.Lines()
	for i, l := range lines {
		if strings.HasPrefix(l, "-----END") {
			lines[i] = "broken"
		}
	}
	m.buf.ReplaceAll(strings.Join(lines, "\n"))
	m.docVersion++
	if m.lineHidden(1) {
		t.Error("an unterminated block must stop folding")
	}
}

// TestPemNonCertBufferUnaffected: a buffer with no PEM block folds nothing.
func TestPemNonCertBufferUnaffected(t *testing.T) {
	m := pemLoaded(t, "hello\nworld\n")
	m.path = "notes.txt"
	m.cursor = buffer.Position{Line: 0}
	if m.hasPemBlocks() {
		t.Error("a plain buffer must report no blocks")
	}
	if m.lineHidden(1) {
		t.Error("a plain buffer must fold nothing")
	}
}
