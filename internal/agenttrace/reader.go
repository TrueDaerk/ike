package agenttrace

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Reader tails one session: its transcript file and the subagent
// transcripts beside it (#2861). Every Update reads the bytes appended since
// the last one and feeds the complete lines to the Parser; a trailing line
// without its newline (the harness is still writing it, possibly in several
// writes) stays unread until a later Update finds it complete. A file that
// was replaced (renamed over, recreated), shrank (truncated) or rewritten in
// place so the byte before the read position is no longer a line end is
// parsed from the start again with a fresh Session (#2857).
//
// Claude Code writes a subagent's sidechain to
// <session-id>/subagents/agent-<id>.jsonl next to <session-id>.jsonl. Every
// Update lists that directory — a subagent starts mid-turn — and tails each
// agent file with its own offset and Parser; its meta file names the Agent
// tool call that spawned it, and the subagent is attached to that call
// (Tool.Subagent) once the call is parsed. The Session stays the one main
// timeline, grown in place.
//
// Update and Read are serialized by the Reader itself, so a read the host
// believes lost can never race the next one.
type Reader struct {
	mu   sync.Mutex
	path string
	main tail
	// subs are the subagent transcripts by file name, in listing order.
	subs  map[string]*subTail
	order []string
	// rev counts the Updates that changed the Session (and the resets).
	rev int
	// ReadFile is handed to the Parsers for edit line resolution; nil means
	// os.ReadFile.
	ReadFile func(path string) ([]byte, error)
}

// tail is the read state of one transcript file.
type tail struct {
	path   string
	offset int64
	parser *Parser
	// file is the transcript the offset belongs to; another file under the
	// same path means it was replaced.
	file      os.FileInfo
	sidechain bool
}

// subTail is one subagent transcript: its tail, the Subagent it fills and
// the call it is attached to.
type subTail struct {
	tail
	meta     string
	metaRead bool
	agent    *Subagent
	attached *Tool
}

// NewReader returns a Reader positioned at the start of path. Nothing is read
// until Update.
func NewReader(path string) *Reader {
	r := &Reader{path: path, main: tail{path: path}}
	r.main.reset()
	r.subs = map[string]*subTail{}
	r.rev++
	return r
}

// Load reads a whole session — the transcript at path and its subagent
// transcripts — in one go. A trailing line without its newline is left out,
// as the harness may still be writing it.
func Load(path string) (*Session, error) {
	r := NewReader(path)
	_, err := r.Update()
	return r.Session(), err
}

func (t *tail) reset() {
	t.offset = 0
	t.file = nil
	if t.sidechain {
		t.parser = newSidechainParser()
	} else {
		t.parser = NewParser()
	}
}

// Revision changes whenever the Session does: an Update that added or
// completed events, or a restart from scratch. The host compares it with the
// revision it last showed instead of trusting a single read's count — a
// result it had to drop is then still shown by the next read.
func (r *Reader) Revision() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rev
}

// Read runs one Update and then view on the Session while no other read can
// touch it — the host builds its tree there, off the Update loop. It
// reports the Update's count and the revision after it.
func (r *Reader) Read(view func(*Session)) (added, rev int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	added, err = r.update()
	if view != nil {
		view(r.main.parser.Session())
	}
	return added, r.rev, err
}

// Path returns the transcript file being tailed.
func (r *Reader) Path() string { return r.path }

// Offset returns the byte position the next Update reads from in the main
// transcript.
func (r *Reader) Offset() int64 { return r.main.offset }

// Session returns the transcript parsed so far. The pointer stays valid
// across Updates; the Events slice grows in place.
func (r *Reader) Session() *Session { return r.main.parser.Session() }

// Update consumes the lines appended since the last call — to the transcript
// and to every subagent transcript — and reports how many events were added
// or completed (a subagent attached to its call counts as one). A missing
// file is not an error: the harness may not have written the first line
// yet.
func (r *Reader) Update() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update()
}

func (r *Reader) update() (int, error) {
	changed, reset, err := r.main.update(r.ReadFile)
	if reset {
		// The calls the subagents hung under are gone with the old Session.
		r.rev++
		for _, st := range r.subs {
			st.attached = nil
		}
	}
	if err != nil {
		return changed, err
	}
	n, err := r.updateSubs()
	changed += n
	if changed > 0 {
		r.rev++
	}
	return changed, err
}

