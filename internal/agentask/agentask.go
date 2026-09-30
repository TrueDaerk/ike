// Package agentask asks a fork of a coding-agent session about one node of
// its trace (#2845, epic 0540): it derives the context of a trace node from
// the parsed session, assembles the `claude -p --resume … --fork-session`
// command that answers on a cheaper model, runs it in the session's working
// directory and parses the JSON result. The package is pure: no UI, no
// config, no clock — the app hands it the settings and the node.
//
// The original session's transcript is never written to. The fork is a new
// session id with the parent's conversation copied in (see the fork notes in
// wiki/architecture/agent-trace.md); the answer lands in the fork's own
// file, and the app hides forks from discovery by their ids.
package agentask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ike/internal/agenttrace"
	"ike/internal/diff"
)

// Binary is the Claude Code executable looked up on PATH.
const Binary = "claude"

// SystemPrompt is appended to the fork's system prompt so the ask never
// turns into an edit — the fork also runs without tools.
const SystemPrompt = "Explain only; do not modify files."

// Options mirror the agent.ask.* settings.
type Options struct {
	// Model is "sonnet", "opus" or a full model id.
	Model string
	// MaxTurns bounds the fork's agentic turns (1–5).
	MaxTurns int
	// ShowContext shows the injected context in the answer overlay.
	ShowContext bool
}

// Defaults are the shipped setting values.
var Defaults = Options{Model: "sonnet", MaxTurns: 1, ShowContext: true}

const (
	// MaxAssistant caps the assistant text injected as context, in runes.
	MaxAssistant = 2000
	// MaxHunkLines caps the diff hunk lines injected as context.
	MaxHunkLines = 60
)

// Context is what a question is about: the facts of the selected trace node
// the prompt is prefixed with. Every field is optional; an empty Context asks
// about the session as a whole.
type Context struct {
	// Turn is the turn number; At the turn's timestamp (the prompt line's).
	Turn int
	At   time.Time
	// Kind is the node kind ("decision", "tool", "file", …); Label its row.
	Kind  string
	Label string
	// Path, Line and Op locate the file of a file or single-file tool node.
	Path string
	Line int
	Op   string
	// Tool names the tool call (name and title) of a tool or file node.
	Tool string
	// Assistant is the assistant text the node belongs to: the decision's
	// own text, or the text that preceded the tool call.
	Assistant string
	// Hunk is the change-feed diff of the file node, unified, when the node
	// links to an entry with a diff.
	Hunk string
}

// Empty reports a context with nothing to say.
func (c Context) Empty() bool {
	return c.Turn == 0 && c.Path == "" && c.Tool == "" && c.Assistant == "" && c.Hunk == "" && c.Label == ""
}

// Lines renders the context as the bullet lines the prompt carries.
func (c Context) Lines() []string { return c.DisplayLines(nil) }

// DisplayLines is Lines with paths shortened through display — what the
// overlay shows (agent.ask.show_context); nil keeps them verbatim.
func (c Context) DisplayLines(display func(string) string) []string {
	if display == nil {
		display = func(p string) string { return p }
	}
	var out []string
	if c.Turn > 0 {
		s := "turn: #" + strconv.Itoa(c.Turn)
		if !c.At.IsZero() {
			s += " (" + c.At.Local().Format("2006-01-02 15:04") + ")"
		}
		out = append(out, s)
	}
	if c.Kind != "" && c.Label != "" {
		out = append(out, c.Kind+": "+c.Label)
	}
	if c.Path != "" {
		s := "file: " + display(c.Path)
		if c.Line > 0 {
			s += ":" + strconv.Itoa(c.Line)
		}
		if c.Op != "" {
			s += " (" + c.Op + ")"
		}
		out = append(out, s)
	}
	if c.Tool != "" {
		tool := c.Tool
		if c.Path != "" {
			tool = strings.Replace(tool, c.Path, display(c.Path), 1)
		}
		out = append(out, "tool: "+tool)
	}
	if c.Assistant != "" {
		out = append(out, "assistant said: "+strings.TrimSpace(c.Assistant))
	}
	if c.Hunk != "" {
		out = append(out, "diff:\n```diff\n"+strings.TrimRight(c.Hunk, "\n")+"\n```")
	}
	return out
}

