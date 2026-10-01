package agenttrace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// MaxOutput caps the tool output text kept per call; a spooled megabyte of
// build log is not what a trace is for.
const MaxOutput = 32 << 10

// maxLine bounds one JSONL line. Claude Code writes tool results of several
// megabytes (a Read of a large file), so the scanner needs headroom.
const maxLine = 64 << 20

// line is the subset of a Claude Code JSONL record the parser looks at.
type line struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	SessionID        string          `json:"sessionId"`
	CWD              string          `json:"cwd"`
	Timestamp        string          `json:"timestamp"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Message          *message        `json:"message"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
	Summary          string          `json:"summary"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// block is one content block of a user or assistant message.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Parser turns transcript lines into Session events. Feed it complete lines
// with Line; the Session grows in place, so hosts that tail a file keep one
// Parser and read Session after every batch.
type Parser struct {
	// ReadFile resolves an edit's old_string / new_string to a line number
	// against the file as it is now. nil means os.ReadFile; set it in tests
	// or to disable resolution (return an error).
	ReadFile func(path string) ([]byte, error)

	sess *Session
	// sidechain makes the parser keep isSidechain lines: a subagent's
	// transcript consists of nothing else. The main transcript's sidechain
	// lines (the old in-file subagent format) stay dropped.
	sidechain bool
	turn      int
	pending   map[string]int
	cache     map[string][]byte
}

// NewParser returns a Parser with an empty Session.
func NewParser() *Parser {
	return &Parser{
		sess:    &Session{Harness: HarnessClaude},
		pending: map[string]int{},
		cache:   map[string][]byte{},
	}
}

// newSidechainParser returns a Parser for a subagent transcript.
func newSidechainParser() *Parser {
	p := NewParser()
	p.sidechain = true
	return p
}

// Session returns the transcript parsed so far.
func (p *Parser) Session() *Session { return p.sess }

// Parse reads a whole transcript. A trailing line without a newline is
// treated as complete; use Reader for a file that is still being written.
func Parse(r io.Reader) (*Session, error) {
	p := NewParser()
	if err := p.Feed(r); err != nil {
		return p.sess, err
	}
	return p.sess, nil
}

// Feed parses every line of r, including a final line without a newline.
func (p *Parser) Feed(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		p.Line(sc.Bytes())
	}
	return sc.Err()
}

// Line parses one complete transcript line. Blank and malformed lines are
// skipped (the latter counted in Session.Malformed); lines of record types the
// trace does not show (attachments, queue operations, cost state, ...) are
// ignored. It reports whether the line produced or completed an event.
func (p *Parser) Line(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	var l line
	if raw[0] != '{' || json.Unmarshal(raw, &l) != nil {
		p.sess.Malformed++
		return false
	}
	p.header(&l)
	switch l.Type {
	case "user":
		return p.user(&l)
	case "assistant":
		return p.assistant(&l)
	case "system":
		return p.system(&l)
	case "summary":
		if l.Summary == "" {
			return false
		}
		p.add(Event{Kind: KindSeparator, Text: l.Summary, UUID: l.UUID})
		return true
	}
	return false
}

// header records session-level facts any line may carry.
func (p *Parser) header(l *line) {
	s := p.sess
	if s.ID == "" && l.SessionID != "" {
		s.ID = l.SessionID
	}
	if s.CWD == "" && l.CWD != "" {
		s.CWD = l.CWD
	}
	if at := parseTime(l.Timestamp); !at.IsZero() {
		if s.StartedAt.IsZero() || at.Before(s.StartedAt) {
			s.StartedAt = at
		}
		if at.After(s.EndedAt) {
			s.EndedAt = at
		}
	}
}

func (p *Parser) add(ev Event) int {
	ev.Turn = p.turn
	p.sess.Events = append(p.sess.Events, ev)
	return len(p.sess.Events) - 1
}

// skip reports a line the parser does not show: no message, or a sidechain
// line of the main transcript.
func (p *Parser) skip(l *line) bool {
	return l.Message == nil || (l.IsSidechain && !p.sidechain)
}

func (p *Parser) user(l *line) bool {
	if p.skip(l) {
		return false
	}
	at := parseTime(l.Timestamp)
	if l.IsCompactSummary {
		p.add(Event{Kind: KindSeparator, At: at, Text: "context compacted", UUID: l.UUID})
		return true
	}
	// Plain string content is a typed prompt.
	var text string
	if err := json.Unmarshal(l.Message.Content, &text); err == nil {
		return p.prompt(l, at, text)
	}
	var blocks []block
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return false
	}
	changed := false
	var parts []string
	for i := range blocks {
		b := &blocks[i]
		switch b.Type {
		case "tool_result":
			if p.result(b, l.ToolUseResult, at) {
				changed = true
			}
		case "text":
			parts = append(parts, b.Text)
		}
	}
	if len(parts) > 0 && p.prompt(l, at, strings.Join(parts, "\n")) {
		changed = true
	}
	return changed
}

var (
	reminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	cmdNameRE  = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	cmdArgsRE  = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	localOutRE = regexp.MustCompile(`(?s)<local-command-(?:stdout|stderr)>.*?</local-command-(?:stdout|stderr)>`)
)

// prompt adds a user event for typed text. Harness-injected reminders are
// stripped; a slash command is rendered as "/name args"; the echoed output of
// a local command is not a prompt at all.
func (p *Parser) prompt(l *line, at time.Time, text string) bool {
	if l.IsMeta {
		return false
	}
	if m := cmdNameRE.FindStringSubmatch(text); m != nil {
		name := strings.TrimSpace(m[1])
		if a := cmdArgsRE.FindStringSubmatch(text); a != nil && strings.TrimSpace(a[1]) != "" {
			name += " " + strings.TrimSpace(a[1])
		}
		text = name
	} else {
		text = localOutRE.ReplaceAllString(text, "")
		text = reminderRE.ReplaceAllString(text, "")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	p.turn++
	p.add(Event{Kind: KindUser, At: at, Text: text, UUID: l.UUID})
	return true
}

func (p *Parser) assistant(l *line) bool {
	if p.skip(l) {
		return false
	}
	var blocks []block
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return false
	}
	at := parseTime(l.Timestamp)
	changed := false
	for i := range blocks {
		b := &blocks[i]
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				p.add(Event{Kind: KindAssistant, At: at, Text: t, UUID: l.UUID})
				changed = true
			}
		case "thinking":
			if t := strings.TrimSpace(b.Thinking); t != "" {
				p.add(Event{Kind: KindAssistant, At: at, Text: t, Reasoning: true, UUID: l.UUID})
				changed = true
			}
		case "tool_use":
			tool := &Tool{ID: b.ID, Name: b.Name, Input: append(json.RawMessage(nil), b.Input...)}
			tool.Title, tool.Paths = p.describe(b.Name, b.Input)
			idx := p.add(Event{Kind: KindTool, At: at, UUID: l.UUID, Tool: tool})
			if b.ID != "" {
				p.pending[b.ID] = idx
			}
			changed = true
		}
	}
	return changed
}

func (p *Parser) system(l *line) bool {
	switch l.Subtype {
	case "compact_boundary":
		p.add(Event{Kind: KindSeparator, At: parseTime(l.Timestamp), Text: "context compacted", UUID: l.UUID})
		return true
	}
	return false
}

// result attaches a tool_result block to its pending call.
func (p *Parser) result(b *block, structured json.RawMessage, at time.Time) bool {
	idx, ok := p.pending[b.ToolUseID]
	if !ok {
		return false
	}
	delete(p.pending, b.ToolUseID)
	tool := p.sess.Events[idx].Tool
	tool.Done = true
	tool.DoneAt = at
	tool.IsError = b.IsError
	tool.Output, tool.Truncated = capOutput(resultText(b.Content))
	p.refine(tool, structured)
	return true
}

// resultText joins the text of a tool_result content field, which is either
// a string or an array of blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for i := range blocks {
		if blocks[i].Type == "text" && blocks[i].Text != "" {
			parts = append(parts, blocks[i].Text)
		}
	}
	return strings.Join(parts, "\n")
}

func capOutput(s string) (string, bool) {
	if len(s) <= MaxOutput {
		return s, false
	}
	cut := MaxOutput
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut], true
}

// structuredResult is the part of Claude Code's toolUseResult the parser
// uses to sharpen a FileRef after the fact.
type structuredResult struct {
	Type            string `json:"type"`
	FilePath        string `json:"filePath"`
	AgentID         string `json:"agentId"`
	StructuredPatch []struct {
		OldStart int `json:"oldStart"`
		NewStart int `json:"newStart"`
	} `json:"structuredPatch"`
}

// refine sharpens a call from its structured result, which is an object for
// a successful call and a plain string for a rejected or failed one (left
// alone). A Write is a create or — on an existing file, type "update" — an
// edit of the whole file (#2861); an edit's line comes from the recorded
// patch, which beats any search of the file; an Agent call learns the id of
// the subagent it spawned. File-changing calls keep the object for the
// diffs.
func (p *Parser) refine(tool *Tool, raw json.RawMessage) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var sr structuredResult
	if json.Unmarshal(raw, &sr) != nil {
		return
	}
	switch tool.Name {
	case "Agent", "Task":
		tool.AgentID = sr.AgentID
		return
	}
	if len(tool.Paths) == 0 {
		return
	}
	switch tool.Name {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		tool.Result = append(json.RawMessage(nil), raw...)
	}
	patchLine := 0
	if len(sr.StructuredPatch) > 0 && sr.StructuredPatch[0].NewStart > 0 {
		patchLine = sr.StructuredPatch[0].NewStart
	}
	switch tool.Name {
	case "Write":
		switch sr.Type {
		case "create":
			tool.Paths[0].Op = OpCreate
		case "update":
			tool.Paths[0].Op = OpEdit
			tool.Paths[0].Line = patchLine
		}
	case "Edit":
		if patchLine > 0 {
			tool.Paths[0].Line = patchLine
		}
	case "MultiEdit":
		// The hunks are in file order, the edits in input order; only an
		// edit the file search could not place takes a hunk's line.
		for i := range tool.Paths {
			if tool.Paths[i].Line == 0 && i < len(sr.StructuredPatch) {
				tool.Paths[i].Line = sr.StructuredPatch[i].NewStart
			}
		}
	}
}

// toolInput is the union of the input fields the file tools use.
type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	OldString    string `json:"old_string"`
	NewString    string `json:"new_string"`
	Offset       int    `json:"offset"`
	EditMode     string `json:"edit_mode"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	Pattern      string `json:"pattern"`
	URL          string `json:"url"`
	Query        string `json:"query"`
	Skill        string `json:"skill"`
	Prompt       string `json:"prompt"`
	Edits        []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// describe derives the title and the file references of a tool call from
