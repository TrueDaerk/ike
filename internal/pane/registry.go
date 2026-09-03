package pane

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archview"
	"ike/internal/breakpanel"
	"ike/internal/dataview"
	"ike/internal/debugdoctor"
	"ike/internal/debugpanel"
	"ike/internal/depspanel"
	"ike/internal/diff"
	"ike/internal/domview"
	"ike/internal/editor/register"
	"ike/internal/espane"
	"ike/internal/ghissues"
	"ike/internal/hexview"
	"ike/internal/host"
	"ike/internal/httppane"
	"ike/internal/imgview"
	"ike/internal/lspdoctor"
	"ike/internal/merge"
	"ike/internal/nbview"
	"ike/internal/preview"
	"ike/internal/problems"
	"ike/internal/remote"
	"ike/internal/structpanel"
	"ike/internal/terminal"
	"ike/internal/testresults"
	"ike/internal/theme"
	"ike/internal/usages"
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

// imageKeyBase is the key of the first image preview; later ones append ":N".
const imageKeyBase = "image"

// diffKeyBase is the key of the first diff viewer; later ones append ":N".
const diffKeyBase = "diff"

// mergeKeyBase is the key of the first merge view; later ones append ":N".
const mergeKeyBase = "merge"

// archiveKeyBase is the key of the first archive viewer; later ones append
// ":N" (#1762).
const archiveKeyBase = "archive"

// dataKeyBase is the key of the first data viewer; later ones append ":N"
// (#1764).
const dataKeyBase = "data"

// hexKeyBase is the key of the first hex viewer; later ones append ":N"
// (#2420).
const hexKeyBase = "hex"

// nbKeyBase is the key of the first notebook viewer; later ones append ":N"
// (#2425).
const nbKeyBase = "notebook"

// esKeyBase prefixes Elasticsearch console keys (#1927): one console per
// configured endpoint, keyed "es:<endpoint>" — the endpoint name is the
// identity, so no counter is minted.
const esKeyBase = "es"

// remoteKeyBase prefixes SFTP remote browser keys (#1997): one browser per
// ssh host alias, keyed "remote:<alias>" — the alias is the identity, so no
// counter is minted, and opening a host twice refocuses.
const remoteKeyBase = "remote"

// VCSKey is the stable key of the singleton VCS tool window (Roadmap 0330).
const VCSKey = "vcs"

// DebugKey is the stable key of the singleton debug tool window (0350, #580).
const DebugKey = "debug"

// ProblemsKey is the stable key of the singleton Problems tool window (#1024).
const ProblemsKey = "problems"

// StructureKey is the stable key of the singleton Structure tool window (#1025).
const StructureKey = "structure"

// UsagesKey is the stable key of the singleton Usages tool window (#1155).
const UsagesKey = "usages"

// HTTPKey is the stable key of the singleton HTTP response viewer (#1250).
const HTTPKey = "http"

// BreakpointsKey is the stable key of the singleton Breakpoints tool window (#1377).
const BreakpointsKey = "breakpoints"

// TestsKey is the stable key of the singleton Test Results tool window (#1911).
const TestsKey = "tests"

// IssuesKey is the stable key of the singleton GitHub Issues tool window (#1934).
const IssuesKey = "issues"

// DOMKey is the stable key of the singleton DOM inspector tool window (#1929).
const DOMKey = "dom"

// DoctorKey is the stable key of the singleton Xdebug Doctor tool window (#1991).
const DoctorKey = "xdoctor"

// LSPDoctorKey is the stable key of the singleton LSP Doctor tool window (#2164).
const LSPDoctorKey = "lspdoctor"

// DepsKey is the stable key of the singleton Dependencies tool window (#2419).
const DepsKey = "deps"

