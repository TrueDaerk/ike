package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/palette"
	"ike/internal/pathcomplete"
)

// picker.go is the project picker behind project.switch (Roadmap 0090, #12):
// a thin adapter producing palette items from the recent-projects history —
// newest first — plus a direct path-entry affordance. All fuzzy/overlay
// behaviour stays in the palette component (Roadmap 0070); this mode only
// ranks entries and names the msg the selection dispatches.

// PickedMsg is emitted when a picker item is activated: Path is the chosen
// project root — an existing history entry's absolute path, or the raw typed
// path (unvalidated; the switch orchestration (#3) validates before acting).
type PickedMsg struct{ Path string }

// PickerPrefix selects the picker mode inside the palette. The root model
// opens the palette locked to it, so the rune has no user-facing prefix story.
const PickerPrefix = '#'

// PeekPickerPrefix selects the picker's peek flavour (#2136): the same list,
// but plain activation peeks instead of switching. Locked-open only, like
// PickerPrefix.
const PeekPickerPrefix = '_'

// PickerMode is the palette Mode listing recent projects. history is
// injectable for tests; by default it reads the process-wide config.
type PickerMode struct {
	history func() []Entry
	// open reports whether a history entry's workspace is currently loaded
	// in memory (#820); such entries carry the "●" badge and a close aux
	// action. Nil marks nothing.
	open func(path string) bool
	// now overrides the clock for the last-opened badge (#842); tests only.
	now func() time.Time
	// projectsDir overrides ProjectsDir(), the base relative path browsing
	// and completion resolve against (#1808); tests only.
	projectsDir func() (string, error)
	// peek flips the mode into its peek flavour (#2136): plain activation
	// emits PeekPickedMsg and alt+enter falls back to the normal switch —
	// the exact inverse of the default mode's primary/alternate pair.
	peek bool
	// git holds the asynchronously probed branch/dirty context per project
	// root (#2178), shared with the peek flavour. Nil — or a root not probed
	// yet — simply renders the row as it always was.
	git *GitCache
}

// SetGitCache installs the shared branch/dirty cache (#2178); the app injects
// one cache into both picker flavours and fills it from GitInfoMsg.
func (m *PickerMode) SetGitCache(c *GitCache) { m.git = c }

// SetOpen installs the in-memory check (#820); the app injects the workspace
// manager's Peek so the picker package stays workspace-agnostic.
func (m *PickerMode) SetOpen(open func(path string) bool) { m.open = open }

// clock returns the injectable now source, defaulting to the wall clock.
func (m *PickerMode) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// NewPickerMode builds the picker mode. A nil history reads the
// recent-projects list from the live config on every open.
func NewPickerMode(history func() []Entry) *PickerMode {
	if history == nil {
		history = func() []Entry { return History(config.Get()) }
	}
	return &PickerMode{history: history, projectsDir: ProjectsDir}
}

// NewPeekPickerMode builds the picker's peek flavour (#2136) behind
// project.peek: identical list and completion, inverted activation.
func NewPeekPickerMode(history func() []Entry) *PickerMode {
	m := NewPickerMode(history)
	m.peek = true
	return m
}

// activation returns the primary and alternate msgs for path, swapped in the
// peek flavour (#2136): the default mode switches on enter and peeks on
// alt+enter, the peek mode the other way round.
func (m *PickerMode) activation(path string) (msg, alt any) {
	if m.peek {
		return PeekPickedMsg{Path: path}, PickedMsg{Path: path}
	}
	return PickedMsg{Path: path}, PeekPickedMsg{Path: path}
}

// projectsBase resolves the configured projects directory that relative path
// input browses and completes against (#1808), falling back to "" — the
// process working directory, the pre-#1808 behavior — when it cannot be
// resolved (e.g. the home directory is unknown).
func (m *PickerMode) projectsBase() string {
	dir, err := m.projectsDir()
	if err != nil {
		return ""
	}
	return dir
}

// currentRoot resolves the project the picker was opened in (#2317). The root
// model always opens the picker with Root "." — the whole IDE is anchored at
// the process working directory — so an Abs of the context root names the
// currently open project. It returns "" when the root cannot be resolved, in
// which case nothing is filtered out.
func (m *PickerMode) currentRoot(cx palette.Context) string {
	root := cx.Root
	if root == "" {
		root = "."
	}
	resolved, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return filepath.Clean(resolved)
}

// Prefix implements palette.Mode.
func (m *PickerMode) Prefix() rune {
	if m.peek {
		return PeekPickerPrefix
	}
	return PickerPrefix
}

// Placeholder implements palette.Mode.
func (m *PickerMode) Placeholder() string {
	if m.peek {
		return "Peek project — open temporarily, one key back…"
	}
	return "Switch project — recent name or path…"
}

