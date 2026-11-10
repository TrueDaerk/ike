package httppane

import (
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// truncated_test.go covers the truncated marker of a cut body (#3117).

func TestTruncatedMarkerOnStatusRow(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	resp := sample()
	resp.Truncated = true
	m.Set("dump", resp)
	if !strings.Contains(m.rows[0].text, "· "+TruncatedMarker) {
		t.Fatalf("status row = %q, want the truncated marker", m.rows[0].text)
	}
	m.Set("dump", sample())
	if strings.Contains(m.rows[0].text, TruncatedMarker) {
		t.Fatalf("status row = %q, a complete body carries no marker", m.rows[0].text)
	}
}

// The assertion block says its verdicts were reached on a cut body.
func TestTruncatedNoteInAssertionBlock(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	resp := sample()
	resp.Truncated = true
	resp.BodySize = 1024
	resp.Assertions = []httpclient.AssertResult{{Pass: true}}
	m.Set("dump", resp)
	found := false
	for _, r := range m.rows {
		if r.kind == kindWarn && strings.Contains(r.text, "body truncated at 1024 bytes") {
			found = true
		}
	}
	if !found {
		t.Fatal("the assertion block must mention the truncation")
	}
}

// A browsed history entry that was cut says so in the history marker.
func TestTruncatedMarkerInHistory(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 24)
	cut := sample()
	cut.Truncated = true
	m.Set("dump", sample())
	m.SetHistory([]HistoryItem{{Resp: sample()}, {Resp: cut, At: time.Now()}})
	m.showHistory(1)
	if got := m.historyMarker(); !strings.HasSuffix(got, "· "+TruncatedMarker) {
		t.Fatalf("history marker = %q, want the truncated marker", got)
	}
	m.showHistory(0)
	if got := m.historyMarker(); strings.Contains(got, TruncatedMarker) {
		t.Fatalf("history marker = %q for a complete entry", got)
	}
}