// Prompt is the text handed to `claude -p`: the context, then the question.
func Prompt(c Context, question string) string {
	question = strings.TrimSpace(question)
	lines := c.Lines()
	if len(lines) == 0 {
		return question
	}
	var sb strings.Builder
	sb.WriteString("Context from the session trace (the node the question is about):\n")
	for _, l := range lines {
		sb.WriteString("- " + l + "\n")
	}
	sb.WriteString("\nQuestion: " + question)
	return sb.String()
}

// NodeContext derives the context of a trace node from the parsed session
// it came from. n may be nil (a question about the session as a whole).
func NodeContext(s *agenttrace.Session, n *agenttrace.Node) Context {
	if n == nil {
		return Context{}
	}
	c := Context{Turn: n.Turn, Kind: n.Kind.String(), Label: n.Label}
	switch n.Kind {
	case agenttrace.NodeTurn:
		// The turn number says it; the fork has the prompt itself.
		c.Kind = ""
	case agenttrace.NodeTool, agenttrace.NodeFile:
		// The tool and file lines carry the row; the label would repeat them.
		c.Label = ""
	}
	if n.Ref != nil {
		c.Path, c.Line, c.Op = n.Ref.Path, n.Ref.Line, n.Ref.Op.String()
	}
	if s == nil {
		return c
	}
	// The turn's timestamp is its prompt line's.
	for i := range s.Events {
		ev := &s.Events[i]
		if ev.Turn == n.Turn && !ev.At.IsZero() {
			c.At = ev.At
			break
		}
	}
	if n.Event < 0 || n.Event >= len(s.Events) {
		return c
	}
	ev := &s.Events[n.Event]
	switch {
	case ev.Kind == agenttrace.KindAssistant:
		c.Assistant = clip(ev.Text, MaxAssistant)
	case ev.Kind == agenttrace.KindTool && ev.Tool != nil:
		c.Tool = strings.TrimSpace(ev.Tool.Name + " " + ev.Tool.Title)
		// The assistant text that preceded the call in the same turn is the
		// decision it followed from.
		for i := n.Event - 1; i >= 0; i-- {
			prev := &s.Events[i]
			if prev.Turn != ev.Turn {
				break
			}
			if prev.Kind == agenttrace.KindAssistant && !prev.Reasoning && strings.TrimSpace(prev.Text) != "" {
				c.Assistant = clip(prev.Text, MaxAssistant)
				break
			}
		}
	case ev.Kind == agenttrace.KindUser:
		// A turn node: its label already is the prompt.
	}
	if c.Assistant != "" {
		// The label is the first line of that text.
		c.Label = ""
	}
	return c
}

// clip cuts text to max runes with an ellipsis.
func clip(text string, max int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	rs := []rune(text)
	return strings.TrimSpace(string(rs[:max-1])) + "…"
}

// UnifiedHunks renders a computed diff as the unified text a prompt can
// carry: the hunks' rows as ' ', '-' and '+' lines, cut at maxLines with a
// trailing "…" line. Empty when there is no hunk.
func UnifiedHunks(res diff.Result, maxLines int) string {
	if len(res.Hunks) == 0 {
		return ""
	}
	var lines []string
	for hi, h := range res.Hunks {
		if hi > 0 {
			lines = append(lines, "@@")
		}
		for i := h.Start; i < h.End && i < len(res.Rows); i++ {
			r := res.Rows[i]
			switch r.Kind {
			case diff.RowSame:
				lines = append(lines, " "+r.Left)
			case diff.RowRemoved:
				lines = append(lines, "-"+r.Left)
			case diff.RowAdded:
				lines = append(lines, "+"+r.Right)
			case diff.RowChanged:
				lines = append(lines, "-"+r.Left, "+"+r.Right)
			}
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "…")
	}
	return strings.Join(lines, "\n")
}

// Command assembles the argv of one ask: a print-mode fork of sessionID on
// opts.Model, without tools, bounded to opts.MaxTurns, answering as JSON,
// with the explain-only system prompt appended. Zero option fields take the
// defaults.
func Command(sessionID string, opts Options, prompt string) []string {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = Defaults.Model
	}
	turns := opts.MaxTurns
	if turns < 1 {
		turns = Defaults.MaxTurns
	}
	return []string{
		Binary, "-p",
		"--resume", sessionID,
		"--fork-session",
		"--model", model,
		"--tools", "",
		"--max-turns", strconv.Itoa(turns),
		"--output-format", "json",
		"--append-system-prompt", SystemPrompt,
		prompt,
	}
}

