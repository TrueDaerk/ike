package pane

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archview"
	"ike/internal/breakpanel"
	"ike/internal/clipboard"
	"ike/internal/dataview"
	"ike/internal/debugdoctor"
	"ike/internal/debugpanel"
	"ike/internal/diff"
	"ike/internal/domview"
	"ike/internal/editor"
	"ike/internal/editor/register"
	"ike/internal/espane"
	"ike/internal/explorer"
	"ike/internal/ghissues"
	"ike/internal/host"
	"ike/internal/httppane"
	"ike/internal/imgview"
	"ike/internal/merge"
	"ike/internal/preview"
	"ike/internal/problems"
	"ike/internal/remote"
	"ike/internal/scratch"
	"ike/internal/structpanel"
	"ike/internal/terminal"
	"ike/internal/testresults"
	"ike/internal/theme"
	"ike/internal/usages"
	"ike/internal/vcspanel"
)

// Kind is the type of component an Instance wraps. The explorer is a singleton;
// editors are many.
type Kind int

const (
	// KindExplorer is the file-tree pane. Exactly one exists, under key "explorer".
	KindExplorer Kind = iota
	// KindEditor is a text editor pane. Any number may exist, tiled side by side.
	KindEditor
	// KindTerminal is an integrated terminal pane (Roadmap 0170); any number
	// may exist, each owning one shell session.
	KindTerminal
	// KindMarkdown is a rendered markdown preview pane (#62); any number may
	// exist, each bound to one source buffer path.
	KindMarkdown
	// KindDiff is a read-only diff viewer pane (#60); any number may exist,
	// each comparing two text versions.
	KindDiff
	// KindVCS is the VCS tool window (Roadmap 0330): a singleton bottom-split
	// panel with the changes list and the git log, under key "vcs".
	KindVCS
	// KindDebug is the debug tool window (0350, #580): a singleton
	// bottom-split panel with the frames list and the variables tree,
	// under key "debug".
	KindDebug
	// KindProblems is the Problems tool window (#1024): a singleton
	// bottom-split panel aggregating LSP diagnostics project-wide,
	// under key "problems".
	KindProblems
	// KindStructure is the Structure tool window (#1025): a singleton side
	// panel with the focused buffer's LSP symbol tree, under key "structure".
	KindStructure
	// KindUsages is the Usages tool window (#1155): a singleton bottom-split
	// panel with the latest panel-targeted find-references results, under
	// key "usages".
	KindUsages
	// KindHTTP is the HTTP response viewer (#1250): a singleton bottom-split
	// read-only panel showing the last dispatched .http request's response,
	// under key "http".
	KindHTTP
	// KindBreakpoints is the Breakpoints tool window (#1377): a singleton
	// bottom-split panel listing every breakpoint in the project, under key
	// "breakpoints".
	KindBreakpoints
	// KindImage is an image preview pane (#1479); any number may exist, each
	// bound to one image file, rendered via the Kitty graphics protocol with
	// a metadata fallback.
	KindImage
	// KindMerge is the three-way merge view for a git-conflicted file
	// (#1478): ours/theirs read-only around an editable result editor. It
	// advertises the editor context so the full editor keymap (including the
	// #1149 merge accepts) drives the result.
	KindMerge
	// KindArchive is an archive viewer pane (#1762); any number may exist,
	// each bound to one archive file, listing its entries and opening one of
	// them in a read-only editor buffer.
	KindArchive
	// KindData is a data viewer pane (#1764); any number may exist, each
	// bound to one database file, listing its tables and views next to a
	// paged read-only grid of the selected one's rows.
	KindData
	// KindES is an Elasticsearch console pane (#1927); any number may exist,
	// each bound to one configured cluster endpoint by name, listing its
	// indices next to a paged read-only grid of search hits.
	KindES
	// KindTests is the Test Results tool window (#1911): a singleton
	// bottom-split panel with the last captured test run's result tree and
	// detail pane, under key "tests".
	KindTests
	// KindIssues is the GitHub Issues tool window (#1934): a singleton panel
	// listing the repository's open issues with detail view and the
	// start-work action, under key "issues".
	KindIssues
	// KindDOM is the DOM inspector tool window (#1929): a singleton side
	// panel with the focused HTML buffer's parsed DOM tree and a CSS
	// selector tester, under key "dom".
	KindDOM
	// KindDoctor is the Xdebug Doctor tool window (#1991): a singleton panel
	// with the DBGp listener status and the per-connection accept/reject
	// trace, under key "xdoctor".
	KindDoctor
	// KindRemote is an SFTP remote file browser pane (#1997); any number may
	// exist, each bound to one ssh host alias, listing the host's files and
	// asking the root model to open one via the download cache.
	KindRemote
)

// Context ids an Instance advertises for context-scoped command/keymap
// resolution, matching the constants internal/app historically owned.
const (
	ctxExplorer = "explorer"
	ctxEditor   = "editor"
	ctxTerminal = "terminal"
	ctxPreview  = "preview"
	ctxDiff     = "diff"
	ctxVCS      = "vcs"
	ctxDebug    = "debug"
	ctxProblems = "problems"
	ctxStruct   = "structure"
	ctxUsages   = "usages"
	ctxHTTP     = "http"
	ctxBreak    = "breakpoints"
	ctxArchive  = "archive"
	ctxData     = "data"
	ctxES       = "es"
	ctxTests    = "tests"
	ctxIssues   = "issues"
	ctxDOM      = "dom"
	ctxDoctor   = "xdoctor"
	ctxRemote   = "remote"
)

