package agenttrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// diffs.go reconstructs what every writing tool call did to its file
// (#2859), from the transcript alone: a change box or file node must show
// its diff even when IKE's change feed never saw the write (a session opened
// later, an ambiguous write). The sources, best first:
//
//   - the structuredPatch hunks of the call's result, with the result's
//     originalFile as the whole content before (DiffPatch);
//   - the input's old_string → new_string, placed in the file as the session
//     last saw it — a Read's output or an earlier change's content — else in
//     the file on disk when it still contains the new text, else as a bare
//     hunk without context (DiffStrings);
//   - for a Write the input's content against the session's earlier content
//     of the path (DiffSession), a create's empty file (DiffCreate), else the
//     content alone (DiffAfterOnly).
//
// The change feed's exact before/after (DiffFeed) is the host's to add: the
// package stays free of the feed. Provenance travels with every diff so the
// UI can say how exact it is.

// DiffSource is where a ChangeDiff came from.
type DiffSource int

const (
	// DiffNone: nothing could be reconstructed.
	DiffNone DiffSource = iota
	// DiffFeed: the change feed's captured before and after — exact. Only
	// the host sets it.
	DiffFeed
	// DiffPatch: the hunks the harness recorded in the result.
	DiffPatch
	// DiffStrings: the input's old_string → new_string.
	DiffStrings
	// DiffSession: a Write's content against the content the session saw
	// earlier (a Read, an earlier Write or edit).
	DiffSession
	// DiffCreate: a Write that created the file.
	DiffCreate
	// DiffAfterOnly: a Write whose previous content is unknown.
	DiffAfterOnly
)

// String is the provenance label the diff view's header shows.
func (d DiffSource) String() string {
	switch d {
	case DiffFeed:
		return "exact (change feed)"
	case DiffPatch:
		return "from structuredPatch"
	case DiffStrings:
		return "from old/new string"
	case DiffSession:
		return "from earlier session content"
	case DiffCreate:
		return "new file"
	case DiffAfterOnly:
		return "after only"
	}
	return "unknown"
}

// DiffHunk is one unified hunk. Lines carry the " ", "-" or "+" prefix.
// OldStart and NewStart are 1-based; 0 means the position is unknown (an
// old/new string pair with nothing to place it in).
type DiffHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// Header is the hunk's "@@ -a,b +c,d @@" line.
func (h DiffHunk) Header() string {
	if h.OldStart == 0 && h.NewStart == 0 {
		return "@@ position unknown @@"
	}
	return "@@ -" + strconv.Itoa(h.OldStart) + "," + strconv.Itoa(h.OldLines) +
		" +" + strconv.Itoa(h.NewStart) + "," + strconv.Itoa(h.NewLines) + " @@"
}

// ChangeDiff is what one tool call did to one file.
type ChangeDiff struct {
	// Key is the node key of the change: the call's first file node of the
	// path ("e<i>/f<n>", a subagent's "e<i>/a<j>/f<n>") — the graph's change
	// stop key. Keys holds every node key showing the change: the call's
	// file nodes of the path, and the call itself when it touched one file.
	Key  string
	Keys []string
	Path string
	Op   Op
	// Tool names the call (Edit, MultiEdit, Write).
	Tool string
	Turn int
	At   time.Time
	// Source is the provenance; Note says what is approximate about it
	// ("context from the file on disk", …), "" when nothing is.
	Source DiffSource
	Note   string
	// Before and After are the whole file before and after the call, when
	// known (HasBefore / HasAfter).
	Before    string
	HasBefore bool
	After     string
	HasAfter  bool
	// Hunks is the change as unified hunks; nil when only whole contents
	// are known and no hunks were recorded (the host diffs them).
	Hunks []DiffHunk
	// Added and Removed count the changed lines when Counted.
	Added, Removed int
	Counted        bool
}

// Unified renders the hunks as unified diff text: each hunk's header, then
// its lines.
func (d ChangeDiff) Unified() string {
	var lines []string
	for _, h := range d.Hunks {
		lines = append(lines, h.Header())
		lines = append(lines, h.Lines...)
	}
	return strings.Join(lines, "\n")
}

// Diffs reconstructs the diff of every writing call of the session and its
// subagents, in timeline order. An old/new string pair the session never
// read the file for is placed in the file on disk (relative paths against
// the session's working directory).
func Diffs(s *Session) []ChangeDiff { return DiffsWith(s, os.ReadFile) }

