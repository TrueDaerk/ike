package agentask

import (
	"context"
	"strconv"
	"strings"

	"ike/internal/agenttrace"
)

// simplify.go is the complexity half of the runner (#2964, epic 0551): the
// Complexity window's `a` asks a *fresh* `claude -p` session — no --resume,
// the agent.ask options, the project root as cwd — to reduce the cognitive
// complexity of one function. The prompt carries the function's source, its
// metrics, its complexity sites and its best extraction candidate, then the
// fixed instruction; the answer is a unified diff and a paragraph, which the
// app applies to the open buffer and shows as a markdown scratch (#3009).
// An answer without a parseable diff gets exactly one follow-up in the same
// session (RunSimplify). The prompt opens with AskMarker like every ask, so
// discovery skips the session the run leaves behind.

// SimplifyInstruction is the fixed instruction every simplify prompt ends
// with. It says outright that the session has no tools (#3009): the
// CLAUDE.md files Claude Code loads push the model toward a Read call,
// which a tool-less session only prints.
const SimplifyInstruction = "Reduce the cognitive complexity of this function without changing its behaviour; " +
	"keep the public signature; keep tests green. " +
	"You have no tools and cannot read files; the complete function source is in the fenced block above. " +
	"Answer with the unified diff against the file first, in a ```diff fence, " +
	"then one paragraph on what you changed and why. " +
	DiffInFull

// SimplifySystemPrompt replaces SystemPrompt for a simplify run (#3009).
const SimplifySystemPrompt = "You have no tools and cannot read or modify files. " +
	"Answer only from the source in the prompt: a unified diff in a ```diff fence first, then one paragraph. " +
	DiffInFull

// DiffInFull ends every diff-asking instruction (#3050): a long function
// tempts the model to stand a "..." placeholder in for a region it leaves
// alone, and such a diff never matches the buffer.
const DiffInFull = "Write every line of the diff in full, exactly as in the source: " +
	"never shorten it with \"...\", \"[...]\" or an \"unchanged\" note standing in for lines."

// NoDiffFollowUp is the one follow-up a diff-less answer gets (#3009).
const NoDiffFollowUp = "Your answer contained no unified diff. Answer with the diff now."

// ElidedDiffFollowUp is the one follow-up a diff that leaves lines out gets
// (#3050): "..." placeholders or a hunk shorter than its header counts.
const ElidedDiffFollowUp = "Your diff left lines out (a \"...\" placeholder, or a hunk shorter than its @@ header counts), so it cannot be applied. " +
	"Answer with the complete diff now, every line written in full."

// MaxSimplifySource caps the function source carried in the prompt, in
// bytes; a longer function is cut at a line boundary with a trailing note.
const MaxSimplifySource = 64 << 10

// MaxSimplifySites caps the sites listed in the prompt.
const MaxSimplifySites = 60

// Simplify describes the function a simplify prompt is about. Lines are
// 0-based like complexity.Function's; the prompt prints them 1-based.
type Simplify struct {
	// Path is the file (project-relative in the prompt, as handed in);
	// Lang its language id.
	Path string
	Lang string
	// Name, Line, EndLine, CCN and Cognitive are the function's row.
	Name      string
	Line      int
	EndLine   int
	CCN       int
	Cognitive int
	// Source is the function's text, header to closing line.
	Source string
	// Sites are the function's complexity sites in source order (#2946),
	// empty when the measurement carries none.
	Sites []SimplifySite
	// Candidate is the best extraction candidate (#2948), nil without one.
	Candidate *SimplifyCandidate
}

// SimplifySite is one complexity increment of the function.
type SimplifySite struct {
	Line      int
	Kind      string
	CCN       int
	Cognitive int
	Nesting   int
}

// SimplifyCandidate is an extraction candidate: the block whose extraction
// into a function of its own would save the most cognitive complexity.
type SimplifyCandidate struct {
	Line, EndLine    int
	Kind             string
	CogSaving        int
	NewCCN, NewCog   int
	RestCCN, RestCog int
}