// Registry maps stable instance keys to live pane components and tracks which
// key currently holds focus. The explorer is a singleton under ExplorerKey;
// editors are allocated monotonic keys ("editor", "editor:2", "editor:3", …)
// that are never reused within a session, so the layout tree, the registry, and
// persistence never disagree on identity.
type Registry struct {
	cfg host.Config
	// regs is the app-wide shared register store handed to every editor
	// created through the registry (#1540); nil (tests) keeps editors on
	// their private stores.
	regs      *register.Store
	pal       *theme.Palette
	instances map[string]*Instance
	order     []string // insertion order, for stable iteration
	focused   string   // key of the focused instance
	editors   int      // count of editors ever allocated, for key minting
	terminals int      // count of terminals ever allocated, for key minting
	previews  int      // count of markdown previews ever allocated, for key minting
	images    int      // count of image previews ever allocated, for key minting
	diffs     int      // count of diff viewers ever allocated, for key minting
	merges    int      // count of merge views ever allocated, for key minting
	archives  int      // count of archive viewers ever allocated, for key minting
	datas     int      // count of data viewers ever allocated, for key minting
	hexes     int      // count of hex viewers ever allocated, for key minting
	notebooks int      // count of notebook viewers ever allocated, for key minting
	// loaded collects the files deferred tabs (#2177) read since the last
	// drain, so the root model can give each the wiring a freshly opened
	// buffer gets. It lives on the registry rather than the model because
	// the tabs do: a workspace parks and resumes with its registry, while
	// the model around it is rebuilt on every project switch.
	loaded []string
}

// NewRegistry returns an empty registry whose new instances are configured
// against cfg. regs is the app-wide shared register store threaded into every
// editor the registry creates (#1540); nil keeps editors on private stores.
func NewRegistry(cfg host.Config, regs *register.Store) *Registry {
	return &Registry{cfg: cfg, regs: regs, instances: map[string]*Instance{}}
}

// NoteLoaded records that a deferred tab just read path (#2177). The deferred
// loader calls it; the root model drains the list once the update pass has
// settled.
func (r *Registry) NoteLoaded(path string) {
	if path != "" {
		r.loaded = append(r.loaded, path)
	}
}

// TakeLoaded returns and clears the recorded lazy-loaded paths (#2177).
func (r *Registry) TakeLoaded() []string {
	if len(r.loaded) == 0 {
		return nil
	}
	out := r.loaded
	r.loaded = nil
	return out
}

// ForgetLoaded drops path from the pending lazy-load record (#2177): the
// caller is an explicit open that runs the post-load wiring itself.
func (r *Registry) ForgetLoaded(path string) {
	kept := r.loaded[:0]
	for _, p := range r.loaded {
		if p != path {
			kept = append(kept, p)
		}
	}
	r.loaded = kept
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

// Palette returns the recorded theme palette (nil before the first
// SetPalette) — the seam that lets the root model assert a rebuilt registry
// was re-themed (#722).
func (r *Registry) Palette() *theme.Palette { return r.pal }

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
	r.put(newInstance(ExplorerKey, KindExplorer, r.cfg, r.pal, r.regs))
	return ExplorerKey
}

// AddEditor creates a fresh editor instance, allocating the next monotonic key,
// and returns that key.
func (r *Registry) AddEditor() string {
	key := r.mintEditorKey()
	r.put(newInstance(key, KindEditor, r.cfg, r.pal, r.regs))
	return key
}

// AddEditorKey recreates an editor instance under an exact key, used by restore
// to rebuild the saved pane set. The minting counter is advanced past any
// numeric suffix so future AddEditor calls never collide with a restored key.
// A terminal- or viewer-shaped key (a pane converted into a tab host, #836,
// #1778) advances that kind's counter instead.
func (r *Registry) AddEditorKey(key string) *Instance {
	inst := newInstance(key, KindEditor, r.cfg, r.pal, r.regs)
	r.put(inst)
	r.advancePastKey(key)
	return inst
}

// advancePastKey bumps the counter matching key's base past its numeric
// suffix, whatever kind the key was minted for.
func (r *Registry) advancePastKey(key string) {
	base := key
	if i := strings.IndexByte(key, ':'); i >= 0 {
		base = key[:i]
	}
	switch base {
	case terminalKeyBase:
		r.advancePastTerminal(key)
	case previewKeyBase:
		r.advancePastPreview(key)
	case diffKeyBase:
		r.advancePastDiff(key)
	case imageKeyBase:
		advanceCounter(key, imageKeyBase, &r.images)
	case archiveKeyBase:
		advanceCounter(key, archiveKeyBase, &r.archives)
	case dataKeyBase:
		advanceCounter(key, dataKeyBase, &r.datas)
	case hexKeyBase:
		advanceCounter(key, hexKeyBase, &r.hexes)
	case nbKeyBase:
		advanceCounter(key, nbKeyBase, &r.notebooks)
	default:
		r.advancePast(key)
	}
}