// Result is the parsed `--output-format json` output of one ask.
type Result struct {
	// Answer is the assistant's final text.
	Answer string
	// ForkID is the fork's session id — what a follow-up resumes, and what
	// discovery must skip.
	ForkID string
	// Subtype is the harness's outcome ("success", "error_max_turns", …).
	Subtype string
	// IsError is the harness's flag.
	IsError bool
	// Turns, Duration and CostUSD are the run's accounting, when reported.
	Turns    int
	Duration time.Duration
	CostUSD  float64
}

// jsonResult mirrors the fields of Claude Code's result line.
type jsonResult struct {
	Type       string   `json:"type"`
	Subtype    string   `json:"subtype"`
	IsError    bool     `json:"is_error"`
	Result     string   `json:"result"`
	SessionID  string   `json:"session_id"`
	NumTurns   int      `json:"num_turns"`
	DurationMS int64    `json:"duration_ms"`
	CostUSD    float64  `json:"total_cost_usd"`
	Errors     []string `json:"errors"`
}

// ParseResult decodes the JSON output. With `--output-format json` the
// output is one object; a stream of objects (one per line) is tolerated by
// taking the last one whose type is "result".
func ParseResult(out []byte) (Result, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return Result{}, errors.New("claude printed no result")
	}
	var last *jsonResult
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var jr jsonResult
		if err := dec.Decode(&jr); err != nil {
			if last != nil {
				break
			}
			return Result{}, fmt.Errorf("claude output is not JSON: %v", err)
		}
		if jr.Type == "result" || last == nil {
			cp := jr
			last = &cp
		}
		if !dec.More() {
			break
		}
	}
	r := Result{
		Answer:   last.Result,
		ForkID:   last.SessionID,
		Subtype:  last.Subtype,
		IsError:  last.IsError,
		Turns:    last.NumTurns,
		Duration: time.Duration(last.DurationMS) * time.Millisecond,
		CostUSD:  last.CostUSD,
	}
	if r.IsError || (r.Subtype != "" && r.Subtype != "success") {
		msg := r.Subtype
		if len(last.Errors) > 0 {
			msg = strings.Join(last.Errors, "; ")
		} else if r.Answer != "" {
			msg = r.Answer
		}
		return r, fmt.Errorf("claude reported %s", msg)
	}
	return r, nil
}

// ErrNotInstalled is returned when the claude binary is not on PATH.
var ErrNotInstalled = errors.New("claude is not installed: the `claude` command was not found on PATH")

// ResumeError is a refused --resume: the session id is unknown to Claude
// Code in the working directory the ask ran in.
type ResumeError struct {
	SessionID string
	Detail    string
}

func (e *ResumeError) Error() string {
	return "claude refused to resume session " + e.SessionID + ": " + e.Detail
}

// Run executes argv (from Command) in dir — which must be the session's
// working directory, or the fork lands in another project directory and the
// resume is refused — and parses its output. ctx cancels the process.
func Run(ctx context.Context, dir string, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("empty command")
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return Result{}, ErrNotInstalled
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if res, perr := ParseResult(stdout.Bytes()); perr == nil || res.ForkID != "" {
			// A non-zero exit with a JSON result: the harness said why.
			if perr != nil {
				return res, perr
			}
			return res, fmt.Errorf("claude exited: %v", err)
		}
		if isResumeRefusal(detail) {
			return Result{}, &ResumeError{SessionID: sessionOf(argv), Detail: firstLine(detail)}
		}
		if detail == "" {
			detail = err.Error()
		}
		return Result{}, errors.New("claude failed: " + firstLine(detail))
	}
	return ParseResult(stdout.Bytes())
}

// isResumeRefusal recognises Claude Code's "unknown session" failures.
func isResumeRefusal(detail string) bool {
	d := strings.ToLower(detail)
	return strings.Contains(d, "no conversation found") ||
		strings.Contains(d, "session not found") ||
		strings.Contains(d, "could not resume") ||
		strings.Contains(d, "cannot resume") ||
		strings.Contains(d, "not found with session id")
}

// sessionOf reads the --resume value back out of argv.
func sessionOf(argv []string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--resume" {
			return argv[i+1]
		}
	}
	return ""
}

// firstLine trims s to its first non-empty line, capped for a dialog.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return clip(l, 200)
		}
	}
	return ""
}
