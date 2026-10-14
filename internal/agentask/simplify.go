package agentask

import (
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
// app shows as a markdown scratch. The prompt opens with AskMarker like
// every ask, so discovery skips the session the run leaves behind.

// SimplifyInstruction is the fixed instruction every simplify prompt ends
// with.
const SimplifyInstruction = "Reduce the cognitive complexity of this function without changing its behaviour; " +
	"keep the public signature; keep tests green; answer with a unified diff against the file " +
	"and one paragraph on what you changed and why."

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
