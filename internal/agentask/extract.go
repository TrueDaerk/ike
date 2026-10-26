package agentask

import (
	"strconv"
	"strings"

	"ike/internal/agenttrace"
)

// extract.go is the duplicates half of the simplify runner (#3045, Roadmap
// 0560): the Duplicates tab's `a` on a clone group asks the same fresh,
// tool-less session (RunSimplify) for one unified diff that introduces a
// shared helper and replaces every occurrence with a call to it. The prompt
// carries every occurrence's source with its line range and — when it fits
// under MaxSimplifySource — the whole file, so the diff's context lines (and
// the helper's place) can be copied from the real text. A group spanning
// several files (#3046) names each occurrence's file, carries every file
// while the texts fit under the cap together, and asks for one diff section
// per changed file, touching only the listed files. Like every ask it opens
// with AskMarker.

// ExtractInstruction is the fixed instruction every same-file extract
// prompt ends with; like SimplifyInstruction it says outright that there
// are no tools.
const ExtractInstruction = "The occurrences above are copies of the same code (identifiers or literals may differ between them). " +
	"Extract it into one shared helper in this file and replace every occurrence with a call to it, without changing behaviour; " +
	"keep public signatures; keep tests green. " +
	"You have no tools and cannot read files; the source is in the fenced blocks above. " +
	"Answer with a single unified diff against the file first, in a ```diff fence, adding the helper and replacing every occurrence; " +
	"copy each hunk's context lines verbatim from the source above. Then one paragraph on what you changed and why. " +
	DiffInFull

// ExtractFilesInstruction ends the prompt of a group spanning several
// files (#3046): the helper goes into one of them and no other file may
// change.
const ExtractFilesInstruction = "The occurrences above are copies of the same code (identifiers or literals may differ between them), spread over the files listed. " +
	"Extract it into one shared helper placed in one of those files and replace every occurrence with a call to it, without changing behaviour; " +
	"keep public signatures; keep tests green. Change only the files listed above. " +
	"You have no tools and cannot read files; the source is in the fenced blocks above. " +
	"Answer with a single unified diff first, in a ```diff fence, with one \"--- a/<path>\" / \"+++ b/<path>\" section per changed file, the paths exactly as listed above, " +
	"adding the helper and replacing every occurrence; copy each hunk's context lines verbatim from the source above. Then one paragraph on what you changed and why. " +
	DiffInFull

// Extract describes the clone group an extract prompt is about. Lines are
// 0-based like complexity.Occurrence's; the prompt prints them 1-based.
type Extract struct {
	// Path is the file (project-relative in the prompt, as handed in);
	// Lang its language id. A multi-file group (Files set) names its files
	// there instead.
	Path string
	Lang string
	// Tokens is the clone's length in tokens.
	Tokens int
	// Occurrences are the copies in file order, each with its source.
	Occurrences []ExtractOccurrence
	// File is the whole file's text, "" to leave it out; a file over
	// MaxSimplifySource is left out.
	File string
	// Files is set when the group spans several files (#3046): each file
	// holding an occurrence, in order, with its text ("" to leave it out).
	// The texts are carried while they fit under MaxSimplifySource
	// together.
	Files []ExtractFile
}

// ExtractOccurrence is one copy of the clone; Path names its file in a
// multi-file group.
type ExtractOccurrence struct {
	Path          string
	Line, EndLine int
	Source        string
}

// ExtractFile is one file of a multi-file group (#3046).
type ExtractFile struct {
	Path string
	Text string
}

// ExtractPrompt is the text handed to `claude -p`: the ask marker, the
// group's facts, every occurrence fenced with its line range, the file
// (or files) when it fits, then the instruction.
func ExtractPrompt(e Extract) string {
	var sb strings.Builder
	sb.WriteString(agenttrace.AskMarker + "\n")
	multi := len(e.Files) > 0
	if multi {
		paths := make([]string, 0, len(e.Files))
		for _, f := range e.Files {
			paths = append(paths, f.Path)
		}
		sb.WriteString("Files: " + strings.Join(paths, ", "))
	} else {
		sb.WriteString("File: " + e.Path)
	}
	if e.Lang != "" {
		sb.WriteString(" (" + e.Lang + ")")
	}
	sb.WriteString("\n")
	sb.WriteString("Duplicate code: " + strconv.Itoa(len(e.Occurrences)) + " occurrences of the same " + strconv.Itoa(e.Tokens) + " tokens")
	if multi {
		sb.WriteString(" in " + strconv.Itoa(len(e.Files)) + " files")
	}
	sb.WriteString("\n")
	for i, o := range e.Occurrences {
		sb.WriteString("Occurrence " + strconv.Itoa(i+1) + ", ")
		if multi {
			sb.WriteString(o.Path + " ")
		}
		sb.WriteString("lines " + strconv.Itoa(o.Line+1) + "–" + strconv.Itoa(o.EndLine+1) + ":\n")
		sb.WriteString("```" + e.Lang + "\n" + strings.TrimRight(capSource(o.Source), "\n") + "\n```\n")
	}
	if !multi {
		if e.File != "" && len(e.File) <= MaxSimplifySource {
			sb.WriteString("The whole file, for context:\n```" + e.Lang + "\n" + strings.TrimRight(e.File, "\n") + "\n```\n")
		}
		sb.WriteString("\n" + ExtractInstruction)
		return sb.String()
	}
	budget := MaxSimplifySource
	for _, f := range e.Files {
		if f.Text == "" || len(f.Text) > budget {
			continue
		}
		budget -= len(f.Text)
		sb.WriteString("The whole file " + f.Path + ", for context:\n```" + e.Lang + "\n" + strings.TrimRight(f.Text, "\n") + "\n```\n")
	}
	sb.WriteString("\n" + ExtractFilesInstruction)
	return sb.String()
}
