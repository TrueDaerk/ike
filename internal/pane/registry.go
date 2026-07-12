package pane

import (
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/preview"
	"ike/internal/terminal"
	"ike/internal/theme"
	"ike/internal/vcspanel"
)

// ExplorerKey is the stable key of the singleton explorer instance. It never
// changes, so context resolution, the default tree, and persistence all agree.
const ExplorerKey = "explorer"

// editorKeyBase is the key of the first editor; subsequent editors append ":N".
const editorKeyBase = "editor"

// terminalKeyBase is the key of the first terminal; later ones append ":N".
const terminalKeyBase = "terminal"

// previewKeyBase is the key of the first markdown preview; later ones append ":N".
const previewKeyBase = "preview"

// diffKeyBase is the key of the first diff viewer; later ones append ":N".
const diffKeyBase = "diff"

// VCSKey is the stable key of the singleton VCS tool window (Roadmap 0330).
const VCSKey = "vcs"

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
	terminals int      // count of terminals ever allocated, for key minting
	previews  int      // count of markdown previews ever allocated, for key minting
	diffs     int      // count of diff viewers ever allocated, for key minting
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

// AddTerminal creates a terminal instance running shell in dir; send is the
// program's async injector (host.Send) for output/exit notifications. It
// returns the new instance's key ("terminal", then "terminal:N").
func (r *Registry) AddTerminal(shell, dir string, env []string, send func(tea.Msg)) string {
	r.terminals++
	key := terminalKeyBase
	if r.terminals > 1 {
		key = terminalKeyBase + ":" + strconv.Itoa(r.terminals)
	}
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = terminal.New(key, shell, dir, 80, 24, env, send)
	inst.term.SetPalette(r.pal)
	r.put(inst)
	return key
}

// AddTerminalKey recreates a terminal under an exact key with a fresh shell
// session — layout restore re-spawns terminals in their saved position (no
// process resurrection). The minting counter advances past the key.
func (r *Registry) AddTerminalKey(key, shell, dir string, env []string, send func(tea.Msg)) *Instance {
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = terminal.New(key, shell, dir, 80, 24, env, send)
	inst.term.SetPalette(r.pal)
	r.put(inst)
	r.advancePastTerminal(key)
	return inst
}

// AdoptTerminal moves a live terminal instance from another registry into
// this one — a project switch keeps existing sessions running (#96). The key
// is kept; the counter advances past it. When the key is already taken by a
// restored terminal — layout restore just spawned a fresh placeholder shell
// for this very session (#320) — the live session takes that slot over: the
// placeholder's shell is closed and the instance replaced in place. It
// returns true on such a takeover (the layout tree already holds the leaf)
// and false when the instance was added fresh and still needs a leaf.
func (r *Registry) AdoptTerminal(inst *Instance) (tookOver bool) {
	if inst == nil || inst.Kind() != KindTerminal {
		return false
	}
	inst.cfg, inst.pal = r.cfg, r.pal
	inst.term.SetPalette(r.pal)
	if existing := r.instances[inst.Key()]; existing != nil {
		if existing.Kind() != KindTerminal {
			return false // foreign key collision: not adopted
		}
		existing.term.Close()
		r.instances[inst.Key()] = inst // order already lists the key
		r.advancePastTerminal(inst.Key())
		return true
	}
	r.put(inst)
	r.advancePastTerminal(inst.Key())
	return false
}

// AddMarkdownPreview creates a markdown preview instance bound to the source
// buffer at path, returning the new instance's key ("preview", then
// "preview:N"). Content arrives afterwards via the preview model's setters.
func (r *Registry) AddMarkdownPreview(path string) string {
	r.previews++
	key := previewKeyBase
	if r.previews > 1 {
		key = previewKeyBase + ":" + strconv.Itoa(r.previews)
	}
	inst := &Instance{key: key, kind: KindMarkdown, cfg: r.cfg, pal: r.pal}
	inst.md = preview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddMarkdownKey recreates a markdown preview under an exact key, used by
// layout restore. The minting counter advances past the key.
func (r *Registry) AddMarkdownKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindMarkdown, cfg: r.cfg, pal: r.pal}
	inst.md = preview.New(key, path, r.pal)
	r.put(inst)
	r.advancePastPreview(key)
	return inst
}

// AddDiff creates a diff viewer instance comparing the files at leftPath and
// rightPath, returning the new instance's key ("diff", then "diff:N").
// Contents arrive afterwards via the diff model's SetContents.
func (r *Registry) AddDiff(leftPath, rightPath string) string {
	r.diffs++
	key := diffKeyBase
	if r.diffs > 1 {
		key = diffKeyBase + ":" + strconv.Itoa(r.diffs)
	}
	inst := &Instance{key: key, kind: KindDiff, cfg: r.cfg, pal: r.pal}
	inst.df = diff.NewFiles(key, leftPath, rightPath, r.pal)
	inst.df.SetEditable(true) // both sides are working-tree files (#496)
	r.applyDiffConfig(inst)
	r.put(inst)
	return key
}