// Results implements palette.Mode: history entries fuzzy-matched on display
// name and path (an empty query lists all, newest first), followed by an
// "open this path" item for the raw query so a project outside the history is
// always reachable.
//
// The currently open project is dropped from the list (#2317), the way the
// recent-files mode drops the active file: the history's newest entry is
// always the project you are standing in, so listing it would put a row that
// only answers "already in …" on top. Without it the first row is the
// *previous* project, and the switch chord plus enter bounces between the two
// projects you alternate between.
//
// Each of the first nine remaining entries carries its MRU digit as the row's
// Hint (#2489): the `ctrl+alt+N` chord that switches there without the picker
// at all. The digit is the entry's rank in the history, not its row number,
// so a typed query re-sorting the list never renumbers a project.
func (m *PickerMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		entry Entry
		score int
		spans []int
		hint  string
	}
	var out []scored
	cur := m.currentRoot(cx)
	// The MRU digit (#2489) is the entry's rank in the *unfiltered* list, not
	// its row number: ctrl+alt+4 always means the same project, whatever the
	// query sorted to the top.
	rank := 0
	for _, e := range m.history() {
		if cur != "" && filepath.Clean(e.Path) == cur {
			continue
		}
		hint := MRUHint(rank)
		rank++
		if r, ok := fuzzy.Match(query, e.Name); ok {
			out = append(out, scored{entry: e, score: r.Score, spans: r.Positions, hint: hint})
			continue
		}
		// Fall back to the path so "code/ike" style queries hit too; spans
		// index the name (the rendered title), so a path match highlights nothing.
		if r, ok := fuzzy.Match(query, e.Path); ok {
			out = append(out, scored{entry: e, score: r.Score, hint: hint})
		}
	}
	// Stable on score only: equal scores keep the history's newest-first order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })

	items := make([]palette.Item, 0, len(out)+1)
	now := m.clock()
	for _, s := range out {
		msg, alt := m.activation(s.entry.Path)
		it := palette.Item{
			Title:  s.entry.Name,
			Detail: CompactPath(s.entry.Path),
			Spans:  s.spans,
			Score:  s.score,
			Msg:    msg,
			// The other activation flavour rides on alt+enter (#2136): peek
			// from the switch picker, full switch from the peek picker.
			Alt: alt,
			// The last-opened time (#842), right-aligned in its own column
			// since #1114; "" for legacy entries without a timestamp.
			Time: RelTime(s.entry.LastOpened, now),
			// The project's MRU digit (#2489): the ctrl+alt+N chord that
			// switches straight here, so the numbers are learned from the
			// list one already looks at. "" past the ninth entry.
			Hint: s.hint,
		}
		if m.open != nil && m.open(s.entry.Path) {
			// Loaded in memory (#820): dot badge + close-in-place aux action,
			// marked with its own glyph (#1418) to tell it apart from removal.
			it.Badge = "●"
			it.Aux = CloseWorkspaceMsg{Path: s.entry.Path}
			it.AuxGlyph = CloseAuxGlyph
		} else {
			// Unloaded entries prune from the history instead (#842).
			it.Aux = RemoveFromHistoryMsg{Path: s.entry.Path}
		}
		// The git context (#2178) shares the badge column with the dot:
		// "● ⎇ main*". It is empty until the row's probe answers, so the
		// list is complete the moment the picker opens and fills in after.
		it.Badge = joinBadge(it.Badge, m.gitBadge(s.entry.Path))
		items = append(items, it)
	}
	if q := strings.TrimSpace(query); q != "" {
		// Any non-empty query also browses the filesystem (#542, #1808):
		// matching directories become selectable items ahead of the raw
		// fallback, ahead of the history fuzzy matches above. Absolute and
		// ~-prefixed input resolves as typed; anything else — "foo",
		// "./foo", "foo/bar" — resolves against the configured projects
		// directory rather than the process working directory.
		base := m.projectsBase()
		for _, c := range pathcomplete.DirsFrom(base, q).Candidates {
			msg, alt := m.activation(resolveAgainstBase(base, c))
			items = append(items, palette.Item{
				Title: "Open " + c,
				Msg:   msg,
				Alt:   alt,
			})
		}
		msg, alt := m.activation(resolveAgainstBase(base, q))
		items = append(items, palette.Item{
			Title: "Open \"" + q + "\"…",
			Msg:   msg,
			Alt:   alt,
		})
	}
	return items
}

// gitBadge is the row's branch/dirty suffix (#2178): "" while the probe is
// still in flight and for every project it could not answer for.
func (m *PickerMode) gitBadge(path string) string {
	info, ok := m.git.Get(path)
	if !ok {
		return ""
	}
	return info.Badge()
}

// joinBadge merges the in-memory dot (#820) and the git badge (#2178) into
// the single Badge column, one space apart; either half may be empty.
func joinBadge(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// Complete implements palette.Completer (#542): tab extends the query to the
// longest unambiguous directory prefix, resolved against the configured
// projects directory for relative input (#1808); anything unmatched is
// inert.
func (m *PickerMode) Complete(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return query
	}
	return pathcomplete.DirsFrom(m.projectsBase(), q).Completed
}

// resolveAgainstBase turns typed path input — in pathcomplete's own notation,
// e.g. a leading "~" kept verbatim — into the absolute filesystem path it
// names: unchanged for absolute or ~-prefixed input, joined against base
// otherwise (#1808). base "" (the projects directory could not be resolved)
// falls back to the pre-#1808 behavior of leaving relative input as typed, so
// it resolves later against the process working directory instead.
func resolveAgainstBase(base, p string) string {
	real := pathcomplete.Expand(p)
	if !filepath.IsAbs(real) {
		real = filepath.Join(base, real)
	}
	return real
}

// maxDetailWidth caps the rendered path chip: the palette row pins Detail to
// the right untruncated, so an over-long path would crowd out the title.
const maxDetailWidth = 40

// CompactPath renders path for constrained chrome — the picker's detail chip
// and the unsaved-changes prompt: the home prefix collapses to "~", and an
// over-long remainder keeps its head and tail around a "…" so the project's
// location stays recognisable. Bounding the width matters beyond looks: the
// floating shell drops a box wider than the terminal outright.
func CompactPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~" + string(filepath.Separator) + rel
		} else if path == home {
			path = "~"
		}
	}
	r := []rune(path)
	if len(r) <= maxDetailWidth {
		return path
	}
	keep := (maxDetailWidth - 1) / 2
	return string(r[:keep]) + "…" + string(r[len(r)-keep:])
}