// SimplifyPrompt is the text handed to `claude -p`: the ask marker, the
// function's facts, its source fenced in its language, then the
// instruction.
func SimplifyPrompt(s Simplify) string {
	var sb strings.Builder
	sb.WriteString(agenttrace.AskMarker + "\n")
	sb.WriteString("File: " + s.Path)
	if s.Lang != "" {
		sb.WriteString(" (" + s.Lang + ")")
	}
	sb.WriteString(", lines " + strconv.Itoa(s.Line+1) + "–" + strconv.Itoa(s.EndLine+1) + "\n")
	sb.WriteString("Function: " + s.Name + " (CCN " + strconv.Itoa(s.CCN) + ", cognitive " + strconv.Itoa(s.Cognitive) + ")\n")
	if len(s.Sites) > 0 {
		sb.WriteString("Complexity sites (line, kind, increments):\n")
		for i, site := range s.Sites {
			if i == MaxSimplifySites {
				sb.WriteString("- … " + strconv.Itoa(len(s.Sites)-i) + " more\n")
				break
			}
			sb.WriteString("- line " + strconv.Itoa(site.Line+1) + " " + site.Kind + ": +" + strconv.Itoa(site.CCN) + " ccn, +" + strconv.Itoa(site.Cognitive) + " cognitive")
			if site.Nesting > 0 {
				sb.WriteString(" (of which " + strconv.Itoa(site.Nesting) + " nesting)")
			}
			sb.WriteString("\n")
		}
	}
	if c := s.Candidate; c != nil {
		sb.WriteString("Best extraction candidate: lines " + strconv.Itoa(c.Line+1) + "–" + strconv.Itoa(c.EndLine+1) + " (" + c.Kind + ")")
		sb.WriteString(", saves " + strconv.Itoa(c.CogSaving) + " cognitive; the new function would have CCN " + strconv.Itoa(c.NewCCN) + " / cognitive " + strconv.Itoa(c.NewCog))
		sb.WriteString(", the rest CCN " + strconv.Itoa(c.RestCCN) + " / cognitive " + strconv.Itoa(c.RestCog) + "\n")
	}
	sb.WriteString("Source:\n```" + s.Lang + "\n" + strings.TrimRight(capSource(s.Source), "\n") + "\n```\n\n")
	sb.WriteString(SimplifyInstruction)
	return sb.String()
}

// capSource cuts a source text over MaxSimplifySource at a line boundary.
func capSource(src string) string {
	if len(src) <= MaxSimplifySource {
		return src
	}
	cut := strings.LastIndexByte(src[:MaxSimplifySource], '\n')
	if cut <= 0 {
		cut = MaxSimplifySource
	}
	return src[:cut] + "\n… (cut: the function is longer than " + strconv.Itoa(MaxSimplifySource>>10) + " KiB)"
}

// FreshCommand assembles the argv of a run in a fresh session: Command
// without --resume and --fork-session — the same model, tool-less,
// bounded, JSON-answering print mode with the explain-only system prompt.
func FreshCommand(opts Options, prompt string) []string {
	var out []string
	args := Command("", opts, prompt)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--resume":
			i++ // and its value
		case "--fork-session":
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// SimplifyCommand is FreshCommand with SimplifySystemPrompt appended
// instead of the explain-only one.
func SimplifyCommand(opts Options, prompt string) []string {
	return withSystemPrompt(FreshCommand(opts, prompt), SimplifySystemPrompt)
}

// SimplifyFollowUp is the one follow-up a run gets: FollowUp resuming the
// run's own session with ask — NoDiffFollowUp or ElidedDiffFollowUp —
// under SimplifySystemPrompt.
func SimplifyFollowUp(sessionID string, opts Options, ask string) []string {
	return withSystemPrompt(FollowUp(sessionID, opts, Prompt(Context{}, ask)), SimplifySystemPrompt)
}

// withSystemPrompt swaps the --append-system-prompt value of argv.
func withSystemPrompt(argv []string, system string) []string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--append-system-prompt" {
			argv[i+1] = system
		}
	}
	return argv
}

// Runner runs one argv in dir — Run, or a fake in tests.
type Runner func(ctx context.Context, dir string, argv []string) (Result, error)

// SimplifyResult is a finished simplify run: the last answer, the parsed
// diff when it held one, and whether the one follow-up ran (a diff-less or
// an elided answer, #3050; Diff.Elided says what the last answer left out). Sessions
// lists every session id the run left behind (the fresh one and, after a
// follow-up, the one it answered in) for discovery to skip.
type SimplifyResult struct {
	Result
	Diff     AnswerDiff
	HasDiff  bool
	Retried  bool
	Sessions []string
}

// RunSimplify runs the simplify prompt in a fresh session and, when the
// answer holds no parseable diff — or one that leaves lines out (#3050) —
// asks once more in the same session with NoDiffFollowUp or
// ElidedDiffFollowUp. It never retries twice; a failed follow-up returns
// the first answer with the follow-up's error.
func RunSimplify(ctx context.Context, run Runner, dir string, opts Options, prompt string) (SimplifyResult, error) {
	res, err := run(ctx, dir, SimplifyCommand(opts, prompt))
	out := SimplifyResult{Result: res}
	out.noteSession(res.ForkID)
	if err != nil {
		return out, err
	}
	out.Diff, out.HasDiff = ParseAnswerDiff(res.Answer)
	if (out.HasDiff && out.Diff.Elided == "") || res.ForkID == "" {
		return out, nil
	}
	ask := NoDiffFollowUp
	if out.HasDiff {
		ask = ElidedDiffFollowUp
	}
	out.Retried = true
	again, err := run(ctx, dir, SimplifyFollowUp(res.ForkID, opts, ask))
	out.noteSession(again.ForkID)
	if err != nil {
		return out, err
	}
	out.Result = again
	if again.ForkID == "" {
		out.ForkID = res.ForkID
	}
	out.Diff, out.HasDiff = ParseAnswerDiff(again.Answer)
	return out, nil
}

func (r *SimplifyResult) noteSession(id string) {
	if id == "" {
		return
	}
	for _, s := range r.Sessions {
		if s == id {
			return
		}
	}
	r.Sessions = append(r.Sessions, id)
}
