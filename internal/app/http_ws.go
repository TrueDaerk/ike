package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/httpclient"
)

// http_ws.go wires the WebSocket sessions of the HTTP client (#2422): the
// dispatcher's session handle arrives over the flight's event channel and
// lands on the flight entry, and the pane's input line sends through it —
// closing the session reuses http.cancel unchanged.

// HTTPWSSessionMsg delivers the handle of a freshly opened websocket session
// (#2422) into the update loop; events is the dispatch's event channel the
// loop keeps reading from, like the stream messages carry it.
type HTTPWSSessionMsg struct {
	Source  string
	Request string
	Session *httpclient.WSSession
	events  chan tea.Msg
}

// HTTPWSSendErrMsg reports one failed interactive send (#2422): the write
// happens off-loop, so its error rides back as a message.
type HTTPWSSendErrMsg struct {
	Request string
	Err     error
}

// storeWSSession attaches the session handle to its flight entry. A flight
// that is already gone (canceled before the handshake message drained) has
// nothing to attach to — the session ends with its context either way.
func (m *Model) storeWSSession(msg HTTPWSSessionMsg) {
	if e, ok := m.httpFlight[httpFlightKey(msg.Source, msg.Request)]; ok {
		e.wsSession = msg.Session
	}
}

// wsSessionOf resolves the open session the response pane is showing (#2422),
// nil when none is.
func (m *Model) wsSessionOf() *httpclient.WSSession {
	p := m.httpPanel()
	if p == nil {
		return nil
	}
	e, ok := m.httpFlight[httpFlightKey(p.Source(), p.Request())]
	if !ok || e.wsSession == nil || e.wsSession.Closed() {
		return nil
	}
	return e.wsSession
}

// sendWSMessage runs the pane's input-line send (#2422): the message goes out
// over the session the pane is showing. The write runs off-loop — it can
// block on a slow peer, and the transcript echo returns through the ordinary
// chunk path anyway.
func (m *Model) sendWSMessage(text string) tea.Cmd {
	s := m.wsSessionOf()
	if s == nil {
		m.host.Notify(host.Info, "http: no open websocket session — run a WEBSOCKET request first")
		return nil
	}
	p := m.httpPanel()
	request := ""
	if p != nil {
		request = p.Request()
	}
	return func() tea.Msg {
		if err := s.Send(text); err != nil {
			return HTTPWSSendErrMsg{Request: request, Err: err}
		}
		return nil
	}
}
