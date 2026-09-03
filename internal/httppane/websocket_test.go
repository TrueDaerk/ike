package httppane

import (
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

func startWSStream(t *testing.T) *Model {
	t.Helper()
	m := startStream(t)
	m.SetWSLive()
	return m
}

func wsKey(m *Model, k string) tea.Cmd {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		r := []rune(k)[0]
		msg = tea.KeyPressMsg{Code: r, Text: k}
	}
	return m.Update(msg)
}

func typeText(m *Model, text string) {
	for _, r := range text {
		wsKey(m, string(r))
	}
}

func TestWSLiveGates(t *testing.T) {
	m := startWSStream(t)
	if !m.WSLive() {
		t.Fatal("not ws-live after SetWSLive")
	}
	// The finalizing Set ends the session view and the input with it.
	m.Set("live", &httpclient.Response{Status: "101 Switching Protocols", StatusCode: 101})
	if m.WSLive() || m.WSInputOpen() {
		t.Fatal("ws state survived the finalizing Set")
	}
	// SetWSLive on a non-streaming pane is a no-op.
	m.SetWSLive()
	if m.WSLive() {
		t.Fatal("ws-live without a stream")
	}
}

func TestWSInputSendsOnEnter(t *testing.T) {
	m := startWSStream(t)
	wsKey(m, "i")
	if !m.WSInputOpen() {
		t.Fatal("input did not open on i")
	}
	typeText(m, "hi")
	cmd := wsKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(WSSendMsg)
	if !ok || msg.Text != "hi" {
		t.Fatalf("send msg: %#v", msg)
	}
	if !m.WSInputOpen() {
		t.Fatal("input closed after send — a session is a conversation")
	}
	// Empty input sends nothing.
	if cmd := wsKey(m, "enter"); cmd != nil {
		t.Fatal("empty enter produced a command")
	}
}

func TestWSInputOpensOnEnterToo(t *testing.T) {
	m := startWSStream(t)
	wsKey(m, "enter")
	if !m.WSInputOpen() {
		t.Fatal("input did not open on enter")
	}
	wsKey(m, "esc")
	if m.WSInputOpen() {
		t.Fatal("esc did not close the input")
	}
}

func TestWSInputHistory(t *testing.T) {
	m := startWSStream(t)
	wsKey(m, "i")
	typeText(m, "one")
	wsKey(m, "enter")
	typeText(m, "two")
	wsKey(m, "enter")
	if got := m.WSSentHistory(); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("history: %v", got)
	}
	typeText(m, "dra")
	wsKey(m, "up")
	if m.wsText != "two" {
		t.Fatalf("first up: %q", m.wsText)
	}
	wsKey(m, "up")
	if m.wsText != "one" {
		t.Fatalf("second up: %q", m.wsText)
	}
	wsKey(m, "up") // past the oldest: stays
	if m.wsText != "one" {
		t.Fatalf("past oldest: %q", m.wsText)
	}
	wsKey(m, "down")
	wsKey(m, "down")
	if m.wsText != "dra" {
		t.Fatalf("draft not restored: %q", m.wsText)
	}
}

func TestWSInputDedupesConsecutive(t *testing.T) {
	m := startWSStream(t)
	wsKey(m, "i")
	typeText(m, "same")
	wsKey(m, "enter")
	typeText(m, "same")
	wsKey(m, "enter")
	if got := m.WSSentHistory(); len(got) != 1 {
		t.Fatalf("history: %v", got)
	}
}

func TestWSFooterAndHeader(t *testing.T) {
	m := startWSStream(t)
	if f := m.footerText(); !strings.Contains(f, "send message") || !strings.Contains(f, "x close session") {
		t.Fatalf("session footer: %q", f)
	}
	if v := m.View(); !strings.Contains(v, "websocket session") {
		t.Fatalf("header marker missing:\n%s", v)
	}
	wsKey(m, "i")
	typeText(m, "abc")
	if f := m.footerText(); !strings.Contains(f, "abc") || !strings.Contains(f, "↩ send") {
		t.Fatalf("input footer: %q", f)
	}
}

func TestWSIncomingFramesAppend(t *testing.T) {
	m := startWSStream(t)
	base := m.Rows()
	m.AppendStream([]byte("← 12:00:00.000 hello\n"))
	if m.Rows() != base+1 || !strings.Contains(m.RowText(base), "hello") {
		t.Fatalf("frame row: %q", m.RowText(base))
	}
}

func TestWSPlainStreamKeepsOldKeys(t *testing.T) {
	// A non-websocket stream must not open the input line on i/enter.
	m := startStream(t)
	wsKey(m, "i")
	if m.WSInputOpen() {
		t.Fatal("input opened on a plain stream")
	}
}

func TestWSStartStreamResetsWS(t *testing.T) {
	m := startWSStream(t)
	m.StartStream("live2", "HTTP/1.1", "200 OK", http.Header{})
	if m.WSLive() {
		t.Fatal("ws flag survived a fresh StartStream")
	}
}