// applyDiffConfig threads the diff.context config key (0340, #494) into a
// fresh diff instance; unset keeps the model's default.
func (r *Registry) applyDiffConfig(inst *Instance) {
	if r.cfg == nil {
		return
	}
	if v, ok := r.cfg.Get("diff.context"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			inst.df.SetContext(n)
		}
	}
}

// AddVCS creates the singleton VCS tool window under VCSKey (Roadmap 0330)
// and returns its key; a second call returns the existing key.
func (r *Registry) AddVCS() string {
	if _, ok := r.instances[VCSKey]; ok {
		return VCSKey
	}
	inst := &Instance{key: VCSKey, kind: KindVCS, cfg: r.cfg, pal: r.pal}
	inst.vp = vcspanel.New(r.pal)
	r.put(inst)
	return VCSKey
}

// AddDiffHead creates a diff viewer comparing a file's HEAD blob (left)
// against its live content (Roadmap 0320, #467). Contents arrive via
// SetContents; a layout restore degrades to an empty left side.
func (r *Registry) AddDiffHead(rightPath string) string {
	r.diffs++
	key := diffKeyBase
	if r.diffs > 1 {
		key = diffKeyBase + ":" + strconv.Itoa(r.diffs)
	}
	inst := &Instance{key: key, kind: KindDiff, cfg: r.cfg, pal: r.pal}
	inst.df = diff.New(key, filepath.Base(rightPath)+" @ HEAD", filepath.Base(rightPath), rightPath, r.pal)
	inst.df.SetEditable(true) // the right side is the working tree (#496)
	r.applyDiffConfig(inst)
	r.put(inst)
	return key
}

// AddDiffTitled creates a diff viewer with explicit column titles (the log
// view's parent-vs-commit diff, 0330 #484); contents arrive via SetContents.
func (r *Registry) AddDiffTitled(leftTitle, rightTitle, rightPath string) string {
	r.diffs++
	key := diffKeyBase
	if r.diffs > 1 {
		key = diffKeyBase + ":" + strconv.Itoa(r.diffs)
	}
	inst := &Instance{key: key, kind: KindDiff, cfg: r.cfg, pal: r.pal}
	inst.df = diff.New(key, leftTitle, rightTitle, rightPath, r.pal)
	r.applyDiffConfig(inst)
	r.put(inst)
	return key
}

// AddDiffKey recreates a diff viewer under an exact key, used by layout
// restore. The minting counter advances past the key.
func (r *Registry) AddDiffKey(key, leftPath, rightPath string) *Instance {
	inst := &Instance{key: key, kind: KindDiff, cfg: r.cfg, pal: r.pal}
	inst.df = diff.NewFiles(key, leftPath, rightPath, r.pal)
	inst.df.SetEditable(true)
	r.applyDiffConfig(inst)
	r.put(inst)
	r.advancePastDiff(key)
	return inst
}

// advancePastDiff bumps the diff counter past key's numeric suffix.
func (r *Registry) advancePastDiff(key string) {
	n := 1
	if len(key) > len(diffKeyBase)+1 && key[:len(diffKeyBase)+1] == diffKeyBase+":" {
		if v, err := strconv.Atoi(key[len(diffKeyBase)+1:]); err == nil {
			n = v
		}
	}
	if n > r.diffs {
		r.diffs = n
	}
}

// advancePastPreview bumps the preview counter past key's numeric suffix.
func (r *Registry) advancePastPreview(key string) {
	n := 1
	if len(key) > len(previewKeyBase)+1 && key[:len(previewKeyBase)+1] == previewKeyBase+":" {
		if v, err := strconv.Atoi(key[len(previewKeyBase)+1:]); err == nil {
			n = v
		}
	}
	if n > r.previews {
		r.previews = n
	}
}

// advancePastTerminal bumps the terminal counter past key's numeric suffix.
func (r *Registry) advancePastTerminal(key string) {
	n := 1
	if len(key) > len(terminalKeyBase)+1 && key[:len(terminalKeyBase)+1] == terminalKeyBase+":" {
		if v, err := strconv.Atoi(key[len(terminalKeyBase)+1:]); err == nil {
			n = v
		}
	}
	if n > r.terminals {
		r.terminals = n
	}
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

// Close drops the instance for key from the registry, ending a terminal's
// shell session. Closing the focused instance leaves focus dangling; the
// caller is responsible for refocusing.
func (r *Registry) Close(key string) {
	inst, ok := r.instances[key]
	if !ok {
		return
	}
	if inst.Kind() == KindTerminal {
		inst.term.Close()
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