// subagentDir is where Claude Code keeps the subagent transcripts of the
// session whose transcript is path.
func subagentDir(path string) string {
	return filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
}

// updateSubs picks up new subagent transcripts, reads every one and
// attaches each to its spawning call.
func (r *Reader) updateSubs() (int, error) {
	dir := subagentDir(r.path)
	ents, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if _, ok := r.subs[name]; ok {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl")
		st := &subTail{
			tail:  tail{path: filepath.Join(dir, name), sidechain: true},
			meta:  filepath.Join(dir, base+".meta.json"),
			agent: &Subagent{ID: strings.TrimPrefix(base, "agent-")},
		}
		st.reset()
		st.agent.Session = st.parser.Session()
		r.subs[name] = st
		r.order = append(r.order, name)
	}
	changed := 0
	for _, name := range r.order {
		st := r.subs[name]
		st.readMeta()
		n, reset, err := st.update(r.ReadFile)
		if reset {
			st.agent.Session = st.parser.Session()
			changed++
		}
		if err != nil {
			return changed, err
		}
		changed += n
	}
	for _, name := range r.order {
		st := r.subs[name]
		if st.attached != nil {
			continue
		}
		if t := r.spawner(st); t != nil {
			t.Subagent = st.agent
			st.attached = t
			changed++
		}
	}
	return changed, nil
}

// readMeta reads the subagent's meta file until it was found and valid; the
// harness may write it after the transcript's first line.
func (st *subTail) readMeta() {
	if st.metaRead {
		return
	}
	data, err := os.ReadFile(st.meta)
	if err != nil {
		return
	}
	var m struct {
		AgentType   string `json:"agentType"`
		Description string `json:"description"`
		ToolUseID   string `json:"toolUseId"`
	}
	if json.Unmarshal(data, &m) != nil {
		return
	}
	st.metaRead = true
	st.agent.Type, st.agent.Description, st.agent.ToolUseID = m.AgentType, m.Description, m.ToolUseID
}

// spawner finds the Agent call that spawned st — by the meta file's
// tool_use id, else by the agent id the call's result reported — in the
// main timeline or in another subagent's (a nested spawn).
func (r *Reader) spawner(st *subTail) *Tool {
	match := func(t *Tool) bool {
		if st.agent.ToolUseID != "" {
			return t.ID == st.agent.ToolUseID
		}
		return t.AgentID != "" && t.AgentID == st.agent.ID
	}
	if t := findTool(r.main.parser.Session().Events, match); t != nil {
		return t
	}
	for _, name := range r.order {
		if other := r.subs[name]; other != st {
			if t := findTool(other.parser.Session().Events, match); t != nil {
				return t
			}
		}
	}
	return nil
}

func findTool(evs []Event, match func(*Tool) bool) *Tool {
	for i := range evs {
		if t := evs[i].Tool; t != nil && (t.Name == "Agent" || t.Name == "Task") && match(t) {
			return t
		}
	}
	return nil
}

// update reads what was appended to the tail's file; reset reports that the
// file was replaced and parsing started over.
func (t *tail) update(readFile func(string) ([]byte, error)) (changed int, reset bool, err error) {
	f, err := os.Open(t.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false, err
	}
	if t.replaced(f, st) {
		t.reset()
		reset = true
	}
	t.file = st
	if st.Size() == t.offset {
		return 0, reset, nil
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return 0, reset, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return 0, reset, err
	}
	// Edits from an earlier batch may have moved lines; resolve against the
	// file as it is now, once per batch.
	t.parser.ReadFile = readFile
	t.parser.cache = map[string][]byte{}
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		if t.parser.Line(data[:nl]) {
			changed++
		}
		t.offset += int64(nl + 1)
		data = data[nl+1:]
	}
	return changed, reset, nil
}

// replaced reports whether the bytes already consumed no longer belong to
// the file at path: another file (rename over, delete + create), a shorter
// one (truncated), or one whose byte before the read position is not the
// line end the last read stopped behind (truncated and rewritten past the
// old size).
func (t *tail) replaced(f *os.File, st os.FileInfo) bool {
	if t.offset == 0 {
		return false
	}
	if t.file != nil && !os.SameFile(t.file, st) {
		return true
	}
	if st.Size() < t.offset {
		return true
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], t.offset-1); err != nil || b[0] != '\n' {
		return true
	}
	return false
}