// Instance is one live pane: a stable key plus the component it drives. An
// explorer instance wraps the singleton explorer model. An editor instance
// hosts an ordered list of open documents — its tabs (#156) — with exactly one
// active tab; the pane renders and routes input to the active tab only, while
// the layout tree stays a pure split-tree of leaves. Component models are value
// types with pointer-receiver methods, so the Instance is held behind a pointer
// in the registry and its accessors hand out pointers into the tab slots.
type Instance struct {
	key  string
	kind Kind
	exp  explorer.Model
	term terminal.Model
	md   preview.Model
	iv   imgview.Model
	df   diff.Model
	vp   vcspanel.Model
	dp   debugpanel.Model
	pp   problems.Model
	sp   structpanel.Model
	up   usages.Model
	hp   httppane.Model
	bp   breakpanel.Model
	mg   merge.Model
	av   archview.Model
	dv   dataview.Model
	es   espane.Model
	tr   testresults.Model
	gi   ghissues.Model
	dm   domview.Model
	xd   debugdoctor.Model
	rm   remote.Model
	// dfEdit is the diff pane's edit-mode editor (0340, #496): non-nil while
	// the right column is a live editor of the underlying file.
	dfEdit *editor.Model

	// debugTerm marks a terminal instance as the debuggee terminal (#1370):
	// persistence records it separately (the pane is session state and never
	// resurrects as a shell) and runs never reuse it for their own output.
	debugTerm bool

	// Editor state: the ordered tab list and the active index. A tab holds a
	// document editor or an embedded terminal (#573). cfg/pal/size and focus
	// are remembered so tabs created later match the live pane.
	tabs   []*Tab
	active int
	useSeq int // monotonic activation counter stamping tab recency (#742)
	cfg    host.Config
	pal    *theme.Palette
	// regs is the app-wide shared register store threaded into every editor
	// tab (#1540); nil leaves each editor its private store.
	regs    *register.Store
	w, h    int
	focused bool

	// Box render cache (#612): the app hands CachedBox a signature that includes
	// a hash of the freshly-computed content plus the chrome. While the signature
	// is unchanged, the whole bordered box — the expensive lipgloss composition
	// (border, padding, per-line width measurement) — is reused. The content is
	// always recomputed and re-hashed by the caller, so the cache can never go
	// stale: it only skips re-composing an identical box.
	bxSig   BoxSig
	bxBox   string
	bxValid bool

	// View cache (#615): the active editor tab's rendered content, reused while
	// its RenderVersion and the active tab index are unchanged — so a pane the
	// user is not touching skips its View() recomputation entirely.
	cvView  string
	cvVer   uint64
	cvTab   int
	cvValid bool
}

// BoxSig is the render-cache key for a pane's bordered box. Content is captured
// by hash (of the freshly rendered content) rather than by a change flag, so two
// equal signatures render byte-identical.
type BoxSig struct {
	ContentHash uint64
	Title       string
	W, H        int
	Border      [4]uint32 // border color RGBA — comparable without a per-frame Sprintf (#1101)
}

// CachedBox returns the pane's bordered box, running compute only when sig
// differs from the last render. compute must be a pure function of the same
// inputs sig captures.
func (i *Instance) CachedBox(sig BoxSig, compute func() string) string {
	if i.bxValid && i.bxSig == sig {
		return i.bxBox
	}
	i.bxBox = compute()
	i.bxSig = sig
	i.bxValid = true
	return i.bxBox
}

// Key returns the instance's stable identity, the same string used as the
// layout leaf id and the persistence key.
func (i *Instance) Key() string { return i.key }

// Kind reports whether the instance is an explorer or an editor.
func (i *Instance) Kind() Kind { return i.kind }

// ContextID is the context id the instance advertises for command/keymap
// resolution: explorer panes resolve under "explorer", editors under "editor".
func (i *Instance) ContextID() string {
	switch i.kind {
	case KindExplorer:
		return ctxExplorer
	case KindEditor:
		// An editor pane whose active tab is a terminal (#573) resolves
		// under the terminal context, so terminal bindings apply while it
		// owns the keystrokes; a content tab (#1778) resolves under its
		// nested kind's context the same way.
		if t := i.activeTab(); t != nil {
			if t.IsTerminal() {
				return ctxTerminal
			}
			if t.inst != nil {
				return t.inst.ContextID()
			}
		}
		return ctxEditor
	case KindTerminal:
		return ctxTerminal
	case KindMarkdown, KindImage:
		return ctxPreview
	case KindMerge:
		// The result editor owns the keys: resolve under the editor context
		// so the full editor keymap (write, merge accepts, motions) applies.
		return ctxEditor
	case KindDiff:
		return ctxDiff
	case KindVCS:
		return ctxVCS
	case KindDebug:
		return ctxDebug
	case KindProblems:
		return ctxProblems
	case KindStructure:
		return ctxStruct
	case KindUsages:
		return ctxUsages
	case KindHTTP:
		return ctxHTTP
	case KindBreakpoints:
		return ctxBreak
	case KindArchive:
		return ctxArchive
	case KindData:
		return ctxData
	case KindES:
		return ctxES
	case KindTests:
		return ctxTests
	case KindIssues:
		return ctxIssues
	case KindDOM:
		return ctxDOM
	case KindDoctor:
		return ctxDoctor
	case KindRemote:
		return ctxRemote
	}
	return ctxEditor
}

// Explorer returns the underlying explorer model. It is only valid for an
// explorer instance; callers gate on Kind first.
func (i *Instance) Explorer() *explorer.Model { return &i.exp }

// Terminal returns the underlying terminal model. It is only valid for a
// terminal instance; callers gate on Kind first.
func (i *Instance) Terminal() *terminal.Model { return &i.term }

// IsDebugTerm reports whether the terminal instance is the debuggee terminal
// pane (#1370).
func (i *Instance) IsDebugTerm() bool { return i.debugTerm }

// ReplaceTerminal swaps the instance's terminal model for t, closing the
// previous session (#1370): a new debug session reuses the debuggee terminal
// pane's slot, and a runInTerminal debuggee takes over the pipe placeholder.
func (i *Instance) ReplaceTerminal(t terminal.Model) {
	w, h := i.term.Size()
	i.term.Close()
	i.term = t
	i.term.SetPalette(i.pal)
	if w > 0 && h > 0 {
		i.term.SetSize(w, h)
	}
}

// Preview returns the underlying markdown preview model. It is only valid for
// a markdown instance; callers gate on Kind first.
func (i *Instance) Preview() *preview.Model { return &i.md }

// Image returns the wrapped image preview model (image panes only).
func (i *Instance) Image() *imgview.Model { return &i.iv }

