package agenttrace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// history.go keeps sessions after the trace stopped following them (#2860):
// a compact per-session Record under the project's state directory
// (.ike/agent-trace/<session-id>.json), written from the live reader at
// every turn boundary and on SessionEnd, and imported once from the
// transcripts Claude Code still has. The record is the harness-neutral
// model — prompts, assistant texts, tool calls with the files they touched,
// the rewinds and the reconstructed diffs — never a copy of the transcript:
// tool inputs and outputs are dropped, texts are capped, and the diffs are
// trimmed until the record fits MaxRecordBytes. Record.Session rebuilds a
// Session the tree and the path are built from like from a live one; the
// stored diffs travel as Session.KnownDiffs.
//
// The Store prunes to Max records, oldest session end first (then oldest
// start), so the directory stays bounded at Max × MaxRecordBytes.

// RecordVersion is the record format version written as "v". A reader
// refuses a newer one; an older one is read with its fields' zero values.
const RecordVersion = 1

// MaxRecordBytes caps one record file: the diffs are dropped largest first
// until the encoding fits (Record.Truncated says so).
const MaxRecordBytes = 512 << 10

// DefaultMaxSessions is the store's default cap (agent.trace.history_max_sessions).
const DefaultMaxSessions = 50

// Text caps inside a record: a prompt or answer keeps maxRecordText bytes,
// an intermediate assistant text or thinking block maxRecordNote.
const (
	maxRecordText = 4 << 10
	maxRecordNote = 512
)

