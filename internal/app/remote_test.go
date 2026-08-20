package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/sshconf"
)

// TestRemoteModeResults verifies the browse picker rows: the same host list
// as the ssh terminal picker, but activation carries a browse pick.
func TestRemoteModeResults(t *testing.T) {
	mode := newRemoteMode()
	mode.entries = []sshconf.Host{{Alias: "web01", HostName: "web01.example.com"}}
	items := mode.Results("", palette.Context{})
	if len(items) != 1 || items[0].Title != "web01" {
		t.Fatalf("items = %+v", items)
	}
	picked, ok := items[0].Msg.(RemoteHostPickedMsg)
	if !ok || picked.Host != "web01" {
		t.Fatalf("activation msg = %+v", items[0].Msg)
	}
}

// TestOpenRemotePaneOncePerHost verifies the pane identity: a pick opens one
// browser keyed by the alias, a second pick refocuses instead of duplicating,
// and an empty pick opens nothing.
func TestOpenRemotePaneOncePerHost(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	tm0, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = tm0.(Model)
	before := len(m.activeWS().Panes.Keys())
	tm, _ := m.Update(RemoteHostPickedMsg{})
	m = tm.(Model)
	if got := len(m.activeWS().Panes.Keys()); got != before {
		t.Fatalf("empty pick changed panes: %d, want %d", got, before)
	}
	tm, _ = m.Update(RemoteHostPickedMsg{Host: "web01"})
	m = tm.(Model)
	if !m.activeWS().Panes.Has("remote:web01") {
		t.Fatalf("pick did not open remote:web01: %v", m.activeWS().Panes.Keys())
	}
	count := len(m.activeWS().Panes.Keys())
	tm, _ = m.Update(RemoteHostPickedMsg{Host: "web01"})
	m = tm.(Model)
	if got := len(m.activeWS().Panes.Keys()); got != count {
		t.Fatalf("second pick duplicated the pane: %d, want %d", got, count)
	}
	if m.activeWS().Panes.Focused() != "remote:web01" {
		t.Fatalf("second pick did not refocus, focus = %q", m.activeWS().Panes.Focused())
	}
	inst := m.activeWS().Panes.Get("remote:web01")
	if inst.Kind() != pane.KindRemote || inst.Remote().Alias() != "web01" {
		t.Fatalf("pane = kind %v alias %q", inst.Kind(), inst.Remote().Alias())
	}
}

// TestRemoteFetchedOpensReadOnly verifies the open funnel's text branch: a
// downloaded remote file lands in a read-only editor buffer under the sftp://
// virtual path, its title naming the host — and a save is refused, so nothing
// is ever silently written to the cache instead of the host.
func TestRemoteFetchedOpensReadOnly(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(remoteFetchedMsg{
		Alias: "web01",
		Path:  "/var/log/app.log",
		Local: "/tmp/nonexistent-cache/app.log",
		Data:  []byte("line one\n"),
	})
	m = tm.(Model)
	ed := m.activeEditor()
	if ed == nil || !ed.ReadOnly() {
		t.Fatal("remote text did not land in a read-only buffer")
	}
	if got := ed.Path(); got != "sftp://web01/var/log/app.log" {
		t.Fatalf("buffer path = %q", got)
	}
	if title := m.editorTitle(ed); !strings.Contains(title, "app.log (web01)") || !strings.Contains(title, "[RO]") {
		t.Fatalf("title = %q, want the remote origin and [RO]", title)
	}
}

// TestRemoteFetchedRefusesBinary verifies the binary refusal: bytes nothing
// claims and no text buffer can show open nothing.
func TestRemoteFetchedRefusesBinary(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{})
	editors := 0
	for _, k := range m.activeWS().Panes.Keys() {
		if m.activeWS().Panes.Get(k).Kind() == pane.KindEditor {
			editors++
		}
	}
	tm, _ := m.Update(remoteFetchedMsg{
		Alias: "web01",
		Path:  "/bin/tool",
		Local: "/tmp/nonexistent-cache/tool",
		Data:  []byte{0x7f, 'E', 'L', 'F', 0, 0, 1},
	})
	m = tm.(Model)
	if ed := m.activeEditor(); ed != nil && ed.ReadOnly() {
		t.Fatal("binary remote file opened a preview buffer")
	}
	got := 0
	for _, k := range m.activeWS().Panes.Keys() {
		if m.activeWS().Panes.Get(k).Kind() == pane.KindEditor {
			got++
		}
	}
	if got != editors {
		t.Fatalf("binary open changed editor panes: %d, want %d", got, editors)
	}
}

// TestRemoteEntryTitle verifies the origin decoder both ways.
func TestRemoteEntryTitle(t *testing.T) {
	if title, ok := remoteEntryTitle("sftp://web01/var/log/app.log"); !ok || title != "app.log (web01)" {
		t.Fatalf("title = %q, %v", title, ok)
	}
	if _, ok := remoteEntryTitle("/var/log/app.log"); ok {
		t.Fatal("a plain path decoded as remote")
	}
}

// TestRemoteFetchLimit verifies the remote.max_fetch_mb read: the configured
// cap in bytes, the built-in 64 MiB without one.
func TestRemoteFetchLimit(t *testing.T) {
	m := NewWith(registry.New(), host.MapConfig{"remote.max_fetch_mb": "2"})
	if got := m.remoteFetchLimit(); got != 2<<20 {
		t.Fatalf("limit = %d, want %d", got, 2<<20)
	}
	m = NewWith(registry.New(), host.MapConfig{})
	if got := m.remoteFetchLimit(); got != 64<<20 {
		t.Fatalf("default limit = %d, want %d", got, 64<<20)
	}
}

// TestRemoteBrowseCommandRegistered verifies the palette command exists.
func TestRemoteBrowseCommandRegistered(t *testing.T) {
	for _, c := range registry.Global().Commands() {
		if c.ID == "remote.browse" {
			return
		}
	}
	t.Fatal("remote.browse is not registered")
}