// Archive returns the underlying archive viewer model (#1762). It is only
// valid for an archive instance; callers gate on Kind first.
func (i *Instance) Archive() *archview.Model { return &i.av }

// Data returns the underlying data viewer model (#1764). It is only valid
// for a data instance; callers gate on Kind first.
func (i *Instance) Data() *dataview.Model { return &i.dv }

// ES returns the underlying Elasticsearch console model (#1927). It is only
// valid for an es instance; callers gate on Kind first.
func (i *Instance) ES() *espane.Model { return &i.es }

// Tests returns the underlying Test Results tool-window model (#1911). It is
// only valid for a tests instance; callers gate on Kind first.
func (i *Instance) Tests() *testresults.Model { return &i.tr }

// Issues returns the underlying GitHub Issues tool-window model (#1934). It
// is only valid for an issues instance; callers gate on Kind first.
func (i *Instance) Issues() *ghissues.Model { return &i.gi }

// DOM returns the underlying DOM inspector tool-window model (#1929). It is
// only valid for a dom instance; callers gate on Kind first.
func (i *Instance) DOM() *domview.Model { return &i.dm }

// Doctor returns the underlying Xdebug Doctor tool-window model (#1991). It
// is only valid for a doctor instance; callers gate on Kind first.
func (i *Instance) Doctor() *debugdoctor.Model { return &i.xd }

// Remote returns the underlying SFTP remote browser model (#1997). It is
// only valid for a remote instance; callers gate on Kind first.
func (i *Instance) Remote() *remote.Model { return &i.rm }

// Diff returns the underlying diff viewer model. It is only valid for a diff
// instance; callers gate on Kind first.
func (i *Instance) Diff() *diff.Model { return &i.df }

// VCS returns the underlying VCS tool-window model. It is only valid for a
// vcs instance; callers gate on Kind first.
func (i *Instance) VCS() *vcspanel.Model { return &i.vp }

// Debug returns the underlying debug tool-window model. It is only valid for
// a debug instance; callers gate on Kind first.
func (i *Instance) Debug() *debugpanel.Model { return &i.dp }

// Problems returns the underlying Problems tool-window model. It is only
// valid for a problems instance; callers gate on Kind first.
func (i *Instance) Problems() *problems.Model { return &i.pp }

// Structure returns the underlying Structure tool-window model (#1025). It is
// only valid for a structure instance; callers gate on Kind first.
func (i *Instance) Structure() *structpanel.Model { return &i.sp }

// Usages returns the underlying Usages tool-window model (#1155). It is only
// valid for a usages instance; callers gate on Kind first.
func (i *Instance) Usages() *usages.Model { return &i.up }

// Breakpoints returns the underlying Breakpoints tool-window model (#1377).
// It is only valid for a breakpoints instance; callers gate on Kind first.
func (i *Instance) Breakpoints() *breakpanel.Model { return &i.bp }

// HTTP returns the underlying HTTP response viewer model (#1250). It is only
// valid for an http instance; callers gate on Kind first.
func (i *Instance) HTTP() *httppane.Model { return &i.hp }

// Merge returns the underlying merge-view model. It is only valid for a
// merge instance; callers gate on Kind first.
func (i *Instance) Merge() *merge.Model { return &i.mg }

// DiffEditor returns the diff pane's edit-mode editor, nil while browsing.
func (i *Instance) DiffEditor() *editor.Model { return i.dfEdit }

// StartDiffEdit mounts ed as the diff pane's editable right column (#496):
// keys route into it, the left column re-aligns per keystroke.
func (i *Instance) StartDiffEdit(ed *editor.Model) {
	if i.kind != KindDiff || ed == nil {
		return
	}
	i.dfEdit = ed
	i.df.SetEditMode(true)
	i.sizeDiffEditor()
	ed.SetFocused(i.focused)
	i.df.Rediff(ed.Text())
}

// StopDiffEdit returns the pane to read-only browsing; the last buffer state
// stays diffed.
func (i *Instance) StopDiffEdit() {
	if i.dfEdit == nil {
		return
	}
	i.df.Rediff(i.dfEdit.Text())
	i.dfEdit = nil
	i.df.SetEditMode(false)
}

// sizeDiffEditor fits the embedded editor into the split's right column.
func (i *Instance) sizeDiffEditor() {
	if i.dfEdit == nil {
		return
	}
	_, right := i.df.EditSplitWidths()
	i.dfEdit.SetSize(right, i.h)
}

// Editor returns the active tab's editor model. It is only valid for an editor
// instance; callers gate on Kind first. It is nil when the active tab hosts a
// terminal (#573), so callers must nil-check before dereferencing.
func (i *Instance) Editor() *editor.Model {
	if t := i.activeTab(); t != nil {
		return t.Editor()
	}
	return nil
}

// activeTab returns the active tab slot, or nil when no tabs exist.
func (i *Instance) activeTab() *Tab {
	if len(i.tabs) == 0 {
		return nil
	}
	return i.tabs[i.active]
}

// Tab returns the tab slot at idx, or nil when out of range.
func (i *Instance) Tab(idx int) *Tab {
	if idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx]
}

// TabTerminal returns the terminal model of tab idx, nil for editor tabs or
// an out-of-range index.
func (i *Instance) TabTerminal(idx int) *terminal.Model {
	if t := i.Tab(idx); t != nil {
		return t.Terminal()
	}
	return nil
}

// ActiveTerminal returns the terminal the instance's input currently reaches:
// the wrapped terminal for a terminal pane, the active tab's terminal for an
// editor pane hosting one (#573), nil otherwise.
func (i *Instance) ActiveTerminal() *terminal.Model {
	if i.kind == KindTerminal {
		return &i.term
	}
	if i.kind != KindEditor {
		return nil
	}
	if t := i.activeTab(); t != nil {
		return t.Terminal()
	}
	return nil
}

// TabCount reports how many tabs the editor instance holds (0 for explorers).
func (i *Instance) TabCount() int { return len(i.tabs) }