// advanceCounter bumps counter past key's numeric suffix relative to base.
func advanceCounter(key, base string, counter *int) {
	n := 1
	if len(key) > len(base)+1 && key[:len(base)+1] == base+":" {
		if v, err := strconv.Atoi(key[len(base)+1:]); err == nil {
			n = v
		}
	}
	if n > *counter {
		*counter = n
	}
}

// AddTerminal creates a terminal instance running shell in dir; send is the
// program's async injector (host.Send) for output/exit notifications. It
// returns the new instance's key ("terminal", then "terminal:N").
func (r *Registry) AddTerminal(shell, dir string, env []string, send func(tea.Msg)) string {
	key := r.MintTerminalKey()
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = terminal.New(key, shell, dir, 80, 24, env, send)
	inst.term.SetPalette(r.pal)
	inst.term.SetAutoSuggest(autosuggestOn(r.cfg))
	r.put(inst)
	return key
}

// AddTerminalPaneFrom wraps an already-running terminal model as a fresh
// terminal instance (#707): a terminal tab dragged out of a tab list becomes
// its own pane without restarting the shell. The pane key is freshly minted;
// the model keeps its original session routing key.
func (r *Registry) AddTerminalPaneFrom(term terminal.Model) string {
	key := r.MintTerminalKey()
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = term
	inst.term.SetPalette(r.pal)
	r.put(inst)
	return key
}

// AddTool creates a terminal pane running argv as a custom TUI tool session
// (#741): a command session marked with the tool name, so chrome, persistence
// and exit handling treat it as a tool pane rather than a terminal.
func (r *Registry) AddTool(name string, argv []string, dir string, env []string, send func(tea.Msg)) string {
	key := r.MintTerminalKey()
	r.put(r.newToolInstance(key, name, argv, dir, env, send))
	return key
}

// AddToolKey recreates a tool pane under an exact key with a fresh process —
// layout restore re-spawns tools in their saved position, like terminals.
func (r *Registry) AddToolKey(key, name string, argv []string, dir string, env []string, send func(tea.Msg)) *Instance {
	inst := r.newToolInstance(key, name, argv, dir, env, send)
	r.put(inst)
	r.advancePastTerminal(key)
	return inst
}

// AdoptToolKey registers an already-running tool session as a pane under an
// exact key (#1890): layout restore re-attaches a live global tool instance in
// its saved slot instead of spawning a second process. The minting counter
// advances past the key; the session keeps its original routing key.
func (r *Registry) AdoptToolKey(key string, t terminal.Model) *Instance {
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = t
	inst.term.SetPalette(r.pal)
	r.put(inst)
	r.advancePastTerminal(key)
	return inst
}

// NewToolSession builds a tool-marked command session without a pane (#836):
// a freshly minted key running argv, ready to host as an editor tab — layout
// restore restarts tab-hosted tools this way.
func (r *Registry) NewToolSession(name string, argv []string, dir string, env []string, send func(tea.Msg)) terminal.Model {
	key := r.MintTerminalKey()
	t := terminal.NewCommand(key, argv, dir, 80, 24, env, send)
	t.SetPalette(r.pal)
	t.SetLabel(name)
	t.SetTool(name)
	return t
}

// newToolInstance builds the shared tool-pane instance (#741).
func (r *Registry) newToolInstance(key, name string, argv []string, dir string, env []string, send func(tea.Msg)) *Instance {
	inst := &Instance{key: key, kind: KindTerminal, cfg: r.cfg, pal: r.pal}
	inst.term = terminal.NewCommand(key, argv, dir, 80, 24, env, send)
	inst.term.SetPalette(r.pal)
	inst.term.SetLabel(name)
	inst.term.SetTool(name)
	return inst
}

// MintTerminalKey allocates the next terminal session key without creating a
// pane — terminal tabs (#573) live inside an editor instance but their
// sessions still need a unique key for output/exit message routing.
func (r *Registry) MintTerminalKey() string {
	r.terminals++
	if r.terminals == 1 {
		return terminalKeyBase
	}
	return terminalKeyBase + ":" + strconv.Itoa(r.terminals)
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

// AddImagePreview creates an image preview instance bound to the file at
// path, returning the new instance's key ("image", then "image:N") (#1479).
func (r *Registry) AddImagePreview(path string) string {
	r.images++
	key := imageKeyBase
	if r.images > 1 {
		key = imageKeyBase + ":" + strconv.Itoa(r.images)
	}
	inst := &Instance{key: key, kind: KindImage, cfg: r.cfg, pal: r.pal}
	inst.iv = imgview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddImageKey recreates an image preview under an exact key, used by layout
// restore. The minting counter advances past the key.
func (r *Registry) AddImageKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindImage, cfg: r.cfg, pal: r.pal}
	inst.iv = imgview.New(key, path, r.pal)
	r.put(inst)
	if len(key) > len(imageKeyBase)+1 && key[:len(imageKeyBase)+1] == imageKeyBase+":" {
		if v, err := strconv.Atoi(key[len(imageKeyBase)+1:]); err == nil && v > r.images {
			r.images = v
		}
	} else if r.images < 1 {
		r.images = 1
	}
	return inst
}

