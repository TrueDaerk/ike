package agentask

import (
	"strconv"
	"strings"

	"ike/internal/agenttrace"
)

// extract.go is the duplicates half of the simplify runner (#3045, Roadmap
// 0560): the Duplicates tab's `a` on a clone group whose occurrences all
// lie in one file asks the same fresh, tool-less session (RunSimplify) for
// one unified diff that introduces a shared helper and replaces every
// occurrence with a call to it. The prompt carries every occurrence's
// source with its line range and — when it fits under MaxSimplifySource —
// the whole file, so the diff's context lines (and the helper's place) can
// be copied from the real text. Like every ask it opens with AskMarker.

// ExtractInstruction is the fixed instruction every extract prompt ends
// with; like SimplifyInstruction it says outright that there are no tools.
const ExtractInstruction = "The occurrences above are copies of the same code (identifiers or literals may differ between them). " +
	"Extract it into one shared helper in this file and replace every occurrence with a call to it, without changing behaviour; " +
	"keep public signatures; keep tests green. " +
	"You have no tools and cannot read files; the source is in the fenced blocks above. " +
	"Answer with a single unified diff against the file first, in a ```diff fence, adding the helper and replacing every occurrence; " +
	"copy each hunk's context lines verbatim from the source above. Then one paragraph on what you changed and why."

// Extract describes the clone group an extract prompt is about. Lines are
// 0-based like complexity.Occurrence's; the prompt prints them 1-based.
type Extract struct {
	// Path is the file (project-relative in the prompt, as handed in);
	// Lang its language id.
	Path string
	Lang string
	// Tokens is the clone's length in tokens.
	Tokens int
	// Occurrences are the copies in file order, each with its source.
	Occurrences []ExtractOccurrence
	// File is the whole file's text, "" to leave it out; a file over
	// MaxSimplifySource is left out.
	File string
}

// ExtractOccurrence is one copy of the clone.
type ExtractOccurrence struct {
	Line, EndLine int
	Source        string
}

// ExtractPrompt is the text handed to `claude -p`: the ask marker, the
// group's facts, every occurrence fenced with its line range, the file
// when it fits, then the instruction.
func ExtractPrompt(e Extract) string {
	var sb strings.Builder
	sb.WriteString(agenttrace.AskMarker + "\n")
	sb.WriteString("File: " + e.Path)
	if e.Lang != "" {
		sb.WriteString(" (" + e.Lang + ")")
	}
	sb.WriteString("\n")
	sb.WriteString("Duplicate code: " + strconv.Itoa(len(e.Occurrences)) + " occurrences of the same " + strconv.Itoa(e.Tokens) + " tokens\n")
	for i, o := range e.Occurrences {
		sb.WriteString("Occurrence " + strconv.Itoa(i+1) + ", lines " + strconv.Itoa(o.Line+1) + "–" + strconv.Itoa(o.EndLine+1) + ":\n")
		sb.WriteString("```" + e.Lang + "\n" + strings.TrimRight(capSource(o.Source), "\n") + "\n```\n")
	}
	if e.File != "" && len(e.File) <= MaxSimplifySource {
		sb.WriteString("The whole file, for context:\n```" + e.Lang + "\n" + strings.TrimRight(e.File, "\n") + "\n```\n")
	}
	sb.WriteString("\n" + ExtractInstruction)
	return sb.String()
}