// IsEmptyEditor reports whether this pane is a reusable blank editor: a single
// editor tab that is empty per editor.Model.IsEmpty (no file, no text) — the
// shared emptiness predicate of the file-open and diff-open paths (#628, #641).
// Opening a file or a diff can take over such a pane in place instead of
// splitting a new one. A pathless tab that already holds typed scratch text is
// not reusable — its content would be lost.
func (i *Instance) IsEmptyEditor() bool {
	if i.kind != KindEditor || len(i.tabs) != 1 {
		return false
	}
	ed := i.Editor()
	return ed != nil && ed.IsEmpty()
}

// ActiveTab returns the index of the active tab.
func (i *Instance) ActiveTab() int { return i.active }

// TabEditor returns the editor model of tab idx, nil when out of range or
// when that tab hosts a terminal (#573).
func (i *Instance) TabEditor(idx int) *editor.Model {
	if idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx].Editor()
}

// Editors returns every editor tab's model in tab order; terminal tabs are
// skipped. Callers that iterate "all documents of this pane" (emitters,
// autosave sweeps, backup drops) use this instead of Editor, which only sees
// the active tab.
func (i *Instance) Editors() []*editor.Model {
	out := make([]*editor.Model, 0, len(i.tabs))
	for _, t := range i.tabs {
		if ed := t.Editor(); ed != nil {
			out = append(out, ed)
		}
	}
	return out
}

// TabForPath returns the index of the first tab showing path, or -1.
func (i *Instance) TabForPath(path string) int {
	for idx, t := range i.tabs {
		if ed := t.Editor(); ed != nil && ed.HasFile() && ed.Path() == path {
			return idx
		}
	}
	return -1
}

// EditorForPath returns the first tab's editor model showing path, or nil.
func (i *Instance) EditorForPath(path string) *editor.Model {
	if idx := i.TabForPath(path); idx >= 0 {
		return i.tabs[idx].Editor()
	}
	return nil
}

// AddTab appends a fresh empty tab, makes it active, and returns its editor
// model. The new tab inherits the pane's size, config, palette and focus. Only
// valid on editor instances.
func (i *Instance) AddTab() *editor.Model {
	if i.kind != KindEditor {
		return nil
	}
	ed := newEditorModel(i.cfg, i.pal, i.regs)
	ed.SetSize(i.w, i.h)
	i.tabs = append(i.tabs, newEditorTab(&ed))
	i.activate(len(i.tabs) - 1)
	return i.tabs[i.active].Editor()
}

// AddTerminalTab appends a tab hosting term, makes it active, and returns the
// hosted model (#573): run output opens next to the file tabs. Only valid on
// editor instances.
func (i *Instance) AddTerminalTab(term terminal.Model) *terminal.Model {
	if i.kind != KindEditor {
		return nil
	}
	term.SetPalette(i.pal)
	term.SetSize(i.w, i.h)
	term.SetAutoSuggest(autosuggestOn(i.cfg))
	i.tabs = append(i.tabs, newTerminalTab(&term))
	i.activate(len(i.tabs) - 1)
	return i.tabs[i.active].Terminal()
}

// KindTabbable reports whether kind's content can live in a tab slot and, by
// the same token, whether its pane can convert into a tab host (#1778):
// editors and terminals natively, plus the viewer kinds. The explorer and the
// singleton tool windows (VCS, Debug, Problems, Structure, Usages,
// Breakpoints) keep their fixed toggle-driven roles, and a merge view stays a
// dedicated pane — its conflict workflow is session-bound.
func KindTabbable(k Kind) bool {
	switch k {
	case KindEditor, KindTerminal, KindMarkdown, KindImage, KindDiff, KindArchive, KindData, KindES, KindHTTP:
		return true
	}
	return false
}

// ConvertToTabHost turns a terminal (or tool, #741) or viewer (#1778) pane
// into an editor-kind instance hosting its live content as the only tab
// (#836), so a center drop can merge more tabs into it — the pane kind
// describes the initial content, not the tab capability. A terminal session
// never restarts; viewer content moves without reloading. Reports success;
// editor panes and non-tabbable kinds refuse.
func (i *Instance) ConvertToTabHost() bool {
	if i.kind == KindTerminal {
		t := i.term
		i.term = terminal.Model{}
		if w, h := t.Size(); w > 0 && h > 0 {
			i.w, i.h = w, h
		}
		i.kind = KindEditor
		i.AddTerminalTab(t)
		return true
	}
	nested, ok := i.DetachContent()
	if !ok {
		return false
	}
	i.kind = KindEditor
	i.AddContentTab(nested)
	return true
}

// DetachContent hands the viewer pane's live component out as a nested
// instance carrying the same key (#1778), leaving zero-value models behind so
// a following registry Close releases nothing that moved — the DetachTerminal
// pattern for content kinds. Only valid on tabbable viewer instances.
func (i *Instance) DetachContent() (*Instance, bool) {
	if i.kind == KindEditor || i.kind == KindTerminal || !KindTabbable(i.kind) {
		return nil, false
	}
	nested := &Instance{key: i.key, kind: i.kind, cfg: i.cfg, pal: i.pal, regs: i.regs, w: i.w, h: i.h}
	switch i.kind {
	case KindMarkdown:
		nested.md, i.md = i.md, preview.Model{}
	case KindImage:
		nested.iv, i.iv = i.iv, imgview.Model{}
	case KindDiff:
		nested.df, i.df = i.df, diff.Model{}
		nested.dfEdit, i.dfEdit = i.dfEdit, nil
	case KindArchive:
		nested.av, i.av = i.av, archview.Model{}
	case KindData:
		nested.dv, i.dv = i.dv, dataview.Model{}
	case KindES:
		nested.es, i.es = i.es, espane.Model{}
	case KindHTTP:
		nested.hp, i.hp = i.hp, httppane.Model{}
	default:
		return nil, false
	}
	return nested, true
}