// Record is one stored session.
type Record struct {
	V          int       `json:"v"`
	ID         string    `json:"id"`
	Harness    string    `json:"harness"`
	CWD        string    `json:"cwd"`
	Transcript string    `json:"transcript,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	// Ended is set once the session is known to be over: SessionEnd, the
	// trace moving on to another session, or an import.
	Ended bool `json:"ended"`
	// Source is "live" for a record the trace wrote while following the
	// session, "import" for one agent.trace.import read from the transcript.
	Source string `json:"source"`
	// TranscriptSize and TranscriptMod are the transcript file's facts when
	// the record was written; the import skips a file that still matches.
	TranscriptSize int64     `json:"transcript_size,omitempty"`
	TranscriptMod  time.Time `json:"transcript_mod,omitempty"`
	// Turns, FilesChanged and FirstPrompt are the picker's columns.
	Turns        int           `json:"turns"`
	FilesChanged int           `json:"files_changed"`
	FirstPrompt  string        `json:"first_prompt,omitempty"`
	Events       []RecordEvent `json:"events"`
	Rewinds      []Rewind      `json:"rewinds,omitempty"`
	Diffs        []RecordDiff  `json:"diffs,omitempty"`
	// Truncated reports that diffs were dropped to fit MaxRecordBytes.
	Truncated bool `json:"truncated,omitempty"`
}

// RecordEvent is one Event without the transcript's raw payloads.
type RecordEvent struct {
	Kind      string      `json:"k"`
	Turn      int         `json:"turn"`
	At        time.Time   `json:"at,omitempty"`
	Text      string      `json:"text,omitempty"`
	Reasoning bool        `json:"thinking,omitempty"`
	Abandoned bool        `json:"abandoned,omitempty"`
	Tool      *RecordTool `json:"tool,omitempty"`
}

// RecordTool is a tool call: name, title, outcome and files — no input, no
// output.
type RecordTool struct {
	Name  string       `json:"name"`
	Title string       `json:"title,omitempty"`
	Error bool         `json:"error,omitempty"`
	Done  bool         `json:"done"`
	Paths []RecordRef  `json:"paths,omitempty"`
	Agent *RecordAgent `json:"agent,omitempty"`
}

// RecordRef is one file a call touched.
type RecordRef struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Op   string `json:"op"`
}

// RecordAgent is the subagent an Agent call spawned (#2861), with its own
// events.
type RecordAgent struct {
	ID          string        `json:"id"`
	Type        string        `json:"type,omitempty"`
	Description string        `json:"description,omitempty"`
	Events      []RecordEvent `json:"events"`
}

// RecordDiff is a reconstructed change (#2859) as the record keeps it: the
// hunks and counts, never the whole contents.
type RecordDiff struct {
	Key     string     `json:"key"`
	Keys    []string   `json:"keys,omitempty"`
	Path    string     `json:"path"`
	Op      string     `json:"op"`
	Tool    string     `json:"tool"`
	Turn    int        `json:"turn"`
	At      time.Time  `json:"at,omitempty"`
	Source  int        `json:"source"`
	Note    string     `json:"note,omitempty"`
	Hunks   []DiffHunk `json:"hunks,omitempty"`
	Added   int        `json:"added"`
	Removed int        `json:"removed"`
	Counted bool       `json:"counted"`
}

// ParseOp is the inverse of Op.String; unknown names read as OpRead.
func ParseOp(s string) Op {
	switch s {
	case "edit":
		return OpEdit
	case "write":
		return OpWrite
	case "create":
		return OpCreate
	case "delete":
		return OpDelete
	}
	return OpRead
}

// NewRecord reduces a session to its record. transcript is the file it was
// read from (kept for ask's resume, #2845); ended marks the session over.
// The diffs are reconstructed without the disk (DiffsWith nil): the record
// is written from the live loop at every turn boundary.
func NewRecord(s *Session, transcript string, ended bool) *Record {
	return NewRecordWith(s, transcript, ended, nil)
}

// NewRecordWith is NewRecord with the disk read the diff reconstruction may
// use (the import passes os.ReadFile).
func NewRecordWith(s *Session, transcript string, ended bool, readFile func(string) ([]byte, error)) *Record {
	r := &Record{V: RecordVersion, Source: "live"}
	if s == nil {
		return r
	}
	r.ID, r.Harness, r.CWD, r.Transcript = s.ID, s.Harness, s.CWD, transcript
	r.StartedAt, r.EndedAt, r.Ended = s.StartedAt, s.EndedAt, ended
	r.Turns = s.Turns()
	r.Rewinds = append([]Rewind(nil), s.Rewinds...)
	r.Events = recordEvents(s.Events)
	for i := range s.Events {
		if ev := s.Events[i]; ev.Kind == KindUser && !ev.Abandoned && !IsAskPrompt(ev.Text) {
			r.FirstPrompt = Collapse(ev.Text)
			break
		}
	}
	changed := map[string]bool{}
	for _, ref := range s.Files() {
		if writing(ref.Op) {
			changed[ref.Path] = true
		}
	}
	r.FilesChanged = len(changed)
	for _, cd := range DiffsWith(s, readFile) {
		r.Diffs = append(r.Diffs, RecordDiff{
			Key: cd.Key, Keys: cd.Keys, Path: cd.Path, Op: cd.Op.String(), Tool: cd.Tool, Turn: cd.Turn, At: cd.At,
			Source: int(cd.Source), Note: cd.Note, Hunks: cd.Hunks, Added: cd.Added, Removed: cd.Removed, Counted: cd.Counted,
		})
	}
	return r
}

// recordEvents converts a timeline, subagents included.
func recordEvents(evs []Event) []RecordEvent {
	out := make([]RecordEvent, 0, len(evs))
	for i := range evs {
		ev := &evs[i]
		re := RecordEvent{Kind: ev.Kind.String(), Turn: ev.Turn, At: ev.At, Reasoning: ev.Reasoning, Abandoned: ev.Abandoned}
		switch ev.Kind {
		case KindUser, KindSeparator:
			re.Text = capText(ev.Text, maxRecordText)
		case KindAssistant:
			// The turn's answer is found when the record is read; every text
			// keeps enough to be it, a thinking block only its label.
			if ev.Reasoning {
				re.Text = capText(ev.Text, maxRecordNote)
			} else {
				re.Text = capText(ev.Text, maxRecordText)
			}
		}
		if t := ev.Tool; t != nil {
			rt := &RecordTool{Name: t.Name, Title: capText(t.Title, maxRecordNote), Error: t.IsError, Done: t.Done}
			for _, ref := range t.Paths {
				rt.Paths = append(rt.Paths, RecordRef{Path: ref.Path, Line: ref.Line, Op: ref.Op.String()})
			}
			if sub := t.Subagent; sub != nil {
				ra := &RecordAgent{ID: sub.ID, Type: sub.Type, Description: capText(sub.Description, maxRecordNote)}
				if sub.Session != nil {
					ra.Events = recordEvents(sub.Session.Events)
				}
				rt.Agent = ra
			}
			re.Tool = rt
		}
		out = append(out, re)
	}
	return out
}

// capText cuts s to at most n bytes on a rune boundary, marking the cut.
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// Session rebuilds the session the tree and the path are built from. Tool
// inputs and results are gone, so the stored diffs come along as
// KnownDiffs and the structured facts (ops, lines) as they were recorded.
func (r *Record) Session() *Session {
	s := &Session{ID: r.ID, Harness: r.Harness, CWD: r.CWD, StartedAt: r.StartedAt, EndedAt: r.EndedAt}
	s.Events = sessionEvents(r.Events)
	s.Rewinds = append([]Rewind(nil), r.Rewinds...)
	s.KnownDiffs = make([]ChangeDiff, 0, len(r.Diffs))
	for _, d := range r.Diffs {
		s.KnownDiffs = append(s.KnownDiffs, ChangeDiff{
			Key: d.Key, Keys: d.Keys, Path: d.Path, Op: ParseOp(d.Op), Tool: d.Tool, Turn: d.Turn, At: d.At,
			Source: DiffSource(d.Source), Note: d.Note, Hunks: d.Hunks, Added: d.Added, Removed: d.Removed, Counted: d.Counted,
		})
	}
	return s
}

func sessionEvents(res []RecordEvent) []Event {
	out := make([]Event, 0, len(res))
	for i := range res {
		re := &res[i]
		ev := Event{Turn: re.Turn, At: re.At, Text: re.Text, Reasoning: re.Reasoning, Abandoned: re.Abandoned}
		switch re.Kind {
		case "user":
			ev.Kind = KindUser
		case "assistant":
			ev.Kind = KindAssistant
		case "tool":
			ev.Kind = KindTool
		case "separator":
			ev.Kind = KindSeparator
		}
		if rt := re.Tool; rt != nil {
			t := &Tool{Name: rt.Name, Title: rt.Title, IsError: rt.Error, Done: rt.Done}
			for _, ref := range rt.Paths {
				t.Paths = append(t.Paths, FileRef{Path: ref.Path, Line: ref.Line, Op: ParseOp(ref.Op)})
			}
			if ra := rt.Agent; ra != nil {
				t.Subagent = &Subagent{ID: ra.ID, Type: ra.Type, Description: ra.Description, Session: &Session{Harness: HarnessClaude, Events: sessionEvents(ra.Events)}}
			}
			ev.Tool = t
		}
		out = append(out, ev)
	}
	return out
}

// Encode marshals the record, dropping diffs largest first until it fits
// MaxRecordBytes.
func (r *Record) Encode() ([]byte, error) {
	for {
		data, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		if len(data) <= MaxRecordBytes || len(r.Diffs) == 0 {
			return data, nil
		}
		// Drop the hunks of the largest diff; a diff without hunks keeps
		// its counts for the boxes.
		big, size := -1, 0
		for i := range r.Diffs {
			n := 0
			for _, h := range r.Diffs[i].Hunks {
				for _, l := range h.Lines {
					n += len(l)
				}
			}
			if n > size {
				big, size = i, n
			}
		}
		if big < 0 {
			// Only countless diffs left and still too large: cut texts.
			if !r.cutTexts() {
				return data, nil
			}
			continue
		}
		r.Diffs[big].Hunks = nil
		r.Truncated = true
	}
}

// cutTexts halves the text caps once; it reports whether anything shrank.
func (r *Record) cutTexts() bool {
	shrank := false
	var walk func(evs []RecordEvent)
	walk = func(evs []RecordEvent) {
		for i := range evs {
			if len(evs[i].Text) > maxRecordNote {
				evs[i].Text = capText(evs[i].Text, maxRecordNote)
				shrank = true
			}
			if t := evs[i].Tool; t != nil && t.Agent != nil {
				walk(t.Agent.Events)
			}
		}
	}
	walk(r.Events)
	if shrank {
		r.Truncated = true
	}
	return shrank
}

// Summary is a record's picker row.
type Summary struct {
	ID           string
	StartedAt    time.Time
	EndedAt      time.Time
	Ended        bool
	Turns        int
	FilesChanged int
	FirstPrompt  string
	Transcript   string
	Source       string
	// TranscriptSize and TranscriptMod are the transcript's facts at write
	// time (import idempotence).
	TranscriptSize int64
	TranscriptMod  time.Time
	// Size is the record file's size.
	Size int64
}

// Duration is the session's span.
func (s Summary) Duration() time.Duration {
	if s.StartedAt.IsZero() || s.EndedAt.Before(s.StartedAt) {
		return 0
	}
	return s.EndedAt.Sub(s.StartedAt)
}

// Store is the record directory.
type Store struct {
	Dir string
	// Max is how many records are kept; <= 0 means DefaultMaxSessions.
	Max int
}

// ErrRecordVersion reports a record written by a newer IKE.
var ErrRecordVersion = errors.New("agenttrace: history record from a newer version")

func (st Store) max() int {
	if st.Max <= 0 {
		return DefaultMaxSessions
	}
	return st.Max
}

// Path is the record file of a session id.
func (st Store) Path(id string) string {
	return filepath.Join(st.Dir, safeName(id)+".json")
}

// safeName keeps a session id usable as a file name.
func safeName(id string) string {
	var b strings.Builder
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// Save writes the record atomically (temp file, rename) and prunes the
// store to its cap. A record without an id is not written.
func (st Store) Save(r *Record) error {
	if r == nil || r.ID == "" {
		return errors.New("agenttrace: history record without a session id")
	}
	if err := os.MkdirAll(st.Dir, 0o755); err != nil {
		return err
	}
	data, err := r.Encode()
	if err != nil {
		return err
	}
	path := st.Path(r.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return st.Prune()
}

// Load reads one record.
func (st Store) Load(id string) (*Record, error) {
	return readRecord(st.Path(id))
}

func readRecord(path string) (*Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.V > RecordVersion {
		return nil, ErrRecordVersion
	}
	return &r, nil
}

// List returns every readable record's summary, newest first (by end, then
// start). Unreadable files are skipped.
func (st Store) List() ([]Summary, error) {
	ents, err := os.ReadDir(st.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Summary
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(st.Dir, e.Name())
		r, err := readRecord(path)
		if err != nil {
			continue
		}
		s := Summary{
			ID: r.ID, StartedAt: r.StartedAt, EndedAt: r.EndedAt, Ended: r.Ended, Turns: r.Turns,
			FilesChanged: r.FilesChanged, FirstPrompt: r.FirstPrompt, Transcript: r.Transcript, Source: r.Source,
			TranscriptSize: r.TranscriptSize, TranscriptMod: r.TranscriptMod,
		}
		if fi, err := e.Info(); err == nil {
			s.Size = fi.Size()
		}
		out = append(out, s)
	}
	sortSummaries(out)
	return out, nil
}

// sortSummaries orders newest first.
func sortSummaries(all []Summary) {
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if !a.EndedAt.Equal(b.EndedAt) {
			return a.EndedAt.After(b.EndedAt)
		}
		return a.StartedAt.After(b.StartedAt)
	})
}

// Prune deletes the oldest records past the cap.
func (st Store) Prune() error {
	all, err := st.List()
	if err != nil {
		return err
	}
	var first error
	for _, s := range all[min(len(all), st.max()):] {
		if err := os.Remove(st.Path(s.ID)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Remove deletes one record.
func (st Store) Remove(id string) error {
	err := os.Remove(st.Path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ImportResult is what one agent.trace.import run did.
type ImportResult struct {
	// Imported counts records written, Unchanged the transcripts whose
	// record was current, Forks the forks (and ask-tagged forks) skipped.
	Imported, Unchanged, Forks int
	// Failed names the transcripts that could not be read, with the reason.
	Failed []string
}

// Import writes a record for every session transcript of cwd under
// projectsDir that the store does not hold yet — or holds from an older
// (size/mtime) version of the file. Forks and ask-tagged forks are skipped;
// a transcript the parser cannot read is reported in Failed. progress, when
// set, is called before every file with (done, total).
func Import(st Store, projectsDir, cwd string, progress func(done, total int)) (ImportResult, error) {
	var res ImportResult
	files, err := transcriptFiles(projectsDir, cwd)
	if err != nil {
		return res, err
	}
	located, err := ListIn(projectsDir, cwd)
	if err != nil {
		return res, err
	}
	byPath := map[string]Located{}
	for _, l := range located {
		byPath[l.Path] = l
	}
	have := map[string]Summary{}
	if all, err := st.List(); err == nil {
		for _, s := range all {
			have[s.ID] = s
		}
	}
	for i, path := range files {
		if progress != nil {
			progress(i, len(files))
		}
		l, ok := byPath[path]
		if !ok {
			res.Failed = append(res.Failed, filepath.Base(path)+": no session header")
			continue
		}
		if l.ParentID != "" || l.Asked {
			res.Forks++
			continue
		}
		fi, err := os.Stat(path)
		if err != nil {
			res.Failed = append(res.Failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		if prev, ok := have[l.ID]; ok && prev.TranscriptSize == fi.Size() && prev.TranscriptMod.Equal(fi.ModTime()) {
			res.Unchanged++
			continue
		}
		sess, err := Load(path)
		if err != nil {
			res.Failed = append(res.Failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		if len(sess.Events) == 0 {
			reason := "no events"
			if sess.Malformed > 0 {
				reason = "malformed transcript"
			}
			res.Failed = append(res.Failed, filepath.Base(path)+": "+reason)
			continue
		}
		if sess.ID == "" {
			sess.ID = l.ID
		}
		r := NewRecordWith(sess, path, true, os.ReadFile)
		r.Source = "import"
		r.TranscriptSize, r.TranscriptMod = fi.Size(), fi.ModTime()
		if err := st.Save(r); err != nil {
			res.Failed = append(res.Failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		res.Imported++
	}
	if progress != nil {
		progress(len(files), len(files))
	}
	return res, nil
}

// transcriptFiles lists every .jsonl under the project directories of cwd
// (as given and symlink-resolved), sorted.
func transcriptFiles(projectsDir, cwd string) ([]string, error) {
	if projectsDir == "" || cwd == "" {
		return nil, nil
	}
	cwd = filepath.Clean(cwd)
	names := []string{EncodeCWD(cwd)}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		names = append(names, EncodeCWD(real))
	}
	var out []string
	for _, name := range names {
		ents, err := os.ReadDir(filepath.Join(projectsDir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			out = append(out, filepath.Join(projectsDir, name, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// DirStamp is the newest modification time of the project directories of
// cwd under projectsDir: a new transcript appearing (a /clear without
// hooks) changes it, an append to an existing one does not. Zero when none
// exists.
func DirStamp(projectsDir, cwd string) time.Time {
	if projectsDir == "" || cwd == "" {
		return time.Time{}
	}
	cwd = filepath.Clean(cwd)
	names := []string{EncodeCWD(cwd)}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		names = append(names, EncodeCWD(real))
	}
	var stamp time.Time
	for _, name := range names {
		if fi, err := os.Stat(filepath.Join(projectsDir, name)); err == nil && fi.ModTime().After(stamp) {
			stamp = fi.ModTime()
		}
	}
	return stamp
}