// DiffsWith is Diffs with the disk read injected; nil disables it.
func DiffsWith(s *Session, readFile func(string) ([]byte, error)) []ChangeDiff {
	if s == nil {
		return nil
	}
	d := differ{cwd: s.CWD, read: readFile, full: true, known: map[string]func() (snapshot, bool){}}
	d.walk(s.Events, "e", -1)
	return d.out
}

// DiffFor returns the diff a node key shows.
func DiffFor(diffs []ChangeDiff, key string) (ChangeDiff, bool) {
	for _, d := range diffs {
		for _, k := range d.Keys {
			if k == key {
				return d, true
			}
		}
	}
	return ChangeDiff{}, false
}

// diffCounts is the cheap pass BuildTree and BuildPath take on every read:
// the counts by node key, without the disk and without assembling whole
// contents no count needs.
func diffCounts(s *Session) map[string]ChangeDiff {
	d := differ{cwd: s.CWD, known: map[string]func() (snapshot, bool){}}
	d.walk(s.Events, "e", -1)
	out := make(map[string]ChangeDiff, len(d.out))
	for _, cd := range d.out {
		if cd.Counted {
			out[cd.Key] = cd
		}
	}
	return out
}

// snapshot is the content of a file as the session last saw it: text whose
// first line is line first of the file; whole when it is all of it.
type snapshot struct {
	text  string
	first int
	whole bool
}

// lazy memoizes a snapshot: most are never needed, and assembling one
// (applying a patch, parsing a Read's output) costs a file's worth.
func lazy(f func() (snapshot, bool)) func() (snapshot, bool) {
	var (
		done bool
		s    snapshot
		ok   bool
	)
	return func() (snapshot, bool) {
		if !done {
			s, ok = f()
			done = true
		}
		return s, ok
	}
}

// differ is one reconstruction pass.
type differ struct {
	cwd  string
	read func(string) ([]byte, error)
	// full assembles whole contents and hunks for display; off, only what
	// the counts need.
	full bool
	// known is the session's view of each file so far, by clean path.
	known map[string]func() (snapshot, bool)
	out   []ChangeDiff
}

// walk visits a timeline; a subagent's calls are taken at their spawning
// call, where the tree nests them. turn overrides the events' own turn for
// a subagent (-1: keep).
func (d *differ) walk(evs []Event, prefix string, turn int) {
	for i := range evs {
		ev := &evs[i]
		t := ev.Tool
		if ev.Kind != KindTool || t == nil {
			continue
		}
		key := prefix + strconv.Itoa(i)
		tn := ev.Turn
		if turn >= 0 {
			tn = turn
		}
		d.tool(ev, key, tn)
		if sub := t.Subagent; sub != nil && sub.Session != nil {
			d.walk(sub.Session.Events, key+"/a", tn)
		}
	}
}

func (d *differ) clean(p string) string {
	if !filepath.IsAbs(p) && d.cwd != "" {
		p = filepath.Join(d.cwd, p)
	}
	return filepath.Clean(p)
}

// fileInput is the input of the file-changing tools.
type fileInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
	Content    string `json:"content"`
	Edits      []struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	} `json:"edits"`
}

// edit is one old → new replacement.
type edit struct {
	old, new string
	all      bool
}

func (in fileInput) edits() []edit {
	if len(in.Edits) == 0 {
		return []edit{{in.OldString, in.NewString, in.ReplaceAll}}
	}
	out := make([]edit, len(in.Edits))
	for i, e := range in.Edits {
		out[i] = edit{e.OldString, e.NewString, e.ReplaceAll}
	}
	return out
}

// resultHead is the cheap part of a file-changing call's result.
type resultHead struct {
	Type            string     `json:"type"`
	StructuredPatch []DiffHunk `json:"structuredPatch"`
}

// resultBody is the content part, decoded only when needed.
type resultBody struct {
	OriginalFile         *string `json:"originalFile"`
	OriginalFileContents *string `json:"originalFileContents"`
}

func (d *differ) tool(ev *Event, key string, turn int) {
	t := ev.Tool
	if len(t.Paths) == 0 {
		return
	}
	path := d.clean(t.Paths[0].Path)
	switch t.Name {
	case "Read":
		if t.Done && !t.IsError {
			d.known[path] = lazy(func() (snapshot, bool) { return readSnapshot(t) })
		}
		return
	case "Edit", "MultiEdit", "Write":
	default:
		return
	}
	if t.IsError {
		// Rejected or failed: nothing changed, and the session's view of the
		// file stays what it was.
		return
	}
	cd := ChangeDiff{Key: key + "/f0", Path: t.Paths[0].Path, Op: t.Paths[0].Op, Tool: t.Name, Turn: turn, At: ev.At}
	for j := range t.Paths {
		cd.Keys = append(cd.Keys, key+"/f"+strconv.Itoa(j))
	}
	if len(t.Paths) == 1 {
		cd.Keys = append(cd.Keys, key)
	}
	var in fileInput
	_ = json.Unmarshal(t.Input, &in)
	var head resultHead
	if len(t.Result) > 0 && t.Result[0] == '{' {
		_ = json.Unmarshal(t.Result, &head)
	}
	body := lazyBody(t.Result)
	if t.Name == "Write" {
		d.write(&cd, path, in, head, body)
	} else {
		d.edit(&cd, path, in, head, body)
	}
	d.out = append(d.out, cd)
}

