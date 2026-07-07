package pane

import (
	"strconv"

	"ike/internal/host"
	"ike/internal/theme"
)

// ExplorerKey is the stable key of the singleton explorer instance. It never
// changes, so context resolution, the default tree, and persistence all agree.
const ExplorerKey = "explorer"

// editorKeyBase is the key of the first editor; subsequent editors append ":N".
const editorKeyBase = "editor"

// Registry maps stable instance keys to live pane components and tracks which
// key currently holds focus. The explorer is a singleton under ExplorerKey;
// editors are allocated monotonic keys ("editor", "editor:2", "editor:3", …)
// that are never reused within a session, so the layout tree, the registry, and
// persistence never disagree on identity.
type Registry struct {
	cfg       host.Config
	pal       *theme.Palette
	instances map[string]*Instance
	order     []string // insertion order, for stable iteration
	focused   string   // key of the focused instance
	editors   int      // count of editors ever allocated, for key minting
}

// NewRegistry returns an empty registry whose new instances are configured
// against cfg.
func NewRegistry(cfg host.Config) *Registry {
	return &Registry{cfg: cfg, instances: map[string]*Instance{}}
}

// SetPalette records the active theme palette and threads it into every
// existing instance; new instances pick it up at construction. Call again on a
// theme change (config reload) to re-theme live.
func (r *Registry) SetPalette(p *theme.Palette) {
	r.pal = p
	for _, key := range r.order {
		r.instances[key].setPalette(p)
	}
}

// Reconfigure replaces the registry's config and re-applies it — together with
// the current palette — to every instance, used on live config reloads.
func (r *Registry) Reconfigure(cfg host.Config) {
	r.cfg = cfg
	for _, key := range r.order {
		inst := r.instances[key]
		inst.setPalette(r.pal)
		inst.configure(cfg)
	}
}

// AddExplorer creates the singleton explorer instance under ExplorerKey and
// returns its key. Calling it twice is a programming error; the second call
// returns the existing key without creating a duplicate.
func (r *Registry) AddExplorer() string {
	if _, ok := r.instances[ExplorerKey]; ok {
		return ExplorerKey
	}
	r.put(newInstance(ExplorerKey, KindExplorer, r.cfg, r.pal))
	return ExplorerKey
}

// AddEditor creates a fresh editor instance, allocating the next monotonic key,
// and returns that key.
func (r *Registry) AddEditor() string {
	key := r.mintEditorKey()
	r.put(newInstance(key, KindEditor, r.cfg, r.pal))
	return key
}

// AddEditorKey recreates an editor instance under an exact key, used by restore
// to rebuild the saved pane set. The minting counter is advanced past any
// numeric suffix so future AddEditor calls never collide with a restored key.
func (r *Registry) AddEditorKey(key string) *Instance {
	inst := newInstance(key, KindEditor, r.cfg, r.pal)
	r.put(inst)
	r.advancePast(key)
	return inst
}

// mintEditorKey returns the next unused editor key.
func (r *Registry) mintEditorKey() string {
	r.editors++
	if r.editors == 1 {
		return editorKeyBase
	}
	return editorKeyBase + ":" + strconv.Itoa(r.editors)
}

// advancePast bumps the editor counter so a later mint cannot reproduce key.
func (r *Registry) advancePast(key string) {
	n := 1
	if len(key) > len(editorKeyBase)+1 && key[:len(editorKeyBase)+1] == editorKeyBase+":" {
		if v, err := strconv.Atoi(key[len(editorKeyBase)+1:]); err == nil {
			n = v
		}
	}
	if n > r.editors {
		r.editors = n
	}
}

func (r *Registry) put(inst *Instance) {
	r.instances[inst.key] = inst
	r.order = append(r.order, inst.key)
}

// Get returns the instance for key, or nil when absent.
func (r *Registry) Get(key string) *Instance { return r.instances[key] }

// Has reports whether an instance exists for key.
func (r *Registry) Has(key string) bool { _, ok := r.instances[key]; return ok }

// Close drops the instance for key from the registry. Closing the focused
// instance leaves focus dangling; the caller is responsible for refocusing.
func (r *Registry) Close(key string) {
	if _, ok := r.instances[key]; !ok {
		return
	}
	delete(r.instances, key)
	for i, k := range r.order {
		if k == key {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	if r.focused == key {
		r.focused = ""
	}
}

// Keys returns the instance keys in insertion order.
func (r *Registry) Keys() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len reports how many instances the registry holds.
func (r *Registry) Len() int { return len(r.instances) }

// Focused returns the focused instance key, or "" when nothing is focused.
func (r *Registry) Focused() string { return r.focused }

// FocusedInstance returns the focused instance, or nil.
func (r *Registry) FocusedInstance() *Instance { return r.instances[r.focused] }

// SetFocused makes key the focused instance and marks every instance's focus
// state accordingly. A key with no instance clears focus without panicking.
func (r *Registry) SetFocused(key string) {
	if _, ok := r.instances[key]; !ok {
		key = ""
	}
	r.focused = key
	for k, inst := range r.instances {
		inst.SetFocused(k == key)
	}
}
