package app

import (
	"testing"
	"time"
)

// TestHTTPFlightTickSingleChain (#2163): the in-flight indicator tick must
// never double-arm. The old guard armed whenever the flight map was empty —
// but the map empties before the chain's final tick lands, so a dispatch in
// that window built a second permanent chain (each tick is a full re-parse
// of every open .http buffer, 4 Hz per chain).
func TestHTTPFlightTickSingleChain(t *testing.T) {
	m := newSized()

	if cmd := m.startHTTPFlight("k1", &httpFlightEntry{label: "GET /a", started: time.Now()}); cmd == nil {
		t.Fatal("first dispatch must arm the tick chain")
	}
	// The request finishes; the chain's last tick is still in flight.
	m.finishHTTPFlight("k1")
	// A new dispatch inside that window: the chain is still armed, so no
	// second one may start.
	if cmd := m.startHTTPFlight("k2", &httpFlightEntry{label: "GET /b", started: time.Now()}); cmd != nil {
		t.Fatal("second dispatch armed a duplicate tick chain")
	}
	// The in-flight tick lands: with a flight running it re-arms — once.
	tm, cmd := m.Update(httpTickMsg{gen: m.modelGen})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("tick must re-arm while a flight is running")
	}
	if !m.httpTickArmed {
		t.Fatal("armed flag must track the live chain")
	}
	// The flight ends and the final tick lands: the chain dies and the flag
	// clears, so the next dispatch can arm again.
	m.finishHTTPFlight("k2")
	tm, cmd = m.Update(httpTickMsg{gen: m.modelGen})
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("tick must not re-arm with no flights")
	}
	if m.httpTickArmed {
		t.Fatal("armed flag must clear when the chain ends")
	}
	if cmd := m.startHTTPFlight("k3", &httpFlightEntry{label: "GET /c", started: time.Now()}); cmd == nil {
		t.Fatal("a fresh dispatch after the chain died must arm again")
	}
}
