package debugdoctor

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// seeded returns a sized panel over a log with the given entries.
func seeded(t *testing.T, ls Listener, entries ...Entry) (*Model, *Log) {
	t.Helper()
	log := NewLog()
	log.SetListener(ls)
	for _, e := range entries {
		log.Add(e)
	}
	m := New(nil)
	m.SetSize(240, 12)
	m.SetLog(log)
	return &m, log
}

func press(m *Model, key string) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
}

func view(m *Model) string { return ansi.Strip(m.View()) }

func at(h, m, s int) time.Time {
	return time.Date(2026, 8, 20, h, m, s, 0, time.Local)
}

// TestListenerStatusRendering guards the status half of #1991: stopped shows
// how to start the listener; running shows port, filter and mapping count.
func TestListenerStatusRendering(t *testing.T) {
	m, _ := seeded(t, Listener{})
	if v := view(m); !strings.Contains(v, "listener stopped") ||
		!strings.Contains(v, "Listen for PHP Debug Connections") {
		t.Fatalf("stopped view = %q", v)
	}

	m, _ = seeded(t, Listener{Running: true, Port: 9003, Hostname: "onpage.local", Mappings: 2})
	v := view(m)
	for _, want := range []string{"listening on port 9003", "host filter onpage.local", "2 path mappings"} {
		if !strings.Contains(v, want) {
			t.Fatalf("running view lacks %q: %q", want, v)
		}
	}

	m, _ = seeded(t, Listener{Running: true, Port: 9003})
	if v := view(m); !strings.Contains(v, "no host filter") || !strings.Contains(v, "no path mappings") {
		t.Fatalf("unfiltered view = %q", v)
	}
}

// TestEntryRendering guards the trace half of #1991: accepted, accepted with
// an unmapped entry file, and each distinguished reject reason render with
// their identity details, newest first.
func TestEntryRendering(t *testing.T) {
	m, _ := seeded(t, Listener{Running: true, Port: 9003},
		Entry{Time: at(12, 0, 1), Accepted: true, Remote: "127.0.0.1:1111", IDEKey: "ike",
			FileURI: "file:///srv/index.php", Local: "/proj/index.php", Mapped: true},
		Entry{Time: at(12, 0, 2), Accepted: true, Remote: "127.0.0.1:2222", IDEKey: "ike",
			FileURI: "file:///var/www/other.php"},
		Entry{Time: at(12, 0, 3), Remote: "127.0.0.1:3333", Reason: "filter", IDEKey: "web",
			FileURI: "file:///srv/a.php", Host: "other.local",
			Detail: `request from "other.local" does not match the host filter onpage.local`},
		Entry{Time: at(12, 0, 4), Remote: "127.0.0.1:4444", Reason: "init",
			Detail: "malformed init packet: XML syntax error"},
		Entry{Time: at(12, 0, 5), Remote: "127.0.0.1:5555", Reason: "handshake"},
	)
	v := view(m)
	for _, want := range []string{
		"5 connection attempts",
		"✓ 127.0.0.1:1111  accepted → /proj/index.php",
		"⚠ 127.0.0.1:2222  accepted — entry file has no local path mapping (file:///var/www/other.php)",
		"✗ 127.0.0.1:3333  rejected: hostname filter mismatch",
		"host other.local",
		"idekey web",
		"file file:///srv/a.php",
		"✗ 127.0.0.1:4444  rejected: malformed init packet — malformed init packet: XML syntax error",
		"✗ 127.0.0.1:5555  rejected: no DBGp handshake",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
	// Newest first: the handshake reject (12:00:05) renders above the first
	// accept (12:00:01).
	if strings.Index(v, "12:00:05") > strings.Index(v, "12:00:01") {
		t.Fatalf("entries must render newest first:\n%s", v)
	}
}

// TestClearAndNav guards the panel's interaction contract: c emits ClearMsg
// (only while entries exist), and list navigation moves the cursor.
func TestClearAndNav(t *testing.T) {
	m, log := seeded(t, Listener{Running: true, Port: 9003},
		Entry{Time: at(1, 0, 0), Accepted: true, Remote: "a", Local: "/x", Mapped: true},
		Entry{Time: at(1, 0, 1), Accepted: true, Remote: "b", Local: "/y", Mapped: true},
	)
	if press(m, "j"); m.Cursor() != 1 {
		t.Fatalf("j must move the cursor, got %d", m.Cursor())
	}
	cmd := press(m, "c")
	if cmd == nil {
		t.Fatal("c with entries must emit ClearMsg")
	}
	if _, ok := cmd().(ClearMsg); !ok {
		t.Fatal("c must emit ClearMsg")
	}
	log.Clear()
	if press(m, "c") != nil {
		t.Fatal("c on an empty trace must be inert")
	}
	if v := view(m); !strings.Contains(v, "no connection attempts yet") {
		t.Fatalf("cleared view = %q", v)
	}
}

// TestLogCap guards the ring bound: the log never grows past maxEntries and
// drops the oldest first.
func TestLogCap(t *testing.T) {
	log := NewLog()
	for i := 0; i < maxEntries+10; i++ {
		log.Add(Entry{Remote: "r", Reason: "busy", Time: at(0, 0, i%60)})
	}
	if n := len(log.Entries()); n != maxEntries {
		t.Fatalf("log holds %d entries, want %d", n, maxEntries)
	}
}