// lazyBody decodes the result's original content once, on demand.
func lazyBody(raw json.RawMessage) func() (string, bool) {
	var (
		done bool
		text string
		ok   bool
	)
	return func() (string, bool) {
		if done {
			return text, ok
		}
		done = true
		if len(raw) == 0 || raw[0] != '{' {
			return "", false
		}
		var b resultBody
		if json.Unmarshal(raw, &b) != nil {
			return "", false
		}
		switch {
		case b.OriginalFile != nil:
			text, ok = *b.OriginalFile, true
		case b.OriginalFileContents != nil:
			text, ok = *b.OriginalFileContents, true
		}
		return text, ok
	}
}

func (cd *ChangeDiff) count() {
	cd.Added, cd.Removed = 0, 0
	for _, h := range cd.Hunks {
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "+"):
				cd.Added++
			case strings.HasPrefix(l, "-"):
				cd.Removed++
			}
		}
	}
	cd.Counted = true
}

// edit reconstructs an Edit or MultiEdit.
func (d *differ) edit(cd *ChangeDiff, path string, in fileInput, head resultHead, body func() (string, bool)) {
	edits := in.edits()
	prev := d.known[path]
	if len(head.StructuredPatch) > 0 {
		cd.Source = DiffPatch
		cd.Hunks = head.StructuredPatch
		cd.count()
		after := lazy(func() (snapshot, bool) {
			orig, ok := body()
			if !ok {
				return snapshot{}, false
			}
			if text, ok := applyPatch(orig, head.StructuredPatch); ok {
				return snapshot{text, 1, true}, true
			}
			if text, ok := applyEdits(orig, edits); ok {
				return snapshot{text, 1, true}, true
			}
			return snapshot{}, false
		})
		if d.full {
			cd.Before, cd.HasBefore = body()
			if s, ok := after(); ok {
				cd.After, cd.HasAfter = s.text, true
			}
		}
		d.known[path] = after
		return
	}
	cd.Source = DiffStrings
	if prev != nil {
		if s, ok := prev(); ok {
			if text, ok := applyEdits(s.text, edits); ok {
				cd.Hunks = lineHunks(splitLines(s.text), splitLines(text), s.first, s.first)
				cd.count()
				cd.Note = "context from the file as the session last saw it"
				if s.whole {
					cd.Before, cd.HasBefore = s.text, true
					cd.After, cd.HasAfter = text, true
				} else {
					cd.Note = "context from the part of the file the session read"
				}
				next := snapshot{text, s.first, s.whole}
				d.known[path] = func() (snapshot, bool) { return next, true }
				return
			}
		}
	}
	delete(d.known, path)
	if d.read != nil {
		if hunks, ok := d.onDisk(path, edits); ok {
			cd.Hunks = hunks
			cd.count()
			cd.Note = "context from the file on disk"
			return
		}
	}
	// Bare: the replacement alone, its position unknown.
	for _, e := range edits {
		for _, h := range lineHunks(splitLines(e.old), splitLines(e.new), 1, 1) {
			h.OldStart, h.NewStart = 0, 0
			cd.Hunks = append(cd.Hunks, h)
		}
	}
	cd.count()
	cd.Note = "no context: the session never read the file and the disk no longer holds the change"
}

// onDisk places the edits in the file as it is now, when it still contains
// every new text: undoing them (last first) gives a before the edits turn
// back into the file.
func (d *differ) onDisk(path string, edits []edit) ([]DiffHunk, bool) {
	data, err := d.read(path)
	if err != nil {
		return nil, false
	}
	now := string(data)
	before := now
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		if e.new == "" || !strings.Contains(before, e.new) {
			return nil, false
		}
		if e.all {
			before = strings.ReplaceAll(before, e.new, e.old)
		} else {
			before = strings.Replace(before, e.new, e.old, 1)
		}
	}
	return lineHunks(splitLines(before), splitLines(now), 1, 1), true
}