// AddContentTab appends a tab hosting the nested content instance, makes it
// active (#1778), and reports success. The content inherits the pane's size,
// config, palette and focus, like a terminal tab. Only valid on editor
// instances, and only for tabbable viewer content.
func (i *Instance) AddContentTab(nested *Instance) bool {
	if i.kind != KindEditor || nested == nil || nested.kind == KindEditor || nested.kind == KindTerminal || !KindTabbable(nested.kind) {
		return false
	}
	nested.setPalette(i.pal)
	nested.configure(i.cfg)
	if i.w > 0 && i.h > 0 {
		nested.SetSize(i.w, i.h)
	}
	i.tabs = append(i.tabs, newContentTab(nested))
	i.activate(len(i.tabs) - 1)
	return true
}

// ActiveContent returns the nested content instance of the active tab
// (#1778): non-nil only for an editor-kind pane whose active tab carries
// viewer content. The seam mouse/status routing uses to treat the tab's body
// like the equivalent dedicated pane.
func (i *Instance) ActiveContent() *Instance {
	if i.kind != KindEditor {
		return nil
	}
	if t := i.activeTab(); t != nil {
		return t.inst
	}
	return nil
}

// TabContent returns the nested content instance of tab idx (#1778), nil for
// editor/terminal tabs or an out-of-range index.
func (i *Instance) TabContent(idx int) *Instance {
	if t := i.Tab(idx); t != nil {
		return t.inst
	}
	return nil
}

// ContentTitle is the short per-kind label of a viewer instance, used as its
// tab title (#1778) — the basename of what it shows, the diff's right-hand
// title, the HTTP viewer's request key.
func (i *Instance) ContentTitle() string {
	switch i.kind {
	case KindMarkdown:
		if p := i.md.Path(); p != "" {
			return filepath.Base(p)
		}
		return "preview"
	case KindImage:
		if p := i.iv.Path(); p != "" {
			return filepath.Base(p)
		}
		return "image"
	case KindArchive:
		if p := i.av.Path(); p != "" {
			return filepath.Base(p)
		}
		return "archive"
	case KindData:
		if p := i.dv.Path(); p != "" {
			return filepath.Base(p)
		}
		return "data"
	case KindES:
		if ep := i.es.Endpoint(); ep != "" {
			return ep
		}
		return "es"
	case KindDiff:
		_, r := i.df.Titles()
		if r != "" {
			return r
		}
		return "diff"
	case KindHTTP:
		if t := i.hp.Title(); t != "" {
			return t
		}
		return "http"
	}
	return "pane"
}

// releaseContent ends the background resources the instance's component holds
// — a terminal's session, a data viewer's database backend, and every tab's
// content for a tab host. Zero-value models (after a detach) release nothing.
func (i *Instance) releaseContent() {
	switch i.kind {
	case KindTerminal:
		i.term.Close()
	case KindData:
		i.dv.Close()
	case KindES:
		i.es.Close()
	case KindRemote:
		// Ends the SFTP session and its ssh subprocess (#1997).
		i.rm.Close()
	case KindEditor:
		for _, t := range i.tabs {
			t.close()
		}
	}
}

// DetachTerminal hands the live terminal model to the caller and leaves the
// instance with a session-less placeholder, so a following registry Close no
// longer ends the moved shell (#708): a terminal pane dropped on an editor's
// center zone becomes a terminal tab there. Only valid on terminal instances.
func (i *Instance) DetachTerminal() (terminal.Model, bool) {
	if i.kind != KindTerminal {
		return terminal.Model{}, false
	}
	t := i.term
	i.term = terminal.Model{}
	return t, true
}

// AdoptTabsFrom moves every tab of src — documents, terminal sessions,
// nested content, pin flags — to the end of i's tab list (#1778), leaving
// the last moved tab active; the caller closes src right after. A file i
// already shows stays behind (the dedupe the file-only merge used to get via
// openInTab) and closes with src. Valid only between editor instances.
func (i *Instance) AdoptTabsFrom(src *Instance) bool {
	if i.kind != KindEditor || src == nil || src == i || src.kind != KindEditor {
		return false
	}
	var kept []*Tab
	moved := false
	for _, t := range src.tabs {
		if ed := t.Editor(); ed != nil && ed.HasFile() && i.TabForPath(ed.Path()) >= 0 {
			kept = append(kept, t)
			continue
		}
		t.setPalette(i.pal)
		t.configure(i.cfg)
		if i.w > 0 && i.h > 0 {
			t.setSize(i.w, i.h)
		}
		i.tabs = append(i.tabs, t)
		moved = true
	}
	src.tabs = kept
	src.active = 0
	if moved {
		i.activate(len(i.tabs) - 1)
	}
	return true
}

// CloseTerminalTabs ends every terminal tab's session; the tab slots stay (a
// registry Close drops the whole instance right after, a project switch just
// stops the shells the new workspace does not carry over).
func (i *Instance) CloseTerminalTabs() {
	for _, t := range i.tabs {
		t.close()
	}
}

// ActivateTab makes tab idx the active one, moving the pane's focus state onto
// it. It reports whether the index was valid.
func (i *Instance) ActivateTab(idx int) bool {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) {
		return false
	}
	i.activate(idx)
	return true
}

// activate switches the active index and re-asserts per-tab focus flags. The
// activated tab gets the next use-sequence stamp — the recency the tab-limit
// eviction orders by (#742).
func (i *Instance) activate(idx int) {
	i.active = idx
	if idx >= 0 && idx < len(i.tabs) {
		i.useSeq++
		i.tabs[idx].lastUsed = i.useSeq
	}
	for n, t := range i.tabs {
		t.setFocused(i.focused && n == i.active)
	}
}

// TabPinned reports whether tab idx is pinned (#1172); false out of range.
func (i *Instance) TabPinned(idx int) bool {
	if idx < 0 || idx >= len(i.tabs) {
		return false
	}
	return i.tabs[idx].pinned
}

// SetTabPinned marks tab idx pinned or unpinned (#1172): pinned tabs are
// exempt from the tab-limit LRU eviction and from "Close Others"; manual
// closes stay allowed. Out-of-range indexes are a no-op.
func (i *Instance) SetTabPinned(idx int, on bool) {
	if idx < 0 || idx >= len(i.tabs) {
		return
	}
	i.tabs[idx].pinned = on
}

