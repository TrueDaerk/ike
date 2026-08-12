package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

// withSnapshot returns the sample response carrying an as-sent request
// (#1832).
func withSnapshot() *httpclient.Response {
	resp := sample()
	resp.Request = &httpclient.RequestSnapshot{
		Method:  "POST",
		URL:     "https://example.test/things",
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"name":"example"}`),
	}
	return resp
}

// TestResendKeyEmitsResendMsg: ctrl+r asks the host to send the shown entry's
// request again (#1832) — including on an entry without a snapshot, so the
// host can explain rather than the key dying silently.
func TestResendKeyEmitsResendMsg(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", withSnapshot())

	cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+r must emit a command")
	}
	if _, ok := cmd().(ResendMsg); !ok {
		t.Fatalf("message type: %T", cmd())
	}

	m.Set("create", sample()) // no snapshot
	if cmd := m.handleKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+r must still reach the host without a snapshot")
	}
}

// TestCurrentRequestFollowsHistory: the snapshot on offer is the one of the
// entry actually on show (#1832).
func TestCurrentRequestFollowsHistory(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	newest := withSnapshot()
	newest.Request.URL = "https://example.test/newest"
	legacy := sample() // stored before the capture existed
	m.Set("create", newest)
	m.SetHistory([]HistoryItem{
		{Resp: newest, At: time.Now()},
		{Resp: legacy, At: time.Now().Add(-time.Hour)},
	})

	if snap := m.CurrentRequest(); snap == nil || snap.URL != "https://example.test/newest" {
		t.Fatalf("newest entry: %+v", snap)
	}
	if !m.CanResend() {
		t.Fatal("an entry with a snapshot must be re-sendable")
	}
	m.handleKey(keyPress("h")) // step to the older, snapshot-less entry
	if idx, _ := m.HistoryIndex(); idx != 1 {
		t.Fatalf("history index: %d, want 1", idx)
	}
	if m.CurrentRequest() != nil || m.CanResend() {
		t.Error("a legacy entry must not offer re-send")
	}
}

// TestResendAffordanceOnlyWithSnapshot: the header carries the clickable
// button exactly when there is something to send (#1832).
func TestResendAffordanceOnlyWithSnapshot(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	m.Set("create", sample())
	// The styled label renders in fragments, so the view is measured plain.
	if strings.Contains(stripANSI(m.View()), resendLabel) {
		t.Error("no snapshot must mean no re-send affordance")
	}

	m.Set("create", withSnapshot())
	if !strings.Contains(stripANSI(m.View()), resendLabel) {
		t.Errorf("the header must offer re-send:\n%s", m.View())
	}
}

// TestResendHitLocatesAffordance: the click target is the label's own cells on
// the header row, nothing else (#1832).
func TestResendHitLocatesAffordance(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("create", withSnapshot())

	head := strings.Split(m.View(), "\n")[0]
	col := strings.Index(stripANSI(head), resendLabel)
	if col < 0 {
		t.Fatalf("affordance not in the header row: %q", head)
	}
	if !m.ResendHit(col, 0) {
		t.Errorf("column %d of the header row must be the re-send target", col)
	}
	if !m.ResendHit(col+len([]rune(resendLabel))-1, 0) {
		t.Error("the label's last cell must hit too")
	}
	if m.ResendHit(col-1, 0) || m.ResendHit(col+len([]rune(resendLabel)), 0) {
		t.Error("the cells around the label must not hit")
	}
	if m.ResendHit(col, 1) {
		t.Error("only the header row carries the affordance")
	}

	m.Set("create", sample())
	if m.ResendHit(col, 0) {
		t.Error("without a snapshot nothing on the header is a re-send target")
	}
}

// stripANSI removes SGR sequences so a rendered row can be measured in cells.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