// AddArchiveView creates an archive viewer instance bound to the file at
// path, returning the new instance's key ("archive", then "archive:N")
// (#1762). The listing is read at construction; a failure surfaces as the
// pane's own error notice.
func (r *Registry) AddArchiveView(path string) string {
	r.archives++
	key := archiveKeyBase
	if r.archives > 1 {
		key = archiveKeyBase + ":" + strconv.Itoa(r.archives)
	}
	inst := &Instance{key: key, kind: KindArchive, cfg: r.cfg, pal: r.pal}
	inst.av = archview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddArchiveKey recreates an archive viewer under an exact key, used by layout
// restore. The minting counter advances past the key.
func (r *Registry) AddArchiveKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindArchive, cfg: r.cfg, pal: r.pal}
	inst.av = archview.New(key, path, r.pal)
	r.put(inst)
	if len(key) > len(archiveKeyBase)+1 && key[:len(archiveKeyBase)+1] == archiveKeyBase+":" {
		if v, err := strconv.Atoi(key[len(archiveKeyBase)+1:]); err == nil && v > r.archives {
			r.archives = v
		}
	} else if r.archives < 1 {
		r.archives = 1
	}
	return inst
}

// AddDataView creates a data viewer instance bound to the database file at
// path, returning the new instance's key ("data", then "data:N") (#1764).
// The backend opens at construction; a failure surfaces as the pane's own
// error notice.
func (r *Registry) AddDataView(path string) string {
	r.datas++
	key := dataKeyBase
	if r.datas > 1 {
		key = dataKeyBase + ":" + strconv.Itoa(r.datas)
	}
	inst := &Instance{key: key, kind: KindData, cfg: r.cfg, pal: r.pal}
	inst.dv = dataview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddDataKey recreates a data viewer under an exact key, used by layout
// restore. The minting counter advances past the key.
func (r *Registry) AddDataKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindData, cfg: r.cfg, pal: r.pal}
	inst.dv = dataview.New(key, path, r.pal)
	r.put(inst)
	if len(key) > len(dataKeyBase)+1 && key[:len(dataKeyBase)+1] == dataKeyBase+":" {
		if v, err := strconv.Atoi(key[len(dataKeyBase)+1:]); err == nil && v > r.datas {
			r.datas = v
		}
	} else if r.datas < 1 {
		r.datas = 1
	}
	return inst
}

// AddHexView creates a hex viewer instance bound to the file at path,
// returning the new instance's key ("hex", then "hex:N") (#2420). The file
// opens for windowed reads at construction; a failure surfaces as the pane's
// own error notice.
func (r *Registry) AddHexView(path string) string {
	r.hexes++
	key := suffixedKey(hexKeyBase, r.hexes)
	inst := &Instance{key: key, kind: KindHex, cfg: r.cfg, pal: r.pal}
	inst.hv = hexview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddHexKey recreates a hex viewer under an exact key, used by layout
// restore. The minting counter advances past the key.
func (r *Registry) AddHexKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindHex, cfg: r.cfg, pal: r.pal}
	inst.hv = hexview.New(key, path, r.pal)
	r.put(inst)
	if len(key) > len(hexKeyBase)+1 && key[:len(hexKeyBase)+1] == hexKeyBase+":" {
		if v, err := strconv.Atoi(key[len(hexKeyBase)+1:]); err == nil && v > r.hexes {
			r.hexes = v
		}
	} else if r.hexes < 1 {
		r.hexes = 1
	}
	return inst
}