// write reconstructs a Write.
func (d *differ) write(cd *ChangeDiff, path string, in fileInput, head resultHead, body func() (string, bool)) {
	content := in.Content
	cd.After, cd.HasAfter = content, true
	prev := d.known[path]
	next := snapshot{content, 1, true}
	d.known[path] = func() (snapshot, bool) { return next, true }
	switch {
	case head.Type == "create":
		cd.Source = DiffCreate
		cd.HasBefore = true
		if !d.full {
			// Every line is new; no need to lay the hunk out.
			cd.Added, cd.Counted = len(splitLines(content)), true
			return
		}
		cd.Hunks = lineHunks(nil, splitLines(content), 0, 1)
		cd.count()
		return
	case len(head.StructuredPatch) > 0:
		cd.Source = DiffPatch
		cd.Hunks = head.StructuredPatch
		cd.count()
		if d.full {
			cd.Before, cd.HasBefore = body()
		}
		return
	}
	if orig, ok := body(); ok {
		// An update whose patch came out empty: the content did not change.
		cd.Source = DiffPatch
		cd.Before, cd.HasBefore = orig, true
		cd.Hunks = lineHunks(splitLines(orig), splitLines(content), 1, 1)
		cd.count()
		return
	}
	if prev != nil {
		if s, ok := prev(); ok && s.whole {
			cd.Source = DiffSession
			cd.Before, cd.HasBefore = s.text, true
			cd.Hunks = lineHunks(splitLines(s.text), splitLines(content), 1, 1)
			cd.count()
			return
		}
	}
	cd.Source = DiffAfterOnly
	cd.Note = "before unknown — the session never saw the file's previous content"
}

// applyEdits applies the replacements in order; false when an old text is
// missing (or empty on a non-empty file).
func applyEdits(text string, edits []edit) (string, bool) {
	for _, e := range edits {
		switch {
		case e.old == "" && text == "":
			text = e.new
		case e.old == "" || !strings.Contains(text, e.old):
			return "", false
		case e.all:
			text = strings.ReplaceAll(text, e.old, e.new)
		default:
			text = strings.Replace(text, e.old, e.new, 1)
		}
	}
	return text, true
}

// applyPatch applies structuredPatch hunks to the original content; false
// when a context or removed line does not match.
func applyPatch(orig string, hunks []DiffHunk) (string, bool) {
	src := splitLines(orig)
	var out []string
	pos := 0
	for _, h := range hunks {
		start := h.OldStart - 1
		if start < pos {
			start = pos
		}
		if start > len(src) {
			return "", false
		}
		out = append(out, src[pos:start]...)
		pos = start
		for _, l := range h.Lines {
			if l == "" {
				return "", false
			}
			switch l[0] {
			case ' ', '-':
				if pos >= len(src) || src[pos] != l[1:] {
					return "", false
				}
				if l[0] == ' ' {
					out = append(out, src[pos])
				}
				pos++
			case '+':
				out = append(out, l[1:])
			case '\\':
				// "\ No newline at end of file"
			default:
				return "", false
			}
		}
	}
	out = append(out, src[pos:]...)
	text := strings.Join(out, "\n")
	if len(out) > 0 && (orig == "" || strings.HasSuffix(orig, "\n")) {
		text += "\n"
	}
	return text, true
}

// readLineRE is one line of a Read's output: the line number, a tab (or the
// arrow newer harness versions print) and the text.
var readLineRE = regexp.MustCompile(`^\s*(\d+)(?:\t|→)(.*)$`)

// readSnapshot parses a Read's output into the window of the file it shows;
// whole when the result's span says it is the entire file and the output
// was not cut.
func readSnapshot(t *Tool) (snapshot, bool) {
	if sp := t.Span; sp != nil && sp.Total == 0 {
		return snapshot{"", 1, true}, true
	}
	var lines []string
	first, next := 0, 0
	for _, l := range strings.Split(t.Output, "\n") {
		m := readLineRE.FindStringSubmatch(strings.TrimSuffix(l, "\r"))
		if m == nil {
			if first > 0 {
				break
			}
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if first > 0 && n != next {
			break
		}
		if first == 0 {
			first = n
		}
		next = n + 1
		lines = append(lines, m[2])
	}
	if first == 0 {
		return snapshot{}, false
	}
	whole := false
	if sp := t.Span; sp != nil && !t.Truncated {
		whole = first == 1 && len(lines) >= sp.Total
	}
	return snapshot{strings.Join(lines, "\n") + "\n", first, whole}, true
}