// ToggleTabPin flips tab idx's pin (#1172) and returns the new state; false
// for an out-of-range index.
func (i *Instance) ToggleTabPin(idx int) bool {
	if idx < 0 || idx >= len(i.tabs) {
		return false
	}
	i.tabs[idx].pinned = !i.tabs[idx].pinned
	return i.tabs[idx].pinned
}

// FileTabCount counts the pane's document tabs — terminal tabs (#573) and
// content tabs (#1778) are exempt from the tab limit (#742), like the
// eviction below only ever picks document tabs.
func (i *Instance) FileTabCount() int {
	n := 0
	for _, t := range i.tabs {
		if t.Editor() != nil {
			n++
		}
	}
	return n
}

// EvictableLRUTab returns the least recently used tab the tab limit may close
// (#742): a file-backed, non-dirty document tab that is not active — dirty
// tabs, scratch tabs (nothing to reopen from), terminals and pinned tabs
// (#1172) are exempt. ok=false when no tab is eligible, in which case the
// limit may be exceeded.
func (i *Instance) EvictableLRUTab() (idx int, ok bool) {
	best := -1
	for n, t := range i.tabs {
		if n == i.active || t.IsTerminal() || t.pinned {
			continue
		}
		ed := t.Editor()
		if ed == nil || !ed.HasFile() || ed.Dirty() {
			continue
		}
		if best < 0 || t.lastUsed < i.tabs[best].lastUsed {
			best = n
		}
	}
	return best, best >= 0
}

// MoveTab reorders the tab at from to position to, keeping the same tab active.
// It reports whether both indexes were valid.
func (i *Instance) MoveTab(from, to int) bool {
	if i.kind != KindEditor || from < 0 || from >= len(i.tabs) || to < 0 || to >= len(i.tabs) {
		return false
	}
	if from == to {
		return true
	}
	activeTab := i.tabs[i.active]
	t := i.tabs[from]
	i.tabs = append(i.tabs[:from], i.tabs[from+1:]...)
	rest := append([]*Tab{}, i.tabs[to:]...)
	i.tabs = append(append(i.tabs[:to:to], t), rest...)
	for n, tab := range i.tabs {
		if tab == activeTab {
			i.active = n
			break
		}
	}
	return true
}

// DetachTerminalTab removes tab idx without ending its session and returns
// the hosted terminal model (#707): a dragged terminal tab moves into another
// pane or splits off as its own terminal pane. Valid only on editor instances
// holding more than one tab with a terminal at idx.
func (i *Instance) DetachTerminalTab(idx int) (terminal.Model, bool) {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 || !i.tabs[idx].IsTerminal() {
		return terminal.Model{}, false
	}
	t := *i.tabs[idx].term
	i.tabs = append(i.tabs[:idx], i.tabs[idx+1:]...)
	switch {
	case i.active > idx:
		i.active--
	case i.active == idx && i.active >= len(i.tabs):
		i.active = len(i.tabs) - 1
	}
	i.activate(i.active)
	return t, true
}

// DetachContentTab removes tab idx without releasing its backend and returns
// the nested content instance (#1778): a dragged viewer tab moves into
// another pane or splits off as its own pane again. Valid only on editor
// instances holding more than one tab with content at idx.
func (i *Instance) DetachContentTab(idx int) (*Instance, bool) {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 || i.tabs[idx].inst == nil {
		return nil, false
	}
	nested := i.tabs[idx].inst
	i.tabs = append(i.tabs[:idx], i.tabs[idx+1:]...)
	switch {
	case i.active > idx:
		i.active--
	case i.active == idx && i.active >= len(i.tabs):
		i.active = len(i.tabs) - 1
	}
	i.activate(i.active)
	return nested, true
}

// CloseTab removes tab idx. The neighbour that slides into its position becomes
// active when the active tab itself closes (the last position falls back to its
// left neighbour). Closing the only tab is refused — the caller closes the pane
// instead, so an editor instance never exists with zero tabs.
func (i *Instance) CloseTab(idx int) bool {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 {
		return false
	}
	i.tabs[idx].close() // a terminal tab's session ends with its tab (#573)
	i.tabs = append(i.tabs[:idx], i.tabs[idx+1:]...)
	switch {
	case i.active > idx:
		i.active--
	case i.active == idx && i.active >= len(i.tabs):
		i.active = len(i.tabs) - 1
	}
	i.activate(i.active)
	return true
}

// SetSize pushes an interior content size into the wrapped component. Editor
// instances size every tab, so switching tabs never renders through a stale
// viewport. Every kind records the size, so content moving between pane and
// tab slot (#1778) carries its extent along.
func (i *Instance) SetSize(w, h int) {
	i.w, i.h = w, h
	switch i.kind {
	case KindExplorer:
		i.exp.SetSize(w, h)
	case KindEditor:
		for _, t := range i.tabs {
			t.setSize(w, h)
		}
	case KindTerminal:
		i.term.SetSize(w, h)
	case KindMarkdown:
		i.md.SetSize(w, h)
	case KindImage:
		i.iv.SetSize(w, h)
	case KindDiff:
		i.df.SetSize(w, h)
		i.sizeDiffEditor()
	case KindMerge:
		i.mg.SetSize(w, h)
	case KindVCS:
		i.vp.SetSize(w, h)
	case KindDebug:
		i.dp.SetSize(w, h)
	case KindProblems:
		i.pp.SetSize(w, h)
	case KindStructure:
		i.sp.SetSize(w, h)
	case KindUsages:
		i.up.SetSize(w, h)
	case KindHTTP:
		i.hp.SetSize(w, h)
	case KindBreakpoints:
		i.bp.SetSize(w, h)
	case KindArchive:
		i.av.SetSize(w, h)
	case KindData:
		i.dv.SetSize(w, h)
	case KindES:
		i.es.SetSize(w, h)
	case KindTests:
		i.tr.SetSize(w, h)
	case KindIssues:
		i.gi.SetSize(w, h)
	case KindDOM:
		i.dm.SetSize(w, h)
	case KindDoctor:
		i.xd.SetSize(w, h)
	case KindRemote:
		i.rm.SetSize(w, h)
	}
}