// AddNotebookView creates a notebook viewer instance bound to the .ipynb file
// at path, returning the new instance's key ("notebook", then "notebook:N")
// (#2425). The file is read and parsed at construction; a read or parse
// failure surfaces as the pane's own error notice.
func (r *Registry) AddNotebookView(path string) string {
	r.notebooks++
	key := suffixedKey(nbKeyBase, r.notebooks)
	inst := &Instance{key: key, kind: KindNotebook, cfg: r.cfg, pal: r.pal}
	inst.nv = nbview.New(key, path, r.pal)
	r.put(inst)
	return key
}

// AddNotebookKey recreates a notebook viewer under an exact key, used by
// layout restore. The minting counter advances past the key.
func (r *Registry) AddNotebookKey(key, path string) *Instance {
	inst := &Instance{key: key, kind: KindNotebook, cfg: r.cfg, pal: r.pal}
	inst.nv = nbview.New(key, path, r.pal)
	r.put(inst)
	if len(key) > len(nbKeyBase)+1 && key[:len(nbKeyBase)+1] == nbKeyBase+":" {
		if v, err := strconv.Atoi(key[len(nbKeyBase)+1:]); err == nil && v > r.notebooks {
			r.notebooks = v
		}
	} else if r.notebooks < 1 {
		r.notebooks = 1
	}
	return inst
}

// NotebooksMinted reports whether this registry ever created a notebook
// viewer — the cheap gate the app's Kitty graphics reconcile checks before
// walking the panes (#2425).
func (r *Registry) NotebooksMinted() bool { return r != nil && r.notebooks > 0 }

// AddES creates (or returns) the Elasticsearch console instance for the named
// endpoint, keyed "es:<endpoint>" (#1927) — one console per cluster, so
// opening it twice refocuses rather than duplicates. The cluster connect
// happens in the pane's background Init, never here.
func (r *Registry) AddES(endpoint string) string {
	key := esKeyBase + ":" + endpoint
	if _, ok := r.instances[key]; ok {
		return key
	}
	inst := &Instance{key: key, kind: KindES, cfg: r.cfg, pal: r.pal}
	inst.es = espane.New(key, endpoint, r.pal)
	r.put(inst)
	return key
}

// AddESKey recreates a console under an exact key, used by layout restore.
func (r *Registry) AddESKey(key, endpoint string) *Instance {
	inst := &Instance{key: key, kind: KindES, cfg: r.cfg, pal: r.pal}
	inst.es = espane.New(key, endpoint, r.pal)
	r.put(inst)
	return inst
}

// AddRemote creates (or returns) the SFTP remote browser instance for the ssh
// host alias, keyed "remote:<alias>" (#1997) — one browser per host, so
// opening it twice refocuses rather than duplicates. The dial happens in the
// pane's background Init, never here.
func (r *Registry) AddRemote(alias string) string {
	key := remoteKeyBase + ":" + alias
	if _, ok := r.instances[key]; ok {
		return key
	}
	r.put(r.newRemoteInstance(key, alias))
	return key
}

// AddRemoteKey recreates a remote browser under an exact key, used by layout
// restore; Init re-dials the host in the background.
func (r *Registry) AddRemoteKey(key, alias string) *Instance {
	inst := r.newRemoteInstance(key, alias)
	r.put(inst)
	return inst
}

