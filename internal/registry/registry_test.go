package registry

import (
	tea "github.com/charmbracelet/bubbletea"

	"reflect"
	"testing"

	"ike/internal/host"
	"ike/internal/plugin"
)

// fake is a configurable test plugin.
type fake struct {
	id   string
	caps plugin.Capabilities
}

func (f fake) ID() string                        { return f.id }
func (f fake) Capabilities() plugin.Capabilities { return f.caps }

func cmd(id string) plugin.Command {
	return plugin.Command{ID: id, Title: id, Scope: plugin.GlobalScope(), Run: func(host.API) tea.Cmd { return nil }}
}

func TestRegisterAndLookup(t *testing.T) {
	r := New()
	r.Add(fake{id: "b", caps: plugin.Capabilities{Commands: []plugin.Command{cmd("b.two")}}})
	r.Add(fake{id: "a", caps: plugin.Capabilities{Commands: []plugin.Command{cmd("a.one")}}})

	got := r.Commands()
	if len(got) != 2 {
		t.Fatalf("want 2 commands, got %d", len(got))
	}
	// Deterministic ordering by command id.
	if got[0].ID != "a.one" || got[1].ID != "b.two" {
		t.Fatalf("commands not ordered by id: %+v", got)
	}
	if _, ok := r.Command("a.one"); !ok {
		t.Fatal("Command(a.one) not found")
	}
}

func TestEnableDisable(t *testing.T) {
	r := New()
	r.Add(fake{id: "a", caps: plugin.Capabilities{Commands: []plugin.Command{cmd("a.one")}}})
	r.SetEnabled("a", false)
	if len(r.Commands()) != 0 {
		t.Fatal("disabled plugin still surfaced commands")
	}
	r.SetEnabled("a", true)
	if len(r.Commands()) != 1 {
		t.Fatal("re-enabled plugin missing commands")
	}
}

func TestCommandConflictDetected(t *testing.T) {
	r := New()
	r.Add(fake{id: "a", caps: plugin.Capabilities{Commands: []plugin.Command{cmd("dup")}}})
	r.Add(fake{id: "b", caps: plugin.Capabilities{Commands: []plugin.Command{cmd("dup")}}})

	cs := r.Conflicts()
	if len(cs) != 1 || cs[0].Kind != "command" || cs[0].Key != "dup" {
		t.Fatalf("expected one command conflict, got %+v", cs)
	}
	if !reflect.DeepEqual(cs[0].Owners, []string{"a", "b"}) {
		t.Fatalf("conflict owners wrong: %+v", cs[0].Owners)
	}
	// First owner by sorted plugin order wins; duplicate dropped.
	if got := r.Commands(); len(got) != 1 || got[0].Owner != "a" {
		t.Fatalf("dedupe failed: %+v", got)
	}
}

func TestKeymapLayeringAndConflict(t *testing.T) {
	r := New()
	r.Add(fake{id: "low", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{
		Keys: "ctrl+e", Scope: plugin.GlobalScope(), Priority: 1,
	}}}})
	r.Add(fake{id: "high", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{
		Keys: "ctrl+e", Scope: plugin.GlobalScope(), Priority: 5,
	}}}})

	k, ok := r.ResolveKey("ctrl+e", "")
	if !ok || k.Owner != "high" {
		t.Fatalf("highest priority binding should win, got %+v", k)
	}
	// Equal-priority same key is an ambiguous conflict.
	r2 := New()
	r2.Add(fake{id: "a", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{Keys: "ctrl+x", Scope: plugin.GlobalScope(), Priority: 1}}}})
	r2.Add(fake{id: "b", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{Keys: "ctrl+x", Scope: plugin.GlobalScope(), Priority: 1}}}})
	if cs := r2.Conflicts(); len(cs) != 1 || cs[0].Kind != "keymap" {
		t.Fatalf("expected keymap conflict, got %+v", cs)
	}
}

func TestScopeFiltering(t *testing.T) {
	r := New()
	g := cmd("g")
	g.Scope = plugin.GlobalScope()
	p := cmd("p")
	p.Scope = plugin.PaneScope("editor")
	r.Add(fake{id: "a", caps: plugin.Capabilities{Commands: []plugin.Command{g, p}}})

	if got := r.CommandsForContext(""); len(got) != 1 || got[0].ID != "g" {
		t.Fatalf("empty context should yield only global, got %+v", got)
	}
	if got := r.CommandsForContext("editor"); len(got) != 2 {
		t.Fatalf("editor context should yield global+scoped, got %+v", got)
	}
}

func TestHandlerResolution(t *testing.T) {
	r := New()
	r.Add(fake{id: "md", caps: plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID: "md.h", Extensions: []string{".MD"}, // case-insensitive
		Open: func(host.API, string) tea.Cmd { return nil },
	}}}})
	r.Add(fake{id: "sniff", caps: plugin.Capabilities{FileHandlers: []plugin.FileHandler{{
		ID:    "sniff.h",
		Match: func(_ string, head []byte) bool { return len(head) > 0 && head[0] == '#' },
		Open:  func(host.API, string) tea.Cmd { return nil },
	}}}})

	if h, ok := r.ResolveHandler("notes.md", nil); !ok || h.ID != "md.h" {
		t.Fatalf("extension match failed: %+v ok=%v", h, ok)
	}
	if h, ok := r.ResolveHandler("plain", []byte("#bang")); !ok || h.ID != "sniff.h" {
		t.Fatalf("content sniff failed: %+v ok=%v", h, ok)
	}
	if _, ok := r.ResolveHandler("plain.txt", []byte("nope")); ok {
		t.Fatal("unexpected handler claim")
	}
}

func TestHooksOrdered(t *testing.T) {
	r := New()
	r.Add(fake{id: "a", caps: plugin.Capabilities{Hooks: []plugin.Hook{{ID: "z", Event: plugin.EventFileOpened}}}})
	r.Add(fake{id: "b", caps: plugin.Capabilities{Hooks: []plugin.Hook{{ID: "a", Event: plugin.EventFileOpened}}}})
	r.Add(fake{id: "c", caps: plugin.Capabilities{Hooks: []plugin.Hook{{ID: "x", Event: plugin.EventBufferSaved}}}})

	got := r.Hooks(plugin.EventFileOpened)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "z" {
		t.Fatalf("hooks not ordered by id: %+v", got)
	}
}

func TestDispatchRoundTrip(t *testing.T) {
	h := host.New(host.MapConfig{})
	r := New()
	type marker struct{ n int }
	r.Add(fake{id: "a", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "a.go", Scope: plugin.GlobalScope(),
		Run: func(api host.API) tea.Cmd { return api.Dispatch(marker{42}) },
	}}}})

	c, ok := r.Command("a.go")
	if !ok {
		t.Fatal("command not found")
	}
	msg := c.Run(h)()
	if m, ok := msg.(marker); !ok || m.n != 42 {
		t.Fatalf("dispatch did not round-trip, got %#v", msg)
	}
}