// SetFocused marks the wrapped component focused or blurred. For editors only
// the active tab ever carries focus.
func (i *Instance) SetFocused(f bool) {
	switch i.kind {
	case KindExplorer:
		i.exp.SetFocused(f)
	case KindEditor:
		i.focused = f
		for n, t := range i.tabs {
			t.setFocused(f && n == i.active)
		}
	case KindTerminal:
		i.term.SetFocused(f)
	case KindMarkdown:
		i.md.SetFocused(f)
	case KindImage:
		i.iv.SetFocused(f)
	case KindDiff:
		i.df.SetFocused(f)
		if i.dfEdit != nil {
			i.dfEdit.SetFocused(f)
		}
	case KindMerge:
		i.focused = f
		i.mg.SetFocused(f)
	case KindVCS:
		i.vp.SetFocused(f)
	case KindDebug:
		i.dp.SetFocused(f)
	case KindProblems:
		i.pp.SetFocused(f)
	case KindStructure:
		i.sp.SetFocused(f)
	case KindUsages:
		i.up.SetFocused(f)
	case KindHTTP:
		i.hp.SetFocused(f)
	case KindBreakpoints:
		i.bp.SetFocused(f)
	case KindArchive:
		i.av.SetFocused(f)
	case KindData:
		i.dv.SetFocused(f)
	case KindES:
		i.es.SetFocused(f)
	case KindTests:
		i.tr.SetFocused(f)
	case KindIssues:
		i.gi.SetFocused(f)
	case KindDOM:
		i.dm.SetFocused(f)
	case KindDoctor:
		i.xd.SetFocused(f)
	case KindRemote:
		i.rm.SetFocused(f)
	}
}

// View renders the wrapped component's content (without pane chrome).
func (i *Instance) View() string {
	switch i.kind {
	case KindExplorer:
		return i.exp.View()
	case KindEditor:
		t := i.activeTab()
		if t == nil {
			return ""
		}
		if t.IsTerminal() || t.inst != nil {
			// Live terminal output and nested viewer content (#1778) — never
			// cached here; the app-level box cache still applies.
			return t.view()
		}
		// Skip recomputing the editor's View when nothing it renders changed
		// (#615): a scroll of another pane, or an idle frame, reuses the cached
		// string. RenderVersion is a complete identity of everything View draws,
		// so this can never serve a stale frame.
		ver := t.ed.RenderVersion()
		if i.cvValid && i.cvTab == i.active && i.cvVer == ver {
			return i.cvView
		}
		v := t.view()
		i.cvView, i.cvVer, i.cvTab, i.cvValid = v, ver, i.active, true
		return v
	case KindTerminal:
		return i.term.View()
	case KindMarkdown:
		return i.md.View()
	case KindImage:
		return i.iv.View()
	case KindDiff:
		if i.dfEdit != nil {
			lines := strings.Split(i.dfEdit.View(), "\n")
			return i.df.RenderEditSplit(lines, i.dfEdit.ScrollTop(), i.h)
		}
		return i.df.View()
	case KindMerge:
		return i.mg.View()
	case KindVCS:
		return i.vp.View()
	case KindDebug:
		return i.dp.View()
	case KindProblems:
		return i.pp.View()
	case KindStructure:
		return i.sp.View()
	case KindUsages:
		return i.up.View()
	case KindHTTP:
		return i.hp.View()
	case KindBreakpoints:
		return i.bp.View()
	case KindArchive:
		return i.av.View()
	case KindData:
		return i.dv.View()
	case KindES:
		return i.es.View()
	case KindTests:
		return i.tr.View()
	case KindIssues:
		return i.gi.View()
	case KindDOM:
		return i.dm.View()
	case KindDoctor:
		return i.xd.View()
	case KindRemote:
		return i.rm.View()
	}
	return ""
}

// Update dispatches a message to the wrapped component — for editors, to the
// active tab — mutating it in place and returning any resulting command.
func (i *Instance) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch i.kind {
	case KindExplorer:
		i.exp, cmd = i.exp.Update(msg)
	case KindEditor:
		cmd = i.tabs[i.active].update(msg)
	case KindTerminal:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			cmd = i.term.Update(k)
		}
	case KindMarkdown:
		cmd = i.md.Update(msg)
	case KindDiff:
		if i.dfEdit != nil {
			// Edit mode (#496): keys belong to the embedded editor; ctrl+e
			// returns to read-only browsing. The left column re-aligns from
			// a re-diff after every message.
			if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "ctrl+e" {
				i.StopDiffEdit()
				return nil
			}
			ed := i.dfEdit
			*ed, cmd = ed.Update(msg)
			i.df.Rediff(ed.Text())
			return cmd
		}
		cmd = i.df.Update(msg)
	case KindMerge:
		cmd = i.mg.Update(msg)
	case KindVCS:
		cmd = i.vp.Update(msg)
	case KindDebug:
		cmd = i.dp.Update(msg)
	case KindProblems:
		cmd = i.pp.Update(msg)
	case KindStructure:
		cmd = i.sp.Update(msg)
	case KindUsages:
		cmd = i.up.Update(msg)
	case KindHTTP:
		cmd = i.hp.Update(msg)
	case KindBreakpoints:
		cmd = i.bp.Update(msg)
	case KindArchive:
		cmd = i.av.Update(msg)
	case KindData:
		cmd = i.dv.Update(msg)
	case KindES:
		cmd = i.es.Update(msg)
	case KindTests:
		cmd = i.tr.Update(msg)
	case KindIssues:
		cmd = i.gi.Update(msg)
	case KindDOM:
		cmd = i.dm.Update(msg)
	case KindDoctor:
		cmd = i.xd.Update(msg)
	case KindRemote:
		cmd = i.rm.Update(msg)
	}
	return cmd
}

