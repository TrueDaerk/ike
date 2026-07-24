package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/watch"
)

// TestWatchEventsFireExternalFileChangeHooks guards the #1144 wiring: per-file
// watcher events reach EventExternalFileChange hook subscribers (the LSP
// bridge) with the right change kind, including the remove-then-recreate
// fixup; directory events stay hook-silent.
func TestWatchEventsFireExternalFileChangeHooks(t *testing.T) {
	var got []plugin.FileChange
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{
		Hooks: []plugin.Hook{{
			ID: "p.watch", Event: plugin.EventExternalFileChange,
			Notify: func(h host.API, payload any) tea.Cmd {
				if fc, ok := payload.(plugin.FileChange); ok {
					got = append(got, fc)
				}
				return nil
			},
		}},
	}})
	m := NewWith(reg, host.MapConfig{})

	dir := t.TempDir()
	existing := filepath.Join(dir, "a.go")
	if err := os.WriteFile(existing, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: existing})
	m = out.(Model)
	// Remove event while the file exists on disk: a replace-in-place — the
	// hook must see a modification, not a deletion.
	out, _ = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: existing})
	m = out.(Model)
	gone := filepath.Join(dir, "gone.go")
	out, _ = m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: gone})
	m = out.(Model)
	// Directory events refresh the explorer only — no file-change hook.
	_, _ = m.Update(watch.EventMsg{Kind: watch.DirChanged, Path: dir})

	want := []plugin.FileChange{
		{Path: existing, Kind: plugin.FileCreated},
		{Path: existing, Kind: plugin.FileModified},
		{Path: gone, Kind: plugin.FileDeleted},
	}
	if len(got) != len(want) {
		t.Fatalf("hook payloads = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("payload[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestBufferSavedHookFires guards #1161: a completed save reaches registered
// EventBufferSaved hooks — the lsp plugin's didSave path hangs off this.
func TestBufferSavedHookFires(t *testing.T) {
	got := make(chan string, 1)
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Hooks: []plugin.Hook{{
		Event: plugin.EventBufferSaved,
		Notify: func(h host.API, payload any) tea.Cmd {
			if p, ok := payload.(string); ok {
				select {
				case got <- p:
				default:
				}
			}
			return nil
		},
	}}}})
	m := sizedWith(t, reg, 100, 40)
	out, _ := m.Update(localHistorySnapshotMsg{path: "/proj/a.go"})
	_ = out
	select {
	case p := <-got:
		if p != "/proj/a.go" {
			t.Fatalf("hook payload = %q", p)
		}
	default:
		t.Fatal("EventBufferSaved hook did not fire")
	}
}