// its input.
func (p *Parser) describe(name string, raw json.RawMessage) (string, []FileRef) {
	var in toolInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}
	switch name {
	case "Read":
		if in.FilePath == "" {
			return name, nil
		}
		return in.FilePath, []FileRef{{Path: in.FilePath, Line: in.Offset, Op: OpRead}}
	case "Edit":
		if in.FilePath == "" {
			return name, nil
		}
		return in.FilePath, []FileRef{{Path: in.FilePath, Line: p.locate(in.FilePath, in.NewString, in.OldString), Op: OpEdit}}
	case "MultiEdit":
		if in.FilePath == "" {
			return name, nil
		}
		refs := make([]FileRef, 0, len(in.Edits))
		for _, e := range in.Edits {
			refs = append(refs, FileRef{Path: in.FilePath, Line: p.locate(in.FilePath, e.NewString, e.OldString), Op: OpEdit})
		}
		if len(refs) == 0 {
			refs = append(refs, FileRef{Path: in.FilePath, Op: OpEdit})
		}
		return in.FilePath, refs
	case "Write":
		if in.FilePath == "" {
			return name, nil
		}
		return in.FilePath, []FileRef{{Path: in.FilePath, Op: OpWrite}}
	case "NotebookEdit":
		if in.NotebookPath == "" {
			return name, nil
		}
		op := OpEdit
		if in.EditMode == "delete" {
			op = OpDelete
		}
		return in.NotebookPath, []FileRef{{Path: in.NotebookPath, Op: op}}
	case "Bash":
		if in.Description != "" {
			return in.Description, nil
		}
		return firstLine(in.Command, name), nil
	case "Grep", "Glob":
		return firstLine(in.Pattern, name), nil
	case "WebFetch":
		return firstLine(in.URL, name), nil
	case "WebSearch":
		return firstLine(in.Query, name), nil
	case "Skill":
		return firstLine(in.Skill, name), nil
	case "Agent", "Task":
		if in.Description != "" {
			return in.Description, nil
		}
		return firstLine(in.Prompt, name), nil
	}
	return name, nil
}

func firstLine(s, fallback string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return fallback
	}
	return s
}

// locate finds the 1-based line where an edit landed, best effort against
// the file as it is now: the new text is what the file should contain after
// the edit, the old text is the fallback for an edit that was reverted or
// never applied. 0 when neither is found or the file cannot be read.
func (p *Parser) locate(path, newString, oldString string) int {
	if newString == "" && oldString == "" {
		return 0
	}
	data, ok := p.cache[path]
	if !ok {
		read := p.ReadFile
		if read == nil {
			read = os.ReadFile
		}
		b, err := read(path)
		if err != nil {
			b = nil
		}
		p.cache[path] = b
		data = b
	}
	if data == nil {
		return 0
	}
	for _, needle := range []string{newString, oldString} {
		if needle == "" {
			continue
		}
		if i := bytes.Index(data, []byte(needle)); i >= 0 {
			return bytes.Count(data[:i], []byte{'\n'}) + 1
		}
	}
	return 0
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