// UpdateForPath dispatches a message to every tab showing path except skip,
// batching the resulting commands. Background tabs share documents too (#142),
// so path-routed messages (sync, highlight, LSP results) must reach the tabs
// the active-tab Update never sees.
func (i *Instance) UpdateForPath(path string, skip *editor.Model, msg tea.Msg) tea.Cmd {
	if i.kind != KindEditor {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range i.tabs {
		ed := t.Editor()
		if ed == nil || ed == skip || !ed.HasFile() || ed.Path() != path {
			continue
		}
		if cmd := t.update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// UpdateTab dispatches a message to tab idx regardless of which tab is active.
func (i *Instance) UpdateTab(idx int, msg tea.Msg) tea.Cmd {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx].update(msg)
}

// Init returns the wrapped component's initialisation command.
func (i *Instance) Init() tea.Cmd {
	switch i.kind {
	case KindExplorer:
		return i.exp.Init()
	case KindEditor:
		if ed := i.Editor(); ed != nil {
			return ed.Init()
		}
	case KindData:
		// The data viewer opens its database in the background (#1795); Init
		// is the one-shot that starts it, whether the pane was just created or
		// restored with the layout.
		return i.dv.Init()
	case KindES:
		// The ES console connects in the background the same way (#1927).
		return i.es.Init()
	case KindRemote:
		// The remote browser dials its host in the background (#1997).
		return i.rm.Init()
	}
	return nil
}

// newInstance builds an instance of the given kind, configuring it against cfg.
// The explorer is rooted at the working directory; editors start with a single
// tab holding an empty scratch buffer.
func newInstance(key string, kind Kind, cfg host.Config, pal *theme.Palette, regs *register.Store) *Instance {
	i := &Instance{key: key, kind: kind, cfg: cfg, pal: pal, regs: regs}
	switch kind {
	case KindExplorer:
		i.exp = explorer.New(".")
		i.exp.SetPalette(pal)
		i.exp.Configure(cfg)
		// The Scratches section (#1963): the store lists below the tree. A
		// dir resolution error just leaves the poll stamp off; the lister
		// reports the same error in the section body.
		dir, _ := scratch.Dir()
		i.exp.EnableScratches(dir, scratch.Entries)
	case KindEditor:
		ed := newEditorModel(cfg, pal, regs)
		i.tabs = []*Tab{newEditorTab(&ed)}
	}
	return i
}

// NewDetachedTerminalHost builds a tab-host instance that lives outside every
// registry and outside the layout tree (#1398): the popup terminal owns it
// directly. It starts as an editor-kind tab host whose only tab is term, so
// the popup gets the full pane-tab machinery without a layout leaf.
func NewDetachedTerminalHost(key string, term terminal.Model, cfg host.Config, pal *theme.Palette) *Instance {
	i := &Instance{key: key, kind: KindTerminal, cfg: cfg, pal: pal, term: term}
	i.ConvertToTabHost()
	return i
}

// SetPalette re-threads the active theme palette into the wrapped components.
// Registry-held instances get this via Registry.SetPalette; detached instances
// (the popup terminal host) need the direct seam.
func (i *Instance) SetPalette(p *theme.Palette) { i.setPalette(p) }

// newEditorModel constructs one tab's editor model configured against cfg.
// regs is the app-wide shared register store (#1540); it is installed before
// Configure/SetClipboard so both apply to the shared store, and nil keeps the
// editor's private store.
func newEditorModel(cfg host.Config, pal *theme.Palette, regs *register.Store) editor.Model {
	ed := editor.New()
	ed.SetRegisters(regs)
	ed.SetPalette(pal)
	ed.Configure(cfg)
	if c := clipboard.System(); c != nil {
		ed.SetClipboard(c)
	}
	return ed
}

// setPalette re-threads the active theme palette into the wrapped component.
func (i *Instance) setPalette(p *theme.Palette) {
	i.pal = p
	switch i.kind {
	case KindExplorer:
		i.exp.SetPalette(p)
	case KindEditor:
		for _, t := range i.tabs {
			t.setPalette(p)
		}
	case KindMarkdown:
		i.md.SetPalette(p)
	case KindImage:
		i.iv.SetPalette(p)
	case KindDiff:
		i.df.SetPalette(p)
	case KindMerge:
		i.mg.SetPalette(p)
	case KindVCS:
		i.vp.SetPalette(p)
	case KindDebug:
		i.dp.SetPalette(p)
	case KindProblems:
		i.pp.SetPalette(p)
	case KindStructure:
		i.sp.SetPalette(p)
	case KindUsages:
		i.up.SetPalette(p)
	case KindHTTP:
		i.hp.SetPalette(p)
	case KindBreakpoints:
		i.bp.SetPalette(p)
	case KindArchive:
		i.av.SetPalette(p)
	case KindData:
		i.dv.SetPalette(p)
	case KindES:
		i.es.SetPalette(p)
	case KindTests:
		i.tr.SetPalette(p)
	case KindIssues:
		i.gi.SetPalette(p)
	case KindDOM:
		i.dm.SetPalette(p)
	case KindDoctor:
		i.xd.SetPalette(p)
	case KindRemote:
		i.rm.SetPalette(p)
	}
}

// configure re-applies configuration to the wrapped component.
func (i *Instance) configure(cfg host.Config) {
	i.cfg = cfg
	switch i.kind {
	case KindExplorer:
		i.exp.Configure(cfg)
	case KindEditor:
		for _, t := range i.tabs {
			t.configure(cfg)
		}
	case KindTerminal:
		i.term.SetAutoSuggest(autosuggestOn(cfg))
		i.term.SetScrollbackLines(scrollbackLines(cfg))
	case KindMerge:
		i.mg.Editor().Configure(cfg)
	}
}

// autosuggestOn reads terminal.autosuggest ("true" unless explicitly off);
// the completion popup's while-typing trigger (#740).
func autosuggestOn(cfg host.Config) bool {
	if cfg == nil {
		return true
	}
	v, ok := cfg.Get("terminal.autosuggest")
	return !ok || v != "false"
}

// scrollbackLines reads terminal.scrollback_lines (#1545); 0 when absent or
// malformed, which the terminal setters treat as "leave unchanged".
func scrollbackLines(cfg host.Config) int {
	if cfg == nil {
		return 0
	}
	v, ok := cfg.Get("terminal.scrollback_lines")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
