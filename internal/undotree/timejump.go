package undotree

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
)

// maxAgeDigits bounds the typed age (in minutes) at four digits — a bit under
// a week, past which "the state from N minutes ago" stops being a useful unit.
const maxAgeDigits = 4

// now returns the clock the overlay ages timestamps against; tests override it.
func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// relAge renders a node timestamp as its age: "just now", "5m ago", "2h ago",
// "3d ago". A zero timestamp (the root state, which predates the history)
// renders empty.
func relAge(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}

// startTimeJump opens the age prompt ("t").
func (m *Model) startTimeJump() {
	m.asking = true
	m.ageInput = ""
}

// updateTimeJump handles one key while the age prompt is open. It returns the
// jump command, if any; the prompt closes on enter and esc.
func (m *Model) updateTimeJump(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "esc":
		m.asking = false
		m.ageInput = ""
	case "enter":
		m.asking = false
		minutes, err := strconv.Atoi(m.ageInput)
		m.ageInput = ""
		if err != nil || minutes < 0 {
			return nil
		}
		return m.timeJump(minutes)
	case "backspace":
		if m.ageInput != "" {
			m.ageInput = m.ageInput[:len(m.ageInput)-1]
		}
	default:
		if len(s) == 1 && s[0] >= '0' && s[0] <= '9' && len(m.ageInput) < maxAgeDigits {
			m.ageInput += s
		}
	}
	return nil
}

// timeJump selects and restores the newest state that is at least `minutes`
// old — "the buffer as it was ~N minutes ago" (#2143). The root state has no
// timestamp and counts as arbitrarily old, so there is always a target.
func (m *Model) timeJump(minutes int) tea.Cmd {
	cutoff := m.clock().Add(-time.Duration(minutes) * time.Minute)
	best := -1
	for i, r := range m.rows {
		if !r.node.At.IsZero() && r.node.At.After(cutoff) {
			continue // too young
		}
		if best < 0 {
			best = i
			continue
		}
		// Prefer the newest candidate; the untimestamped root loses to any
		// timestamped one, and equal stamps break towards the later seq.
		b := m.rows[best].node
		switch {
		case b.At.IsZero() && !r.node.At.IsZero(),
			r.node.At.After(b.At),
			r.node.At.Equal(b.At) && !b.At.IsZero() && r.node.Seq > b.Seq:
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	m.cursor = best
	return m.jumpCurrent()
}