// newRemoteInstance builds one browser, seeding its hidden-entry filter from
// the explorer's setting so both trees agree on what a fresh pane shows; the
// runtime "." toggle stays authoritative afterwards.
func (r *Registry) newRemoteInstance(key, alias string) *Instance {
	inst := &Instance{key: key, kind: KindRemote, cfg: r.cfg, pal: r.pal}
	inst.rm = remote.New(key, alias, nil, r.pal)
	if r.cfg != nil {
		if v, ok := r.cfg.Get("explorer.show_hidden"); ok && v == "true" {
			inst.rm.SetShowHidden(true)
		}
	}
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

// applyDiffConfig threads the diff.* config keys — context (0340, #494) and
// ignore_whitespace (#2170) — into a fresh diff instance; unset keys keep the
// model's defaults.
func (r *Registry) applyDiffConfig(inst *Instance) { applyDiffCfg(r.cfg, inst) }

// AddMerge creates a three-way merge view for the conflicted file at path
// (#1478), returning the new instance's key ("merge", then "merge:N"). The
// result editor loads the file (language, editorconfig, save path); contents
// arrive afterwards via the merge model's SetContents.
func (r *Registry) AddMerge(path string) string {
	r.merges++
	key := mergeKeyBase
	if r.merges > 1 {
		key = mergeKeyBase + ":" + strconv.Itoa(r.merges)
	}
	inst := &Instance{key: key, kind: KindMerge, cfg: r.cfg, pal: r.pal}
	ed := newEditorModel(r.cfg, r.pal, r.regs)
	if err := ed.Load(path); err != nil {
		ed.NewFile(path)
	}
	inst.mg = merge.New(key, path, &ed, r.pal)
	r.put(inst)
	return key
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

// AddDebug creates the singleton debug tool window under DebugKey (0350,
// #580) and returns its key; a second call returns the existing key.
func (r *Registry) AddDebug() string {
	if _, ok := r.instances[DebugKey]; ok {
		return DebugKey
	}
	inst := &Instance{key: DebugKey, kind: KindDebug, cfg: r.cfg, pal: r.pal}
	inst.dp = debugpanel.New(r.pal)
	r.put(inst)
	return DebugKey
}

// AddProblems creates the singleton Problems tool window under ProblemsKey
// (#1024) and returns its key; a second call returns the existing key.
func (r *Registry) AddProblems() string {
	if _, ok := r.instances[ProblemsKey]; ok {
		return ProblemsKey
	}
	inst := &Instance{key: ProblemsKey, kind: KindProblems, cfg: r.cfg, pal: r.pal}
	inst.pp = problems.New(r.pal)
	r.put(inst)
	return ProblemsKey
}

// AddStructure creates the singleton Structure tool window under StructureKey
// (#1025) and returns its key; a second call returns the existing key.
func (r *Registry) AddStructure() string {
	if _, ok := r.instances[StructureKey]; ok {
		return StructureKey
	}
	inst := &Instance{key: StructureKey, kind: KindStructure, cfg: r.cfg, pal: r.pal}
	inst.sp = structpanel.New(r.pal)
	r.put(inst)
	return StructureKey
}

// AddUsages creates the singleton Usages tool window under UsagesKey (#1155)
// and returns its key; a second call returns the existing key.
func (r *Registry) AddUsages() string {
	if _, ok := r.instances[UsagesKey]; ok {
		return UsagesKey
	}
	inst := &Instance{key: UsagesKey, kind: KindUsages, cfg: r.cfg, pal: r.pal}
	inst.up = usages.New(r.pal)
	r.put(inst)
	return UsagesKey
}

// AddTests creates the singleton Test Results tool window under TestsKey
// (#1911) and returns its key; a second call returns the existing key.
func (r *Registry) AddTests() string {
	if _, ok := r.instances[TestsKey]; ok {
		return TestsKey
	}
	inst := &Instance{key: TestsKey, kind: KindTests, cfg: r.cfg, pal: r.pal}
	inst.tr = testresults.New(r.pal)
	r.put(inst)
	return TestsKey
}

// AddIssues creates the singleton GitHub Issues tool window under IssuesKey
// (#1934) and returns its key; a second call returns the existing key.
func (r *Registry) AddIssues() string {
	if _, ok := r.instances[IssuesKey]; ok {
		return IssuesKey
	}
	inst := &Instance{key: IssuesKey, kind: KindIssues, cfg: r.cfg, pal: r.pal}
	inst.gi = ghissues.New(r.pal)
	inst.gi.Configure(r.cfg)
	r.put(inst)
	return IssuesKey
}

// AddDOM creates the singleton DOM inspector tool window under DOMKey (#1929)
// and returns its key; a second call returns the existing key.
func (r *Registry) AddDOM() string {
	if _, ok := r.instances[DOMKey]; ok {
		return DOMKey
	}
	inst := &Instance{key: DOMKey, kind: KindDOM, cfg: r.cfg, pal: r.pal}
	inst.dm = domview.New(r.pal)
	r.put(inst)
	return DOMKey
}

// AddDoctor creates the singleton Xdebug Doctor tool window under DoctorKey
// (#1991) and returns its key; a second call returns the existing key.
func (r *Registry) AddDoctor() string {
	if _, ok := r.instances[DoctorKey]; ok {
		return DoctorKey
	}
	inst := &Instance{key: DoctorKey, kind: KindDoctor, cfg: r.cfg, pal: r.pal}
	inst.xd = debugdoctor.New(r.pal)
	r.put(inst)
	return DoctorKey
}

// AddLSPDoctor creates the singleton LSP Doctor tool window under
// LSPDoctorKey (#2164) and returns its key; a second call returns the
// existing key.
func (r *Registry) AddLSPDoctor() string {
	if _, ok := r.instances[LSPDoctorKey]; ok {
		return LSPDoctorKey
	}
	inst := &Instance{key: LSPDoctorKey, kind: KindLSPDoctor, cfg: r.cfg, pal: r.pal}
	inst.ld = lspdoctor.New(r.pal)
	r.put(inst)
	return LSPDoctorKey
}

// AddDeps creates the singleton Dependencies tool window under DepsKey
// (#2419) and returns its key; a second call returns the existing key.
func (r *Registry) AddDeps() string {
	if _, ok := r.instances[DepsKey]; ok {
		return DepsKey
	}
	inst := &Instance{key: DepsKey, kind: KindDeps, cfg: r.cfg, pal: r.pal}
	inst.dep = depspanel.New(r.pal)
	r.put(inst)
	return DepsKey
}

// AddBreakpoints creates the singleton Breakpoints tool window under
// BreakpointsKey (#1377) and returns its key; a second call returns the
// existing key.
func (r *Registry) AddBreakpoints() string {
	if _, ok := r.instances[BreakpointsKey]; ok {
		return BreakpointsKey
	}
	inst := &Instance{key: BreakpointsKey, kind: KindBreakpoints, cfg: r.cfg, pal: r.pal}
	inst.bp = breakpanel.New(r.pal)
	r.put(inst)
	return BreakpointsKey
}

// AddHTTP creates the singleton HTTP response viewer under HTTPKey (#1250)
// and returns its key; a second call returns the existing key.
func (r *Registry) AddHTTP() string {
	if _, ok := r.instances[HTTPKey]; ok {
		return HTTPKey
	}
	inst := &Instance{key: HTTPKey, kind: KindHTTP, cfg: r.cfg, pal: r.pal}
	inst.hp = httppane.New(r.pal)
	r.put(inst)
	return HTTPKey
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
	inst.df.SetRevs("HEAD", "")
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

// AddDiffRevKey recreates a revision-backed diff viewer under an exact key
// (#508): a non-empty rev labels its side "name @ rev" and marks it for
// git-blob restore; a revision-backed right side is read-only.
func (r *Registry) AddDiffRevKey(key, leftPath, rightPath, leftRev, rightRev string) *Instance {
	inst := r.newDiffRevInstance(key, leftPath, rightPath, leftRev, rightRev)
	r.put(inst)
	r.advancePastDiff(key)
	return inst
}

// newDiffRevInstance builds a revision-backed diff instance without
// registering it — shared by AddDiffRevKey and the content-tab restore
// (#1778).
func (r *Registry) newDiffRevInstance(key, leftPath, rightPath, leftRev, rightRev string) *Instance {
	name := filepath.Base(rightPath)
	leftTitle := name + " @ " + shortRev(leftRev)
	if leftRev == "" {
		leftTitle = filepath.Base(leftPath)
	}
	rightTitle := name
	if rightRev != "" {
		rightTitle = name + " @ " + shortRev(rightRev)
	}
	inst := &Instance{key: key, kind: KindDiff, cfg: r.cfg, pal: r.pal}
	inst.df = diff.New(key, leftTitle, rightTitle, rightPath, r.pal)
	inst.df.SetRevs(leftRev, rightRev)
	inst.df.SetEditable(rightRev == "")
	r.applyDiffConfig(inst)
	return inst
}

// shortRev clips a full sha to seven characters, keeping suffixes ("^") and
// symbolic names ("HEAD") intact.
func shortRev(rev string) string {
	base, suffix := rev, ""
	if strings.HasSuffix(rev, "^") {
		base, suffix = rev[:len(rev)-1], "^"
	}
	if len(base) == 40 {
		base = base[:7]
	}
	return base + suffix
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

// suffixedKey renders the Nth key of a base: "base" for 1, "base:N" beyond.
func suffixedKey(base string, n int) string {
	if n == 1 {
		return base
	}
	return base + ":" + strconv.Itoa(n)
}

// mintContentKey allocates the next key of a viewer kind (#1778) — the same
// counters the Add* constructors advance, so tab-detached content re-keys
// without collisions. Unknown kinds — the HTTP viewer included, which is a
// fixed-position tool window and never lives in a tab (#2042) — yield "".
func (r *Registry) mintContentKey(kind Kind) string {
	switch kind {
	case KindMarkdown:
		r.previews++
		return suffixedKey(previewKeyBase, r.previews)
	case KindImage:
		r.images++
		return suffixedKey(imageKeyBase, r.images)
	case KindDiff:
		r.diffs++
		return suffixedKey(diffKeyBase, r.diffs)
	case KindArchive:
		r.archives++
		return suffixedKey(archiveKeyBase, r.archives)
	case KindData:
		r.datas++
		return suffixedKey(dataKeyBase, r.datas)
	case KindHex:
		r.hexes++
		return suffixedKey(hexKeyBase, r.hexes)
	case KindNotebook:
		r.notebooks++
		return suffixedKey(nbKeyBase, r.notebooks)
	}
	return ""
}

// NewContentPane builds a fresh viewer instance of kind without registering
// it (#1778): the tab restore's constructor, mirroring what the Add*Key
// restore paths build for dedicated panes. path/path2/rev/rev2 follow the
// paneIdentity conventions (diff panes use all four, the others just path).
// It returns nil for kinds that cannot live in tabs — the HTTP viewer
// included since #2042, so a legacy layout.json with a nested "http" tab
// restores without that tab instead of crashing.
func (r *Registry) NewContentPane(kind Kind, path, path2, rev, rev2 string) *Instance {
	if kind == KindES {
		// The console's identity is its endpoint name (carried as path), like
		// the HTTP viewer's singleton key — no counter to mint (#1927).
		key := esKeyBase + ":" + path
		inst := &Instance{key: key, kind: KindES, cfg: r.cfg, pal: r.pal}
		inst.es = espane.New(key, path, r.pal)
		return inst
	}
	key := r.mintContentKey(kind)
	if key == "" {
		return nil
	}
	inst := &Instance{key: key, kind: kind, cfg: r.cfg, pal: r.pal}
	switch kind {
	case KindMarkdown:
		inst.md = preview.New(key, path, r.pal)
	case KindImage:
		inst.iv = imgview.New(key, path, r.pal)
	case KindArchive:
		inst.av = archview.New(key, path, r.pal)
	case KindData:
		inst.dv = dataview.New(key, path, r.pal)
	case KindHex:
		inst.hv = hexview.New(key, path, r.pal)
	case KindNotebook:
		inst.nv = nbview.New(key, path, r.pal)
	case KindDiff:
		if rev != "" || rev2 != "" {
			return r.newDiffRevInstance(key, path, path2, rev, rev2)
		}
		inst.df = diff.NewFiles(key, path, path2, r.pal)
		inst.df.SetEditable(true)
		r.applyDiffConfig(inst)
	default:
		return nil
	}
	return inst
}

// AddContentPaneFrom registers a live content instance — detached from a tab
// (#1778) — as its own pane under a freshly minted key of its kind, so a
// dragged-out viewer tab becomes a dedicated pane again without reloading.
// It returns the new pane key and success.
func (r *Registry) AddContentPaneFrom(inst *Instance) (string, bool) {
	if inst == nil {
		return "", false
	}
	key := r.mintContentKey(inst.kind)
	if inst.kind == KindES {
		// Per-endpoint identity, like the HTTP singleton: refused while a
		// dedicated console for that cluster already exists (#1927).
		key = esKeyBase + ":" + inst.ES().Endpoint()
	}
	if key == "" || r.Has(key) {
		return "", false
	}
	inst.key = key
	inst.cfg = r.cfg
	inst.setPalette(r.pal)
	r.put(inst)
	return key, true
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
	// Release the instance's background resources: a terminal's session, a
	// data viewer's backend (#1764), and — for a tab host — every tab's
	// content (#573, #1778).
	inst.releaseContent()
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

// ImagesMinted reports whether this registry ever created an image preview
// (#2187). Every construction path — AddImagePreview, AddImageKey and the
// tab restore's NewContentPane — bumps the mint counter, so a false answer
// means no image pane can exist and the per-message Kitty reconcile walk can
// be skipped without touching a single instance.
func (r *Registry) ImagesMinted() bool { return r != nil && r.images > 0 }

// PreviewsMinted reports whether this registry ever created a markdown
// preview (#2180). Previews carry inline image placements of their own, so
// the Kitty reconcile walk has to run for them too — and can still be skipped
// entirely by a workspace that has neither kind.
func (r *Registry) PreviewsMinted() bool { return r != nil && r.previews > 0 }

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
